//go:build integration && linux

// End-to-end checks against the live host: go test -tags integration ./...
package gops

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ownRow(procs []*models.ProcessInfo) *models.ProcessInfo {
	return rowFor(procs, int32(os.Getpid()))
}

func TestIntegrationGetMetaAll(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()
	for _, modules := range [][]string{{"sensors", "memory-modules"}, {"all"}} {
		_, err := g.GetMeta(ctx, modules, MetaParams{NetRateAllInterfaces: true})
		assert.NoError(t, err, "%v", modules)
	}

	mods := []string{"all", "process-io"}
	first, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	require.NotEmpty(t, first.Cursor)
	time.Sleep(100 * time.Millisecond)
	second, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: first.Cursor})
	require.NoError(t, err)
	row := ownRow(second.Processes)
	require.NotNil(t, row)
	assert.NotNil(t, row.DiskRead)
}

func TestIntegrationGetMetaAllProcessGPU(t *testing.T) {
	g := NewGopsUtil()
	g.detectGPUEntries = func() ([]gpuEntry, error) { return nil, nil }
	ctx := context.Background()
	mods := []string{"all", "process-gpu"}
	first, err := g.GetMeta(ctx, mods, MetaParams{})
	require.NoError(t, err)
	second, err := g.GetMeta(ctx, mods, MetaParams{ProcCursor: first.Cursor})
	require.NoError(t, err)
	row := ownRow(second.Processes)
	require.NotNil(t, row)
	require.NotNil(t, row.VRAMKB)
	require.NotNil(t, row.GPU)
	assert.Zero(t, *row.VRAMKB)
	assert.Zero(t, *row.GPU)
}

func TestIntegrationGetMetaAllProcessNet(t *testing.T) {
	if _, err := dumpSockets(); err != nil {
		t.Skip("sock_diag unavailable")
	}
	g := NewGopsUtil()
	meta, err := g.GetMeta(context.Background(), []string{"all", "process-net"}, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	row := ownRow(meta.Processes)
	require.NotNil(t, row)
	assert.NotNil(t, row.Connections)
}
