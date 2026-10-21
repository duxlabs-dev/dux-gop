package gops

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ownRow(procs []*models.ProcessInfo) *models.ProcessInfo {
	for _, p := range procs {
		if p.PID == int32(os.Getpid()) {
			return p
		}
	}
	return nil
}

func TestGetMetaModules(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()

	mods, err := g.GetModules()
	require.NoError(t, err)
	assert.Subset(t, mods.Available, []string{"sensors", "memory-modules", "process-gpu", "process-io", "process-net"})

	for _, bad := range [][]string{{"nope"}, {"process-io", "nope"}} {
		_, err := g.GetMeta(ctx, bad, MetaParams{})
		assert.Error(t, err, "%v", bad)
	}

	for _, modules := range [][]string{{"sensors", "memory-modules"}, {"all"}} {
		_, err := g.GetMeta(ctx, modules, MetaParams{NetRateAllInterfaces: true})
		assert.NoError(t, err, "%v", modules)
	}
}

func TestGetMetaProcessModifiers(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()
	for _, mods := range [][]string{{"processes", "process-io"}, {"process-io"}, {"all", "process-io"}} {
		first, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true})
		require.NoError(t, err, "%v", mods)
		require.NotEmpty(t, first.Cursor)
		time.Sleep(100 * time.Millisecond)
		second, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: first.Cursor})
		require.NoError(t, err)
		row := ownRow(second.Processes)
		require.NotNil(t, row, "%v", mods)
		assert.NotNil(t, row.DiskRead, "%v", mods)
	}

	// A plain processes poll, even with an old cursor, carries no module data.
	first, _ := g.GetMeta(ctx, []string{"processes"}, MetaParams{})
	second, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{ProcCursor: first.Cursor})
	require.NoError(t, err)
	require.NotEmpty(t, second.Processes)
	for _, p := range second.Processes {
		assert.True(t, p.GPU == nil && p.VRAMKB == nil && p.DiskRead == nil && p.DiskWrite == nil, "extras on %d", p.PID)
	}
}

func TestGetMetaAllProcessGPU(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("per-process GPU is Linux-only")
	}
	g := NewGopsUtil()
	g.detectGPUEntries = func() ([]gpuEntry, error) { return nil, nil }
	ctx := context.Background()
	first, err := g.GetMeta(ctx, []string{"all", "process-gpu"}, MetaParams{})
	require.NoError(t, err)
	second, err := g.GetMeta(ctx, []string{"all", "process-gpu"}, MetaParams{ProcCursor: first.Cursor})
	require.NoError(t, err)
	row := ownRow(second.Processes)
	require.NotNil(t, row)
	require.NotNil(t, row.VRAMKB)
	require.NotNil(t, row.GPU)
	assert.Zero(t, *row.VRAMKB)
	assert.Zero(t, *row.GPU)
}

func TestGetMetaTogglingModifiersKeepsCPURates(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()
	own := int32(os.Getpid())
	// Moving the own baseline back one CPU second makes every rate positive.
	rewind := func(cursor string) string {
		var entries []processSample
		for _, e := range decodeProcessCursor(cursor) {
			if e.PID == own {
				e.Ticks--
			}
			entries = append(entries, *e)
		}
		return encodeProcessCursor(entries)
	}
	first, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	cursor := first.Cursor
	for _, mods := range [][]string{{"processes", "process-io"}, {"processes"}, {"process-io"}} {
		next, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: rewind(cursor)})
		require.NoError(t, err)
		row := ownRow(next.Processes)
		require.NotNil(t, row, "%v", mods)
		assert.Positive(t, row.CPU, "%v: own CPU rate lost", mods)
		cursor = next.Cursor
	}
}

func TestMetaParamsProcessOptions(t *testing.T) {
	opts, modifier := MetaParams{}.processOptions([]string{"process-net"})
	assert.True(t, opts.Net && modifier)
	assert.False(t, opts.IO || opts.GPU)
	opts, _ = MetaParams{}.processOptions([]string{"processes"})
	assert.False(t, opts.Net)

	// Omitted ids stay nil and an empty list stays non-nil: neither reads a GPU.
	opts, _ = MetaParams{}.processOptions(nil)
	assert.Nil(t, opts.GPUPciIds)
	opts, _ = MetaParams{GPUPciIds: []string{}}.processOptions(nil)
	assert.NotNil(t, opts.GPUPciIds)
	assert.Empty(t, opts.GPUPciIds)
	opts, _ = MetaParams{GPUPciIds: []string{"10de:2684"}}.processOptions(nil)
	assert.Equal(t, []string{"10de:2684"}, opts.GPUPciIds)
}
