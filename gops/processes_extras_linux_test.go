//go:build linux

package gops

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/gops/mocks"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetProcessExtras_GPUFromCursorClients(t *testing.T) {
	const now = int64(1_755_000_010_000)
	// A nonzero baseline: an unmatched client would rate its whole 6 s as new, giving 60.
	prevCursor := encodeProcessCursor([]processSample{{
		ProcessCursorData: models.ProcessCursorData{PID: 42, Timestamp: now - 10_000},
		gpuOK:             true,
		gpu:               []gpuCursorClient{{PDev: "0000:0c:00.0", ID: 7, Engines: map[string][3]uint64{"gfx": {1_000_000_000, 0, 0}}}},
	}})
	prev := decodeProcessCursor(prevCursor)[42]
	require.NotNil(t, prev)

	key := drmClientKey{PDev: "0000:0c:00.0", ID: 7}
	gpu := procGPU{Readable: true, UsageKnown: true, Clients: drmClientSet{key: {
		Driver: "amdgpu", PDev: key.PDev, ID: key.ID,
		Engines: map[string]drmEngine{"gfx": {BusyNs: 6_000_000_000, HasBusy: true, Capacity: 1}},
	}}}
	cur := &processSample{ProcessCursorData: models.ProcessCursorData{PID: 42, Timestamp: now}, gpuOK: true, gpu: cursorClients(gpu.Clients)}

	var info models.ProcessInfo
	setProcessExtras(&info, prev, cur, gpu)
	require.NotNil(t, info.GPU)
	assert.InDelta(t, 50.0, *info.GPU, 1e-9)
}

func TestProcessExtras_FirstPollNetRateFromBaselineDump(t *testing.T) {
	c, _ := loopbackConn(t)
	inode := socketInode(t, c.(*net.TCPConn))

	self := ownProcess(t)
	g := newProcessTestUtil(t, self)
	var calls uint64
	g.sockDump = func() ([]sockDiagEntry, error) {
		calls++
		return []sockDiagEntry{{
			Inode: inode, Protocol: 6, Remote: netip.MustParseAddrPort("8.8.8.8:443"),
			RxBytes: calls * 1_000_000, TxBytes: calls * 1_000_000, HasTCPInfo: true,
		}}, nil
	}

	res, err := g.GetProcessesWithOptions(ProcessOptions{EnableCPU: true, Net: true})
	require.NoError(t, err)
	row := res.Processes[0]
	require.NotNil(t, row.NetRx)
	assert.Greater(t, *row.NetRx, 0.0, "the baseline dump to the second dump is the first rate")
	assert.Greater(t, *row.NetTx, 0.0)
	assert.EqualValues(t, 2, calls)
	require.NotNil(t, row.Connections)
	assert.Equal(t, 1, *row.Connections)
}

func TestParseProcStat(t *testing.T) {
	st, ok := parseProcStat("1234 (Web Content (x)) S 1 1234 1234 34817 -1 4194560 100 0 0 0 5 3 0 0 20 5 27 0 100 0 0\n")
	require.True(t, ok)
	assert.Equal(t, procStat{PPID: 1, State: "S", Nice: 5, Threads: 27, TTY: 34817, CPUTime: 0.08, Start: 100}, st)

	st, ok = parseProcStat("7 (kworker/0:1) I 2 0 0 0 -1 69238880 0 0 0 0 0 0 0 0 0 -20 1 0 50\n")
	require.True(t, ok)
	assert.Equal(t, procStat{PPID: 2, State: "I", Nice: -20, Threads: 1, Start: 50}, st)

	_, ok = parseProcStat("1 (init) S 0 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0")
	assert.False(t, ok, "starttime missing")
	_, ok = parseProcStat("")
	assert.False(t, ok)
}

func writeProcFixture(t *testing.T, root string, pid int, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, itoa(pid))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
}

