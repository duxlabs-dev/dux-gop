//go:build linux

package gops

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSysfs(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, value := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(value+"\n"), 0o644))
	}
}

func testSampler(t *testing.T, smi func(fields, busIds []string) ([]byte, error)) gpuSampler {
	t.Helper()
	if smi == nil {
		smi = func(_, _ []string) ([]byte, error) {
			t.Fatal("nvidia-smi must not run")
			return nil, nil
		}
	}
	return gpuSampler{pciDevices: t.TempDir(), thermal: t.TempDir(), nvidiaSmi: smi, hwmon: NewGopsUtil().readHwmonChip}
}

func TestParseNvidiaSmi(t *testing.T) {
	out := []byte("00000000:01:00.0, 51, 3, 265, 12227, 19.20, 420, 7001, 2100, 7001, 200.00, 45, 4, 4, 16, 16, 12, 30\n" +
		"00000000:02:00.0, [N/A], [N/A], 10, 4096, [Not Supported], 300, [N/A], 2520, 10501, 450.00, [Not Supported], 1, 4, 8, 16, [N/A], 0\n" +
		"00000000:03:00.0, 40, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 1, 1, 1\n")

	rows := parseNvidiaSmi(out)

	first := rows["0000:01:00.0"]
	assert.Equal(t, 51.0, first.Temperature)
	assert.Equal(t, uint64(265*1024), *first.MemoryUsed)
	assert.Equal(t, 19.2, *first.Power)
	assert.Equal(t, 30.0, *first.DecoderUsage)
	assert.Equal(t, "16.0 GT/s PCIe", first.PcieLinkSpeed)

	second := rows["0000:02:00.0"]
	assert.Zero(t, second.Temperature)
	assert.Nil(t, second.Usage, "[N/A] is unknown, not 0")
	assert.Nil(t, second.Power)
	assert.Equal(t, 0.0, *second.DecoderUsage)

	assert.Nil(t, rows["0000:03:00.0"].EncoderUsage, "rows without the video fields still parse")
	assert.Empty(t, parseNvidiaSmi([]byte("00000000:01:00.0, 37, 1024\n")))
}

func TestSampleSkipsSuspendedGPU(t *testing.T) {
	s := testSampler(t, nil)
	writeSysfs(t, s.pciDevices, map[string]string{
		"0000:01:00.0/power/runtime_status": "suspended",
		"0000:03:00.0/power/runtime_status": "suspended",
		"0000:03:00.0/gpu_busy_percent":     "80",
	})

	samples, _ := s.sample([]gpuEntry{
		{Driver: "nvidia", BusId: "0000:01:00.0"},
		{Driver: "amdgpu", BusId: "0000:03:00.0"},
	}, gpuCursor{}, time.Now())

	for _, sample := range samples {
		assert.Equal(t, models.GPUStateSuspended, sample.State)
		assert.Equal(t, 0.0, *sample.Usage)
		assert.Nil(t, sample.MemoryUsed)
		assert.Zero(t, sample.Temperature)
	}
}

func TestSampleNvidiaQueriesOnlyActiveCards(t *testing.T) {
	var queried []string
	s := testSampler(t, func(_, ids []string) ([]byte, error) {
		queried = ids
		return []byte("00000000:02:00.0, 60, 97, 8000, 12227, 220.5, 2800, 14001, 3000, 14001, 300, 40, 4, 4, 16, 16, 5, 6\n"), nil
	})
	writeSysfs(t, s.pciDevices, map[string]string{
		"0000:01:00.0/power/runtime_status": "suspended",
		"0000:02:00.0/power/runtime_status": "active",
	})

	samples, _ := s.sample([]gpuEntry{
		{Driver: "nvidia", BusId: "0000:01:00.0"},
		{Driver: "nvidia", BusId: "0000:02:00.0"},
	}, gpuCursor{}, time.Now())

	assert.Equal(t, []string{"0000:02:00.0"}, queried)
	assert.Equal(t, models.GPUStateActive, samples[1].State)
	assert.Equal(t, 97.0, *samples[1].Usage)
	assert.Equal(t, 60.0, samples[1].Temperature)
	assert.Equal(t, "nvidia", samples[1].Hwmon)
	assert.Equal(t, 220.5, *samples[1].Power)
	assert.Equal(t, 300.0, *samples[1].PowerLimit)
	assert.Equal(t, 5.0, *samples[1].EncoderUsage)
}

