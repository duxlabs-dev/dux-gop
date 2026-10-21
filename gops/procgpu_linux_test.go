//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const amdgpuEngineClient = "drm-driver:\tamdgpu\ndrm-client-id:\t7\ndrm-pdev:\t0000:0c:00.0\ndrm-engine-gfx:\t500 ns\ndrm-resident-vram:\t2048 KiB\n"

func writeFDLink(t *testing.T, procRoot string, pid, fd int, target string) {
	t.Helper()
	dir := filepath.Join(procRoot, itoa(pid), "fd")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, itoa(fd))))
}

func readGPU(r *procGPUReader, procRoot string, pid int32) procGPU {
	g, _ := processExtras{gpu: r}.readFDs(procRoot, pid)
	return g
}

func TestProcGPUReader_TriState(t *testing.T) {
	procRoot := t.TempDir()
	nodes := map[string]gpuNodeState{"renderD128": gpuNodeRead, "renderD129": gpuNodeRead}

	writeDRMClient(t, procRoot, 1, 4, "renderD128", amdgpuEngineClient)
	// Same client through a second fd must count once.
	writeDRMClient(t, procRoot, 1, 5, "renderD128", amdgpuEngineClient)
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "2", "fd"), 0o755))
	// pid 3: no fd dir.
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "3"), 0o755))
	writeDRMClient(t, procRoot, 4, 3, "renderD129", "drm-driver:\tnvidia-drm\ndrm-client-id:\t1\ndrm-pdev:\t0000:01:00.0\n")
	// pid 5: CUDA process, only /dev/nvidia* fds.
	writeFDLink(t, procRoot, 5, 3, "/dev/nvidiactl")
	writeFDLink(t, procRoot, 5, 4, "/dev/nvidia0")
	// pid 6: listed by nvidia-smi, no GPU fds visible.
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "6", "fd"), 0o755))
	// pid 7: nvidiactl alone is not a GPU.
	writeFDLink(t, procRoot, 7, 3, "/dev/nvidiactl")
	// pid 8: nvidia fd plus an engine-reporting client.
	writeDRMClient(t, procRoot, 8, 4, "renderD128", amdgpuEngineClient)
	writeFDLink(t, procRoot, 8, 5, "/dev/nvidia0")

	r := &procGPUReader{gpuNodeSet: gpuNodeSet{nodes: nodes, nvidia: []string{"0000:01:00.0"}},
		nvidiaVRAM: map[int32]uint64{4: 100, 3: 5, 6: 0}}
	read := func(pid int32) procGPU { return readGPU(r, procRoot, pid) }

	a := read(1)
	assert.True(t, a.Readable && a.UsageKnown)
	assert.Equal(t, uint64(2<<20), a.VRAMBytes)
	require.Len(t, a.Clients, 1)
	assert.Equal(t, []gpuCursorClient{{PDev: "0000:0c:00.0", ID: 7, Engines: map[string][3]uint64{"gfx": {500, 0, 0}}}}, cursorClients(a.Clients))

	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, read(2))
	assert.Equal(t, procGPU{}, read(3), "unreadable stays unknown even with nvidia-smi data")

	d := read(4)
	assert.Equal(t, procGPU{Readable: true, VRAMBytes: 100}, d)

	assert.Equal(t, procGPU{Readable: true}, read(5), "nvidia fd without engine clients")
	assert.Equal(t, procGPU{Readable: true}, read(6), "listed by nvidia-smi without engine clients")
	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, read(7))
	h := read(8)
	assert.False(t, h.UsageKnown, "hybrid: nvidia fd plus iGPU client is unknown")
	assert.Len(t, h.Clients, 1, "iGPU client is still kept")
}

func TestProcGPUReader_UnmonitoredAndUnclassified(t *testing.T) {
	procRoot := t.TempDir()
	writeFDLink(t, procRoot, 1, 3, "/dev/nvidia0")
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "2", "fd"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "3"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "4", "fd"), 0o755))

	r := &procGPUReader{}
	assert.Equal(t, procGPU{Unmonitored: true}, readGPU(r, procRoot, 1), "an NVIDIA GPU that is excluded or asleep is not 0")
	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, readGPU(r, procRoot, 2))
	assert.Equal(t, procGPU{}, readGPU(r, procRoot, 3))

	writeFDLink(t, procRoot, 4, 3, "/dev/dri/renderD130")
	r = &procGPUReader{gpuNodeSet: gpuNodeSet{nodes: map[string]gpuNodeState{"renderD128": gpuNodeRead}}}
	assert.Equal(t, procGPU{Readable: true}, readGPU(r, procRoot, 4), "a GPU we don't classify is unknown usage, not 0")
}