func TestProcReader_Basics(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 5000)
	writeProcFixture(t, root, 7, map[string]string{
		"status":  "Name:\tsome-long-daemo\nUmask:\t0022\nState:\tS (sleeping)\nUid:\t0\t1000\t1000\t1000\nVmRSS:\t    2048 kB\nThreads:\t3\n",
		"cmdline": "/usr/libexec/some-long-daemon\x00--flag\x00\x00" + long + "\x00",
		"io":      "rchar: 10\nwchar: 20\nread_bytes: 4096\nwrite_bytes: 8192\ncancelled_write_bytes: 0\n",
	})
	require.NoError(t, os.Symlink("/usr/libexec/some-long-daemon (deleted)", filepath.Join(root, "7", "exe")))
	writeProcFixture(t, root, 8, map[string]string{
		"status":  "Name:\tback\\\\slash\\nnl\nUid:\t1000\t1000\t1000\t1000\n",
		"cmdline": "",
	})

	r := &procReader{root: root, buf: make([]byte, 64)}
	b := r.basics(&process.Process{Pid: 7})
	assert.Equal(t, procBasics{
		Name:     "some-long-daemon",
		Cmdline:  "/usr/libexec/some-long-daemon --flag " + long,
		Exe:      "/usr/libexec/some-long-daemon (deleted)",
		Username: "root",
		RSS:      2048 * 1024,
	}, b, "truncated comm extends from argv[0]; empty arguments dropped; a cmdline past the buffer is read whole")

	read, write, ok := r.diskBytes(&process.Process{Pid: 7})
	assert.True(t, ok)
	assert.Equal(t, [2]uint64{4096, 8192}, [2]uint64{read, write})

	b = r.basics(&process.Process{Pid: 8})
	assert.Equal(t, "back\\slash\nnl", b.Name, "status escapes what comm holds raw")
	assert.Empty(t, b.Cmdline, "kernel threads have none")
	assert.Zero(t, b.RSS)

	assert.Equal(t, procBasics{}, r.basics(&process.Process{Pid: 9}))
	_, _, ok = r.diskBytes(&process.Process{Pid: 9})
	assert.False(t, ok)
}

// netTestUtil serves procs on every call, so one GopsUtil (and its tails)
// can be polled more than once.
func netTestUtil(t *testing.T, dump func() ([]sockDiagEntry, error), procs ...*process.Process) *GopsUtil {
	t.Helper()
	mockProc := mocks.NewMockProcessInfoProvider(t)
	mockProc.EXPECT().Processes().Return(procs, nil)
	mockMem := mocks.NewMockMemoryInfoProvider(t)
	mockMem.EXPECT().VirtualMemory().Return(&mem.VirtualMemoryStat{Total: 16 << 30}, nil)
	g := NewGopsUtilWithProviders(nil, mockMem, nil, nil, mockProc, nil, nil, nil)
	g.sockDump = dump
	t.Cleanup(g.Close)
	return g
}

func loopbackConn(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	client, err = net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	server, err = ln.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { client.Close(); server.Close() })
	return client, server
}

func TestProcessExtras_NetTailCreditedToPreviousHolder(t *testing.T) {
	c, _ := loopbackConn(t)
	inode := socketInode(t, c.(*net.TCPConn))
	remote := netip.MustParseAddrPort("8.8.8.8:443")
	open := true
	g := netTestUtil(t, func() ([]sockDiagEntry, error) {
		if !open {
			return nil, nil
		}
		return []sockDiagEntry{{Inode: inode, Cookie: 77, Protocol: 6, Remote: remote, RxBytes: 1000, TxBytes: 1000, HasTCPInfo: true}}, nil
	}, ownProcess(t))
	var emit func(sockDiagEntry)
	g.sockDestroys = func(e func(sockDiagEntry)) (func(), error) { emit = e; return func() {}, nil }

	opts := ProcessOptions{Net: true}
	first, err := g.GetProcessesWithOptions(opts)
	require.NoError(t, err)
	require.NotNil(t, emit, "process-net starts the listener")

	open = false
	emit(sockDiagEntry{Cookie: 77, Protocol: 6, Remote: remote, RxBytes: 5000, TxBytes: 3000, HasTCPInfo: true})
	time.Sleep(20 * time.Millisecond)
	opts.Cursor = first.Cursor
	res, err := g.GetProcessesWithOptions(opts)
	require.NoError(t, err)
	row := res.Processes[0]
	require.NotNil(t, row.NetRx)
	assert.Positive(t, *row.NetRx, "the bytes after the last dump count for the process that held the socket")
	assert.InDelta(t, 2.0, *row.NetRx / *row.NetTx, 1e-9)
}

func socketInode(t *testing.T, c syscall.Conn) uint32 {
	raw, err := c.SyscallConn()
	require.NoError(t, err)
	var target string
	require.NoError(t, raw.Control(func(fd uintptr) {
		target, err = os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	}))
	require.NoError(t, err)
	var inode uint32
	_, err = fmt.Sscanf(target, "socket:[%d]", &inode)
	require.NoError(t, err)
	return inode
}
