//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const amdgpuIdleFdinfo = `pos:	0
flags:	02100002
mnt_id:	38
ino:	629
drm-driver:	amdgpu
drm-client-id:	49
drm-pdev:	0000:0c:00.0
pasid:	48
drm-total-cpu:	0
drm-total-gtt:	2048 KiB
drm-resident-gtt:	2048 KiB
drm-total-vram:	12 KiB
drm-shared-vram:	0
drm-resident-vram:	12 KiB
drm-purgeable-vram:	0
drm-memory-vram:	12 KiB
drm-memory-gtt: 	2048 KiB
drm-memory-cpu: 	0 KiB
amd-evicted-vram:	0 KiB
amd-requested-vram:	12 KiB
`

const i915Fdinfo = `drm-driver:	i915
drm-client-id:	7
drm-pdev:	0000:00:02.0
drm-total-system0:	8192 KiB
drm-resident-system0:	8192 KiB
drm-total-local0:	4 MiB
drm-resident-local0:	4 MiB
drm-engine-render:	25662044495 ns
drm-engine-copy:	0 ns
drm-engine-video:	0 ns
drm-engine-capacity-video:	2
drm-engine-video-enhance:	0 ns
`

const xeFdinfo = `drm-driver:	xe
drm-client-id:	3
drm-pdev:	0000:03:00.0
drm-resident-vram0:	1 GiB
drm-cycles-rcs:	28257900
drm-total-cycles-rcs:	7655183225
drm-cycles-ccs:	0
drm-total-cycles-ccs:	7655183225
drm-engine-capacity-ccs:	4
`

func TestParseDRMFdinfoAmdgpu(t *testing.T) {
	client, ok := parseDRMFdinfo(amdgpuIdleFdinfo)
	require.True(t, ok)
	assert.Equal(t, "amdgpu", client.Driver)
	assert.Equal(t, "0000:0c:00.0", client.PDev)
	assert.Equal(t, uint64(49), client.ID)
	assert.Equal(t, uint64(12*1024), client.VRAMBytes)
	assert.Equal(t, uint64(2048*1024), client.GTTBytes)
	assert.Empty(t, client.Engines, "An idle client has no engine lines")

	busy, ok := parseDRMFdinfo(amdgpuIdleFdinfo + "drm-engine-gfx:\t21199820945 ns\ndrm-engine-dec:\t5655790 ns\n")
	require.True(t, ok)
	assert.Equal(t, drmEngine{BusyNs: 21199820945, HasBusy: true, Capacity: 1}, busy.Engines["gfx"])
	assert.Equal(t, uint64(5655790), busy.Engines["dec"].BusyNs)
}

func TestParseDRMFdinfoLegacyVRAM(t *testing.T) {
	client, ok := parseDRMFdinfo("drm-driver:\tamdgpu\ndrm-client-id:\t1\ndrm-memory-vram:\t3 MiB\n")
	require.True(t, ok)
	assert.Equal(t, uint64(3<<20), client.VRAMBytes)

	legacy, ok := parseDRMFdinfo("drm-driver:\tamdgpu\ndrm-client-id:\t1\ndrm-memory-gtt:\t5 MiB\n")
	require.True(t, ok)
	assert.Equal(t, uint64(5<<20), legacy.GTTBytes)
	assert.Empty(t, client.PDev)
}

func TestParseDRMFdinfoNotAClient(t *testing.T) {
	_, ok := parseDRMFdinfo("pos:\t0\nflags:\t02100002\n")
	assert.False(t, ok)
}

func TestParseDRMFdinfoI915Capacity(t *testing.T) {
	client, ok := parseDRMFdinfo(i915Fdinfo)
	require.True(t, ok)
	assert.Equal(t, uint64(4<<20), client.VRAMBytes, "Only local memory counts as VRAM")
	assert.Equal(t, uint64(2), client.Engines["video"].Capacity)
	assert.Equal(t, uint64(1), client.Engines["render"].Capacity)
	assert.Equal(t, uint64(25662044495), client.Engines["render"].BusyNs)
	assert.Contains(t, client.Engines, "video-enhance")
}

func TestParseDRMFdinfoXeCycles(t *testing.T) {
	client, ok := parseDRMFdinfo(xeFdinfo)
	require.True(t, ok)
	assert.Equal(t, uint64(1<<30), client.VRAMBytes)
	assert.Equal(t, drmEngine{Cycles: 28257900, TotalCycles: 7655183225, Capacity: 1}, client.Engines["rcs"])
	assert.Equal(t, uint64(4), client.Engines["ccs"].Capacity)
}

func nsClient(id uint64, engine string, busyNs, capacity uint64) drmClient {
	return drmClient{PDev: "0000:0c:00.0", ID: id, Engines: map[string]drmEngine{
		engine: {BusyNs: busyNs, HasBusy: true, Capacity: capacity},
	}}
}

func drmSet(clients ...drmClient) drmClientSet {
	s := drmClientSet{}
	for _, c := range clients {
		s[c.key()] = c
	}
	return s
}

func TestDRMEngineUtilization(t *testing.T) {
	sec := uint64(time.Second)

	gfx := drmEngineUtilization(
		drmSet(nsClient(1, "gfx", 10*sec, 1)),
		drmSet(nsClient(1, "gfx", 10*sec+sec/2, 1)),
		time.Second)
	assert.InDelta(t, 50.0, gfx["gfx"], 0.001)

	video := drmEngineUtilization(
		drmSet(nsClient(1, "video", 4*sec, 2)),
		drmSet(nsClient(1, "video", 5*sec, 2)),
		time.Second)
	assert.InDelta(t, 50.0, video["video"], 0.001, "Busy time is normalized by engine capacity")

	xeEngine := func(cycles, total uint64) drmClient {
		return drmClient{ID: 3, Engines: map[string]drmEngine{"rcs": {Cycles: cycles, TotalCycles: total, Capacity: 1}}}
	}
	xe := drmEngineUtilization(drmSet(xeEngine(100, 1000)), drmSet(xeEngine(350, 2000)), time.Second)
	assert.InDelta(t, 25.0, xe["rcs"], 0.001)

	reset := drmEngineUtilization(
		drmSet(nsClient(1, "gfx", 10*sec, 1)),
		drmSet(nsClient(1, "gfx", sec/10, 1)),
		time.Second)
	assert.Equal(t, 0.0, reset["gfx"], "A counter going backwards contributes nothing")
}

func TestDRMEngineUtilizationClients(t *testing.T) {
	sec := uint64(time.Second)
	prev := drmSet(nsClient(1, "gfx", 10*sec, 1))

	newClient := drmEngineUtilization(prev,
		drmSet(nsClient(1, "gfx", 10*sec+sec/4, 1), nsClient(2, "gfx", sec/4, 1)),
		time.Second)
	assert.InDelta(t, 50.0, newClient["gfx"], 0.001, "A client created during the interval counts from zero")

	stale := drmEngineUtilization(prev,
		drmSet(nsClient(1, "gfx", 10*sec, 1), nsClient(3, "gfx", 90*sec, 1)),
		time.Second)
	assert.Equal(t, 0.0, stale["gfx"], "A new client whose counter exceeds the interval is ignored")

	closed := drmEngineUtilization(drmSet(nsClient(1, "gfx", 10*sec, 1), nsClient(2, "gfx", 50*sec, 1)),
		drmSet(nsClient(1, "gfx", 10*sec+sec/10, 1)),
		time.Second)
	assert.InDelta(t, 10.0, closed["gfx"], 0.001, "A closed client does not produce a negative delta")
}

func TestDRMEngineUtilizationXeNoBaseline(t *testing.T) {
	cur := drmSet(drmClient{PDev: "0000:03:00.0", ID: 3, Engines: map[string]drmEngine{
		"rcs": {Cycles: 500, TotalCycles: 1000, Capacity: 1},
	}})
	u := drmEngineUtilization(drmSet(), cur, time.Second)
	assert.Equal(t, 0.0, u["rcs"], "xe cycles without a baseline are not rated against the interval")
}

func TestMaxEngineUtilization(t *testing.T) {
	assert.Equal(t, 0.0, maxEngineUtilization(nil))
	assert.Equal(t, 70.0, maxEngineUtilization(map[string]float64{"gfx": 70, "dec": 20}))
	assert.Equal(t, 100.0, maxEngineUtilization(map[string]float64{"gfx": 130}))
}

func TestMaxVideoEngineUtilization(t *testing.T) {
	assert.Equal(t, 0.0, maxVideoEngineUtilization(map[string]float64{"gfx": 70, "compute": 40}))
	assert.Equal(t, 35.0, maxVideoEngineUtilization(map[string]float64{"gfx": 70, "dec": 20, "enc_1": 35, "jpeg": 5}))
	assert.Equal(t, 12.0, maxVideoEngineUtilization(map[string]float64{"rcs": 90, "vecs": 12}))
}

func TestDRMDriverReportsEngines(t *testing.T) {
	assert.True(t, drmDriverReportsEngines("amdgpu"))
	assert.True(t, drmDriverReportsEngines("xe"))
	assert.False(t, drmDriverReportsEngines("nvidia-drm"))
	assert.False(t, drmDriverReportsEngines("msm"), "non-PCI GPUs are not supported")
	assert.False(t, drmDriverReportsEngines(""))
}

func TestScanProcFDs(t *testing.T) {
	root := t.TempDir()
	fdDir := filepath.Join(root, "42", "fd")
	infoDir := filepath.Join(root, "42", "fdinfo")
	require.NoError(t, os.MkdirAll(fdDir, 0o755))
	require.NoError(t, os.MkdirAll(infoDir, 0o755))

	for fd, target := range map[string]string{
		"3": "/dev/dri/renderD128",
		"4": "/dev/dri/renderD128",
		"5": "socket:[12345]",
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(fdDir, fd)))
		require.NoError(t, os.WriteFile(filepath.Join(infoDir, fd), []byte(amdgpuIdleFdinfo), 0o644))
	}
	require.NoError(t, os.Symlink("/dev/dri/card1", filepath.Join(fdDir, "6")))

	scan, err := scanProcFDs(root, 42, map[string]gpuNodeState{"renderD128": gpuNodeRead, "card1": gpuNodeRead}, false)
	require.NoError(t, err)
	require.Len(t, scan.DRM, 1, "Duplicated fds of one client count once; the socket and the fd without fdinfo are skipped")
	assert.Equal(t, uint64(49), scan.DRM[drmClientKey{PDev: "0000:0c:00.0", ID: 49}].ID)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "43"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "43", "fd"), nil, 0o644))
	_, err = scanProcFDs(root, 43, nil, false)
	assert.Error(t, err, "An unreadable fd dir is an error, not an empty result")

	_, err = scanProcFDs(root, 44, nil, false)
	assert.Error(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "self"), 0o755))
	pids, err := listProcPIDs(root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int32{42, 43}, pids)
}

func TestScanProcFDsLive(t *testing.T) {
	if _, err := os.Stat("/dev/dri"); err != nil {
		t.Skip("no /dev/dri")
	}
	entries, _ := os.ReadDir("/sys/class/drm")
	nodes := map[string]gpuNodeState{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "renderD") || strings.HasPrefix(e.Name(), "card") && !strings.Contains(e.Name(), "-") {
			nodes[e.Name()] = gpuNodeRead
		}
	}
	_, err := scanProcFDs("/proc", int32(os.Getpid()), nodes, true)
	assert.NoError(t, err)
}

func TestScanProcFDsSkipsUnwantedNodes(t *testing.T) {
	root := t.TempDir()
	fdDir := filepath.Join(root, "42", "fd")
	infoDir := filepath.Join(root, "42", "fdinfo")
	require.NoError(t, os.MkdirAll(fdDir, 0o755))
	require.NoError(t, os.MkdirAll(infoDir, 0o755))
	require.NoError(t, os.Symlink("/dev/dri/renderD128", filepath.Join(fdDir, "3")))
	require.NoError(t, os.WriteFile(filepath.Join(infoDir, "3"), []byte(amdgpuIdleFdinfo), 0o644))
	// Opening a FIFO without a writer blocks, so reading it would hang the scan.
	require.NoError(t, os.Symlink("/dev/dri/renderD129", filepath.Join(fdDir, "4")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(infoDir, "4"), 0o644))

	done := make(chan procFDScan, 1)
	go func() {
		scan, _ := scanProcFDs(root, 42, map[string]gpuNodeState{"renderD128": gpuNodeRead}, false)
		done <- scan
	}()
	select {
	case scan := <-done:
		assert.Len(t, scan.DRM, 1)
		assert.True(t, scan.UnknownGPU, "a node of no detected GPU is flagged, not read")
	case <-time.After(5 * time.Second):
		t.Fatal("fdinfo of an unwanted DRM node was opened")
	}
}

func TestGPUNodes(t *testing.T) {
	pciRoot := t.TempDir()
	writeHwmonFiles(t, pciRoot, map[string]string{
		"0000:0c:00.0/drm/card1/dev":        "226:1\n",
		"0000:0c:00.0/drm/renderD128/dev":   "226:128\n",
		"0000:00:02.0/drm/renderD129/dev":   "226:129\n",
		"0000:00:02.0/power/runtime_status": "suspended\n",
		"0000:03:00.0/drm/renderD130/dev":   "226:130\n",
		"0000:01:00.0/drm/renderD131/dev":   "226:131\n",
	})
	gpus := []models.GPU{
		{BusId: "0000:0c:00.0", PciId: "1002:744c", Driver: "amdgpu"},
		{BusId: "0000:00:02.0", PciId: "8086:a780", Driver: "i915"},
		{BusId: "0000:03:00.0", PciId: "8086:56a0", Driver: "xe"},
		{BusId: "0000:01:00.0", PciId: "10de:2786", Driver: "nvidia"},
	}

	set := gpuNodes(pciRoot, gpus, []string{"1002:744c", "8086:a780", "10de:2786"})
	assert.Equal(t, map[string]gpuNodeState{
		"card1":      gpuNodeRead,
		"renderD128": gpuNodeRead,
		"renderD129": gpuNodeSuspended,
		"renderD130": gpuNodeExcluded,
		"renderD131": gpuNodeRead,
	}, set.nodes)
	assert.Equal(t, []string{"0000:01:00.0"}, set.nvidia)

	assert.Empty(t, gpuNodes(pciRoot, gpus, []string{"1002:744c"}).nvidia, "an excluded NVIDIA GPU is not monitored")
	writeHwmonFiles(t, pciRoot, map[string]string{"0000:01:00.0/power/runtime_status": "suspended\n"})
	assert.Empty(t, gpuNodes(pciRoot, gpus, nil).nvidia, "a suspended NVIDIA GPU is not monitored")
}
