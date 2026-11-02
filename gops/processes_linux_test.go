//go:build linux

package gops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPssDirty(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root, 100, map[string]string{"smaps_rollup": "Rss:    5000 kB\nPss_Dirty:    1234 kB\n"})
	writeProcFixture(t, root, 101, map[string]string{"smaps_rollup": "Rss:    5000 kB\n"})

	kb, err := getPssDirty(root, 100)
	require.NoError(t, err)
	assert.Equal(t, uint64(1234), kb)

	_, err = getPssDirty(root, 101)
	assert.Error(t, err, "no Pss_Dirty line")
	_, err = getPssDirty(root, 102)
	assert.Error(t, err, "no such process")
}
