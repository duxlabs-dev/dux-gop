package gops

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	cpuBaselineInterval   = 200 * time.Millisecond
	maxCursorDecodedBytes = 4 << 20
	maxProcRemotes        = 32
)

// The cursor's gzip writer and reader live as long as the process: a fresh
// pair allocates ~1 MB, and a sync.Pool is emptied by the GC between polls.
var cursorGzip struct {
	sync.Mutex
	w *gzip.Writer
	r *gzip.Reader
}

type processCursorBase struct {
	BaseMillis   int64   `json:"t"`
	PIDs         []int32 `json:"pid"`
	CPUMillis    []int64 `json:"cpu"`
	OffsetMillis []int64 `json:"dt"`
	// Starts holds each procStat.Start; cursors from before it have none.
	Starts []uint64 `json:"st,omitempty"`
}

type processCursorWire struct {
	processCursorBase
	// Disk counters, parallel to IOPIDs.
	IOPIDs  []int32  `json:"iop,omitempty"`
	IORead  []uint64 `json:"ior,omitempty"`
	IOWrite []uint64 `json:"iow,omitempty"`
	// Every PID whose GPU usage was sampled, with its engine clients ([] for none).
	GPU map[int32][]gpuCursorClient `json:"g,omitzero"`
	// Every non-loopback TCP socket of the dump taken at NetAt (Unix ms) as
	// [cookie, received, acked, holder PID or 0].
	NetSocks [][4]uint64 `json:"ns,omitempty"`
	NetAt    int64       `json:"na,omitempty"`
}

// processSample is one process's cursor entry. ioOK and gpuOK say whether io and gpu were sampled.
type processSample struct {
	models.ProcessCursorData
	start uint64
	ioOK  bool
	io    [2]uint64
	gpuOK bool
	gpu   []gpuCursorClient
}

// netBaseline is one socket dump's TCP sockets by cookie; at 0 means none.
type netBaseline struct {
	at    int64
	socks map[uint64]sockBaseline
}

type sockBaseline struct {
	rx, tx uint64
	pid    int32
}

func encodeCursor(entries []processSample, net netBaseline) string {
	wire := processCursorWire{processCursorBase: processCursorBase{
		PIDs:         make([]int32, 0, len(entries)),
		CPUMillis:    make([]int64, 0, len(entries)),
		OffsetMillis: make([]int64, 0, len(entries)),
		Starts:       make([]uint64, 0, len(entries)),
	}}
	for i, e := range entries {
		if i == 0 || e.Timestamp < wire.BaseMillis {
			wire.BaseMillis = e.Timestamp
		}
	}
	for _, e := range entries {
		wire.PIDs = append(wire.PIDs, e.PID)
		wire.CPUMillis = append(wire.CPUMillis, int64(math.Round(e.Ticks*1000)))
		wire.OffsetMillis = append(wire.OffsetMillis, e.Timestamp-wire.BaseMillis)
		wire.Starts = append(wire.Starts, e.start)
		if e.ioOK {
			wire.IOPIDs = append(wire.IOPIDs, e.PID)
			wire.IORead = append(wire.IORead, e.io[0])
			wire.IOWrite = append(wire.IOWrite, e.io[1])
		}
		if e.gpuOK {
			if wire.GPU == nil {
				wire.GPU = map[int32][]gpuCursorClient{}
			}
			wire.GPU[e.PID] = append([]gpuCursorClient{}, e.gpu...)
		}
	}
	if net.at > 0 {
		wire.NetAt = net.at
		wire.NetSocks = make([][4]uint64, 0, len(net.socks))
		for cookie, b := range net.socks {
			wire.NetSocks = append(wire.NetSocks, [4]uint64{cookie, b.rx, b.tx, uint64(b.pid)})
		}
		slices.SortFunc(wire.NetSocks, func(a, b [4]uint64) int { return cmp.Compare(a[0], b[0]) })
	}

	raw, _ := json.Marshal(wire)
	var buf bytes.Buffer
	cursorGzip.Lock()
	if cursorGzip.w == nil {
		cursorGzip.w, _ = gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	} else {
		cursorGzip.w.Reset(&buf)
	}
	_, _ = cursorGzip.w.Write(raw)
	_ = cursorGzip.w.Close()
	cursorGzip.Unlock()
	return base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func decodeCursor(cursor string) (out map[int32]*processSample, net netBaseline) {
	out = make(map[int32]*processSample)
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) == 0 {
		return out, net
	}

	if raw, err = gunzipCursor(raw); err != nil {
		return out, net
	}

	var wire processCursorWire
	if json.Unmarshal(raw, &wire) != nil {
		// Malformed extras must not cost the CPU baseline.
		wire = processCursorWire{}
		if json.Unmarshal(raw, &wire.processCursorBase) != nil {
			return out, net
		}
	}
	if len(wire.CPUMillis) != len(wire.PIDs) || len(wire.OffsetMillis) != len(wire.PIDs) {
		return out, net
	}
	entries := make([]processSample, len(wire.PIDs))
	for i, pid := range wire.PIDs {
		entries[i] = processSample{
			ProcessCursorData: models.ProcessCursorData{
				PID:       pid,
				Ticks:     float64(wire.CPUMillis[i]) / 1000,
				Timestamp: wire.BaseMillis + wire.OffsetMillis[i],
			},
		}
		if len(wire.Starts) == len(wire.PIDs) {
			entries[i].start = wire.Starts[i]
		}
		entries[i].gpu, entries[i].gpuOK = wire.GPU[pid]
		out[pid] = &entries[i]
	}
	if len(wire.IORead) == len(wire.IOPIDs) && len(wire.IOWrite) == len(wire.IOPIDs) {
		for i, pid := range wire.IOPIDs {
			if e := out[pid]; e != nil {
				e.ioOK, e.io = true, [2]uint64{wire.IORead[i], wire.IOWrite[i]}
			}
		}
	}
	if wire.NetAt > 0 {
		net = netBaseline{at: wire.NetAt, socks: make(map[uint64]sockBaseline, len(wire.NetSocks))}
		for _, s := range wire.NetSocks {
			net.socks[s[0]] = sockBaseline{s[1], s[2], int32(s[3])}
		}
	}
	return out, net
}

