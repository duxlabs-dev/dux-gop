package gops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMetaProcessNetModifier(t *testing.T) {
	if _, err := dumpSockets(); err != nil {
		t.Skip("sock_diag unavailable")
	}
	g := NewGopsUtil()
	ctx := context.Background()
	for _, mods := range [][]string{{"process-net"}, {"processes", "process-net"}, {"all", "process-net"}} {
		meta, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true})
		require.NoError(t, err)
		row := ownRow(meta.Processes)
		require.NotNil(t, row, "%v", mods)
		assert.NotNil(t, row.Connections, "%v", mods)
	}

	meta, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{EnableCPU: true})
	require.NoError(t, err)
	row := ownRow(meta.Processes)
	require.NotNil(t, row)
	assert.Nil(t, row.Connections)
}
