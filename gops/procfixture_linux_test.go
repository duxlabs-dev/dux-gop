//go:build linux

package gops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/duxlabs-dev/dux-gop/gops/mocks"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/require"
)

// fakeProc is a /proc tree of synthetic processes.
type fakeProc struct {
	t    *testing.T
	root string
}

type fakeProcess struct {
	pid, ppid  int
	name       string
	cpuTicks   uint64
	start      uint64
	rssKB      uint64
	readBytes  uint64
	writeBytes uint64
	// fds maps fd names to link targets, such as "3": "socket:[777]".
	fds map[string]string
}

func newFakeProc(t *testing.T) *fakeProc { return &fakeProc{t: t, root: t.TempDir()} }

func (self *fakeProc) add(p fakeProcess) *process.Process {
	self.t.Helper()
	if p.name == "" {
		p.name = "proc" + itoa(p.pid)
	}
	exe := "/usr/bin/" + p.name
	dir := filepath.Join(self.root, itoa(p.pid))
	require.NoError(self.t, os.MkdirAll(filepath.Join(dir, "fd"), 0o755))
	writeProcFixture(self.t, self.root, p.pid, map[string]string{
		"stat":    fmt.Sprintf("%d (%s) S %d %d %d 0 -1 4194560 0 0 0 0 %d 0 0 0 20 0 1 0 %d 0 0\n", p.pid, p.name, p.ppid, p.pid, p.pid, p.cpuTicks, p.start),
		"status":  fmt.Sprintf("Name:\t%s\nState:\tS (sleeping)\nUid:\t1000\t1000\t1000\t1000\nVmRSS:\t%d kB\nThreads:\t1\n", p.name, p.rssKB),
		"cmdline": exe + "\x00",
		"io":      fmt.Sprintf("rchar: 0\nwchar: 0\nread_bytes: %d\nwrite_bytes: %d\n", p.readBytes, p.writeBytes),
	})
	require.NoError(self.t, os.Symlink(exe, filepath.Join(dir, "exe")))
	for name, target := range p.fds {
		require.NoError(self.t, os.Symlink(target, filepath.Join(dir, "fd", name)))
	}
	return &process.Process{Pid: int32(p.pid)}
}

func writeProcFixture(t *testing.T, root string, pid int, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, itoa(pid))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
}

// procTestUtil lists procs from f on every call, with every other source stubbed.
func procTestUtil(t *testing.T, f *fakeProc, procs ...*process.Process) *GopsUtil {
	t.Helper()
	mockProc := mocks.NewMockProcessInfoProvider(t)
	mockProc.EXPECT().Processes().Return(procs, nil)
	mockMem := mocks.NewMockMemoryInfoProvider(t)
	mockMem.EXPECT().VirtualMemory().Return(&mem.VirtualMemoryStat{Total: 16 << 30}, nil)
	g := NewGopsUtilWithProviders(
		mocks.NewMockCPUInfoProvider(t),
		mockMem,
		mocks.NewMockDiskInfoProvider(t),
		mocks.NewMockNetworkInfoProvider(t),
		mockProc,
		mocks.NewMockHostInfoProvider(t),
		mocks.NewMockLoadInfoProvider(t),
		mocks.NewMockFileSystem(t),
	)
	g.procRoot = f.root
	g.numCPU = 4
	g.detectGPUEntries = func() ([]gpuEntry, error) { return nil, nil }
	g.sockDump = func() ([]sockDiagEntry, error) { return nil, nil }
	g.sockDestroys = func(func(sockDiagEntry)) (func(), error) { return func() {}, nil }
	g.netNS = func() string { return "" }
	t.Cleanup(g.Close)
	return g
}

// rewindCursor moves pid's sample back one CPU second and one wall second,
// so the next poll rates it without waiting.
func rewindCursor(cursor string, pid int32) string {
	var entries []processSample
	for _, e := range decodeProcessCursor(cursor) {
		if e.PID == pid {
			e.Ticks--
			e.Timestamp -= 1000
		}
		entries = append(entries, *e)
	}
	return encodeProcessCursor(entries)
}
