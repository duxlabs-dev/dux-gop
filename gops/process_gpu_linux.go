//go:build linux

package gops

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type processGPUScanner struct {
	procRoot   string
	drmClass   string
	pciDevices string
	nvidiaPmon func(busIds []string) ([]byte, error)
}

var defaultProcessGPUScanner = processGPUScanner{
	procRoot:   "/proc",
	drmClass:   "/sys/class/drm",
	pciDevices: "/sys/bus/pci/devices",
	nvidiaPmon: runNvidiaPmon,
}

func scanProcessGPU(pids []int32, entries []gpuEntry, now time.Time) processGPUSample {
	return defaultProcessGPUScanner.scan(pids, entries, now)
}

func (s processGPUScanner) scan(pids []int32, entries []gpuEntry, now time.Time) processGPUSample {
	sample := newProcessGPUSample(now)
	suspended := map[string]bool{}

	for _, pid := range pids {
		s.scanProcess(pid, sample, suspended)
	}

	var nvidia []string
	for _, entry := range entries {
		if entry.Driver == "nvidia" && entry.BusId != "" && !isRuntimeSuspended(filepath.Join(s.pciDevices, entry.BusId)) {
			nvidia = append(nvidia, entry.BusId)
		}
	}
	if len(nvidia) > 0 {
		if out, err := s.nvidiaPmon(nvidia); err == nil {
			parseNvidiaPmon(out, sample)
		}
	}

	return sample
}

// scanProcess reads the DRM usage stats of every GPU file the process holds.
// Other users' processes are not readable and are skipped.
func (s processGPUScanner) scanProcess(pid int32, sample processGPUSample, suspended map[string]bool) {
	pidDir := filepath.Join(s.procRoot, strconv.Itoa(int(pid)))
	fds, err := os.ReadDir(filepath.Join(pidDir, "fd"))
	if err != nil {
		return
	}

	seen := map[string]bool{}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(pidDir, "fd", fd.Name()))
		if err != nil || !strings.HasPrefix(target, "/dev/dri/") {
			continue
		}

		// Reading fdinfo of an xe client resumes the device.
		node := filepath.Base(target)
		asleep, known := suspended[node]
		if !known {
			asleep = isRuntimeSuspended(filepath.Join(s.drmClass, node, "device"))
			suspended[node] = asleep
		}
		if asleep {
			continue
		}

		data, err := os.ReadFile(filepath.Join(pidDir, "fdinfo", fd.Name()))
		if err != nil {
			continue
		}
		parseDRMFdinfo(pid, data, sample, seen)
	}
}

// parseDRMFdinfo follows Documentation/gpu/drm-usage-stats.rst. Duplicated
// file descriptors share a drm-client-id and are counted once.
func parseDRMFdinfo(pid int32, data []byte, sample processGPUSample, seen map[string]bool) {
	fields := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if ok && strings.HasPrefix(key, "drm-") {
			fields[key] = strings.TrimSpace(value)
		}
	}

	client := fields["drm-client-id"]
	if client == "" {
		return
	}
	pdev := fields["drm-pdev"]
	if pdev == "" {
		pdev = fields["drm-driver"]
	}
	if seen[pdev+"/"+client] {
		return
	}
	seen[pdev+"/"+client] = true
	sample.Clients[pid] = true

	residentFound := false
	var vramKB, legacyVramKB uint64
	for key, value := range fields {
		switch {
		case strings.HasPrefix(key, "drm-engine-capacity-"):
			engine := strings.TrimPrefix(key, "drm-engine-capacity-")
			if capacity, err := strconv.ParseUint(value, 10, 64); err == nil && capacity > 0 {
				if sample.Capacity[pid] == nil {
					sample.Capacity[pid] = map[string]uint64{}
				}
				sample.Capacity[pid][gpuCounterKey(pdev, client, engine, "ns")] = capacity
			}
		case strings.HasPrefix(key, "drm-engine-"):
			if ns, ok := parseDRMUint(value, "ns"); ok {
				sample.setCounter(pid, gpuCounterKey(pdev, client, strings.TrimPrefix(key, "drm-engine-"), "ns"), ns)
			}
		case strings.HasPrefix(key, "drm-total-cycles-"):
			if cycles, ok := parseDRMUint(value, ""); ok {
				sample.setCounter(pid, gpuCounterKey(pdev, client, strings.TrimPrefix(key, "drm-total-cycles-"), "tot"), cycles)
			}
		case strings.HasPrefix(key, "drm-cycles-"):
			if cycles, ok := parseDRMUint(value, ""); ok {
				sample.setCounter(pid, gpuCounterKey(pdev, client, strings.TrimPrefix(key, "drm-cycles-"), "cyc"), cycles)
			}
		case strings.HasPrefix(key, "drm-resident-"):
			if isDeviceMemoryRegion(strings.TrimPrefix(key, "drm-resident-")) {
				residentFound = true
				vramKB += parseDRMMemoryKB(value)
			}
		case strings.HasPrefix(key, "drm-memory-"):
			if isDeviceMemoryRegion(strings.TrimPrefix(key, "drm-memory-")) {
				legacyVramKB += parseDRMMemoryKB(value)
			}
		}
	}

	// drm-memory-<region> is the deprecated spelling of drm-resident-<region>.
	if !residentFound {
		vramKB = legacyVramKB
	}
	sample.MemoryKB[pid] += vramKB
}

