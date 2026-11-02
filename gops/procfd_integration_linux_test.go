//go:build integration && linux

package gops

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func linkedSocketInode(t *testing.T, c syscall.Conn) uint32 {
	t.Helper()
	raw, err := c.SyscallConn()
	require.NoError(t, err)
	var target string
	require.NoError(t, raw.Control(func(fd uintptr) {
		target, err = os.Readlink(filepath.Join("/proc/self/fd", strconv.Itoa(int(fd))))
	}))
	require.NoError(t, err)
	inode, ok := parseSocketLink([]byte(target))
	require.True(t, ok, target)
	return inode
}

func TestScanProcFDs_TypesOpenFilesByStat(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	inode := linkedSocketInode(t, l.(*net.TCPListener))
	null, err := os.Open("/dev/null")
	require.NoError(t, err)
	defer null.Close()

	scan, err := scanProcFDs("/proc", int32(os.Getpid()), gpuNodeSet{}, true)
	require.NoError(t, err)
	assert.Contains(t, scan.SocketInodes, inode, "the socket inode comes from stat, matching the link text")
	assert.False(t, scan.UnknownGPU, "a character device that is no DRM node is not a GPU")
	assert.False(t, scan.NvidiaGPU)

	entries, _ := os.ReadDir("/dev/dri")
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "renderD") {
			continue
		}
		node, err := os.Open(filepath.Join("/dev/dri", e.Name()))
		if err != nil {
			continue
		}
		defer node.Close()
		dev, ok := readDevNumber(filepath.Join("/sys/class/drm", e.Name(), "dev"))
		require.True(t, ok)

		scan, err = scanProcFDs("/proc", int32(os.Getpid()), gpuNodeSet{devs: map[uint64]gpuNodeState{dev: gpuNodeExcluded}}, false)
		require.NoError(t, err)
		assert.True(t, scan.ExcludedGPU, "a DRM node is matched by device number")
		assert.False(t, scan.UnknownGPU)

		scan, err = scanProcFDs("/proc", int32(os.Getpid()), gpuNodeSet{}, false)
		require.NoError(t, err)
		assert.True(t, scan.UnknownGPU, "an unlisted device number falls back to the link name")
		return
	}
	t.Log("no openable DRM render node; device-number match not exercised")
}
