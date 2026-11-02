//go:build linux

package gops

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	// devs holds the same states by device number, from each node's sysfs dev file.
	devs map[uint64]gpuNodeState
	// Bus ids of the enabled, awake NVIDIA GPUs.
	nvidia []string
}

// gpuNodes reads sysfs only. Only gpuNodeRead nodes may have their fdinfo
// read: that runs the driver, which can resume a suspended GPU.
func gpuNodes(pciRoot string, gpus []models.GPU, pciIds []string) gpuNodeSet {
	set := gpuNodeSet{nodes: map[string]gpuNodeState{}, devs: map[uint64]gpuNodeState{}}
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
		drmDir := filepath.Join(pciRoot, g.BusId, "drm")
		entries, _ := os.ReadDir(drmDir)
		for _, e := range entries {
			set.nodes[e.Name()] = state
			if dev, ok := readDevNumber(filepath.Join(drmDir, e.Name(), "dev")); ok {
				set.devs[dev] = state
			}
		}
	}
	return set
}

func readDevNumber(path string) (uint64, bool) {
	major, minor, ok := strings.Cut(readTrimmed(path), ":")
	if !ok {
		return 0, false
	}
	ma, err := strconv.ParseUint(major, 10, 32)
	if err != nil {
		return 0, false
	}
	mi, err := strconv.ParseUint(minor, 10, 32)
	if err != nil {
		return 0, false
	}
	return unix.Mkdev(uint32(ma), uint32(mi)), true
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

// Cached attributes suffice to type an open file; a network mount must not round-trip.
const fdStatxFlags = unix.AT_STATX_DONT_SYNC | unix.AT_NO_AUTOMOUNT

type fdScanner struct {
	scan    procFDScan
	pidDir  string
	dirfd   int
	nodes   gpuNodeSet
	sockets bool
	buf     []byte
}

// scanProcFDs errors only when the fd directory is unreadable, so an unknown process differs from one without matching fds.
func scanProcFDs(procRoot string, pid int32, nodes gpuNodeSet, sockets bool) (procFDScan, error) {
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

	s := fdScanner{pidDir: pidDir, dirfd: int(dir.Fd()), nodes: nodes, sockets: sockets, buf: make([]byte, 64)}
	if sockets {
		if n, err := unix.Readlinkat(s.dirfd, "../ns/net", s.buf); err == nil {
			s.scan.NetNS = string(s.buf[:n])
		}
	}
	for _, name := range names {
		s.classify(name)
	}
	return s.scan, nil
}

// classify types the open file through the fd link; the link text is read only
// when stat fails or a character device is not a known GPU node.
func (self *fdScanner) classify(name string) {
	var stx unix.Statx_t
	if err := unix.Statx(self.dirfd, name, fdStatxFlags, unix.STATX_TYPE|unix.STATX_INO, &stx); err != nil {
		self.classifyLink(name)
		return
	}
	switch stx.Mode & unix.S_IFMT {
	case unix.S_IFSOCK:
		if !self.sockets {
			return
		}
		if stx.Mask&unix.STATX_INO == 0 {
			self.classifyLink(name)
			return
		}
		self.scan.SocketInodes = append(self.scan.SocketInodes, uint32(stx.Ino))
	case unix.S_IFCHR:
		state, known := self.nodes.devs[unix.Mkdev(stx.Rdev_major, stx.Rdev_minor)]
		if !known {
			self.classifyLink(name)
			return
		}
		self.addGPUNode(name, state)
	}
}

func (self *fdScanner) classifyLink(name string) {
	n, err := unix.Readlinkat(self.dirfd, name, self.buf)
	if err != nil {
		return
	}
	target := self.buf[:n]
	switch {
	case self.sockets && bytes.HasPrefix(target, socketPrefix):
		if inode, ok := parseSocketLink(target); ok {
			self.scan.SocketInodes = append(self.scan.SocketInodes, inode)
		}
	case bytes.HasPrefix(target, nvidiaPrefix):
		self.scan.NvidiaGPU = self.scan.NvidiaGPU || isNvidiaGPUDevice(target)
	case bytes.HasPrefix(target, driPrefix):
		self.addGPUNode(name, self.nodes.nodes[string(target[len(driPrefix):])])
	}
}

func (self *fdScanner) addGPUNode(name string, state gpuNodeState) {
	switch state {
	case gpuNodeUnknown:
		self.scan.UnknownGPU = true
	case gpuNodeExcluded:
		self.scan.ExcludedGPU = true
	case gpuNodeRead:
		data, err := os.ReadFile(filepath.Join(self.pidDir, "fdinfo", name))
		if err != nil {
			return
		}
		client, ok := parseDRMFdinfo(string(data))
		if !ok {
			return
		}
		if self.scan.DRM == nil {
			self.scan.DRM = drmClientSet{}
		}
		self.scan.DRM[client.key()] = client
	}
}

func (self processExtras) readFDs(procRoot string, pid int32) (g procGPU, n procNet) {
	sockets := self.net != nil && self.net.ok
	if self.gpu == nil && !sockets {
		return g, n
	}
	var nodes gpuNodeSet
	if self.gpu != nil {
		nodes = self.gpu.gpuNodeSet
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
