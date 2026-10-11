//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AvengeMedia/dgop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const amdgpuFdinfo = `pos:	0
flags:	02100002
drm-driver:	amdgpu
drm-client-id:	15
drm-pdev:	0000:09:00.0
drm-resident-gtt:	2 MiB
drm-resident-vram:	3 MiB
drm-memory-vram:	3072 KiB
drm-engine-gfx:	2000000000 ns
drm-engine-compute:	500000000 ns
`

const xeFdinfo = `drm-driver:	xe
drm-client-id:	7
drm-pdev:	0000:03:00.0
drm-resident-vram0:	1048576
drm-cycles-rcs:	1000
drm-total-cycles-rcs:	10000
`

const i915Fdinfo = `drm-driver:	i915
drm-client-id:	3
drm-pdev:	0000:00:02.0
drm-engine-render:	0 ns
drm-engine-video:	4000000000 ns
drm-engine-capacity-video:	2
drm-memory-local0:	4096 KiB
`

func TestParseDRMFdinfoCountsDuplicatedClientsOnce(t *testing.T) {
	sample := newProcessGPUSample(time.Unix(0, 0))
	seen := map[string]bool{}
	parseDRMFdinfo(42, []byte(amdgpuFdinfo), sample, seen)
	parseDRMFdinfo(42, []byte(amdgpuFdinfo), sample, seen)

	assert.True(t, sample.Clients[42])
	assert.Equal(t, uint64(3*1024), sample.MemoryKB[42], "resident vram only, not gtt, counted once")
	assert.Equal(t, uint64(2000000000), sample.Counters[42]["0000:09:00.0/15/gfx/ns"])
	assert.Len(t, sample.Counters[42], 2)
}

func TestParseDRMFdinfoReadsCyclesCapacityAndLegacyMemory(t *testing.T) {
	sample := newProcessGPUSample(time.Unix(0, 0))
	parseDRMFdinfo(1, []byte(xeFdinfo), sample, map[string]bool{})
	parseDRMFdinfo(2, []byte(i915Fdinfo), sample, map[string]bool{})

	assert.Equal(t, uint64(1000), sample.Counters[1]["0000:03:00.0/7/rcs/cyc"])
	assert.Equal(t, uint64(10000), sample.Counters[1]["0000:03:00.0/7/rcs/tot"])
	assert.Equal(t, uint64(1024), sample.MemoryKB[1], "a bare number is bytes")
	assert.Equal(t, uint64(2), sample.Capacity[2]["0000:00:02.0/3/video/ns"])
	assert.Equal(t, uint64(4096), sample.MemoryKB[2], "drm-memory-<region> is the legacy resident spelling")
}

func TestParseDRMFdinfoIgnoresFilesWithoutClientStats(t *testing.T) {
	sample := newProcessGPUSample(time.Unix(0, 0))
	parseDRMFdinfo(9, []byte("pos:\t0\nflags:\t02100002\nmnt_id:\t37\n"), sample, map[string]bool{})
	assert.False(t, sample.Clients[9])
}

func TestProcessGPUUsage(t *testing.T) {
	prev := newProcessGPUSample(time.UnixMilli(1000))
	cur := newProcessGPUSample(time.UnixMilli(3000))
	for _, s := range []processGPUSample{prev, cur} {
		s.Clients[1], s.Clients[2], s.Clients[3], s.Clients[4] = true, true, true, true
	}

	// 1 s of gfx and 0.5 s of compute over 2 s on one GPU, plus a second GPU.
	prev.setCounter(1, "amd/15/gfx/ns", 2e9)
	cur.setCounter(1, "amd/15/gfx/ns", 3e9)
	prev.setCounter(1, "amd/15/compute/ns", 0)
	cur.setCounter(1, "amd/15/compute/ns", 5e8)
	cur.setDirect(1, "nvidia:0", 10)

	// Two video engines: 2 s busy over 2 s is one of two engines.
	prev.setCounter(2, "intel/3/video/ns", 0)
	cur.setCounter(2, "intel/3/video/ns", 2e9)
	cur.Capacity[2] = map[string]uint64{"intel/3/video/ns": 2}

	prev.setCounter(3, "xe/7/rcs/cyc", 1000)
	cur.setCounter(3, "xe/7/rcs/cyc", 4000)
	prev.setCounter(3, "xe/7/rcs/tot", 10000)
	cur.setCounter(3, "xe/7/rcs/tot", 20000)

	// A counter that dipped reports no work instead of wrapping.
	prev.setCounter(4, "amd/16/gfx/ns", 5e9)
	cur.setCounter(4, "amd/16/gfx/ns", 4e9)

	usage := processGPUUsage(prev, cur)

	assert.InDelta(t, 75, usage[1], 0.001, "busiest GPU wins, engines on it add up")
	assert.InDelta(t, 50, usage[2], 0.001)
	assert.InDelta(t, 30, usage[3], 0.001)
	assert.Equal(t, 0.0, usage[4])
	assert.Equal(t, uint64(5e9), nextGPUCounters(prev, cur)[4]["amd/16/gfx/ns"], "the larger value is kept")
}

func TestProcessGPUUsageWithoutBaselineOnlyKnowsDirectUsage(t *testing.T) {
	cur := newProcessGPUSample(time.UnixMilli(3000))
	cur.Clients[1] = true
	cur.setCounter(1, "amd/15/gfx/ns", 3e9)
	cur.setDirect(2, "nvidia:0", 12)

	usage := processGPUUsage(processGPUSample{}, cur)

	_, known := usage[1]
	assert.False(t, known)
	assert.Equal(t, 12.0, usage[2])
}

func TestParseNvidiaPmon(t *testing.T) {
	out := []byte(`# gpu         pid   type     sm    mem    enc    dec    jpg    ofa     fb   ccpm    command