func TestSampleIntelUsageFromIdleResidency(t *testing.T) {
	s := testSampler(t, nil)
	bus := "0000:00:02.0"
	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/tile0/gt0/gtidle/name":              "gt0-mc",
		bus + "/tile0/gt0/gtidle/idle_residency_ms": "0",
		bus + "/tile0/gt1/gtidle/name":              "gt1-rc",
		bus + "/tile0/gt1/gtidle/idle_residency_ms": "10000",
		bus + "/tile0/gt1/freq0/act_freq":           "1550",
	})
	entries := []gpuEntry{{Driver: "xe", BusId: bus}}
	start := time.Unix(1000, 0)

	first, cursor := s.sample(entries, gpuCursor{}, start)
	assert.Nil(t, first[0].Usage, "usage needs two samples")
	assert.Equal(t, 1550.0, *first[0].CoreClock)

	writeSysfs(t, s.pciDevices, map[string]string{bus + "/tile0/gt1/gtidle/idle_residency_ms": "10500"})
	second, cursor := s.sample(entries, decodeGPUCursor(encodeGPUCursor(cursor)), start.Add(2*time.Second))
	assert.InDelta(t, 75.0, *second[0].Usage, 0.001)

	writeSysfs(t, s.pciDevices, map[string]string{bus + "/tile0/gt1/gtidle/idle_residency_ms": "5"})
	reset, _ := s.sample(entries, cursor, start.Add(4*time.Second))
	assert.Nil(t, reset[0].Usage, "a counter that went backwards is not a sample")
}

func TestSampleI915PowerFromEnergy(t *testing.T) {
	s := testSampler(t, nil)
	bus := "0000:03:00.0"
	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/drm/card1/gt/gt0/rc6_residency_ms": "0",
		bus + "/drm/card1/gt/gt0/rps_act_freq_mhz": "2400",
		bus + "/drm/card1-DP-1/status":             "connected",
		bus + "/hwmon/hwmon3/name":                 "i915_gt0",
		bus + "/hwmon/hwmon3/energy1_input":        "0",
		bus + "/hwmon/hwmon5/name":                 "i915",
		bus + "/hwmon/hwmon5/energy1_input":        "1000000",
	})
	entries := []gpuEntry{{Driver: "i915", BusId: bus}}
	start := time.Unix(1000, 0)

	first, cursor := s.sample(entries, gpuCursor{}, start)
	assert.Nil(t, first[0].Power)
	assert.Equal(t, 2400.0, *first[0].CoreClock)

	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/hwmon/hwmon3/energy1_input":        "999000000",
		bus + "/hwmon/hwmon5/energy1_input":        "61000000",
		bus + "/drm/card1/gt/gt0/rc6_residency_ms": "1000",
	})
	second, _ := s.sample(entries, cursor, start.Add(2*time.Second))
	assert.InDelta(t, 30.0, *second[0].Power, 0.001)
	assert.InDelta(t, 50.0, *second[0].Usage, 0.001)
}

func TestGPURequested(t *testing.T) {
	gpu := models.GPU{PciId: "10de:2f04", BusId: "0000:01:00.0"}

	assert.True(t, gpuRequested(gpu, []string{"10de:2f04"}))
	assert.True(t, gpuRequested(gpu, []string{"00000000:01:00.0"}))
	assert.True(t, gpuRequested(gpu, []string{" 0000:01:00.0 "}))
	assert.False(t, gpuRequested(gpu, []string{"0000:02:00.0", "1002:13c0"}))
	assert.False(t, gpuRequested(models.GPU{PciId: "10de:2f04"}, []string{""}))
	assert.False(t, gpuRequested(gpu, nil), "omitted selects none")
}