func TestBuildProcGPUReader_AllowList(t *testing.T) {
	pciRoot := t.TempDir()
	for slot, node := range map[string]string{"0000:0c:00.0": "renderD128", "0000:01:00.0": "renderD129"} {
		require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, slot, "drm", node), 0o755))
	}
	detected := []models.GPU{
		{BusId: "0000:0c:00.0", PciId: "1002:744c", Driver: "amdgpu"},
		{BusId: "0000:01:00.0", PciId: "10de:2786", Driver: "nvidia"},
	}
	smi := 0
	var queried []string
	query := func(ids []string) map[int32]uint64 { smi++; queried = ids; return map[int32]uint64{} }

	r := buildProcGPUReader(pciRoot, detected, []string{"1002:744c", "10de:2786"}, query)
	assert.Equal(t, map[string]gpuNodeState{"renderD128": gpuNodeRead, "renderD129": gpuNodeRead}, r.nodes)
	assert.Equal(t, 1, smi)
	assert.Equal(t, []string{"0000:01:00.0"}, queried)

	smi = 0
	for _, ids := range [][]string{nil, {}} {
		r = buildProcGPUReader(pciRoot, detected, ids, query)
		assert.Equal(t, map[string]gpuNodeState{"renderD128": gpuNodeExcluded, "renderD129": gpuNodeExcluded}, r.nodes, "omitted or empty reads no GPU")
	}
	assert.Equal(t, 0, smi)

	r = buildProcGPUReader(pciRoot, detected, []string{"1002:744c"}, query)
	assert.Equal(t, map[string]gpuNodeState{"renderD128": gpuNodeRead, "renderD129": gpuNodeExcluded}, r.nodes)
	assert.Empty(t, r.nvidia)
	assert.Equal(t, 0, smi, "nvidia-smi must not run when no NVIDIA GPU is enabled")

	r = buildProcGPUReader(pciRoot, detected, []string{"10de:2786"}, query)
	assert.Equal(t, map[string]gpuNodeState{"renderD128": gpuNodeExcluded, "renderD129": gpuNodeRead}, r.nodes)
	assert.Equal(t, []string{"0000:01:00.0"}, r.nvidia)
	assert.Equal(t, 1, smi)
}

func TestProcGPUReader_ExcludedGPUIsUnknown(t *testing.T) {
	pciRoot := t.TempDir()
	for slot, node := range map[string]string{"0000:0c:00.0": "renderD128", "0000:00:02.0": "renderD129"} {
		require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, slot, "drm", node), 0o755))
	}
	detected := []models.GPU{
		{BusId: "0000:0c:00.0", PciId: "1002:744c", Driver: "amdgpu"},
		{BusId: "0000:00:02.0", PciId: "8086:a780", Driver: "i915"},
	}
	procRoot := t.TempDir()
	writeDRMClient(t, procRoot, 1, 4, "renderD129", i915Client(1, 10, 10)) // excluded iGPU
	writeDRMClient(t, procRoot, 2, 4, "renderD128", amdgpuEngineClient)    // enabled dGPU
	require.NoError(t, os.MkdirAll(filepath.Join(procRoot, "3", "fd"), 0o755))
	// pid 4: excluded GPU through a bare link; fdinfo must never be needed.
	writeFDLink(t, procRoot, 4, 3, "/dev/dri/card1")

	read := func(ids []string, pid int32) procGPU {
		return readGPU(buildProcGPUReader(pciRoot, detected, ids, func([]string) map[int32]uint64 { return nil }), procRoot, pid)
	}
	dGPU := []string{"1002:744c"}

	assert.Equal(t, procGPU{Unmonitored: true}, read(dGPU, 1), "excluded GPU is unknown, not 0")
	assert.True(t, read(dGPU, 2).UsageKnown, "enabled GPU is measured")
	assert.Equal(t, uint64(2<<20), read(dGPU, 2).VRAMBytes)
	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, read(dGPU, 3), "no GPU fd stays known 0")

	require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, "0000:00:02.0", "drm", "card1"), 0o755))
	assert.Equal(t, procGPU{Unmonitored: true}, read(dGPU, 4), "link alone marks the process unknown")

	all := []string{"1002:744c", "8086:a780"}
	assert.True(t, read(all, 1).UsageKnown, "every named GPU is measured")
	assert.True(t, read(all, 2).UsageKnown)
	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, read(all, 3))

	writeDRMClient(t, procRoot, 5, 4, "renderD128", amdgpuEngineClient)
	writeFDLink(t, procRoot, 5, 5, "/dev/dri/renderD129")
	assert.Equal(t, procGPU{Unmonitored: true}, read(dGPU, 5), "an excluded GPU wins over a partial sum")
}

