//go:build linux

package gops

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetProcessExtras_GPUFromCursorClients(t *testing.T) {
	const now = int64(1_755_000_010_000)
	// A nonzero baseline: an unmatched client would rate its whole 6 s as new, giving 60.
	prevCursor := encodeProcessCursor([]processSample{{
		ProcessCursorData: models.ProcessCursorData{PID: 42, Timestamp: now - 10_000},
		gpuOK:             true,
		gpu:               []gpuCursorClient{{PDev: "0000:0c:00.0", ID: 7, Engines: map[string][3]uint64{"gfx": {1_000_000_000, 0, 0}}}},
	}})
	prev := decodeProcessCursor(prevCursor)[42]
	require.NotNil(t, prev)

	key := drmClientKey{PDev: "0000:0c:00.0", ID: 7}
	gpu := procGPU{Readable: true, UsageKnown: true, Clients: drmClientSet{key: {
		Driver: "amdgpu", PDev: key.PDev, ID: key.ID,
		Engines: map[string]drmEngine{"gfx": {BusyNs: 6_000_000_000, HasBusy: true, Capacity: 1}},
	}}}
	cur := &processSample{ProcessCursorData: models.ProcessCursorData{PID: 42, Timestamp: now}, gpuOK: true, gpu: cursorClients(gpu.Clients)}

	var info models.ProcessInfo
	setProcessExtras(&info, prev, cur, gpu)
	require.NotNil(t, info.GPU)
	assert.InDelta(t, 50.0, *info.GPU, 1e-9)
}

func TestProcessExtras_FirstPollNetRateFromBaselineDump(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, fds: map[string]string{"3": "socket:[777]"}}))
	var calls uint64
	g.sockDump = func() ([]sockDiagEntry, error) {
		calls++
		return []sockDiagEntry{{
			Inode: 777, Protocol: 6, Remote: netip.MustParseAddrPort("8.8.8.8:443"),
			RxBytes: calls * 1_000_000, TxBytes: calls * 1_000_000, HasTCPInfo: true,
		}}, nil
	}

	res, err := g.GetProcessesWithOptions(ProcessOptions{EnableCPU: true, Net: true})
	require.NoError(t, err)
	row := res.Processes[0]
	require.NotNil(t, row.NetRx)
	assert.Greater(t, *row.NetRx, 0.0, "the baseline dump to the second dump is the first rate")
	assert.Greater(t, *row.NetTx, 0.0)
	assert.EqualValues(t, 2, calls)
	require.NotNil(t, row.Connections)
	assert.Equal(t, 1, *row.Connections)
}

func TestProcessExtras_RatesFromCursor(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, cpuTicks: 1000, start: 500, readBytes: 4096, writeBytes: 8192}))
	now := time.Now().UnixMilli()
	poll := func(opts ProcessOptions, prev processSample) *models.ProcessInfo {
		prev.PID = 100
		opts.Cursor = encodeProcessCursor([]processSample{prev})
		res, err := g.GetProcessesWithOptions(opts)
		require.NoError(t, err)
		require.Len(t, res.Processes, 1)
		return res.Processes[0]
	}
	base := models.ProcessCursorData{Timestamp: now - 10_000}

	// No previous counters means unknown, never 0; the baseline pass still rates on the first call.
	row := poll(ProcessOptions{IO: true}, processSample{ProcessCursorData: base})
	assert.Nil(t, row.DiskRead)
	assert.Nil(t, row.DiskWrite)

	row = poll(ProcessOptions{IO: true}, processSample{ProcessCursorData: base, ioOK: true, io: [2]uint64{4096, 8192}})
	assert.NotNil(t, row.DiskRead)
	assert.NotNil(t, row.DiskWrite)

	res, err := g.GetProcessesWithOptions(ProcessOptions{IO: true})
	require.NoError(t, err)
	assert.NotNil(t, res.Processes[0].DiskRead, "extras get first-response rates with process CPU off")

	// A previous sample from another process with the same PID is ignored.
	stale := processSample{
		ProcessCursorData: models.ProcessCursorData{Ticks: 9, Timestamp: now - 10},
		start:             501, ioOK: true,
	}
	row = poll(ProcessOptions{EnableCPU: true, IO: true}, stale)
	assert.Nil(t, row.DiskRead)
	assert.Zero(t, row.CPU)
	stale.start = 500
	row = poll(ProcessOptions{EnableCPU: true, IO: true}, stale)
	assert.NotNil(t, row.DiskRead)
	assert.Positive(t, row.CPU)

	row = poll(ProcessOptions{GPU: true}, processSample{ProcessCursorData: base, gpuOK: true})
	assert.NotNil(t, row.GPU)
	assert.NotNil(t, row.VRAMKB)
	row = poll(ProcessOptions{GPU: true}, processSample{ProcessCursorData: base})
	assert.Nil(t, row.GPU, "no previous GPU sample means unknown")
	assert.NotNil(t, row.VRAMKB, "VRAM needs no previous sample")
}

