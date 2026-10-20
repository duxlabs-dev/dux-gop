//go:build freebsd

package gops

import (
	"golang.org/x/sys/unix"

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
	cached := (v.Cached + v.Inactive + v.Laundry) / 1024
	available := v.Available / 1024

	arcSize, arcMin := readZfsArcStats()
	arcSizeKB := arcSize / 1024
	arcMinKB := arcMin / 1024

	var freeableArc uint64
	switch {
	case arcSizeKB > arcMinKB:
		freeableArc = arcSizeKB - arcMinKB
	}

	cached += arcSizeKB
	available += freeableArc

	usedDiff := free + cached + buffers
	var used uint64
	switch {
	case total >= usedDiff:
		used = total - usedDiff
	default:
		used = total - free
	}

	var usedPercent float64
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}

	var swapTotal, swapFree uint64
	if s, err := self.memProvider.SwapMemory(); err == nil {
		swapTotal = s.Total / 1024
		swapFree = s.Free / 1024
	}

	return &models.MemoryInfo{
		Total:       total,
		Used:        used,
		UsedPercent: usedPercent,
		Free:        free,
		Available:   available,
		Buffers:     buffers,
		Cached:      cached,
		ZfsArcSize:  arcSizeKB,
		SwapTotal:   swapTotal,
		SwapFree:    swapFree,
	}, nil
}

// OpenZFS exposes kstats as sysctls under kstat.zfs.<module>.<name>,
// per module/os/freebsd/spl/spl_kstat.c.
func readZfsArcStats() (size uint64, cMin uint64) {
	size, err := unix.SysctlUint64("kstat.zfs.misc.arcstats.size")
	if err != nil {
		return 0, 0
	}
	cMin, _ = unix.SysctlUint64("kstat.zfs.misc.arcstats.c_min")
	return size, cMin
}
