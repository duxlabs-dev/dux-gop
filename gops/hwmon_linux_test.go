//go:build linux

package gops

import (
	"fmt"
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

func TestReadHwmonChips_Parsing(t *testing.T) {
	summary := func(r hwmonReading) string {
		out := fmt.Sprintf("%s/%s=%g", r.Label, r.Kind, r.Value)
		if r.Max != nil {
			out += fmt.Sprintf(" max=%g", *r.Max)
		}
		if r.Critical != nil {
			out += fmt.Sprintf(" crit=%g", *r.Critical)
		}
		return out
	}
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"amdgpu units, labels and limits", map[string]string{
			"name": "amdgpu", "temp1_input": "45000", "temp1_label": "edge", "temp1_crit": "100000",
			"fan1_input": "1200", "fan1_max": "3600", "power1_average": "85000000", "power1_cap": "327000000",
			"freq1_input": "123", "temp1_emergency": "1",
		}, []string{"edge/temperature=45 crit=100", "fan1/fan=1200 max=3600", "power1/power=85 max=327", "freq1/frequency=0.000123"}},
		{"power input wins over average", map[string]string{
			"name": "x", "power1_input": "5000000", "power1_average": "9000000",
		}, []string{"power1/power=5"}},
		{"unlabeled readings get a default label and garbage is dropped", map[string]string{
			"name": "k10temp", "temp1_input": "30500", "temp2_input": "garbage", "temp3_input": "40000", "temp3_label": "Tccd1",
		}, []string{"temp1/temperature=30.5", "Tccd1/temperature=40"}},
		{"zero and absurd limits are not limits", map[string]string{
			"name": "mix", "temp1_input": "40000", "temp1_max": "85000", "temp2_input": "30000", "temp2_max": "0", "temp2_crit": "0",
			"temp3_input": "30000", "temp3_max": "65261850", "fan1_input": "100", "fan1_max": "0",
		}, []string{"temp1/temperature=40 max=85", "temp2/temperature=30", "temp3/temperature=30", "fan1/fan=100"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeHwmonFiles(t, filepath.Join(root, "hwmon0"), tt.files)
			chips := NewGopsUtil().readHwmonChips(root, nil)
			require.Len(t, chips, 1)
			var got []string
			for _, r := range chips[0].Readings {
				got = append(got, summary(r))
			}
			assert.ElementsMatch(t, tt.want, got)
		})
	}
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
