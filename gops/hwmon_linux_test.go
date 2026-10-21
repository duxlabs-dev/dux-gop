//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHwmonFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

// linkHwmonDevice makes <hwmonDir>/device point at <root>/devices/<rel>.
func linkHwmonDevice(t *testing.T, root, hwmonDir, rel string) {
	t.Helper()
	target := filepath.Join(root, "devices", rel)
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(hwmonDir, "device")))
}

func TestReadHwmonChips_AmdgpuLayout(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon", "hwmon3")
	writeHwmonFiles(t, hw, map[string]string{
		"name":            "amdgpu\n",
		"temp1_input":     "45000\n",
		"temp1_label":     "edge\n",
		"temp1_crit":      "100000\n",
		"temp2_input":     "52000\n",
		"temp2_label":     "junction\n",
		"temp2_crit":      "110000\n",
		"temp3_input":     "60000\n",
		"temp3_label":     "mem\n",
		"fan1_input":      "1200\n",
		"fan1_max":        "3600\n",
		"power1_average":  "85000000\n",
		"power1_cap":      "327000000\n",
		"power1_cap_max":  "400000000\n",
		"freq1_input":     "123\n",
		"temp1_emergency": "1\n",
	})
	linkHwmonDevice(t, root, hw, "pci0000:00/0000:0c:00.0")

	chips := NewGopsUtil().readHwmonChips(filepath.Join(root, "hwmon"), nil)
	require.Len(t, chips, 1)
	chip := chips[0]
	assert.Equal(t, "amdgpu", chip.Name)
	assert.Equal(t, "0000:0c:00.0", chip.Device)
	require.Len(t, chip.Readings, 6)

	byLabel := map[string]hwmonReading{}
	for _, r := range chip.Readings {
		byLabel[r.Label] = r
	}
	edge := byLabel["edge"]
	assert.Equal(t, sensorTemperature, edge.Kind)
	assert.InDelta(t, 45, edge.Value, 1e-9)
	require.NotNil(t, edge.Critical)
	assert.InDelta(t, 100, *edge.Critical, 1e-9)
	assert.Nil(t, edge.Max)

	fan := byLabel["fan1"]
	assert.Equal(t, sensorFan, fan.Kind)
	assert.InDelta(t, 1200, fan.Value, 1e-9)
	require.NotNil(t, fan.Max)
	assert.InDelta(t, 3600, *fan.Max, 1e-9)

	pw := byLabel["power1"]
	assert.Equal(t, sensorPower, pw.Kind)
	assert.InDelta(t, 85, pw.Value, 1e-9)
	require.NotNil(t, pw.Max)
	assert.InDelta(t, 327, *pw.Max, 1e-9)
}

func TestReadHwmonChips_PowerInputPreferredOverAverage(t *testing.T) {
	root := t.TempDir()
	writeHwmonFiles(t, filepath.Join(root, "hwmon0"), map[string]string{
		"name":           "x",
		"power1_input":   "5000000",
		"power1_average": "9000000",
	})
	chips := NewGopsUtil().readHwmonChips(root, nil)
	require.Len(t, chips, 1)
	require.Len(t, chips[0].Readings, 1)
	assert.InDelta(t, 5, chips[0].Readings[0].Value, 1e-9)
}

func TestReadHwmonChips_UnlabeledAndNonNumeric(t *testing.T) {
	root := t.TempDir()
	writeHwmonFiles(t, filepath.Join(root, "hwmon0"), map[string]string{
		"name":        "k10temp",
		"temp1_input": "30500",
		"temp2_input": "garbage",
		"temp3_input": "40000",
		"temp3_label": "Tccd1",
	})
	chips := NewGopsUtil().readHwmonChips(root, nil)
	require.Len(t, chips, 1)
	assert.Empty(t, chips[0].Device)
	require.Len(t, chips[0].Readings, 2)
	assert.Equal(t, "temp1", chips[0].Readings[0].Label)
	assert.Equal(t, "Tccd1", chips[0].Readings[1].Label)
}

