package gops

import (
	"cmp"
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func ownProcess(t *testing.T) *process.Process {
	t.Helper()
	self, err := process.NewProcess(int32(os.Getpid()))
	require.NoError(t, err)
	return self
}

func TestProcessExtras_RatesFromCursor(t *testing.T) {
	self := ownProcess(t)
	read, write, ok := newProcReader().diskBytes(self)
	if !ok {
		t.Skip("own process disk counters unreadable on this platform")
	}
	created, err := self.CreateTime()
	require.NoError(t, err)
	st, ok := newProcReader().stat(self)
	require.True(t, ok)
	poll := func(opts ProcessOptions, prev processSample) *models.ProcessInfo {
		prev.PID = self.Pid
		opts.Cursor = encodeProcessCursor([]processSample{prev})
		res, err := newProcessTestUtil(t, self).GetProcessesWithOptions(opts)
		require.NoError(t, err)
		return res.Processes[0]
	}
	base := models.ProcessCursorData{Timestamp: created}

	// No previous counters means unknown, never 0; the baseline pass still rates on the first call.
	row := poll(ProcessOptions{IO: true}, processSample{ProcessCursorData: models.ProcessCursorData{Timestamp: time.Now().UnixMilli() - 10_000}})
	assert.Nil(t, row.DiskRead)
	assert.Nil(t, row.DiskWrite)

	row = poll(ProcessOptions{IO: true}, processSample{ProcessCursorData: base, ioOK: true, io: [2]uint64{read, write}})
	assert.NotNil(t, row.DiskRead)
	assert.NotNil(t, row.DiskWrite)

	res, err := newProcessTestUtil(t, self).GetProcessesWithOptions(ProcessOptions{IO: true})
	require.NoError(t, err)
	assert.NotNil(t, res.Processes[0].DiskRead, "extras get first-response rates with process CPU off")

	// A previous sample from another process with the same PID is ignored.
	stale := processSample{
		ProcessCursorData: models.ProcessCursorData{Ticks: st.CPUTime - 1, Timestamp: time.Now().UnixMilli() - 10},
		start:             st.Start + 1, ioOK: true,
	}
	row = poll(ProcessOptions{EnableCPU: true, IO: true}, stale)
	assert.Nil(t, row.DiskRead)
	assert.Zero(t, row.CPU)
	stale.start = st.Start
	row = poll(ProcessOptions{EnableCPU: true, IO: true}, stale)
	assert.NotNil(t, row.DiskRead)
	assert.Positive(t, row.CPU)

	if runtime.GOOS == "linux" {
		row = poll(ProcessOptions{GPU: true}, processSample{ProcessCursorData: base, gpuOK: true})
		assert.NotNil(t, row.GPU)
		assert.NotNil(t, row.VRAMKB)
		row = poll(ProcessOptions{GPU: true}, processSample{ProcessCursorData: base})
		assert.Nil(t, row.GPU, "no previous GPU sample means unknown")
		assert.NotNil(t, row.VRAMKB, "VRAM needs no previous sample")
	}
}

func TestProcessCursor_Extras(t *testing.T) {
	clients := []gpuCursorClient{{PDev: "0000:0c:00.0", ID: 7, Engines: map[string][3]uint64{"gfx": {123, 0, 0}}}}
	samples := []processSample{
		{ProcessCursorData: models.ProcessCursorData{PID: 1, Ticks: 1, Timestamp: 1000}, start: 5, ioOK: true, io: [2]uint64{10, 20}, gpuOK: true, gpu: clients},
		{ProcessCursorData: models.ProcessCursorData{PID: 2, Ticks: 2, Timestamp: 1001}, gpuOK: true},
		{ProcessCursorData: models.ProcessCursorData{PID: 3, Ticks: 3, Timestamp: 1002}},
	}
	net := netBaseline{at: 1000, socks: map[uint64]sockBaseline{9: {10, 20, 1}, 1 << 60: {1, 2, 0}}}
	got, gotNet := decodeCursor(encodeCursor(samples, net))
	require.Len(t, got, 3)
	assert.Equal(t, samples[0], *got[1])
	assert.True(t, got[2].gpuOK, "sampled with no clients")
	assert.False(t, got[3].gpuOK, "not sampled")
	assert.Equal(t, net, gotNet)

	// Malformed extras drop only the extras; the per-PID net format of older cursors is not a baseline.
	for name, payload := range map[string]string{
		"bad io and gpu": `{"t":5,"pid":[1,2],"cpu":[1000,2000],"dt":[0,1],"iop":[1,2],"ior":[1],"iow":[1,2],"g":"nope"}`,
		"bad sockets":    `{"t":5,"pid":[1,2],"cpu":[1000,2000],"dt":[0,1],"ns":"nope","na":5}`,
		"old net":        `{"t":5,"pid":[1,2],"cpu":[1000,2000],"dt":[0,1],"n":{"1":[[9,10,20]]},"nt":5}`,
	} {
		procs, n := decodeCursor(gzipB64(t, payload))
		require.Len(t, procs, 2, name)
		assert.Equal(t, 2.0, procs[2].Ticks, name)
		assert.False(t, procs[1].ioOK || procs[1].gpuOK, name)
		assert.Zero(t, n.at, name)
	}
}

func TestProcessCursor_LegacyStillRatesCPU(t *testing.T) {
	self := ownProcess(t)
	times, err := self.Times()
	require.NoError(t, err)
	legacy, err := json.Marshal(map[string]any{
		"t": time.Now().UnixMilli() - 10_000, "pid": []int32{self.Pid},
		"cpu": []int64{int64((times.User + times.System - 5) * 1000)}, "dt": []int64{0},
	})
	require.NoError(t, err)

	res, err := newProcessTestUtil(t, self).GetProcessesWithOptions(ProcessOptions{EnableCPU: true, IO: true, GPU: true, Cursor: gzipB64(t, string(legacy))})
	require.NoError(t, err)
	expected := 50.0 / float64(runtime.NumCPU())
	assert.InDelta(t, expected, res.Processes[0].CPU, expected/10)
	assert.Nil(t, res.Processes[0].DiskRead, "a legacy cursor has no IO baseline")
	assert.Nil(t, res.Processes[0].GPU, "a legacy cursor has no GPU baseline")
}

func TestMergeProcesses_Extras(t *testing.T) {
	merge := func(rows ...*models.ProcessInfo) map[int32]*models.ProcessInfo {
		out := map[int32]*models.ProcessInfo{}
		for _, p := range mergeProcessesByExecutable(rows) {
			out[p.PID] = p
		}
		return out
	}

	t.Run("sums are clamped and unknown members make a sum unknown", func(t *testing.T) {
		root := merge(
			&models.ProcessInfo{PID: 1, ExecutablePath: "/bin/a", GPU: ptr(30.0), GPUVideo: ptr(70.0), VRAMKB: ptr(uint64(100)), DiskRead: ptr(5.0), StartTime: 111},
			&models.ProcessInfo{PID: 2, PPID: 1, ExecutablePath: "/bin/a", GPU: ptr(90.0), GPUVideo: ptr(40.0), VRAMKB: ptr(uint64(50)), StartTime: 222},
		)[1]
		assert.Equal(t, 100.0, *root.GPU)
		assert.Equal(t, 100.0, *root.GPUVideo)
		assert.Equal(t, uint64(150), *root.VRAMKB)
		assert.Nil(t, root.DiskRead, "known parent plus unknown child is unknown")
		assert.Equal(t, int64(111), root.StartTime)
	})

	t.Run("an unmonitored member drops GPU and VRAM", func(t *testing.T) {
		known := func(pid, ppid int32) *models.ProcessInfo {
			return &models.ProcessInfo{PID: pid, PPID: ppid, ExecutablePath: "/bin/a", GPU: ptr(10.0), VRAMKB: ptr(uint64(100))}
		}
		unmonitored := func(pid, ppid int32) *models.ProcessInfo {
			return &models.ProcessInfo{PID: pid, PPID: ppid, ExecutablePath: "/bin/a", GPUUnmonitored: true}
		}
		for name, root := range map[string]*models.ProcessInfo{
			"child":  merge(known(1, 0), unmonitored(2, 1))[1],
			"parent": merge(unmonitored(1, 0), known(2, 1))[1],
		} {
			assert.True(t, root.GPUUnmonitored, name)
			assert.Nil(t, root.GPU, name)
			assert.Nil(t, root.VRAMKB, name)
		}
		root := merge(known(1, 0), known(2, 1))[1]
		assert.False(t, root.GPUUnmonitored)
		assert.Equal(t, 20.0, *root.GPU)
	})

	t.Run("network sums, caps remotes and stays exact", func(t *testing.T) {
		remotes := func(n int, host string) []models.NetConnection {
			out := make([]models.NetConnection, n, n+10)
			for i := range out {
				out[i] = models.NetConnection{Protocol: "tcp", Remote: host}
			}
			return out
		}
		parent := &models.ProcessInfo{PID: 1, ExecutablePath: "/bin/a", Connections: ptr(30), LocalConnections: ptr(1), Remotes: remotes(30, "1.1.1.1:443"), NetRx: ptr(100.0), NetTx: ptr(10.0)}
		child := &models.ProcessInfo{PID: 2, PPID: 1, ExecutablePath: "/bin/a", Connections: ptr(5), LocalConnections: ptr(2), Remotes: remotes(5, "8.8.8.8:443"), NetRx: ptr(50.0), NetPartial: true}
		unknown := &models.ProcessInfo{PID: 3, PPID: 1, ExecutablePath: "/bin/a"}

		root := merge(parent, child, unknown)[1]
		assert.Nil(t, root.Connections, "a member with unknown sockets makes the count unknown")
		assert.Len(t, root.Remotes, maxProcRemotes)
		assert.True(t, root.NetPartial, "partial if any member is")

		known := merge(parent, child)[1]
		assert.Equal(t, 35, *known.Connections)
		assert.Equal(t, 3, *known.LocalConnections)
		assert.Equal(t, 150.0, *known.NetRx)
		assert.Nil(t, known.NetTx, "the child's unknown tx makes the sum unknown")
		assert.Equal(t, 30, *parent.Connections, "the original rows must not be written through")
		assert.Equal(t, models.NetConnection{}, parent.Remotes[:31][30], "the parent's spare capacity must not be appended into")
	})
}

func TestSortProcesses_ExtrasPutUnknownLast(t *testing.T) {
	pids := func(list []*models.ProcessInfo) []int32 {
		out := make([]int32, len(list))
		for i, p := range list {
			out[i] = p.PID
		}
		return out
	}
	rows := func() []*models.ProcessInfo {
		return []*models.ProcessInfo{
			{PID: 1},
			{PID: 2, GPU: ptr(10.0), VRAMKB: ptr(uint64(5)), DiskRead: ptr(1.0), DiskWrite: ptr(1.0)},
			{PID: 3},
			{PID: 4, GPU: ptr(0.0), VRAMKB: ptr(uint64(50)), DiskRead: ptr(0.0), DiskWrite: ptr(10.0)},
		}
	}
	for sortBy, want := range map[ProcSortBy][]int32{SortByGPU: {2, 4}, SortByVRAM: {4, 2}, SortByDisk: {4, 2}} {
		list := rows()
		sortProcesses(list, sortBy)
		assert.Equal(t, want, pids(list)[:2], string(sortBy))
		assert.ElementsMatch(t, []int32{1, 3}, pids(list)[2:], string(sortBy))
	}

	list := []*models.ProcessInfo{
		{PID: 1},
		{PID: 2, Connections: ptr(2), NetRx: ptr(1.0), NetTx: ptr(0.0)},
		{PID: 3, Connections: ptr(3)},
		{PID: 4, Connections: ptr(2), NetRx: ptr(5.0), NetTx: ptr(0.0)},
		{PID: 5, Connections: ptr(0), NetRx: ptr(0.0), NetTx: ptr(0.0)},
	}
	sortProcesses(list, SortByNetwork)
	assert.Equal(t, []int32{3, 4, 2, 5, 1}, pids(list))
}

func netRows(nets map[int32]procNet, prevOK ...int32) []netRow {
	rows := make([]netRow, 0, len(nets))
	for pid, n := range nets {
		n.Known = true
		rows = append(rows, netRow{pid: pid, info: &models.ProcessInfo{PID: pid}, net: n, prevOK: slices.Contains(prevOK, pid)})
	}
	slices.SortFunc(rows, func(a, b netRow) int { return cmp.Compare(a.pid, b.pid) })
	return rows
}

func tcp(cookie, rx, tx uint64) tcpCounters { return tcpCounters{cookie, rx, tx} }

func dumpOf(at int64, socks ...tcpCounters) *netDump {
	d := &netDump{at: at}
	for _, s := range socks {
		d.socks = append(d.socks, dumpSocket{tcpCounters: s})
	}
	return d
}

func TestApplyNetRates(t *testing.T) {
	const now = int64(1_755_000_010_000)
	const mb = 1_000_000
	base := func(socks map[uint64]sockBaseline) netBaseline { return netBaseline{at: now - 1_000, socks: socks} }
	shared := []tcpCounters{tcp(1, 100, 100)}
	moved := []tcpCounters{tcp(1, 150, 120), tcp(2, 600, 500), tcp(3, 1000, 501)}
	kept := []tcpCounters{tcp(1, 11*mb, 5*mb), tcp(3, 1, 1), tcp(4, mb/2, mb/2+1)} // 4 is new: bytes_acked counts the SYN
	keptPrev := map[uint64]sockBaseline{1: {10 * mb, 5 * mb, 7}, 2: {7 * mb, 7 * mb, 7}, 3: {4 * mb, 4 * mb, 7}}

	tests := []struct {
		name   string
		prev   netBaseline
		cur    *netDump
		nets   map[int32]procNet
		prevOK []int32
		wantRx map[int32]*float64 // nil value = unknown
		wantTx map[int32]*float64
	}{
		{"kept, closed, backwards and new sockets", base(keptPrev), dumpOf(now, kept...), map[int32]procNet{7: {TCP: kept}}, []int32{7},
			map[int32]*float64{7: ptr(1.5 * mb)}, map[int32]*float64{7: ptr(0.5 * mb)}},
		{"all sockets closed is 0", base(keptPrev), dumpOf(now), map[int32]procNet{7: {}}, []int32{7},
			map[int32]*float64{7: ptr(0.0)}, nil},
		{"no baseline", netBaseline{}, dumpOf(now, kept...), map[int32]procNet{7: {TCP: kept}}, []int32{7},
			map[int32]*float64{7: nil}, nil},
		{"moved socket is a delta, a new holder still has a baseline per socket",
			base(map[uint64]sockBaseline{1: {100, 100, 10}, 2: {500, 500, 0}}), dumpOf(now, moved...),
			map[int32]procNet{10: {}, 20: {TCP: moved[:1]}, 30: {TCP: moved[1:]}}, []int32{10},
			map[int32]*float64{10: ptr(0.0), 20: ptr(50.0), 30: ptr(1100.0)}, map[int32]*float64{20: ptr(20.0), 30: ptr(500.0)}},
		{"a reused inode is told apart by cookie", base(map[uint64]sockBaseline{5: {1 << 30, 1 << 30, 7}}), dumpOf(now, tcp(6, 10, 11)),
			map[int32]procNet{7: {TCP: []tcpCounters{tcp(6, 10, 11)}}}, []int32{7},
			map[int32]*float64{7: ptr(10.0)}, map[int32]*float64{7: ptr(10.0)}},
		{"a shared socket counts once", base(map[uint64]sockBaseline{1: {0, 0, 4}}), dumpOf(now, shared...),
			map[int32]procNet{3: {TCP: shared}, 4: {TCP: shared}}, []int32{3, 4},
			map[int32]*float64{3: ptr(100.0), 4: ptr(0.0)}, nil},
		{"UDP only is unknown", base(map[uint64]sockBaseline{1: {0, 0, 2}}), dumpOf(now, tcp(1, 40, 0)),
			map[int32]procNet{1: {UDP: true}, 2: {UDP: true, TCP: []tcpCounters{tcp(1, 40, 0)}}, 3: {RatesUnknown: true}}, []int32{1, 2, 3},
			map[int32]*float64{1: nil, 2: ptr(40.0), 3: nil}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := netRows(tt.nets, tt.prevOK...)
			applyNetRates(tt.prev, tt.cur, rows)
			for _, r := range rows {
				if want, ok := tt.wantRx[r.pid]; ok {
					if want == nil {
						assert.Nil(t, r.info.NetRx, "rx %d", r.pid)
					} else if assert.NotNil(t, r.info.NetRx, "rx %d", r.pid) {
						assert.InDelta(t, *want, *r.info.NetRx, 1e-6, "rx %d", r.pid)
					}
				}
				if want, ok := tt.wantTx[r.pid]; ok && assert.NotNil(t, r.info.NetTx, "tx %d", r.pid) {
					assert.InDelta(t, *want, *r.info.NetTx, 1e-6, "tx %d", r.pid)
				}
			}
		})
	}
}