func TestProcGPUReader_SuspendedGPUIsIdle(t *testing.T) {
	pciRoot := t.TempDir()
	slot := "0000:0c:00.0"
	require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, slot, "drm", "renderD128"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, slot, "power"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pciRoot, slot, "power", "runtime_status"), []byte("suspended\n"), 0o644))
	procRoot := t.TempDir()
	writeDRMClient(t, procRoot, 1, 4, "renderD128", amdgpuEngineClient)
	// An fdinfo that cannot be opened: reading it would fail the scan or make the process readable.
	fdinfo := filepath.Join(procRoot, "1", "fdinfo", "4")
	require.NoError(t, os.Remove(fdinfo))
	require.NoError(t, os.Mkdir(fdinfo, 0o755))
	detected := []models.GPU{{BusId: slot, PciId: "1002:744c", Driver: "amdgpu"}}
	r := buildProcGPUReader(pciRoot, detected, []string{"1002:744c"}, func([]string) map[int32]uint64 { return nil })
	assert.Equal(t, procGPU{Readable: true, UsageKnown: true}, readGPU(r, procRoot, 1), "a runtime-suspended GPU is idle: 0, not unknown")
}

func TestSetProcessExtras_UnmonitoredReason(t *testing.T) {
	var info models.ProcessInfo
	setProcessExtras(&info, nil, &processSample{}, procGPU{Unmonitored: true})
	assert.True(t, info.GPUUnmonitored)
	assert.Nil(t, info.GPU)
	assert.Nil(t, info.VRAMKB)

	info = models.ProcessInfo{}
	prev := &processSample{ProcessCursorData: models.ProcessCursorData{Timestamp: 1000}, gpuOK: true}
	cur := &processSample{ProcessCursorData: models.ProcessCursorData{Timestamp: 2000}, gpuOK: true}
	setProcessExtras(&info, prev, cur, procGPU{Unmonitored: true})
	assert.True(t, info.GPUUnmonitored)
	assert.Nil(t, info.GPU, "a previous sample must not turn unmonitored into 0%")
	assert.Nil(t, info.VRAMKB)
}

func writeDRMClient(t *testing.T, procRoot string, pid, fd int, node, fdinfo string) {
	t.Helper()
	pidDir := filepath.Join(procRoot, itoa(pid))
	require.NoError(t, os.MkdirAll(filepath.Join(pidDir, "fd"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pidDir, "fdinfo"), 0o755))
	link := filepath.Join(pidDir, "fd", itoa(fd))
	_ = os.Remove(link)
	require.NoError(t, os.Symlink("/dev/dri/"+node, link))
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "fdinfo", itoa(fd)), []byte(fdinfo), 0o644))
}

func itoa(n int) string { return strconv.Itoa(n) }

func i915Client(id int, renderNs, videoNs int64) string {
	return "drm-driver:\ti915\ndrm-client-id:\t" + itoa(id) + "\ndrm-pdev:\t0000:00:02.0\n" +
		"drm-engine-render:\t" + strconv.FormatInt(renderNs, 10) + " ns\ndrm-engine-video:\t" + strconv.FormatInt(videoNs, 10) + " ns\n"
}

func TestBuildProcGPUReader_NvidiaQueriesOnlyActiveCards(t *testing.T) {
	pciRoot := t.TempDir()
	for _, slot := range []string{"0000:01:00.0", "0000:02:00.0", "0000:03:00.0"} {
		require.NoError(t, os.MkdirAll(filepath.Join(pciRoot, slot, "power"), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(pciRoot, "0000:02:00.0", "power", "runtime_status"), []byte("suspended\n"), 0o644))
	detected := []models.GPU{
		{BusId: "0000:01:00.0", PciId: "10de:2786", Driver: "nvidia"},
		{BusId: "0000:02:00.0", PciId: "10de:2786", Driver: "nvidia"},
		{BusId: "0000:03:00.0", PciId: "10de:1234", Driver: "nvidia"},
	}
	var queried []string
	buildProcGPUReader(pciRoot, detected, []string{"10de:2786"}, func(ids []string) map[int32]uint64 {
		queried = ids
		return map[int32]uint64{}
	})
	assert.Equal(t, []string{"0000:01:00.0"}, queried, "suspended and excluded NVIDIA GPUs are not queried")
}