func gunzipCursor(data []byte) ([]byte, error) {
	cursorGzip.Lock()
	defer cursorGzip.Unlock()
	var err error
	if cursorGzip.r == nil {
		cursorGzip.r, err = gzip.NewReader(bytes.NewReader(data))
	} else {
		err = cursorGzip.r.Reset(bytes.NewReader(data))
	}
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(cursorGzip.r, maxCursorDecodedBytes))
}

type procBasics struct {
	Name, Cmdline, Exe, Username string
	RSS                          uint64
}

// pssMaxAge bounds how stale a cached Pss_Dirty can be; RSS moving more than 5% also refreshes it.
const pssMaxAge = 30 * time.Second

// pssCache keeps Pss_Dirty per process: smaps_rollup costs milliseconds for a large process.
type pssCache struct {
	mu      sync.Mutex
	entries map[int32]pssEntry
	read    func(pid int32) (uint64, error)
}

type pssEntry struct {
	start, rssKB, pssKB uint64
	at                  time.Time
}

func (self *pssCache) get(pid int32, start, rssKB uint64, now time.Time) (uint64, error) {
	self.mu.Lock()
	e, ok := self.entries[pid]
	self.mu.Unlock()
	if ok && e.start == start && now.Sub(e.at) < pssMaxAge && max(rssKB, e.rssKB)-min(rssKB, e.rssKB) <= e.rssKB/20 {
		return e.pssKB, nil
	}
	read := self.read
	if read == nil {
		read = getPssDirty
	}
	pss, err := read(pid)
	if err != nil {
		return 0, err
	}
	at := now
	if !ok {
		// Backdate new entries by up to 9/10 of pssMaxAge, so that processes
		// first seen together don't all expire in the same sample.
		at = now.Add(-time.Duration(pid%10) * pssMaxAge / 10)
	}
	self.mu.Lock()
	if self.entries == nil {
		self.entries = map[int32]pssEntry{}
	}
	self.entries[pid] = pssEntry{start: start, rssKB: rssKB, pssKB: pss, at: at}
	self.mu.Unlock()
	return pss, nil
}

func (self *pssCache) prune(now time.Time) {
	self.mu.Lock()
	defer self.mu.Unlock()
	for pid, e := range self.entries {
		if now.Sub(e.at) >= pssMaxAge {
			delete(self.entries, pid)
		}
	}
}

