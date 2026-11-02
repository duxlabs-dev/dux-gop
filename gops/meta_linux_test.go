//go:build linux

package gops

import (
	"context"
	"net/netip"
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rowFor(procs []*models.ProcessInfo, pid int32) *models.ProcessInfo {
	for _, p := range procs {
		if p.PID == pid {
			return p
		}
	}
	return nil
}

func TestGetMetaProcessModifiers(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, cpuTicks: 1000, readBytes: 4096}))
	ctx := context.Background()
	for _, mods := range [][]string{{"processes", "process-io"}, {"process-io"}} {
		first, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true})
		require.NoError(t, err, "%v", mods)
		require.NotEmpty(t, first.Cursor)
		second, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: rewindCursor(first.Cursor, 100)})
		require.NoError(t, err)
		row := rowFor(second.Processes, 100)
		require.NotNil(t, row, "%v", mods)
		assert.NotNil(t, row.DiskRead, "%v", mods)
	}

	// A plain processes poll, even with an old cursor, carries no module data.
	first, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{})
	require.NoError(t, err)
	second, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{ProcCursor: first.Cursor})
	require.NoError(t, err)
	require.NotEmpty(t, second.Processes)
	for _, p := range second.Processes {
		assert.True(t, p.GPU == nil && p.VRAMKB == nil && p.DiskRead == nil && p.DiskWrite == nil, "extras on %d", p.PID)
	}
}

func TestGetMetaProcessGPUModifier(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100}))
	ctx := context.Background()
	mods := []string{"processes", "process-gpu"}
	first, err := g.GetMeta(ctx, mods, MetaParams{})
	require.NoError(t, err)
	second, err := g.GetMeta(ctx, mods, MetaParams{ProcCursor: rewindCursor(first.Cursor, 100)})
	require.NoError(t, err)
	row := rowFor(second.Processes, 100)
	require.NotNil(t, row)
	require.NotNil(t, row.VRAMKB)
	require.NotNil(t, row.GPU)
	assert.Zero(t, *row.VRAMKB)
	assert.Zero(t, *row.GPU)
}

func TestGetMetaTogglingModifiersKeepsCPURates(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, cpuTicks: 1000}))
	ctx := context.Background()
	first, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	cursor := first.Cursor
	for _, mods := range [][]string{{"processes", "process-io"}, {"processes"}, {"process-io"}} {
		next, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: rewindCursor(cursor, 100)})
		require.NoError(t, err)
		row := rowFor(next.Processes, 100)
		require.NotNil(t, row, "%v", mods)
		assert.Positive(t, row.CPU, "%v: own CPU rate lost", mods)
		cursor = next.Cursor
	}
}

func TestGetMetaProcessNetModifier(t *testing.T) {
	f := newFakeProc(t)
	g := procTestUtil(t, f, f.add(fakeProcess{pid: 100, fds: map[string]string{"3": "socket:[777]"}}))
	g.sockDump = func() ([]sockDiagEntry, error) {
		return []sockDiagEntry{{Inode: 777, Protocol: 6, Remote: netip.MustParseAddrPort("8.8.8.8:443"), HasTCPInfo: true}}, nil
	}
	ctx := context.Background()
	for _, mods := range [][]string{{"process-net"}, {"processes", "process-net"}} {
		meta, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true})
		require.NoError(t, err)
		row := rowFor(meta.Processes, 100)
		require.NotNil(t, row, "%v", mods)
		require.NotNil(t, row.Connections, "%v", mods)
		assert.Equal(t, 1, *row.Connections)
	}

	meta, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	row := rowFor(meta.Processes, 100)
	require.NotNil(t, row)
	assert.Nil(t, row.Connections)
}