func TestSampleAMDGPU(t *testing.T) {
	s := testSampler(t, nil)
	writeSysfs(t, s.pciDevices, map[string]string{
		"0000:03:00.0/power/runtime_status":        "active",
		"0000:03:00.0/gpu_busy_percent":            "42",
		"0000:03:00.0/mem_info_vram_used":          "1073741824",
		"0000:03:00.0/mem_info_vram_total":         "8589934592",
		"0000:03:00.0/hwmon/hwmon4/name":           "amdgpu",
		"0000:03:00.0/hwmon/hwmon4/temp1_input":    "61000",
		"0000:03:00.0/hwmon/hwmon4/power1_average": "150000000",
		"0000:03:00.0/hwmon/hwmon4/power1_input":   "1",
		"0000:03:00.0/hwmon/hwmon4/freq1_input":    "2500000000",
		"0000:03:00.0/hwmon/hwmon4/freq1_label":    "sclk",
		"0000:03:00.0/hwmon/hwmon4/freq2_input":    "1000000000",
		"0000:03:00.0/hwmon/hwmon4/freq2_label":    "mclk",
	})

	samples, _ := s.sample([]gpuEntry{{Driver: "amdgpu", BusId: "0000:03:00.0"}}, gpuCursor{}, time.Now())

	sample := samples[0]
	assert.Equal(t, 42.0, *sample.Usage)
	assert.Equal(t, uint64(1024*1024), *sample.MemoryUsed)
	assert.Equal(t, uint64(8*1024*1024), *sample.MemoryTotal)
	assert.Equal(t, 61.0, sample.Temperature)
	assert.Equal(t, "hwmon4", sample.Hwmon)
	assert.Equal(t, 150.0, *sample.Power, "power1_average wins over the instantaneous reading")
	assert.Equal(t, 2500.0, *sample.CoreClock)
	assert.Equal(t, 1000.0, *sample.MemoryClock)
}

func TestSampleXeTemperatureFromPackageSensor(t *testing.T) {
	s := testSampler(t, nil)
	bus := "0000:03:00.0"
	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/power/runtime_status":         "active",
		bus + "/hwmon/hwmon6/name":            "xe",
		bus + "/hwmon/hwmon6/temp2_label":     "pkg",
		bus + "/hwmon/hwmon6/temp2_input":     "48000",
		bus + "/hwmon/hwmon6/temp3_label":     "vram",
		bus + "/hwmon/hwmon6/temp3_input":     "70000",
		bus + "/hwmon/hwmon6/energy1_input":   "5",
		bus + "/tile0/gt0/gtidle/name":        "gt0-rc",
		bus + "/tile0/gt0/freq0/act_freq":     "0",
		bus + "/power/runtime_suspended_time": "0",
	})

	samples, _ := s.sample([]gpuEntry{{Driver: "xe", BusId: bus}}, gpuCursor{}, time.Now())

	assert.Equal(t, 48.0, samples[0].Temperature)
	assert.Equal(t, "hwmon6", samples[0].Hwmon)
	assert.Equal(t, 0.0, *samples[0].CoreClock, "xe reports 0 MHz while the GT sleeps in RC6")
}

func TestSampleTreatsSuspendingAsSuspended(t *testing.T) {
	s := testSampler(t, nil)
	writeSysfs(t, s.pciDevices, map[string]string{"0000:01:00.0/power/runtime_status": "suspending"})

	samples, _ := s.sample([]gpuEntry{{Driver: "nvidia", BusId: "0000:01:00.0"}}, gpuCursor{}, time.Now())

	assert.Equal(t, models.GPUStateSuspended, samples[0].State)
}

func xeRegion(class uint16, total, used uint64) []byte {
	region := make([]byte, xeMemRegionSize)
	binary.NativeEndian.PutUint16(region[0:2], class)
	binary.NativeEndian.PutUint64(region[8:16], total)
	binary.NativeEndian.PutUint64(region[16:24], used)
	return region
}

func TestParseXeMemRegions(t *testing.T) {
	build := func(regions ...[]byte) []byte {
		buf := make([]byte, xeMemRegionsHeaderSize)
		binary.NativeEndian.PutUint32(buf[0:4], uint32(len(regions)))
		for _, region := range regions {
			buf = append(buf, region...)
		}
		return buf
	}

	used, total, ok := parseXeMemRegions(build(
		xeRegion(0, 64<<30, 10<<30),
		xeRegion(xeMemRegionClassVRAM, 8<<30, 1<<30),
		xeRegion(xeMemRegionClassVRAM, 8<<30, 2<<30),
	))
	assert.True(t, ok)
	assert.Equal(t, uint64(16<<30), total, "system memory is not VRAM")
	assert.Equal(t, uint64(3<<30), used)

	_, _, ok = parseXeMemRegions(build(xeRegion(xeMemRegionClassVRAM, 8<<30, 0)))
	assert.False(t, ok, "pre-6.18 kernels hide usage as zero")

	_, _, ok = parseXeMemRegions(build(xeRegion(xeMemRegionClassVRAM, 8<<30, 1))[:20])
	assert.False(t, ok, "truncated reply")
}

