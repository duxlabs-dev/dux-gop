package gops

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	cpuBaselineInterval   = 200 * time.Millisecond
	maxCursorDecodedBytes = 4 << 20
)

// A fresh gzip writer allocates ~850KB of flate hash tables, which is wasteful
// for the ~4KB cursor emitted on every poll. Reuse them instead.
var cursorWriters = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.BestCompression)
		return gz
	},
}

type processCursorWire struct {
	BaseMillis   int64   `json:"t"`
	PIDs         []int32 `json:"pid"`
	CPUMillis    []int64 `json:"cpu"`
	OffsetMillis []int64 `json:"dt"`

	GPUMillis int64    `json:"gt,omitempty"`
	GPUPIDs   []int32  `json:"gp,omitempty"`
	GPUKeys   []string `json:"gk,omitempty"`
	GPUValues []uint64 `json:"gv,omitempty"`
}

func encodeProcessCursor(entries []models.ProcessCursorData) string {
	return encodeProcessCursorWithGPU(entries, 0, nil)
}

func encodeProcessCursorWithGPU(entries []models.ProcessCursorData, gpuMillis int64, gpuCounters map[int32]map[string]uint64) string {
	wire := processCursorWire{
		PIDs:         make([]int32, 0, len(entries)),
		CPUMillis:    make([]int64, 0, len(entries)),
		OffsetMillis: make([]int64, 0, len(entries)),
	}
	for i, e := range entries {
		if i == 0 || e.Timestamp < wire.BaseMillis {
			wire.BaseMillis = e.Timestamp
		}
	}
	for _, e := range entries {
		wire.PIDs = append(wire.PIDs, e.PID)
		wire.CPUMillis = append(wire.CPUMillis, int64(math.Round(e.Ticks*1000)))
		wire.OffsetMillis = append(wire.OffsetMillis, e.Timestamp-wire.BaseMillis)
	}
	// The timestamp is written even without counters, so a sample that found
	// no DRM client is still a baseline for the next request.
	if gpuMillis > 0 {
		wire.GPUMillis = gpuMillis
		for pid, counters := range gpuCounters {
			for key, value := range counters {
				wire.GPUPIDs = append(wire.GPUPIDs, pid)
				wire.GPUKeys = append(wire.GPUKeys, key)
				wire.GPUValues = append(wire.GPUValues, value)
			}
		}
	}

	raw, _ := json.Marshal(wire)
	var buf bytes.Buffer
	gz := cursorWriters.Get().(*gzip.Writer)
	defer cursorWriters.Put(gz)
	gz.Reset(&buf)
	_, _ = gz.Write(raw)
	_ = gz.Close()
	return base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func decodeProcessCursorWire(cursor string) (processCursorWire, bool) {
	var wire processCursorWire
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) == 0 {
		return wire, false
	}

	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return wire, false
	}
	defer func() { _ = gz.Close() }()
	if raw, err = io.ReadAll(io.LimitReader(gz, maxCursorDecodedBytes)); err != nil {
		return wire, false
	}

	if json.Unmarshal(raw, &wire) != nil {
		return processCursorWire{}, false
	}
	return wire, true
}

func decodeProcessCursor(cursor string) map[int32]*models.ProcessCursorData {
	out := make(map[int32]*models.ProcessCursorData)
	wire, ok := decodeProcessCursorWire(cursor)
	if !ok || len(wire.CPUMillis) != len(wire.PIDs) || len(wire.OffsetMillis) != len(wire.PIDs) {
		return out
	}
	entries := make([]models.ProcessCursorData, len(wire.PIDs))
	for i, pid := range wire.PIDs {
		entries[i] = models.ProcessCursorData{
			PID:       pid,
			Ticks:     float64(wire.CPUMillis[i]) / 1000,
			Timestamp: wire.BaseMillis + wire.OffsetMillis[i],
		}
		out[pid] = &entries[i]
	}
	return out
}

func decodeProcessGPUCursor(cursor string) processGPUSample {
	sample := newProcessGPUSample(time.Time{})
	sample.TimeMs = 0
	wire, ok := decodeProcessCursorWire(cursor)
	if !ok || wire.GPUMillis <= 0 || len(wire.GPUKeys) != len(wire.GPUPIDs) || len(wire.GPUValues) != len(wire.GPUPIDs) {
		return sample
	}
	sample.TimeMs = wire.GPUMillis
	for i, pid := range wire.GPUPIDs {
		sample.setCounter(pid, wire.GPUKeys[i], wire.GPUValues[i])
	}
	return sample
}

// gopsutil can panic on macOS when reading process info for system processes
// or processes that exit mid-read, and this runs on the request goroutine
// where a panic would take down the whole call.
func readProcessTimes(p *process.Process) (times *cpu.TimesStat) {
	defer func() {
		if recover() != nil {
			times = nil
		}
	}()
	times, _ = p.Times()
	return times
}