func TestProcessCursor_LegacyStillRatesCPU(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, cpuTicks: 1000}))
	legacy, err := json.Marshal(map[string]any{
		"t": time.Now().UnixMilli() - 10_000, "pid": []int32{100},
		"cpu": []int64{(10 - 5) * 1000}, "dt": []int64{0},
	})
	require.NoError(t, err)

	res, err := g.GetProcessesWithOptions(ProcessOptions{EnableCPU: true, IO: true, GPU: true, Cursor: gzipB64(t, string(legacy))})
	require.NoError(t, err)
	assert.InDelta(t, 12.5, res.Processes[0].CPU, 1.25)
	assert.Nil(t, res.Processes[0].DiskRead, "a legacy cursor has no IO baseline")
	assert.Nil(t, res.Processes[0].GPU, "a legacy cursor has no GPU baseline")
}

func TestParseProcStat(t *testing.T) {
	st, ok := parseProcStat("1234 (Web Content (x)) S 1 1234 1234 34817 -1 4194560 100 0 0 0 5 3 0 0 20 5 27 0 100 0 0\n")
	require.True(t, ok)
	assert.Equal(t, procStat{PPID: 1, State: "S", Nice: 5, Threads: 27, TTY: 34817, CPUTime: 0.08, Start: 100}, st)

	st, ok = parseProcStat("7 (kworker/0:1) I 2 0 0 0 -1 69238880 0 0 0 0 0 0 0 0 0 -20 1 0 50\n")
	require.True(t, ok)
	assert.Equal(t, procStat{PPID: 2, State: "I", Nice: -20, Threads: 1, Start: 50}, st)

	_, ok = parseProcStat("1 (init) S 0 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0")
	assert.False(t, ok, "starttime missing")
	_, ok = parseProcStat("")
	assert.False(t, ok)
}

func TestProcReader_Basics(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 5000)
	writeProcFixture(t, root, 7, map[string]string{
		"status":  "Name:\tsome-long-daemo\nUmask:\t0022\nState:\tS (sleeping)\nUid:\t0\t1000\t1000\t1000\nVmRSS:\t    2048 kB\nThreads:\t3\n",
		"cmdline": "/usr/libexec/some-long-daemon\x00--flag\x00\x00" + long + "\x00",
		"io":      "rchar: 10\nwchar: 20\nread_bytes: 4096\nwrite_bytes: 8192\ncancelled_write_bytes: 0\n",
	})
	require.NoError(t, os.Symlink("/usr/libexec/some-long-daemon (deleted)", filepath.Join(root, "7", "exe")))
	writeProcFixture(t, root, 8, map[string]string{
		"status":  "Name:\tback\\\\slash\\nnl\nUid:\t1000\t1000\t1000\t1000\n",
		"cmdline": "",
	})

	r := &procReader{root: root, buf: make([]byte, 64)}
	b := r.basics(&process.Process{Pid: 7})
	assert.Equal(t, procBasics{
		Name:     "some-long-daemon",
		Cmdline:  "/usr/libexec/some-long-daemon --flag " + long,
		Exe:      "/usr/libexec/some-long-daemon (deleted)",
		Username: "root",
		RSS:      2048 * 1024,
	}, b, "truncated comm extends from argv[0]; empty arguments dropped; a cmdline past the buffer is read whole")

	read, write, ok := r.diskBytes(&process.Process{Pid: 7})
	assert.True(t, ok)
	assert.Equal(t, [2]uint64{4096, 8192}, [2]uint64{read, write})

	b = r.basics(&process.Process{Pid: 8})
	assert.Equal(t, "back\\slash\nnl", b.Name, "status escapes what comm holds raw")
	assert.Empty(t, b.Cmdline, "kernel threads have none")
	assert.Zero(t, b.RSS)

	assert.Equal(t, procBasics{}, r.basics(&process.Process{Pid: 9}))
	_, _, ok = r.diskBytes(&process.Process{Pid: 9})
	assert.False(t, ok)
}

func TestProcessExtras_NetTailCreditedToPreviousHolder(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, fds: map[string]string{"3": "socket:[777]"}}))
	remote := netip.MustParseAddrPort("8.8.8.8:443")
	open := true
	g.sockDump = func() ([]sockDiagEntry, error) {
		if !open {
			return nil, nil
		}
		return []sockDiagEntry{{Inode: 777, Cookie: 77, Protocol: 6, Remote: remote, RxBytes: 1000, TxBytes: 1000, HasTCPInfo: true}}, nil
	}
	var emit func(sockDiagEntry)
	g.sockDestroys = func(e func(sockDiagEntry)) (func(), error) { emit = e; return func() {}, nil }

	opts := ProcessOptions{Net: true}
	first, err := g.GetProcessesWithOptions(opts)
	require.NoError(t, err)
	require.NotNil(t, emit, "process-net starts the listener")

	open = false
	emit(sockDiagEntry{Cookie: 77, Protocol: 6, Remote: remote, RxBytes: 5000, TxBytes: 3000, HasTCPInfo: true})
	time.Sleep(20 * time.Millisecond)
	opts.Cursor = first.Cursor
	res, err := g.GetProcessesWithOptions(opts)
	require.NoError(t, err)
	row := res.Processes[0]
	require.NotNil(t, row.NetRx)
	assert.Positive(t, *row.NetRx, "the bytes after the last dump count for the process that held the socket")
	assert.InDelta(t, 2.0, *row.NetRx / *row.NetTx, 1e-9)
}
