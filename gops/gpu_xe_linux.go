//go:build linux

package gops

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// From include/uapi/drm/xe_drm.h. xe has no sysfs file for VRAM use; the
// memory region query is the unprivileged source since Linux 6.18.
const (
	xeDeviceQueryMemRegions = 1
	xeMemRegionClassVRAM    = 1
	xeMemRegionsHeaderSize  = 8
	xeMemRegionSize         = 88
)

// DRM_IOWR(DRM_COMMAND_BASE + DRM_XE_DEVICE_QUERY, struct drm_xe_device_query)
var xeIoctlDeviceQuery = uintptr(0xc0000000 | unsafe.Sizeof(xeDeviceQuery{})<<16 | 'd'<<8 | 0x40)

type xeDeviceQuery struct {
	Extensions uint64
	Query      uint32
	Size       uint32
	Data       uint64
	Reserved   [2]uint64
}

func readXeVRAM(dev string, sample *gpuSample) {
	nodes, _ := filepath.Glob(filepath.Join(dev, "drm", "renderD*"))
	if len(nodes) == 0 {
		return
	}

	node, err := os.OpenFile(filepath.Join("/dev/dri", filepath.Base(nodes[0])), os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer node.Close()
	fd := int(node.Fd())

	query := xeDeviceQuery{Query: xeDeviceQueryMemRegions}
	if xeIoctl(fd, &query) != nil || query.Size < xeMemRegionsHeaderSize {
		return
	}
	buf := make([]byte, query.Size)
	query.Data = uint64(uintptr(unsafe.Pointer(&buf[0])))
	err = xeIoctl(fd, &query)
	runtime.KeepAlive(buf)
	if err != nil {
		return
	}

	if used, total, ok := parseXeMemRegions(buf); ok {
		sample.MemoryUsed = new(used / 1024)
		sample.MemoryTotal = new(total / 1024)
	}
}

func xeIoctl(fd int, query *xeDeviceQuery) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), xeIoctlDeviceQuery, uintptr(unsafe.Pointer(query)))
	if errno != 0 {
		return errno
	}
	return nil
}

// parseXeMemRegions sums the VRAM regions of all tiles. Kernels before 6.18
// report used as zero without CAP_PERFMON; a live card always has firmware
// buffers in VRAM, so zero means unknown rather than empty.
func parseXeMemRegions(buf []byte) (used, total uint64, ok bool) {
	if len(buf) < xeMemRegionsHeaderSize {
		return 0, 0, false
	}
	count := int(binary.NativeEndian.Uint32(buf[0:4]))
	for i := range count {
		off := xeMemRegionsHeaderSize + i*xeMemRegionSize
		if off+xeMemRegionSize > len(buf) {
			break
		}
		region := buf[off : off+xeMemRegionSize]
		if binary.NativeEndian.Uint16(region[0:2]) != xeMemRegionClassVRAM {
			continue
		}
		total += binary.NativeEndian.Uint64(region[8:16])
		used += binary.NativeEndian.Uint64(region[16:24])
	}
	return used, total, total > 0 && used > 0
}