func (self *GopsUtil) GetProcesses(sortBy ProcSortBy, limit int, enableCPU bool, mergeChildren bool) (*models.ProcessListResponse, error) {
	return self.GetProcessesWithCursor(sortBy, limit, enableCPU, "", mergeChildren)
}

func (self *GopsUtil) GetProcessesWithCursor(sortBy ProcSortBy, limit int, enableCPU bool, cursor string, mergeChildren bool) (*models.ProcessListResponse, error) {
	return self.GetProcessesWithOptions(ProcessOptions{
		SortBy:        sortBy,
		Limit:         limit,
		EnableCPU:     enableCPU,
		Cursor:        cursor,
		MergeChildren: mergeChildren,
	})
}

type ProcessOptions struct {
	SortBy    ProcSortBy
	Limit     int
	EnableCPU bool
	// EnableGPU samples per-process usage of the GPUs named in GPUPciIds:
	// DRM file descriptors for DRM cards, nvidia-smi pmon for NVIDIA cards.
	// Sorting by GPU turns it on.
	EnableGPU     bool
	GPUPciIds     []string
	Cursor        string
	MergeChildren bool
}

func (self *GopsUtil) GetProcessesWithOptions(opts ProcessOptions) (*models.ProcessListResponse, error) {
	sortBy, limit, enableCPU, mergeChildren := opts.SortBy, opts.Limit, opts.EnableCPU, opts.MergeChildren
	enableGPU := opts.EnableGPU || sortBy == SortByGPU

	procs, err := self.procProvider.Processes()
	if err != nil {
		return nil, err
	}

	totalMem, _ := self.memProvider.VirtualMemory()

	cursorMap := decodeProcessCursor(opts.Cursor)

	pids := make([]int32, len(procs))
	for i, p := range procs {
		pids[i] = p.Pid
	}
	var gpuEntries []gpuEntry
	var gpuPrev processGPUSample
	if enableGPU {
		gpuEntries = requestedGPUEntries(opts.GPUPciIds)
		enableGPU = len(gpuEntries) > 0
	}
	if enableGPU {
		gpuPrev = decodeProcessGPUCursor(opts.Cursor)
	}
	gpuBaseline := enableGPU && gpuPrev.TimeMs == 0

	if enableCPU && len(cursorMap) == 0 {
		for _, p := range procs {
			times := readProcessTimes(p)
			if times == nil {
				continue
			}
			cursorMap[p.Pid] = &models.ProcessCursorData{
				PID:       p.Pid,
				Ticks:     times.User + times.System,
				Timestamp: time.Now().UnixMilli(),
			}
		}
		if gpuBaseline {
			gpuPrev = scanProcessGPU(pids, gpuEntries, time.Now())
		}
		time.Sleep(cpuBaselineInterval)
	} else if gpuBaseline {
		gpuPrev = scanProcessGPU(pids, gpuEntries, time.Now())
		time.Sleep(cpuBaselineInterval)
	}

	type procResult struct {
		index     int
		info      *models.ProcessInfo
		sampledAt int64
		sampled   bool
	}

	numCPU := float64(runtime.NumCPU())

	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}

	jobs := make(chan int, len(procs))
	results := make(chan procResult, len(procs))

	for w := 0; w < numWorkers; w++ {
		go func() {
			for idx := range jobs {
				p := procs[idx]

				// gopsutil can panic on macOS when reading process info
				// for system processes or processes that exit mid-read.
				func() {
					defer func() {
						if r := recover(); r != nil {
							results <- procResult{
								index: idx,
								info: &models.ProcessInfo{
									PID:     p.Pid,
									Command: fmt.Sprintf("[pid %d]", p.Pid),
								},
							}
						}
					}()

					name, _ := p.Name()
					cmdline, _ := p.Cmdline()
					ppid, _ := p.Ppid()
					memInfo, _ := p.MemoryInfo()
					times, _ := p.Times()
					sampledAt := time.Now().UnixMilli()
					username, _ := p.Username()
					exePath, _ := p.Exe()

					currentCPUTime := float64(0)
					if times != nil {
						currentCPUTime = times.User + times.System
					}

					cpuPercent := 0.0
					if enableCPU {
						if cursorData, hasCursor := cursorMap[p.Pid]; hasCursor {
							coreRate := calculateProcessCPUPercentageWithCursor(cursorData, currentCPUTime, sampledAt)
							cpuPercent = coreRate / numCPU
							if cpuPercent > 100 {
								cpuPercent = 100
							}
						}
					}

					rssKB := uint64(0)
					rssPercent := float32(0)
					pssKB := uint64(0)
					pssPercent := float32(0)
					memKB := uint64(0)
					memPercent := float32(0)
					memCalc := "rss"

					if memInfo != nil {
						rssKB = memInfo.RSS / 1024
						rssPercent = float32(memInfo.RSS) / float32(totalMem.Total) * 100

						memKB = rssKB
						memPercent = rssPercent

						if rssKB > 102400 {
							pssDirty, err := getPssDirty(p.Pid)
							if err == nil && pssDirty > 0 {
								memKB = pssDirty
								memPercent = float32(memKB*1024) / float32(totalMem.Total) * 100
								memCalc = "pss_dirty"
							}
						}
					}

					results <- procResult{
						index:     idx,
						sampledAt: sampledAt,
						sampled:   times != nil,
						info: &models.ProcessInfo{
							PID:               p.Pid,
							PPID:              ppid,
							CPU:               cpuPercent,
							PTicks:            currentCPUTime,
							MemoryPercent:     memPercent,
							MemoryKB:          memKB,
							MemoryCalculation: memCalc,
							RSSKB:             rssKB,
							RSSPercent:        rssPercent,
							PSSKB:             pssKB,
							PSSPercent:        pssPercent,
							Username:          username,
							Command:           name,
							FullCommand:       cmdline,
							ExecutablePath:    exePath,
						},
					}
				}()
			}
		}()
	}

	for i := range procs {
		jobs <- i
	}
	close(jobs)

	procList := make([]*models.ProcessInfo, len(procs))
	cursorList := make([]models.ProcessCursorData, 0, len(procs))
	for i := 0; i < len(procs); i++ {
		r := <-results
		procList[r.index] = r.info
		if r.sampled {
			cursorList = append(cursorList, models.ProcessCursorData{
				PID:       r.info.PID,
				Ticks:     r.info.PTicks,
				Timestamp: r.sampledAt,
			})
		}
	}

	var gpuCounters map[int32]map[string]uint64
	var gpuMillis int64
	if enableGPU {
		cur := scanProcessGPU(pids, gpuEntries, time.Now())
		usage := processGPUUsage(gpuPrev, cur)
		for _, info := range procList {
			if pct, ok := usage[info.PID]; ok {
				info.GPU = &pct
			}
			if cur.Clients[info.PID] {
				kb := cur.MemoryKB[info.PID]
				info.GPUMemoryKB = &kb
			}
		}
		gpuCounters = nextGPUCounters(gpuPrev, cur)
		gpuMillis = cur.TimeMs
	}

	if mergeChildren {
		procList = mergeProcessesByExecutable(procList)
	}

	switch sortBy {
	case SortByCPU:
		sort.Slice(procList, func(i, j int) bool {
			return procList[i].CPU > procList[j].CPU
		})
	case SortByMemory:
		sort.Slice(procList, func(i, j int) bool {
			return procList[i].MemoryPercent > procList[j].MemoryPercent
		})
	case SortByName:
		sort.Slice(procList, func(i, j int) bool {
			return procList[i].Command < procList[j].Command
		})
	case SortByPID:
		sort.Slice(procList, func(i, j int) bool {
			return procList[i].PID < procList[j].PID
		})
	case SortByGPU:
		sort.SliceStable(procList, func(i, j int) bool {
			return gpuSortValue(procList[i]) > gpuSortValue(procList[j])
		})
	default:
		sort.Slice(procList, func(i, j int) bool {
			return procList[i].CPU > procList[j].CPU
		})
	}

	if limit > 0 && len(procList) > limit {
		procList = procList[:limit]
	}

	return &models.ProcessListResponse{
		Processes: procList,
		Cursor:    encodeProcessCursorWithGPU(cursorList, gpuMillis, gpuCounters),
	}, nil
}

