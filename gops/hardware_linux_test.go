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

func TestGPUNameSources(t *testing.T) {
	dir := t.TempDir()
	ids := filepath.Join(dir, "amdgpu.ids")
	require.NoError(t, os.WriteFile(ids, []byte("# comment\n\n1.0.0\n744C,\tC8,\tAMD Radeon RX 7900 XTX\n744C,\tCC,\tAMD Radeon RX 7900 XT\n"), 0o644))
	assert.Equal(t, "AMD Radeon RX 7900 XT", lookupAMDGPUName(ids, "744c", "0xcc"))
	assert.Empty(t, lookupAMDGPUName(ids, "744c", "0xc0"), "unknown revision")
	assert.Empty(t, lookupAMDGPUName(filepath.Join(dir, "missing"), "744c", "0xc8"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "+pci:0000:00:02.0"), []byte("I:1\nE:ID_VENDOR_FROM_DATABASE=Intel Corporation\nE:ID_MODEL_FROM_DATABASE=Kaby Lake-H GT2 [HD Graphics 630]\n"), 0o644))
	assert.Equal(t, "Kaby Lake-H GT2 [HD Graphics 630]", lookupUdevModel(dir, "0000:00:02.0"))
	assert.Empty(t, lookupUdevModel(dir, "0000:00:03.0"))
}