// procStat is what the process list reads from /proc/<pid>/stat on Linux.
// Elsewhere only PPID, CPUTime, Start and CreatedMillis are set.
type procStat struct {
	PPID    int32
	State   string
	Nice    int
	Threads int
	TTY     int
	// CPUTime is user plus system time in seconds.
	CPUTime float64
	// Start tells a reused PID apart: clock ticks after boot on Linux,
	// the creation time elsewhere.
	Start         uint64
	CreatedMillis int64
	// Ext is set only when the full stat line was parsed.
	Ext *procStatExt
}

// procStatExt holds the stat fields only the on-demand process detail reads.
type procStatExt struct {
	Session           int
	MinFlt, MajFlt    uint64
	UTime, STime      float64
	Priority          int
	Processor, Policy int
}

func (self *GopsUtil) GetProcesses(sortBy ProcSortBy, limit int, enableCPU bool, mergeChildren bool) (*models.ProcessListResponse, error) {
	return self.GetProcessesWithCursor(sortBy, limit, enableCPU, "", mergeChildren)
}

func (self *GopsUtil) GetProcessesWithCursor(sortBy ProcSortBy, limit int, enableCPU bool, cursor string, mergeChildren bool) (*models.ProcessListResponse, error) {
	return self.GetProcessesWithOptions(ProcessOptions{SortBy: sortBy, Limit: limit, EnableCPU: enableCPU, Cursor: cursor, MergeChildren: mergeChildren})
}

// ProcessOptions selects the process list. GPU, IO and Net each cost extra reads per process.
type ProcessOptions struct {
	SortBy        ProcSortBy
	Limit         int
	EnableCPU     bool
	Cursor        string
	MergeChildren bool
	GPU           bool
	// GPUPciIds names the GPUs to read; empty reads none.
	GPUPciIds []string
	IO        bool
	Net       bool
}

func (self ProcessOptions) effectiveSort() ProcSortBy {
	switch self.SortBy {
	case SortByGPU, SortByVRAM:
		if !self.GPU {
			return SortByCPU
		}
	case SortByDisk:
		if !self.IO {
			return SortByCPU
		}
	case SortByNetwork:
		if !self.Net {
			return SortByCPU
		}
	}
	return self.SortBy
}

type processExtras struct {
	gpu *procGPUReader
	net *procNetReader
	io  bool
}

// sample recovers because the baseline pass calls it on the request goroutine.
func (self processExtras) sample(r *procReader, p *process.Process, s *processSample) (g procGPU, n procNet) {
	defer func() {
		if recover() != nil {
			g, n = procGPU{}, procNet{}
			s.ioOK, s.gpuOK = false, false
		}
	}()
	if self.io {
		var read, write uint64
		read, write, s.ioOK = r.diskBytes(p)
		s.io = [2]uint64{read, write}
	}
	g, n = self.readFDs("/proc", p.Pid)
	if g.Readable && g.UsageKnown {
		s.gpuOK, s.gpu = true, cursorClients(g.Clients)
	}
	return g, n
}