type ProcSortBy string

const (
	SortByCPU    ProcSortBy = "cpu"
	SortByMemory ProcSortBy = "memory"
	SortByName   ProcSortBy = "name"
	SortByPID    ProcSortBy = "pid"
	SortByGPU    ProcSortBy = "gpu"
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
			root.GPU = sumGPUUsage(root.GPU, p.GPU)
			root.GPUMemoryKB = sumGPUMemory(root.GPUMemoryKB, p.GPUMemoryKB)
			root.ChildCount++
		}
	}

	result := make([]*models.ProcessInfo, 0, len(rootProcs))
	for _, p := range rootProcs {
		result = append(result, p)
	}
	return result
}

// Processes without a GPU client sort below idle clients.
func gpuSortValue(p *models.ProcessInfo) float64 {
	if p.GPU == nil {
		return -1
	}
	return *p.GPU
}

func sumGPUUsage(a, b *float64) *float64 {
	if a == nil && b == nil {
		return nil
	}
	sum := 0.0
	if a != nil {
		sum += *a
	}
	if b != nil {
		sum += *b
	}
	sum = min(100, sum)
	return &sum
}

func sumGPUMemory(a, b *uint64) *uint64 {
	if a == nil && b == nil {
		return nil
	}
	var sum uint64
	if a != nil {
		sum += *a
	}
	if b != nil {
		sum += *b
	}
	return &sum
}
