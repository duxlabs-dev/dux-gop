package gops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMetaModules(t *testing.T) {
	g := NewGopsUtilWithProviders(nil, nil, nil, nil, nil, nil, nil, nil)
	mods, err := g.GetModules()
	require.NoError(t, err)
	assert.Subset(t, mods.Available, []string{"sensors", "memory-modules", "process-gpu", "process-io", "process-net", "process-members"})

	for _, bad := range [][]string{{"nope"}, {"process-io", "nope"}} {
		_, err := g.GetMeta(context.Background(), bad, MetaParams{})
		assert.Error(t, err, "%v", bad)
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
