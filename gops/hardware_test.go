package gops

import (
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferVendorFromId(t *testing.T) {
	tests := []struct {
		name     string
		vendorId string
		driver   string
		expected string
	}{
		{"NVIDIA vendor ID", "10de", "", "NVIDIA"},
		{"AMD vendor ID", "1002", "", "AMD"},
		{"Intel vendor ID", "8086", "", "Intel"},
		{"Unknown with nvidia driver", "1234", "nvidia", "NVIDIA"},
		{"Unknown with amdgpu driver", "5678", "amdgpu", "AMD"},
		{"Unknown with i915 driver", "9abc", "i915", "Intel"},
		{"Completely unknown", "ffff", "unknown", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferVendorFromId(tt.vendorId, tt.driver)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferVendor(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		line     string
		expected string
	}{
		{"nvidia driver", "nvidia", "", "NVIDIA"},
		{"nouveau driver", "nouveau", "", "NVIDIA"},
		{"amdgpu driver", "amdgpu", "", "AMD"},
		{"radeon driver", "radeon", "", "AMD"},
		{"i915 driver", "i915", "", "Intel"},
		{"xe driver", "xe", "", "Intel"},
		{"nvidia in line", "", "NVIDIA GeForce GTX 1080", "NVIDIA"},
		{"amd in line", "", "AMD Radeon RX 5700", "AMD"},
		{"ati in line", "", "ATI Radeon HD 5870", "AMD"},
		{"intel in line", "", "Intel UHD Graphics 630", "Intel"},
		{"unknown", "", "Some Random GPU", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferVendor(tt.driver, tt.line)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseGPUInfo(t *testing.T) {
	tests := []struct {
		name                string
		rawLine             string
		expectedDisplayName string
		expectedPciId       string
	}{
		{
			name:                "NVIDIA GPU",
			rawLine:             "0000:01:00.0 Display controller: NVIDIA Corporation TU106 [GeForce RTX 2070] [10de:1f02]",
			expectedDisplayName: "NVIDIA Corporation TU106 [GeForce RTX 2070]",
			expectedPciId:       "10de:1f02",
		},
		{
			name:                "AMD GPU",
			rawLine:             "0000:03:00.0 VGA compatible controller: AMD/ATI Navi 10 [Radeon RX 5700] [1002:731f]",
			expectedDisplayName: "AMD/ATI Navi 10 [Radeon RX 5700]",
			expectedPciId:       "1002:731f",
		},
		{
			name:                "Intel GPU",
			rawLine:             "0000:00:02.0 VGA compatible controller: Intel Corporation UHD Graphics 630 [8086:3e9b]",
			expectedDisplayName: "Intel Corporation UHD Graphics 630",
			expectedPciId:       "8086:3e9b",
		},
		{
			name:                "empty line",
			rawLine:             "",
			expectedDisplayName: "Unknown",
			expectedPciId:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			displayName, pciId := parseGPUInfo(tt.rawLine)
			assert.Equal(t, tt.expectedPciId, pciId)
			if tt.rawLine != "" {
				assert.NotEmpty(t, displayName)
			} else {
				assert.Equal(t, "Unknown", displayName)
			}
		})
	}
}

func TestRemoveVendorPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"NVIDIA Corporation", "NVIDIA Corporation GeForce RTX 3080", "GeForce RTX 3080"},
		{"NVIDIA only", "NVIDIA GeForce GTX 1080", "GeForce GTX 1080"},
		{"AMD full", "Advanced Micro Devices, Inc. Radeon RX 5700", "Radeon RX 5700"},
		{"AMD/ATI", "AMD/ATI Radeon HD 7970", "Radeon HD 7970"},
		{"AMD only", "AMD Radeon RX 6800", "Radeon RX 6800"},
		{"ATI only", "ATI Radeon HD 5870", "Radeon HD 5870"},
		{"Intel Corporation", "Intel Corporation UHD Graphics 630", "UHD Graphics 630"},
		{"Intel only", "Intel Iris Xe Graphics", "Iris Xe Graphics"},
		{"no prefix", "GeForce RTX 4090", "GeForce RTX 4090"},
		{"case insensitive", "nvidia geforce mx150", "geforce mx150"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeVendorPrefixes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildFullName(t *testing.T) {
	tests := []struct {
		name        string
		vendor      string
		displayName string
		expected    string
	}{
		{"NVIDIA GPU", "NVIDIA", "GeForce RTX 3080", "NVIDIA GeForce RTX 3080"},
		{"AMD GPU", "AMD", "Radeon RX 5700", "AMD Radeon RX 5700"},
		{"Intel GPU", "Intel", "UHD Graphics 630", "Intel UHD Graphics 630"},
		{"Unknown vendor", "SomeVendor", "Some GPU", "Some GPU"},
		{"Unknown display", "NVIDIA", "Unknown", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFullName(tt.vendor, tt.displayName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDistroName(t *testing.T) {
	result := getDistroName()
	assert.NotEmpty(t, result)
}

func BenchmarkRemoveVendorPrefixes(b *testing.B) {
	testString := "NVIDIA Corporation GeForce RTX 3080"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		removeVendorPrefixes(testString)
	}
}

func BenchmarkInferVendor(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inferVendor("nvidia", "NVIDIA GeForce RTX 3080")
	}
}

func TestGetGPUInfoWithStats_IdenticalGPUsToldApartByBusId(t *testing.T) {
	var sampled []string
	g := &GopsUtil{
		detectGPUEntries: func() ([]gpuEntry, error) {
			return []gpuEntry{
				{Driver: "amdgpu", BusId: "0000:0c:00.0", RawLine: "0000:0c:00.0 Display controller: Navi 31 [1002:744c]"},
				{Driver: "amdgpu", BusId: "0000:0d:00.0", RawLine: "0000:0d:00.0 Display controller: Navi 31 [1002:744c]"},
			}, nil
		},
		sampleGPUs: func(entries []gpuEntry, _ gpuCursor, now time.Time) ([]gpuSample, gpuCursor) {
			samples := make([]gpuSample, len(entries))
			for i, e := range entries {
				sampled = append(sampled, e.BusId)
				samples[i] = gpuSample{State: models.GPUStateActive, Temperature: float64(len(sampled))}
			}
			return samples, newGPUCursor(now)
		},
	}

	for name, ids := range map[string][]string{"nil": nil, "empty": {}} {
		sampled = nil
		info, err := g.GetGPUInfoWithStats(ids, "")
		require.NoError(t, err, name)
		assert.Empty(t, sampled, name)
		assert.Len(t, info.GPUs, 2, name)
	}

	sampled = nil
	info, err := g.GetGPUInfoWithStats([]string{"0000:0d:00.0"}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"0000:0d:00.0"}, sampled)
	assert.Empty(t, info.GPUs[0].State)
	assert.Equal(t, models.GPUStateActive, info.GPUs[1].State)
	assert.Equal(t, 1.0, info.GPUs[1].Temperature)

	sampled = nil
	info, _ = g.GetGPUInfoWithStats([]string{"1002:744c"}, "")
	assert.Equal(t, []string{"0000:0c:00.0", "0000:0d:00.0"}, sampled, "a PCI ID selects every card of that model")
	assert.Equal(t, []float64{1, 2}, []float64{info.GPUs[0].Temperature, info.GPUs[1].Temperature})
}
