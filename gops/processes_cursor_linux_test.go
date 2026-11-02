//go:build linux

package gops

import (
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProcessesWithCursor_UsesCursorForCPU(t *testing.T) {
	f := newFakeProc(t)
	self := f.add(fakeProcess{pid: 100, cpuTicks: 1000})
	cursor := encodeProcessCursor([]processSample{{ProcessCursorData: models.ProcessCursorData{
		PID:       100,
		Ticks:     10 - 5,
		Timestamp: time.Now().UnixMilli() - 10_000,
	}}})

	res, err := procTestUtil(t, f, self).GetProcessesWithCursor(SortByCPU, 0, true, cursor, false)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	// 5 CPU-seconds over a 10s interval is half a core, reported as a share of the 4 CPUs.
	assert.InDelta(t, 12.5, res.Processes[0].CPU, 1.25,
		"CPU must be the rate over the cursor interval, not an average over the process lifetime")
}

func TestGetProcessesWithCursor_ClampsToWholeMachine(t *testing.T) {
	f := newFakeProc(t)
	self := f.add(fakeProcess{pid: 100, cpuTicks: 1000})
	// An implausible amount of CPU over a very short interval, as a stale or
	// hand-crafted cursor would produce.
	cursor := encodeProcessCursor([]processSample{{ProcessCursorData: models.ProcessCursorData{
		PID:       100,
		Ticks:     10 - 1000,
		Timestamp: time.Now().UnixMilli() - 10,
	}}})

	res, err := procTestUtil(t, f, self).GetProcessesWithCursor(SortByCPU, 0, true, cursor, false)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	assert.Equal(t, 100.0, res.Processes[0].CPU,
		"CPU is a share of the whole machine and must never be published above 100")
}

func TestGetProcessesWithCursor_SkipsCursorEntryWhenTimesUnreadable(t *testing.T) {
	util := procTestUtil(t, newFakeProc(t), &process.Process{Pid: 999999})

	res, err := util.GetProcessesWithCursor(SortByCPU, 0, true, "", false)
	require.NoError(t, err)

	assert.Empty(t, decodeProcessCursor(res.Cursor),
		"a process whose CPU times could not be read must not enter the cursor as 0 ticks")
}

func TestGetProcessesWithCursor_CursorCoversAllProcesses(t *testing.T) {
	f := newFakeProc(t)
	parent := f.add(fakeProcess{pid: 100, name: "parent"})
	child := f.add(fakeProcess{pid: 101, ppid: 100, name: "child"})

	res, err := procTestUtil(t, f, parent, child).GetProcessesWithCursor(SortByCPU, 1, false, "", true)
	require.NoError(t, err)
	require.Len(t, res.Processes, 1)

	assert.Len(t, decodeProcessCursor(res.Cursor), 2,
		"cursor must cover every process, not only the merged and limited page, or excluded processes report 0 forever")
}

func TestGetProcessesWithCursor_TimestampsTrackEachRead(t *testing.T) {
	f := newFakeProc(t)
	self := f.add(fakeProcess{pid: 100})

	before := time.Now().UnixMilli()
	res, err := procTestUtil(t, f, self).GetProcessesWithCursor(SortByCPU, 0, true, "", false)
	require.NoError(t, err)
	after := time.Now().UnixMilli()

	entry, ok := decodeProcessCursor(res.Cursor)[100]
	require.True(t, ok)

	assert.GreaterOrEqual(t, entry.Timestamp, before+cpuBaselineInterval.Milliseconds(),
		"cursor timestamp must be when times were sampled, not when the request started")
	assert.LessOrEqual(t, entry.Timestamp, after)
}
