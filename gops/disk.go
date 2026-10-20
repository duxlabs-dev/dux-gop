package gops

import (
	"fmt"
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
)

func (self *GopsUtil) GetDiskInfo() ([]*models.DiskInfo, error) {
	diskIO, err := self.diskProvider.IOCounters()
	res := make([]*models.DiskInfo, 0)
	if err == nil {
		for name, d := range diskIO {
			// Filter to match bash script patterns
			if matchesDiskDevice(name) {
				res = append(res, &models.DiskInfo{
					Name:  name,
					Read:  d.ReadBytes / 512,  // Convert to sectors
					Write: d.WriteBytes / 512, // Convert to sectors
				})
			}
		}
	}
	return res, nil
}

func (self *GopsUtil) GetDiskMounts() ([]*models.DiskMountInfo, error) {
	partitions, err := self.diskProvider.Partitions(true)
	if err != nil {
		return nil, err
	}

	var metrics []*models.DiskMountInfo
	seen := make(map[string]struct{})
	for _, p := range partitions {
		switch {
		case isVirtualFS(p.Fstype), isVirtualMount(p.Mountpoint):
			continue
		}

		identity := mountIdentity(p.Device, p.Mountpoint)
		if _, dup := seen[identity]; dup {
			continue
		}

		usage, err := self.diskProvider.Usage(p.Mountpoint)
		if err != nil {
			continue
		}

		seen[identity] = struct{}{}
		metrics = append(metrics, &models.DiskMountInfo{
			Device:  p.Device,
			Mount:   p.Mountpoint,
			FSType:  p.Fstype,
			Size:    formatBytes(usage.Total),
			Used:    formatBytes(usage.Used),
			Avail:   formatBytes(usage.Free),
			Percent: fmt.Sprintf("%.0f%%", usage.UsedPercent),
		})
	}

	return metrics, nil
}

// Non-block sources (FUSE, overlay, nfs) name the filesystem, not the mount, so
// independent mounts collide on Device alone.
func mountIdentity(device, mountpoint string) string {
	if strings.HasPrefix(device, "/") {
		return device
	}
	return device + " " + mountpoint
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(bytes)/float64(div), "KMGTPE"[exp])
}