func TestApplyNetRates_ClosedSocketTails(t *testing.T) {
	const now = int64(1_755_000_010_000)
	prev := netBaseline{at: now - 1_000, socks: map[uint64]sockBaseline{
		1: {100, 100, 7}, // destroyed, final counters known
		2: {100, 100, 8}, // destroyed, held by a PID that is now someone else
		3: {100, 100, 7}, // orphaned: no fd, still closing
		4: {100, 100, 0}, // destroyed, never attributed
		5: {100, 100, 7}, // destroyed, no final counters
	}}
	cur := dumpOf(now)
	cur.socks = append(cur.socks, dumpSocket{tcp(3, 130, 110), true})
	cur.tail = func(cookie uint64) (uint64, uint64, bool) {
		if cookie == 5 {
			return 0, 0, false
		}
		return 300, 250, true
	}
	rows := netRows(map[int32]procNet{7: {}, 8: {}}, 7)
	next := applyNetRates(prev, cur, rows)
	assert.Equal(t, []float64{230, 160}, []float64{*rows[0].info.NetRx, *rows[0].info.NetTx}, "tail of 1 plus the orphan's delta")
	assert.Equal(t, 0.0, *rows[1].info.NetRx, "a reused PID gets nothing")
	assert.Equal(t, map[uint64]sockBaseline{3: {130, 110, 7}}, next.socks, "an orphan keeps its holder")
}
