//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dmiFixture = `E:MEMORY_DEVICE_0_PRESENT=0
E:MEMORY_DEVICE_0_FORM_FACTOR=Unknown
E:MEMORY_DEVICE_0_LOCATOR=DIMM_A1
E:MEMORY_DEVICE_0_TYPE=Unknown
E:MEMORY_DEVICE_1_SIZE=17179869184
E:MEMORY_DEVICE_1_FORM_FACTOR=DIMM
E:MEMORY_DEVICE_1_LOCATOR=DIMM_A2
E:MEMORY_DEVICE_1_TYPE=DDR4
E:MEMORY_DEVICE_1_SPEED_MTS=2197
E:MEMORY_DEVICE_1_MANUFACTURER=G Skill Intl
E:MEMORY_DEVICE_1_ASSET_TAG=Not Specified
E:MEMORY_DEVICE_1_PART_NUMBER=F4-4000C16-16GTZR
E:MEMORY_DEVICE_1_CONFIGURED_SPEED_MTS=2197
E:MEMORY_DEVICE_2_PRESENT=0
E:MEMORY_DEVICE_2_LOCATOR=DIMM_B1
E:MEMORY_DEVICE_3_SIZE=17179869184
E:MEMORY_DEVICE_3_FORM_FACTOR=DIMM
E:MEMORY_DEVICE_3_LOCATOR=DIMM_B2
E:MEMORY_DEVICE_3_TYPE=DDR4
E:MEMORY_DEVICE_3_SPEED_MTS=2197
E:MEMORY_DEVICE_3_MANUFACTURER=Undefined
E:MEMORY_DEVICE_3_PART_NUMBER=Unknown
E:MEMORY_DEVICE_3_CONFIGURED_SPEED_MTS=2133
E:MEMORY_ARRAY_LOCATION=Motherboard
`

func TestParseMemoryModules(t *testing.T) {
	info := parseMemoryModules(dmiFixture)
	assert.Equal(t, 4, info.Slots)
	require.Len(t, info.Modules, 2)
	assert.Equal(t, models.MemoryModule{
		Locator: "DIMM_A2", Size: 16 << 20, Type: "DDR4", Speed: 2197, ConfiguredSpeed: 2197,
		Manufacturer: "G Skill Intl", PartNumber: "F4-4000C16-16GTZR", FormFactor: "DIMM",
	}, info.Modules[0])
	assert.Equal(t, models.MemoryModule{
		Locator: "DIMM_B2", Size: 16 << 20, Type: "DDR4", Speed: 2197, ConfiguredSpeed: 2133, FormFactor: "DIMM",
	}, info.Modules[1])
}

func TestParseMemoryModulesUdevadmFormat(t *testing.T) {
	plain := strings.ReplaceAll(dmiFixture, "E:", "")
	assert.Equal(t, parseMemoryModules(dmiFixture), parseMemoryModules(plain))
}

func TestParseMemoryModulesEmpty(t *testing.T) {
	info := parseMemoryModules("")
	assert.Equal(t, 0, info.Slots)
	assert.Empty(t, info.Modules)
}

func TestReadZramStats(t *testing.T) {
	dir := t.TempDir()
	for name, stat := range map[string]string{
		"zram0": "2097152 1048576 1100000 0 0 0 0 0\n",
		"zram1": "1048576 524288 600000 0 0 0 0 0\n",
		"sda":   "999999999 999999999\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "mm_stat"), []byte(stat), 0o644))
	}
	orig, compr := readZramStats(dir)
	assert.Equal(t, uint64(3072), orig)
	assert.Equal(t, uint64(1536), compr)

	orig, compr = readZramStats(t.TempDir())
	assert.Zero(t, orig)
	assert.Zero(t, compr)
}

func TestReadMemoryModulesFailsWithoutAnySource(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := readMemoryModules(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err, "no udev data and no udevadm is an error, not an empty list")

	path := filepath.Join(t.TempDir(), "dmi")
	require.NoError(t, os.WriteFile(path, []byte(dmiFixture), 0o644))
	info, err := readMemoryModules(path)
	require.NoError(t, err)
	assert.NotEmpty(t, info.Modules)
}

func TestGetMemoryModules_ReadOnce(t *testing.T) {
	g := NewGopsUtil()
	g.memModules.read = true
	g.memModules.info = &models.MemoryModulesInfo{Slots: 4, Modules: []models.MemoryModule{{Locator: "DIMM_A1"}}}

	first, err := g.GetMemoryModules()
	require.NoError(t, err)
	assert.Equal(t, 4, first.Slots, "a cached result is never re-read")
	first.Modules[0].Locator = "changed"

	second, err := g.GetMemoryModules()
	require.NoError(t, err)
	assert.Equal(t, "DIMM_A1", second.Modules[0].Locator, "callers get their own copy")
}

func TestGetMemoryModules_FailureIsCached(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "dmi")
	old := dmiUdevData
	dmiUdevData = path
	t.Cleanup(func() { dmiUdevData = old })

	g := NewGopsUtil()
	_, err := g.GetMemoryModules()
	require.Error(t, err)

	require.NoError(t, os.WriteFile(path, []byte(dmiFixture), 0o644))
	_, err = g.GetMemoryModules()
	assert.Error(t, err, "a failed read is not retried")
}
