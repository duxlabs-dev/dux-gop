//go:build linux

package gops

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
)

func (self *GopsUtil) GetMemoryInfo() (*models.MemoryInfo, error) {
	v, err := self.memProvider.VirtualMemory()
	if err != nil {
		return nil, err
	}

	total := v.Total / 1024
	free := v.Free / 1024
	buffers := v.Buffers / 1024
	cached := v.Cached / 1024
	sreclaimable := v.Sreclaimable / 1024
	shared := v.Shared / 1024
	available := v.Available / 1024

	rawCached := cached - sreclaimable

	arcSize, arcMin := readZfsArcStats()
	arcSizeKB := arcSize / 1024
	arcMinKB := arcMin / 1024

	var freeableArc uint64
	switch {
	case arcSizeKB > arcMinKB:
		freeableArc = arcSizeKB - arcMinKB
	}

	rawCached += arcSizeKB
	available += freeableArc

	gpuActive, gpuReclaim := readGPUMemStats()
	available += gpuReclaim

	usedDiff := free + rawCached + sreclaimable + buffers + gpuReclaim
	var used uint64
	switch {
	case total >= usedDiff:
		used = total - usedDiff + shared
	default:
		used = total - free
	}

	var usedPercent float64
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}

	zramOrig, zramCompr := readZramStats("/sys/block")

	return &models.MemoryInfo{
		Total:        total,
		Used:         used,
		UsedPercent:  usedPercent,
		Free:         free,
		Available:    available,
		Buffers:      buffers,
		Cached:       rawCached,
		SReclaimable: sreclaimable,
		Shared:       shared,
		ZfsArcSize:   arcSizeKB,
		GPUActive:    gpuActive,
		GPUReclaim:   gpuReclaim,
		SwapTotal:    v.SwapTotal / 1024,
		SwapFree:     v.SwapFree / 1024,
		ZramOrig:     zramOrig,
		ZramCompr:    zramCompr,
		Committed:    v.CommittedAS / 1024,
		CommitLimit:  v.CommitLimit / 1024,
		SwapDevices:  readSwapDevices(),
	}, nil
}

func readSwapDevices() []models.SwapDevice {
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil
	}
	return parseProcSwaps(string(data))
}

// parseProcSwaps parses /proc/swaps; sizes there are already in KB.
func parseProcSwaps(text string) []models.SwapDevice {
	var devices []models.SwapDevice
	for _, line := range strings.Split(text, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		size, err1 := strconv.ParseUint(fields[2], 10, 64)
		used, err2 := strconv.ParseUint(fields[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		kind := fields[1]
		if strings.HasPrefix(fields[0], "/dev/zram") {
			kind = "zram"
		}
		devices = append(devices, models.SwapDevice{Name: fields[0], Kind: kind, Size: size, Used: used})
	}
	return devices
}

// readZramStats sums orig_data_size and compr_data_size (fields 1 and 2 of
// mm_stat, bytes) over all zram devices under blockDir, returning KB.
func readZramStats(blockDir string) (orig, compr uint64) {
	paths, _ := filepath.Glob(filepath.Join(blockDir, "zram*", "mm_stat"))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(data))
		if len(fields) < 2 {
			continue
		}
		o, err1 := strconv.ParseUint(fields[0], 10, 64)
		c, err2 := strconv.ParseUint(fields[1], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		orig += o / 1024
		compr += c / 1024
	}
	return orig, compr
}

func readGPUMemStats() (active uint64, reclaim uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "GPUActive:":
			active, _ = strconv.ParseUint(fields[1], 10, 64)
		case "GPUReclaim:":
			reclaim, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return active, reclaim
}

func readZfsArcStats() (size uint64, cMin uint64) {
	f, err := os.Open("/proc/spl/kstat/zfs/arcstats")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 {
			continue
		}
		switch fields[0] {
		case "size":
			size, _ = strconv.ParseUint(fields[2], 10, 64)
		case "c_min":
			cMin, _ = strconv.ParseUint(fields[2], 10, 64)
		}
	}
	return size, cMin
}
