//go:build linux

package gops

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/duxlabs-dev/dux-gop/models"
	"golang.org/x/sys/unix"
)

type gpuNodeState uint8

const (
	gpuNodeUnknown gpuNodeState = iota // a node of no detected GPU
	gpuNodeRead
	// A runtime-suspended GPU has been idle for at least its autosuspend delay.
	gpuNodeSuspended
	gpuNodeExcluded
)

type gpuNodeSet struct {
	nodes map[string]gpuNodeState
	// Bus ids of the enabled, awake NVIDIA GPUs.
	nvidia []string
}

// gpuNodes reads sysfs only. Only gpuNodeRead nodes may have their fdinfo
// read: that runs the driver, which can resume a suspended GPU.
func gpuNodes(pciRoot string, gpus []models.GPU, pciIds []string) gpuNodeSet {
	set := gpuNodeSet{nodes: map[string]gpuNodeState{}}
	for _, g := range gpus {
		state := gpuNodeRead
		switch {
		case !gpuRequested(g, pciIds):
			state = gpuNodeExcluded
		case isRuntimeSuspended(filepath.Join(pciRoot, g.BusId)):
			state = gpuNodeSuspended
		}
		if g.Driver == "nvidia" && state == gpuNodeRead && g.BusId != "" {
			set.nvidia = append(set.nvidia, g.BusId)
		}
		entries, _ := os.ReadDir(filepath.Join(pciRoot, g.BusId, "drm"))
		for _, e := range entries {
			set.nodes[e.Name()] = state
		}
	}
	return set
}

type procFDScan struct {
	DRM drmClientSet
	// NvidiaGPU is set for an fd on /dev/nvidia<N>, which CUDA and other
	// proprietary-driver clients use without any /dev/dri fd.
	NvidiaGPU   bool
	ExcludedGPU bool
	UnknownGPU  bool
	// SocketInodes and NetNS (the ns/net link) are read only when sockets are requested.
	SocketInodes []uint32
	NetNS        string
}

var (
	driPrefix    = []byte("/dev/dri/")
	nvidiaPrefix = []byte("/dev/nvidia")
	socketPrefix = []byte("socket:[")
)

// scanProcFDs errors only when the fd directory is unreadable, so an unknown process differs from one without matching fds.
func scanProcFDs(procRoot string, pid int32, nodes map[string]gpuNodeState, sockets bool) (procFDScan, error) {
	pidDir := filepath.Join(procRoot, strconv.Itoa(int(pid)))
	dir, err := os.Open(filepath.Join(pidDir, "fd"))
	if err != nil {
		return procFDScan{}, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return procFDScan{}, err
	}

	var scan procFDScan
	dirfd := int(dir.Fd())
	buf := make([]byte, 64)
	if sockets {
		if n, err := unix.Readlinkat(dirfd, "../ns/net", buf); err == nil {
			scan.NetNS = string(buf[:n])
		}
	}
	for _, name := range names {
		n, err := unix.Readlinkat(dirfd, name, buf)
		if err != nil {
			continue
		}
		target := buf[:n]
		switch {
		case sockets && bytes.HasPrefix(target, socketPrefix):
			if inode, ok := parseSocketLink(target); ok {
				scan.SocketInodes = append(scan.SocketInodes, inode)
			}
		case bytes.HasPrefix(target, nvidiaPrefix):
			scan.NvidiaGPU = scan.NvidiaGPU || isNvidiaGPUDevice(target)
		case bytes.HasPrefix(target, driPrefix):
			switch nodes[string(target[len(driPrefix):])] {
			case gpuNodeUnknown:
				scan.UnknownGPU = true
			case gpuNodeExcluded:
				scan.ExcludedGPU = true
			case gpuNodeRead:
				data, err := os.ReadFile(filepath.Join(pidDir, "fdinfo", name))
				if err != nil {
					continue
				}
				if client, ok := parseDRMFdinfo(string(data)); ok {
					if scan.DRM == nil {
						scan.DRM = drmClientSet{}
					}
					scan.DRM[client.key()] = client
				}
			}
		}
	}
	return scan, nil
}

func (self processExtras) readFDs(procRoot string, pid int32) (g procGPU, n procNet) {
	sockets := self.net != nil && self.net.ok
	if self.gpu == nil && !sockets {
		return g, n
	}
	var nodes map[string]gpuNodeState
	if self.gpu != nil {
		nodes = self.gpu.nodes
	}
	scan, err := scanProcFDs(procRoot, pid, nodes, sockets)
	if err != nil {
		return g, n
	}
	if self.gpu != nil {
		g = self.gpu.fromScan(pid, scan)
	}
	if sockets && (!self.net.foreign(scan) || self.net.loopbackOnly(procRoot, pid, scan.NetNS)) {
		n = self.net.fromScan(scan)
	}
	return g, n
}

func parseSocketLink(target []byte) (uint32, bool) {
	s, ok := bytes.CutPrefix(target, socketPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(string(bytes.TrimSuffix(s, []byte("]"))), 10, 32)
	return uint32(n), err == nil
}

func isNvidiaGPUDevice(target []byte) bool {
	n, ok := bytes.CutPrefix(target, nvidiaPrefix)
	if !ok || len(n) == 0 {
		return false
	}
	for _, c := range n {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func listProcPIDs(procRoot string) ([]int32, error) {
	fd, err := unix.Open(procRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()
	pids := make([]int32, 0, 1024)
	var buf [16 << 10]byte
	for {
		n, err := unix.Getdents(fd, buf[:])
		if err != nil {
			return nil, err
		}
		if n <= 0 {
			return pids, nil
		}
		// linux_dirent64: d_reclen at offset 16, the NUL-terminated d_name at 19.
		for off := 0; off+19 < n; {
			reclen := int(binary.NativeEndian.Uint16(buf[off+16:]))
			if pid, ok := parsePID(buf[off+19 : off+reclen]); ok {
				pids = append(pids, pid)
			}
			off += reclen
		}
	}
}

func parsePID(name []byte) (int32, bool) {
	var pid int64
	i := 0
	for ; i < len(name) && name[i] != 0; i++ {
		if name[i] < '0' || name[i] > '9' || pid > math.MaxInt32/10 {
			return 0, false
		}
		pid = pid*10 + int64(name[i]-'0')
	}
	return int32(pid), pid > 0 && pid <= math.MaxInt32
}
