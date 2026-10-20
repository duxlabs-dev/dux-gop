package gops

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/gops/mocks"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProcessTestUtil(t *testing.T, procs ...*process.Process) *GopsUtil {
	t.Helper()

	mockProc := mocks.NewMockProcessInfoProvider(t)
	mockProc.EXPECT().Processes().Return(procs, nil).Once()

	mockMem := mocks.NewMockMemoryInfoProvider(t)
	mockMem.EXPECT().VirtualMemory().Return(&mem.VirtualMemoryStat{Total: 16 << 30}, nil).Once()

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
	t.Cleanup(g.Close)
	return g
}

func TestGetProcessesWithCursor_UsesCursorForCPU(t *testing.T) {
	self, err := process.NewProcess(int32(os.Getpid()))
	require.NoError(t, err)

	times, err := self.Times()
	require.NoError(t, err)

	cursor := encodeProcessCursor([]processSample{{ProcessCursorData: models.ProcessCursorData{
		PID:       self.Pid,
		Ticks:     times.User + times.System - 5.0,
		Timestamp: time.Now().UnixMilli() - 10_000,
	}}})

	util := newProcessTestUtil(t, self)

	res, err := util.GetProcessesWithCursor(SortByCPU, 0, true, cursor, false)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	// 5 CPU-seconds over a 10s interval is half a core, reported as a share of the whole machine.
	expected := 50.0 / float64(runtime.NumCPU())
	assert.InDelta(t, expected, res.Processes[0].CPU, expected/10,
		"CPU must be the rate over the cursor interval, not an average over the process lifetime")
}

func TestGetProcessesWithCursor_ClampsToWholeMachine(t *testing.T) {
	self, err := process.NewProcess(int32(os.Getpid()))
	require.NoError(t, err)

	times, err := self.Times()
	require.NoError(t, err)

	// An implausible amount of CPU over a very short interval, as a stale or
	// hand-crafted cursor would produce.
	cursor := encodeProcessCursor([]processSample{{ProcessCursorData: models.ProcessCursorData{
		PID:       self.Pid,
		Ticks:     times.User + times.System - 1000.0,
		Timestamp: time.Now().UnixMilli() - 10,
	}}})

	util := newProcessTestUtil(t, self)

	res, err := util.GetProcessesWithCursor(SortByCPU, 0, true, cursor, false)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	assert.Equal(t, 100.0, res.Processes[0].CPU,
		"CPU is a share of the whole machine and must never be published above 100")
}

func TestGetProcessesWithCursor_SkipsCursorEntryWhenTimesUnreadable(t *testing.T) {
	util := newProcessTestUtil(t, &process.Process{Pid: 999999})

	res, err := util.GetProcessesWithCursor(SortByCPU, 0, true, "", false)
	require.NoError(t, err)

	assert.Empty(t, decodeProcessCursor(res.Cursor),
		"a process whose CPU times could not be read must not enter the cursor as 0 ticks")
}

func TestGetProcessesWithCursor_CursorCoversAllProcesses(t *testing.T) {
	self, err := process.NewProcess(int32(os.Getpid()))
	require.NoError(t, err)
	parent, err := process.NewProcess(int32(os.Getppid()))
	require.NoError(t, err)

	util := newProcessTestUtil(t, self, parent)

	res, err := util.GetProcessesWithCursor(SortByCPU, 1, false, "", true)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	assert.Len(t, decodeProcessCursor(res.Cursor), 2,
		"cursor must cover every process, not only the merged and limited page, or excluded processes report 0 forever")
}

func TestGetProcessesWithCursor_TimestampsTrackEachRead(t *testing.T) {
	self, err := process.NewProcess(int32(os.Getpid()))
	require.NoError(t, err)

	before := time.Now().UnixMilli()
	util := newProcessTestUtil(t, self)

	res, err := util.GetProcessesWithCursor(SortByCPU, 0, true, "", false)
	require.NoError(t, err)
	after := time.Now().UnixMilli()

	entry, ok := decodeProcessCursor(res.Cursor)[self.Pid]
	require.True(t, ok)

	assert.GreaterOrEqual(t, entry.Timestamp, before+cpuBaselineInterval.Milliseconds(),
		"cursor timestamp must be when times were sampled, not when the request started")
	assert.LessOrEqual(t, entry.Timestamp, after)
}