func (self *GopsUtil) GetProcessesWithOptions(opts ProcessOptions) (*models.ProcessListResponse, error) {
	limit, enableCPU, mergeChildren := opts.Limit, opts.EnableCPU, opts.MergeChildren
	procs, err := self.procProvider.Processes()
	if err != nil {
		return nil, err
	}

	totalMem, _ := self.memProvider.VirtualMemory()

	cursorMap, prevNet := decodeCursor(opts.Cursor)

	extras := processExtras{io: opts.IO}
	if opts.GPU {
		extras.gpu = self.newProcGPUReader(opts.GPUPciIds)
	}
	if opts.Net {
		extras.net = self.newProcNetReader()
	}

	refreshUsernames()

	if (enableCPU || opts.GPU || opts.IO || opts.Net) && len(cursorMap) == 0 {
		r := newProcReader()
		var netRows []netRow
		for _, p := range procs {
			st, ok := r.stat(p)
			if !ok {
				continue
			}
			s := &processSample{ProcessCursorData: models.ProcessCursorData{
				PID:       p.Pid,
				Ticks:     st.CPUTime,
				Timestamp: time.Now().UnixMilli(),
			}, start: st.Start}
			_, n := extras.sample(r, p, s)
			cursorMap[p.Pid] = s
			if n.Known {
				netRows = append(netRows, netRow{pid: p.Pid, net: n})
			}
		}
		time.Sleep(cpuBaselineInterval)
		if extras.net != nil {
			prevNet = applyNetRates(netBaseline{}, &extras.net.netDump, netRows)
			extras.net = self.newProcNetReader()
		}
	}

	type procResult struct {
		info    *models.ProcessInfo
		sample  processSample
		sampled bool
		net     procNet
		prevOK  bool
	}

	numCPU := float64(runtime.NumCPU())
	now := time.Now()

	// /proc reads contend in the kernel: past 4 workers CPU time grows
	// faster than wall time shrinks.
	numWorkers := min(runtime.NumCPU(), 4)

	jobs := make(chan int, len(procs))
	results := make(chan procResult, len(procs))

	for w := 0; w < numWorkers; w++ {
		go func() {
			r := newProcReader()
			for idx := range jobs {
				p := procs[idx]

				// gopsutil can panic on macOS when reading process info
				// for system processes or processes that exit mid-read.
				func() {
					defer func() {
						if r := recover(); r != nil {
							results <- procResult{
								info: &models.ProcessInfo{
									PID:     p.Pid,
									Command: fmt.Sprintf("[pid %d]", p.Pid),
								},
							}
						}
					}()

					st, sampled := r.stat(p)
					if !sampled && runtime.GOOS == "linux" {
						// stat is world-readable, so the process exited.
						results <- procResult{}
						return
					}
					b := r.basics(p)
					sample := processSample{ProcessCursorData: models.ProcessCursorData{PID: p.Pid, Timestamp: time.Now().UnixMilli()}, start: st.Start}
					gpu, netInfo := extras.sample(r, p, &sample)
					sampledAt := sample.Timestamp
					currentCPUTime := st.CPUTime
					sample.Ticks = currentCPUTime
					prev := cursorMap[p.Pid]
					if prev != nil && prev.start != 0 && prev.start != st.Start {
						prev = nil // the PID was reused
					}

					cpuPercent := 0.0
					if enableCPU {
						if prev != nil {
							coreRate := calculateProcessCPUPercentageWithCursor(&prev.ProcessCursorData, currentCPUTime, sampledAt)
							cpuPercent = coreRate / numCPU
							if cpuPercent > 100 {
								cpuPercent = 100
							}
						}
					}

					pssKB := uint64(0)
					pssPercent := float32(0)
					memCalc := "rss"

					rssKB := b.RSS / 1024
					rssPercent := float32(b.RSS) / float32(totalMem.Total) * 100

					memKB := rssKB
					memPercent := rssPercent

					if rssKB > 102400 {
						pssDirty, err := self.pss.get(p.Pid, st.Start, rssKB, now)
						if err == nil && pssDirty > 0 {
							memKB = pssDirty
							memPercent = float32(memKB*1024) / float32(totalMem.Total) * 100
							memCalc = "pss_dirty"
						}
					}

					info := &models.ProcessInfo{
						PID:               p.Pid,
						PPID:              st.PPID,
						CPU:               cpuPercent,
						PTicks:            currentCPUTime,
						MemoryPercent:     memPercent,
						MemoryKB:          memKB,
						MemoryCalculation: memCalc,
						RSSKB:             rssKB,
						RSSPercent:        rssPercent,
						PSSKB:             pssKB,
						PSSPercent:        pssPercent,
						Username:          b.Username,
						Command:           b.Name,
						FullCommand:       b.Cmdline,
						ExecutablePath:    b.Exe,
						StartTime:         st.CreatedMillis / 1000,
					}
					if st.State != "" {
						info.Status, info.Threads, info.Nice = st.State, st.Threads, &st.Nice
					}
					setProcessExtras(info, prev, &sample, gpu)
					if netInfo.Known {
						info.Connections, info.LocalConnections = new(netInfo.Count), new(netInfo.LocalCount)
						for _, r := range netInfo.Remotes {
							info.Remotes = append(info.Remotes, models.NetConnection{Protocol: r.Protocol, Remote: r.Remote, Local: r.Local})
						}
					}

					results <- procResult{
						sample:  sample,
						sampled: sampled,
						info:    info,
						net:     netInfo,
						prevOK:  prev != nil,
					}
				}()
			}
		}()
	}

	for i := range procs {
		jobs <- i
	}
	close(jobs)

	procList := make([]*models.ProcessInfo, 0, len(procs))
	cursorList := make([]processSample, 0, len(procs))
	var netRows []netRow
	for i := 0; i < len(procs); i++ {
		r := <-results
		if r.info != nil {
			procList = append(procList, r.info)
		}
		if r.sampled {
			cursorList = append(cursorList, r.sample)
		}
		if r.net.Known {
			netRows = append(netRows, netRow{pid: r.info.PID, info: r.info, net: r.net, prevOK: r.prevOK})
		}
	}
	self.pss.prune(now)

	var nextNet netBaseline
	if extras.net != nil {
		nextNet = applyNetRates(prevNet, &extras.net.netDump, netRows)
	}

	if mergeChildren {
		procList = mergeProcessesByExecutable(procList)
	}

	sortProcesses(procList, opts.effectiveSort())

	if limit > 0 && len(procList) > limit {
		procList = procList[:limit]
	}

	return &models.ProcessListResponse{
		Processes: procList,
		Cursor:    encodeCursor(cursorList, nextNet),
	}, nil
}