func TestReadHwmonChips_SkipsSuspendedDrivetempAndUnnamedGPU(t *testing.T) {
	root := t.TempDir()
	susp := filepath.Join(root, "hwmon0")
	writeHwmonFiles(t, susp, map[string]string{"name": "amdgpu", "temp1_input": "1000"})
	linkHwmonDevice(t, root, susp, "pci/0000:03:00.0")
	writeHwmonFiles(t, filepath.Join(root, "devices", "pci", "0000:03:00.0", "power"), map[string]string{"runtime_status": "suspended\n"})

	writeHwmonFiles(t, filepath.Join(root, "hwmon1"), map[string]string{"name": "drivetemp", "temp1_input": "1000"})
	writeHwmonFiles(t, filepath.Join(root, "hwmon2"), map[string]string{"name": "nvme", "temp1_input": "1000"})

	awake := filepath.Join(root, "hwmon3")
	writeHwmonFiles(t, awake, map[string]string{"name": "amdgpu", "temp1_input": "1000"})
	linkHwmonDevice(t, root, awake, "pci/0000:0c:00.0")

	chips := NewGopsUtil().readHwmonChips(root, map[string]bool{"0000:0c:00.0": true})
	require.Len(t, chips, 1)
	assert.Equal(t, "nvme", chips[0].Name)
}

func TestReadHwmonChip_I2CNeverBlocksCaller(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon0")
	writeHwmonFiles(t, hw, map[string]string{"name": "jc42"})
	linkHwmonDevice(t, root, hw, "pci/i2c-10/10-0018")

	release := make(chan struct{})
	var calls atomic.Int32
	g := NewGopsUtil()
	g.slowChipRead = func(string) []hwmonReading {
		calls.Add(1)
		<-release
		return []hwmonReading{{Kind: sensorTemperature, Label: "temp1", Value: 33}}
	}
	g.slowChipWait = 20 * time.Millisecond
	t.Cleanup(func() {
		hwmonMu.Lock()
		delete(hwmonCache, hw)
		hwmonMu.Unlock()
	})

	start := time.Now()
	chip, ok := g.readHwmonChip(hw)
	require.True(t, ok)
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, "10-0018", chip.Device)
	assert.Empty(t, chip.Readings)
	g.readHwmonChip(hw)
	assert.Equal(t, int32(1), calls.Load())

	close(release)
	require.Eventually(t, func() bool {
		c, _ := g.readHwmonChip(hw)
		return len(c.Readings) == 1 && c.Readings[0].Value == 33
	}, 2*time.Second, 10*time.Millisecond)
}

