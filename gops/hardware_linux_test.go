//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDistroName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "os-release")
	require.NoError(t, os.WriteFile(path, []byte("NAME=\"Arch Linux\"\nPRETTY_NAME=\"Arch Linux\"\nID=arch\n"), 0o644))
	assert.Equal(t, "Arch Linux", distroName(path))

	require.NoError(t, os.WriteFile(path, []byte("ID=arch\n"), 0o644))
	assert.Equal(t, "Unknown", distroName(path), "no PRETTY_NAME")
	assert.Equal(t, "Unknown", distroName(filepath.Join(t.TempDir(), "missing")))
}