// setProcessExtras fills the requested extras of info from the current and
// previous samples. Rates without a previous sample stay unknown.
func setProcessExtras(info *models.ProcessInfo, prev, cur *processSample, gpu procGPU) {
	info.GPUUnmonitored = gpu.Unmonitored
	if gpu.Readable {
		info.VRAMKB = new(gpu.VRAMBytes / 1024)
		info.GPUGttKB = new(gpu.GTTBytes / 1024)
	}
	if prev == nil || cur.Timestamp <= prev.Timestamp {
		return
	}
	elapsed := time.Duration(cur.Timestamp-prev.Timestamp) * time.Millisecond
	if cur.gpuOK && prev.gpuOK && !gpu.Unmonitored {
		usage, video := procGPUUsage(prev.gpu, gpu.Clients, elapsed)
		info.GPU, info.GPUVideo = &usage, &video
	}
	if cur.ioOK && prev.ioOK && cur.io[0] >= prev.io[0] && cur.io[1] >= prev.io[1] {
		read := float64(cur.io[0]-prev.io[0]) / elapsed.Seconds()
		write := float64(cur.io[1]-prev.io[1]) / elapsed.Seconds()
		info.DiskRead, info.DiskWrite = &read, &write
	}
}

type netRow struct {
	pid  int32
	info *models.ProcessInfo
	net  procNet
	// prevOK means the cursor holds this same process, so the last bytes of
	// sockets it closed since are its own.
	prevOK bool
}

// applyNetRates sets each row's TCP rates from cur against prev and returns
// the next baseline. A socket absent from prev is
// new and counts from zero, less the SYN in bytes_acked; one held by several
// processes counts for the lowest PID.
func applyNetRates(prev netBaseline, cur *netDump, rows []netRow) (next netBaseline) {
	owner := make(map[uint64]int32)
	for _, r := range rows {
		for _, s := range r.net.TCP {
			if o, ok := owner[s.Cookie]; !ok || r.pid < o {
				owner[s.Cookie] = r.pid
			}
		}
	}
	next = netBaseline{at: cur.at, socks: make(map[uint64]sockBaseline, len(cur.socks))}
	carried := make(map[int32][2]uint64)
	for _, s := range cur.socks {
		pid := owner[s.Cookie]
		if p, seen := prev.socks[s.Cookie]; seen && s.Orphan && p.pid != 0 {
			pid = p.pid
			carried[pid] = addCounters(carried[pid], counterDelta(p, s.RxBytes, s.TxBytes))
		}
		next.socks[s.Cookie] = sockBaseline{s.RxBytes, s.TxBytes, pid}
	}
	if cur.tail != nil {
		for cookie, p := range prev.socks {
			if _, open := next.socks[cookie]; open || p.pid == 0 {
				continue
			}
			if frx, ftx, ok := cur.tail(cookie); ok {
				carried[p.pid] = addCounters(carried[p.pid], counterDelta(p, frx, ftx))
			}
		}
	}
	if prev.at <= 0 || cur.at <= prev.at {
		return next
	}
	secs := float64(cur.at-prev.at) / 1000
	for _, r := range rows {
		n := r.net
		if n.RatesUnknown || n.UDP && len(n.TCP) == 0 {
			continue
		}
		var bytes [2]uint64
		for _, s := range n.TCP {
			if owner[s.Cookie] != r.pid {
				continue
			}
			if p, seen := prev.socks[s.Cookie]; seen {
				bytes = addCounters(bytes, counterDelta(p, s.RxBytes, s.TxBytes))
			} else {
				bytes = addCounters(bytes, [2]uint64{s.RxBytes, s.TxBytes - min(s.TxBytes, 1)})
			}
		}
		if r.prevOK {
			bytes = addCounters(bytes, carried[r.pid])
		}
		r.info.NetRx, r.info.NetTx = new(float64(bytes[0])/secs), new(float64(bytes[1])/secs)
		r.info.NetPartial = n.UDP
	}
	return next
}