# Idx           #    C/G      %      %      %      %      %      %     MB     MB    name
    0       1244     G      4      0      -      -      -      -      3      0    Hyprland
    0       2539   C+G      -      -      -      -      -      -    168      0    sunshine
    1       2539     C     30      2      -      -      -      -     10      0    sunshine
`)
	sample := newProcessGPUSample(time.Unix(0, 0))
	parseNvidiaPmon(out, sample)

	assert.Equal(t, 4.0, sample.Direct[1244]["nvidia:0"])
	assert.Equal(t, 0.0, sample.Direct[2539]["nvidia:0"], "a dash means idle")
	assert.Equal(t, 30.0, sample.Direct[2539]["nvidia:1"])
	assert.Equal(t, uint64(178*1024), sample.MemoryKB[2539])
	assert.Equal(t, 30.0, processGPUUsage(processGPUSample{}, sample)[2539])
}

func TestScanProcessGPUSkipsSuspendedDevices(t *testing.T) {
	root := t.TempDir()
	s := processGPUScanner{
		procRoot:   filepath.Join(root, "proc"),
		drmClass:   filepath.Join(root, "drm"),
		pciDevices: filepath.Join(root, "pci"),
		nvidiaPmon: func([]string) ([]byte, error) {
			t.Fatal("pmon must not run for a suspended card")
			return nil, nil
		},
	}
	files := map[string]string{
		"proc/100/fdinfo/5":                          amdgpuFdinfo,
		"proc/100/fdinfo/6":                          xeFdinfo,
		"drm/renderD128/device/power/runtime_status": "active",
		"drm/renderD129/device/power/runtime_status": "suspended",
		"pci/0000:01:00.0/power/runtime_status":      "suspended",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "proc/100/fd"), 0o755))
	require.NoError(t, os.Symlink("/dev/dri/renderD128", filepath.Join(root, "proc/100/fd/5")))
	require.NoError(t, os.Symlink("/dev/dri/renderD129", filepath.Join(root, "proc/100/fd/6")))
	require.NoError(t, os.Symlink("/dev/null", filepath.Join(root, "proc/100/fd/7")))

	sample := s.scan([]int32{100, 101}, []gpuEntry{{Driver: "nvidia", BusId: "0000:01:00.0"}}, time.Unix(10, 0))

	assert.True(t, sample.Clients[100])
	assert.Contains(t, sample.Counters[100], "0000:09:00.0/15/gfx/ns")
	assert.NotContains(t, sample.Counters[100], "0000:03:00.0/7/rcs/cyc", "fdinfo of a sleeping xe card is not read")
}

func TestProcessCursorCarriesGPUCounters(t *testing.T) {
	entries := []models.ProcessCursorData{{PID: 7, Ticks: 1.5, Timestamp: 1000}}
	counters := map[int32]map[string]uint64{7: {"amd/15/gfx/ns": 123}}

	cursor := encodeProcessCursorWithGPU(entries, 5000, counters)

	assert.Len(t, decodeProcessCursor(cursor), 1)
	gpu := decodeProcessGPUCursor(cursor)
	assert.Equal(t, int64(5000), gpu.TimeMs)
	assert.Equal(t, uint64(123), gpu.Counters[7]["amd/15/gfx/ns"])
	assert.Equal(t, int64(0), decodeProcessGPUCursor(encodeProcessCursor(entries)).TimeMs, "an old cursor has no GPU baseline")
}

func TestMergeAndSortByGPU(t *testing.T) {
	pct := func(v float64) *float64 { return &v }
	kb := func(v uint64) *uint64 { return &v }
	procs := []*models.ProcessInfo{
		{PID: 1, ExecutablePath: "/usr/bin/a", GPU: pct(70), GPUMemoryKB: kb(100)},
		{PID: 2, PPID: 1, ExecutablePath: "/usr/bin/a", GPU: pct(50), GPUMemoryKB: kb(50)},
		{PID: 3, ExecutablePath: "/usr/bin/b"},
		{PID: 4, ExecutablePath: "/usr/bin/c", GPU: pct(0)},
	}

	merged := mergeProcessesByExecutable(procs)
	byPID := map[int32]*models.ProcessInfo{}
	for _, p := range merged {
		byPID[p.PID] = p
	}

	assert.Equal(t, 100.0, *byPID[1].GPU, "capped at one GPU's worth")
	assert.Equal(t, uint64(150), *byPID[1].GPUMemoryKB)
	assert.Equal(t, 70.0, *procs[0].GPU, "merging does not touch the source process")
	assert.Nil(t, byPID[3].GPU)
	assert.Greater(t, gpuSortValue(byPID[4]), gpuSortValue(byPID[3]), "an idle client sorts above a process without GPU")
}
