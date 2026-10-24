//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadThermalTemp(t *testing.T) {
	_, _, err := readThermalTemp("/nonexistent/path", "thermal_zone0")
	assert.Error(t, err, "Should error on nonexistent path")
}

func TestReadThermalType(t *testing.T) {
	_, err := readThermalType("/nonexistent/path", "thermal_zone0")
	assert.Error(t, err, "Should error on nonexistent path")
}

func TestGetMaxACPITZTemperature(t *testing.T) {
	result := getMaxACPITZTemperature("/nonexistent", []os.DirEntry{}, 20, 100, true)
	assert.Equal(t, float64(0), result, "Should return 0 for empty entries")
}

func writeTemp(t *testing.T, dir, name, value string) {
	t.Helper()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(value), 0o644))
}

func TestReadCoretempInputPackage(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "temp1_input", "45000\n")
	writeTemp(t, dir, "temp2_input", "73000\n")

	temp, path, ok := readCoretempInput(dir)
	assert.True(t, ok)
	assert.Equal(t, 45.0, temp, "Should prefer the package sensor (temp1_input)")
	assert.Equal(t, filepath.Join(dir, "temp1_input"), path)
}

func TestReadCoretempInputPerCoreFallback(t *testing.T) {
	// Older CPUs (e.g. Nehalem i7 920XM) expose no package sensor, so
	// temp1_input is absent and only per-core inputs exist.
	dir := t.TempDir()
	writeTemp(t, dir, "temp2_input", "72000\n")
	writeTemp(t, dir, "temp3_input", "76500\n")
	writeTemp(t, dir, "temp4_input", "74000\n")

	temp, path, ok := readCoretempInput(dir)
	assert.True(t, ok)
	assert.Equal(t, 76.5, temp, "Should use the hottest core when no package sensor exists")
	assert.Equal(t, filepath.Join(dir, "temp3_input"), path)
}

func TestReadCoretempInputMissing(t *testing.T) {
	_, _, ok := readCoretempInput(t.TempDir())
	assert.False(t, ok, "Should report no reading when no temp inputs exist")
}

func writeZone(t *testing.T, root, zone, zoneType, temp string) {
	t.Helper()
	dir := filepath.Join(root, zone)
	assert.NoError(t, os.MkdirAll(dir, 0o755))
	writeTemp(t, dir, "type", zoneType+"\n")
	writeTemp(t, dir, "temp", temp+"\n")
}

func TestGetMaxACPITZTemperatureZoneTypes(t *testing.T) {
	tests := []struct {
		name     string
		zones    [][3]string
		want     float64
		wantZone string
	}{
		{
			// Snapdragon 8cx Gen 3 (ThinkPad X13s): no ACPI, one zone per core.
			name: "device tree per-core zones",
			zones: [][3]string{
				{"thermal_zone0", "gpu-thermal", "61000"},
				{"thermal_zone1", "cpu0-thermal", "38000"},
				{"thermal_zone5", "cpu4-thermal", "52500"},
				{"thermal_zone11", "qcom-battmgr-bat", "30500"},
			},
			want:     52.5,
			wantZone: "thermal_zone5",
		},
		{
			name: "acpi",
			zones: [][3]string{
				{"thermal_zone0", "acpitz", "47000"},
				{"thermal_zone1", "x86_pkg_temp", "65000"},
			},
			want:     47,
			wantZone: "thermal_zone0",
		},
	}

	t.Cleanup(func() { cpuTracker.tempPath = "" })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for _, z := range tt.zones {
				writeZone(t, root, z[0], z[1], z[2])
			}
			entries, err := os.ReadDir(root)
			assert.NoError(t, err)

			cpuTracker.tempPath = ""
			assert.Equal(t, tt.want, getMaxACPITZTemperature(root, entries, 20, 100, true))
			assert.Equal(t, filepath.Join(root, tt.wantZone, "temp"), cpuTracker.tempPath)
		})
	}
}