func TestSampleAMDGPUExtras(t *testing.T) {
	s := testSampler(t, nil)
	bus := "0000:0c:00.0"
	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/power/runtime_status":     "active",
		bus + "/mem_info_gtt_used":        "104857600",
		bus + "/pp_dpm_sclk":              "S: 0Mhz\n1: 500Mhz *\n2: 2526Mhz ",
		bus + "/pp_dpm_mclk":              "0: 96Mhz \n1: 1249Mhz *",
		bus + "/hwmon/hwmon3/name":        "amdgpu",
		bus + "/hwmon/hwmon3/temp1_input": "45000",
		bus + "/hwmon/hwmon3/temp1_label": "edge",
		bus + "/hwmon/hwmon3/temp2_input": "52000",
		bus + "/hwmon/hwmon3/temp2_label": "junction",
		bus + "/hwmon/hwmon3/fan1_input":  "1200",
		bus + "/hwmon/hwmon3/fan1_max":    "3600",
		bus + "/hwmon/hwmon3/freq1_input": "1800000000",
		bus + "/hwmon/hwmon3/freq1_label": "sclk",
	})

	samples, _ := s.sample([]gpuEntry{{Driver: "amdgpu", BusId: bus}}, gpuCursor{}, time.Now())
	g := samples[0]

	assert.Equal(t, uint64(104857600/1024), *g.GTTUsed)
	assert.Equal(t, 1800.0, *g.CoreClock, "hwmon wins over the DPM level")
	assert.Equal(t, 2526.0, *g.CoreClockMax)
	assert.Equal(t, 1249.0, *g.MemoryClock, "DPM level without an mclk sensor")
	assert.Equal(t, 52.0, *g.Hotspot)
	assert.InDelta(t, 33.33, *g.FanPercent, 0.01)

	writeSysfs(t, s.pciDevices, map[string]string{
		bus + "/hwmon/hwmon3/pwm1":     "40",
		bus + "/hwmon/hwmon3/pwm1_max": "255",
		bus + "/pp_dpm_mclk":           "S: 0Mhz *\n0: 96Mhz \n1: 1249Mhz ",
	})
	samples, _ = s.sample([]gpuEntry{{Driver: "amdgpu", BusId: bus}}, gpuCursor{}, time.Now())
	assert.InDelta(t, 15.69, *samples[0].FanPercent, 0.01, "PWM duty wins over rpm")
	assert.Nil(t, samples[0].MemoryClock, "the S: level is not a clock")
}

func TestSampleNvidiaRetriesWithoutVideoFields(t *testing.T) {
	t.Cleanup(func() { nvidiaSmiNoVideo.Store(false) })
	var calls [][]string
	s := testSampler(t, func(fields, _ []string) ([]byte, error) {
		calls = append(calls, fields)
		if len(fields) > len(nvidiaSmiFields) {
			return []byte(`Field "utilization.encoder" is not a valid field to query.`), errors.New("exit status 2")
		}
		return []byte("00000000:01:00.0, 60, 97, 8000, 12227, 220.5, 2800, 14001, 3000, 14001, 300, 40, 4, 4, 16, 16\n"), nil
	})
	entries := []gpuEntry{{Driver: "nvidia", BusId: "0000:01:00.0"}}

	samples, _ := s.sample(entries, gpuCursor{}, time.Now())
	assert.Equal(t, 97.0, *samples[0].Usage)
	assert.Nil(t, samples[0].EncoderUsage)
	assert.Len(t, calls, 2)
	assert.True(t, nvidiaSmiNoVideo.Load())

	s.sample(entries, gpuCursor{}, time.Now())
	assert.Len(t, calls, 3, "the rejection is remembered")
	assert.Len(t, calls[2], len(nvidiaSmiFields))
}