// isDeviceMemoryRegion keeps on-card memory: amdgpu "vram", xe "vram0",
// i915 "local0". System memory regions (gtt, cpu, system, memory) are skipped.
func isDeviceMemoryRegion(region string) bool {
	return strings.HasPrefix(region, "vram") || strings.HasPrefix(region, "local")
}

func parseDRMUint(value, unit string) (uint64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 || (unit != "" && (len(fields) != 2 || fields[1] != unit)) {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[0], 10, 64)
	return v, err == nil
}

func parseDRMMemoryKB(value string) uint64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	switch {
	case len(fields) == 1:
		return v / 1024
	case fields[1] == "KiB":
		return v
	case fields[1] == "MiB":
		return v * 1024
	}
	return 0
}

func runNvidiaPmon(busIds []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "nvidia-smi", "pmon", "--count", "1", "--select", "um",
		"--id", strings.Join(busIds, ",")).Output()
}

// parseNvidiaPmon reads columns by the header, which differs between driver
// versions. A "-" in sm means no work in the sample window, which is 0%.
func parseNvidiaPmon(out []byte, sample processGPUSample) {
	var columns []string
	index := func(name string) int {
		for i, column := range columns {
			if column == name {
				return i
			}
		}
		return -1
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "#" {
			if columns == nil && len(fields) > 1 && fields[1] == "gpu" {
				columns = fields[1:]
			}
			continue
		}
		gpuCol, pidCol, smCol, fbCol := index("gpu"), index("pid"), index("sm"), index("fb")
		if gpuCol < 0 || pidCol < 0 || smCol < 0 || len(fields) <= max(gpuCol, pidCol, smCol) {
			continue
		}
		pid, err := strconv.ParseInt(fields[pidCol], 10, 32)
		if err != nil {
			continue
		}

		usage, err := strconv.ParseFloat(fields[smCol], 64)
		if err != nil && fields[smCol] != "-" {
			continue
		}
		sample.setDirect(int32(pid), "nvidia:"+fields[gpuCol], usage)

		if fbCol >= 0 && fbCol < len(fields) {
			if mib, err := strconv.ParseUint(fields[fbCol], 10, 64); err == nil {
				sample.MemoryKB[int32(pid)] += mib * 1024
			}
		}
	}
}

func (s processGPUSample) setDirect(pid int32, gpu string, usage float64) {
	if s.Direct[pid] == nil {
		s.Direct[pid] = map[string]float64{}
	}
	s.Direct[pid][gpu] = usage
	s.Clients[pid] = true
}

func gpuCounterKey(pdev, client, engine, kind string) string {
	return pdev + "/" + client + "/" + engine + "/" + kind
}