func TestGetSensors_Live(t *testing.T) {
	entries, _ := os.ReadDir(hwmonRoot)
	if len(entries) == 0 {
		t.Skip("no hwmon chips")
	}
	info, err := NewGopsUtil().GetSensors(nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	for _, s := range info.Sensors {
		assert.NotEmpty(t, s.Chip)
		assert.NotEmpty(t, s.Kind)
		assert.NotEmpty(t, s.Label)
	}
}

func TestReadHwmonChips_LimitsAndFreq(t *testing.T) {
	root := t.TempDir()
	writeHwmonFiles(t, filepath.Join(root, "hwmon0"), map[string]string{
		"name":        "mix",
		"temp1_input": "40000", "temp1_max": "85000", "temp1_crit": "100000",
		"temp2_input": "30000", "temp2_max": "0", "temp2_crit": "0",
		"temp3_input": "30000", "temp3_max": "65261850",
		"fan1_input": "100", "fan1_max": "0",
		"power1_input": "1000000", "power1_cap": "0", "power1_max": "50000000",
		"freq1_input": "2500000000", "freq1_label": "sclk",
	})
	chips := NewGopsUtil().readHwmonChips(root, nil)
	require.Len(t, chips, 1)
	by := map[string]hwmonReading{}
	for _, r := range chips[0].Readings {
		by[r.Label] = r
	}
	assert.InDelta(t, 85, *by["temp1"].Max, 1e-9)
	assert.InDelta(t, 100, *by["temp1"].Critical, 1e-9)
	assert.Nil(t, by["temp2"].Max)
	assert.Nil(t, by["temp2"].Critical)
	assert.Nil(t, by["temp3"].Max)
	assert.Nil(t, by["fan1"].Max)
	assert.InDelta(t, 50, *by["power1"].Max, 1e-9)
	assert.Equal(t, sensorFrequency, by["sclk"].Kind)
	assert.InDelta(t, 2500, by["sclk"].Value, 1e-9)

	for _, s := range sensorsFromChips(chips).Sensors {
		assert.NotEqual(t, sensorFrequency, s.Kind)
	}
}

func TestReadHwmonChip_SuspendedIsSkipped(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon0")
	writeHwmonFiles(t, hw, map[string]string{"name": "amdgpu", "temp1_input": "1000"})
	linkHwmonDevice(t, root, hw, "pci/0000:03:00.0")
	writeHwmonFiles(t, filepath.Join(root, "devices", "pci", "0000:03:00.0", "power"), map[string]string{"runtime_status": "suspended"})
	_, ok := NewGopsUtil().readHwmonChip(hw)
	assert.False(t, ok)

	_, ok = NewGopsUtil().readHwmonChip(filepath.Join(root, "missing"))
	assert.False(t, ok)
}

func TestReadHwmonChips_EvictsStaleI2CCache(t *testing.T) {
	root := t.TempDir()
	hwmonMu.Lock()
	hwmonCache[filepath.Join(root, "gone")] = &hwmonState{}
	hwmonMu.Unlock()
	writeHwmonFiles(t, filepath.Join(root, "hwmon0"), map[string]string{"name": "x"})
	NewGopsUtil().readHwmonChips(root, nil)
	hwmonMu.Lock()
	defer hwmonMu.Unlock()
	assert.NotContains(t, hwmonCache, filepath.Join(root, "gone"))
}

func TestReadHwmonChip_StaticFilesCachedPerChip(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon0")
	writeHwmonFiles(t, hw, map[string]string{
		"name": "k10temp", "temp1_input": "40000", "temp1_label": "Tctl", "temp1_crit": "95000",
		"power1_input": "1000000", "power1_cap": "50000000",
	})
	t.Cleanup(func() {
		hwmonMu.Lock()
		delete(hwmonCache, hw)
		hwmonMu.Unlock()
	})
	g := NewGopsUtil()
	g.readHwmonChip(hw)

	writeHwmonFiles(t, hw, map[string]string{"temp1_input": "41000", "temp1_label": "renamed", "temp1_crit": "1000", "power1_cap": "60000000"})
	chip, _ := g.readHwmonChip(hw)
	require.Len(t, chip.Readings, 2)
	assert.InDelta(t, 60, *chip.Readings[0].Max, 1e-9, "power caps change at runtime and are re-read")
	temp := chip.Readings[1]
	assert.InDelta(t, 41, temp.Value, 1e-9)
	assert.Equal(t, "Tctl", temp.Label, "labels and limits are read once")
	assert.InDelta(t, 95, *temp.Critical, 1e-9)

	writeHwmonFiles(t, hw, map[string]string{"name": "nct6798"})
	chip, _ = g.readHwmonChip(hw)
	assert.Equal(t, "renamed", chip.Readings[1].Label, "another chip in the directory starts over")
}

func TestReadHwmonChip_SlowChipReturnsLastReadings(t *testing.T) {
	root := t.TempDir()
	hw := filepath.Join(root, "hwmon0")
	writeHwmonFiles(t, hw, map[string]string{"name": "nvme", "temp1_input": "40000"})
	t.Cleanup(func() {
		hwmonMu.Lock()
		delete(hwmonCache, hw)
		hwmonMu.Unlock()
	})
	g := NewGopsUtil()
	g.readHwmonChip(hw)
	hwmonMu.Lock()
	st := hwmonCache[hw]
	st.async, st.haveRead, st.readings = true, true, []hwmonReading{{Kind: sensorTemperature, Label: "temp1", Value: 40}}
	hwmonMu.Unlock()

	release := make(chan struct{})
	g.slowChipRead = func(string) []hwmonReading {
		<-release
		return []hwmonReading{{Kind: sensorTemperature, Label: "temp1", Value: 50}}
	}
	g.slowChipWait = time.Hour

	start := time.Now()
	chip, _ := g.readHwmonChip(hw)
	assert.Less(t, time.Since(start), time.Second, "a chip with readings never waits")
	assert.InDelta(t, 40, chip.Readings[0].Value, 1e-9)

	close(release)
	require.Eventually(t, func() bool {
		c, _ := g.readHwmonChip(hw)
		return c.Readings[0].Value == 50
	}, 2*time.Second, 10*time.Millisecond)
}