func counterDelta(p sockBaseline, rx, tx uint64) [2]uint64 {
	return [2]uint64{rx - min(rx, p.rx), tx - min(tx, p.tx)}
}

func addCounters(a, b [2]uint64) [2]uint64 { return [2]uint64{a[0] + b[0], a[1] + b[1]} }

// Extras sort descending with unknown values last; ties go by PID.
func sortProcesses(procList []*models.ProcessInfo, sortBy ProcSortBy) {
	var by func(a, b *models.ProcessInfo) int
	switch sortBy {
	case SortByMemory:
		by = func(a, b *models.ProcessInfo) int { return cmp.Compare(b.MemoryPercent, a.MemoryPercent) }
	case SortByName:
		by = func(a, b *models.ProcessInfo) int { return strings.Compare(a.Command, b.Command) }
	case SortByPID:
		by = func(a, b *models.ProcessInfo) int { return 0 }
	case SortByGPU:
		by = func(a, b *models.ProcessInfo) int { return knownDesc(a.GPU, b.GPU) }
	case SortByVRAM:
		by = func(a, b *models.ProcessInfo) int { return knownDesc(a.VRAMKB, b.VRAMKB) }
	case SortByDisk:
		by = func(a, b *models.ProcessInfo) int { return knownDesc(diskTotal(a), diskTotal(b)) }
	case SortByNetwork:
		by = func(a, b *models.ProcessInfo) int {
			if c := knownDesc(a.Connections, b.Connections); c != 0 {
				return c
			}
			return knownDesc(netTotal(a), netTotal(b))
		}
	default:
		by = func(a, b *models.ProcessInfo) int { return cmp.Compare(b.CPU, a.CPU) }
	}
	slices.SortFunc(procList, func(a, b *models.ProcessInfo) int {
		if c := by(a, b); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
}

func knownDesc[T int | float64 | uint64](a, b *T) int {
	switch {
	case a != nil && b != nil:
		return cmp.Compare(*b, *a)
	case a != nil:
		return -1
	case b != nil:
		return 1
	}
	return 0
}

func diskTotal(p *models.ProcessInfo) *float64 {
	if p.DiskRead == nil || p.DiskWrite == nil {
		return nil
	}
	return new(*p.DiskRead + *p.DiskWrite)
}

func netTotal(p *models.ProcessInfo) *float64 {
	if p.NetRx == nil || p.NetTx == nil {
		return nil
	}
	return new(*p.NetRx + *p.NetTx)
}

type ProcSortBy string

const (
	SortByCPU    ProcSortBy = "cpu"
	SortByMemory ProcSortBy = "memory"
	SortByName   ProcSortBy = "name"
	SortByPID    ProcSortBy = "pid"
	SortByGPU    ProcSortBy = "gpu"
	SortByVRAM   ProcSortBy = "vram"
	SortByDisk   ProcSortBy = "disk"
	// SortByNetwork orders by internet connections, then TCP rx+tx.
	SortByNetwork ProcSortBy = "network"
)

// Register enum in OpenAPI specification
// https://github.com/danielgtaylor/huma/issues/621
func (u ProcSortBy) Schema(r huma.Registry) *huma.Schema {
	if r.Map()["ProcSortBy"] == nil {
		schemaRef := r.Schema(reflect.TypeOf(""), true, "ProcSortBy")
		schemaRef.Title = "ProcSortBy"
		schemaRef.Enum = append(schemaRef.Enum, []any{
			string(SortByCPU),
			string(SortByMemory),
			string(SortByName),
			string(SortByPID),
			string(SortByGPU),
			string(SortByVRAM),
			string(SortByDisk),
			string(SortByNetwork),
		}...)
		r.Map()["ProcSortBy"] = schemaRef
	}
	return &huma.Schema{Ref: "#/components/schemas/ProcSortBy"}
}

func calculateProcessCPUPercentageWithCursor(cursor *models.ProcessCursorData, currentCPUTime float64, currentTime int64) float64 {
	if cursor.Timestamp == 0 || currentCPUTime <= cursor.Ticks {
		return 0
	}

	wallTimeDiff := float64(currentTime-cursor.Timestamp) / 1000.0
	if wallTimeDiff <= 0 {
		return 0
	}

	cpuTimeDiff := currentCPUTime - cursor.Ticks
	return (cpuTimeDiff / wallTimeDiff) * 100.0
}

func findMergeRoot(p *models.ProcessInfo, pidMap map[int32]*models.ProcessInfo) *models.ProcessInfo {
	parent, exists := pidMap[p.PPID]
	switch {
	case !exists:
		return p
	case parent.ExecutablePath == "" || p.ExecutablePath == "":
		return p
	case parent.ExecutablePath != p.ExecutablePath:
		return p
	default:
		return findMergeRoot(parent, pidMap)
	}
}

func mergeProcessesByExecutable(procList []*models.ProcessInfo) []*models.ProcessInfo {
	pidMap := make(map[int32]*models.ProcessInfo)
	for _, p := range procList {
		pidMap[p.PID] = p
	}

	mergeRoots := make(map[int32]int32)
	for _, p := range procList {
		root := findMergeRoot(p, pidMap)
		mergeRoots[p.PID] = root.PID
	}

	rootProcs := make(map[int32]*models.ProcessInfo)
	for _, p := range procList {
		rootPID := mergeRoots[p.PID]
		root := rootProcs[rootPID]
		if root == nil {
			clone := *pidMap[rootPID]
			clone.ChildCount = 0
			rootProcs[rootPID] = &clone
			root = rootProcs[rootPID]
		}

		if p.PID != rootPID {
			root.CPU += p.CPU
			root.MemoryKB += p.MemoryKB
			root.MemoryPercent += p.MemoryPercent
			root.RSSKB += p.RSSKB
			root.RSSPercent += p.RSSPercent
			root.PSSKB += p.PSSKB
			root.PSSPercent += p.PSSPercent
			root.GPU = addKnown(root.GPU, p.GPU)
			if root.GPU != nil && *root.GPU > 100 {
				root.GPU = new(100.0)
			}
			root.GPUVideo = addKnown(root.GPUVideo, p.GPUVideo)
			if root.GPUVideo != nil && *root.GPUVideo > 100 {
				root.GPUVideo = new(100.0)
			}
			root.GPUUnmonitored = root.GPUUnmonitored || p.GPUUnmonitored
			root.VRAMKB = addKnown(root.VRAMKB, p.VRAMKB)
			root.GPUGttKB = addKnown(root.GPUGttKB, p.GPUGttKB)
			root.DiskRead = addKnown(root.DiskRead, p.DiskRead)
			root.DiskWrite = addKnown(root.DiskWrite, p.DiskWrite)
			root.Connections = addKnown(root.Connections, p.Connections)
			root.LocalConnections = addKnown(root.LocalConnections, p.LocalConnections)
			root.NetPartial = root.NetPartial || p.NetPartial
			root.NetRx = addKnown(root.NetRx, p.NetRx)
			root.NetTx = addKnown(root.NetTx, p.NetTx)
			if len(p.Remotes) > 0 {
				// Clipped so the append never writes into the cloned row's array.
				root.Remotes = append(slices.Clip(root.Remotes), p.Remotes...)
				root.Remotes = root.Remotes[:min(len(root.Remotes), maxProcRemotes)]
			}
			root.Threads += p.Threads
			root.ChildCount++
		}
	}

	result := make([]*models.ProcessInfo, 0, len(rootProcs))
	for _, p := range rootProcs {
		result = append(result, p)
	}
	return result
}

// addKnown is unknown if either value is. The sum is a new allocation since
// merged roots share pointers with the rows they were cloned from.
func addKnown[T int | float64 | uint64](a, b *T) *T {
	if a == nil || b == nil {
		return nil
	}
	return new(*a + *b)
}
