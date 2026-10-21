//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const amdgpuIdleFdinfo = `drm-driver:	amdgpu
drm-client-id:	49
drm-pdev:	0000:0c:00.0
drm-resident-gtt:	2048 KiB
drm-resident-vram:	12 KiB
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

func TestParseDRMFdinfo(t *testing.T) {
	_, ok := parseDRMFdinfo("pos:\t0\nflags:\t02100002\n")
	assert.False(t, ok, "not a DRM client")

	tests := []struct {
		name        string
		text        string
		vram, gtt   uint64
		engine      string
		want        drmEngine
		wantEngines int
	}{
		{"amdgpu idle", amdgpuIdleFdinfo, 12 << 10, 2048 << 10, "", drmEngine{}, 0},
		{"amdgpu busy", amdgpuIdleFdinfo + "drm-engine-gfx:\t21199820945 ns\n", 12 << 10, 2048 << 10, "gfx", drmEngine{BusyNs: 21199820945, HasBusy: true, Capacity: 1}, 1},
		{"legacy amdgpu memory keys", "drm-driver:\tamdgpu\ndrm-client-id:\t1\ndrm-memory-vram:\t3 MiB\ndrm-memory-gtt:\t5 MiB\n", 3 << 20, 5 << 20, "", drmEngine{}, 0},
		{"i915 counts only local memory as VRAM", i915Fdinfo, 4 << 20, 0, "video", drmEngine{BusyNs: 0, HasBusy: true, Capacity: 2}, 4},
		{"xe cycles", xeFdinfo, 1 << 30, 0, "rcs", drmEngine{Cycles: 28257900, TotalCycles: 7655183225, Capacity: 1}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, ok := parseDRMFdinfo(tt.text)
			require.True(t, ok)
			assert.Equal(t, tt.vram, client.VRAMBytes)
			assert.Equal(t, tt.gtt, client.GTTBytes)
			assert.Len(t, client.Engines, tt.wantEngines)
			if tt.engine != "" {
				assert.Equal(t, tt.want, client.Engines[tt.engine])
			}
		})
	}
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
	xe := func(cycles, total uint64) drmClient {
		return drmClient{ID: 3, Engines: map[string]drmEngine{"rcs": {Cycles: cycles, TotalCycles: total, Capacity: 1}}}
	}
	tests := []struct {
		name      string
		prev, cur drmClientSet
		engine    string
		want      float64
	}{
		{"busy time", drmSet(nsClient(1, "gfx", 10*sec, 1)), drmSet(nsClient(1, "gfx", 10*sec+sec/2, 1)), "gfx", 50},
		{"normalized by engine capacity", drmSet(nsClient(1, "video", 4*sec, 2)), drmSet(nsClient(1, "video", 5*sec, 2)), "video", 50},
		{"xe cycles", drmSet(xe(100, 1000)), drmSet(xe(350, 2000)), "rcs", 25},
		{"xe cycles without a baseline", drmSet(), drmSet(xe(500, 1000)), "rcs", 0},
		{"a counter going backwards", drmSet(nsClient(1, "gfx", 10*sec, 1)), drmSet(nsClient(1, "gfx", sec/10, 1)), "gfx", 0},
		{"a client created during the interval counts from zero", drmSet(nsClient(1, "gfx", 10*sec, 1)),
			drmSet(nsClient(1, "gfx", 10*sec+sec/4, 1), nsClient(2, "gfx", sec/4, 1)), "gfx", 50},
		{"a new client already past the interval is ignored", drmSet(nsClient(1, "gfx", 10*sec, 1)),
			drmSet(nsClient(1, "gfx", 10*sec, 1), nsClient(3, "gfx", 90*sec, 1)), "gfx", 0},
		{"a closed client produces no negative delta", drmSet(nsClient(1, "gfx", 10*sec, 1), nsClient(2, "gfx", 50*sec, 1)),
			drmSet(nsClient(1, "gfx", 10*sec+sec/10, 1)), "gfx", 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.want, drmEngineUtilization(tt.prev, tt.cur, time.Second)[tt.engine], 0.001)
		})
	}
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
	assert.Empty(t, gpuNodes(pciRoot, gpus, []string{"10de:2786"}).nvidia, "a suspended NVIDIA GPU is not monitored")
}
