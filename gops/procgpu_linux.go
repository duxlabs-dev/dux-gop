//go:build linux

package gops

import (
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

type procGPUReader struct {
	gpuNodeSet
	// VRAM bytes per process listed by nvidia-smi; nil when the query failed.
	nvidiaVRAM map[int32]uint64
}

// newProcGPUReader reads only the GPUs named in pciIds, and runs nvidia-smi only for named, awake NVIDIA ones.
func (self *GopsUtil) newProcGPUReader(pciIds []string) *procGPUReader {
	entries, _ := self.detectGPUEntries()
	return buildProcGPUReader(pciDevicesRoot, gpusFromEntries(entries), pciIds, queryNvidiaPIDs)
}

func buildProcGPUReader(pciRoot string, detected []models.GPU, pciIds []string, nvidiaPIDs func(busIds []string) map[int32]uint64) *procGPUReader {
	r := &procGPUReader{gpuNodeSet: gpuNodes(pciRoot, detected, pciIds)}
	if len(r.nvidia) > 0 {
		r.nvidiaVRAM = nvidiaPIDs(r.nvidia)
	}
	return r
}

func (self *procGPUReader) fromScan(pid int32, scan procFDScan) procGPU {
	if scan.ExcludedGPU || scan.NvidiaGPU && len(self.nvidia) == 0 {
		return procGPU{Unmonitored: true}
	}
	if scan.NvidiaGPU && self.nvidiaVRAM == nil {
		return procGPU{}
	}
	vram, listed := self.nvidiaVRAM[pid]
	res := procGPU{Readable: true, VRAMBytes: vram}
	for key, client := range scan.DRM {
		res.VRAMBytes += client.VRAMBytes
		res.GTTBytes += client.GTTBytes
		if !drmDriverReportsEngines(client.Driver) {
			continue
		}
		if res.Clients == nil {
			res.Clients = drmClientSet{}
		}
		res.Clients[key] = client
	}
	// An NVIDIA process is never known: on a hybrid laptop its DRM clients
	// would show only the integrated GPU's share.
	nvidiaProc := scan.NvidiaGPU || listed
	res.UsageKnown = !nvidiaProc && !scan.UnknownGPU && (len(res.Clients) > 0 || len(scan.DRM) == 0)
	return res
}

func procGPUUsage(prev []gpuCursorClient, cur drmClientSet, elapsed time.Duration) (all, video float64) {
	engines := drmEngineUtilization(cursorClientSet(prev), cur, elapsed)
	return maxEngineUtilization(engines), maxVideoEngineUtilization(engines)
}

func cursorClients(clients drmClientSet) []gpuCursorClient {
	result := make([]gpuCursorClient, 0, len(clients))
	for _, client := range clients {
		if c := toCursorClient(client); len(c.Engines) > 0 {
			result = append(result, c)
		}
	}
	return result
}

func toCursorClient(client drmClient) gpuCursorClient {
	c := gpuCursorClient{PDev: client.PDev, ID: client.ID, Engines: map[string][3]uint64{}}
	for name, e := range client.Engines {
		if e.BusyNs|e.Cycles|e.TotalCycles != 0 {
			c.Engines[name] = [3]uint64{e.BusyNs, e.Cycles, e.TotalCycles}
		}
	}
	return c
}

// cursorClientSet restores a previous sample; only HasBusy and Capacity of the current one are used.
func cursorClientSet(clients []gpuCursorClient) drmClientSet {
	set := drmClientSet{}
	for _, c := range clients {
		client := drmClient{PDev: c.PDev, ID: c.ID, Engines: map[string]drmEngine{}}
		for name, v := range c.Engines {
			client.Engines[name] = drmEngine{BusyNs: v[0], HasBusy: true, Cycles: v[1], TotalCycles: v[2], Capacity: 1}
		}
		set[client.key()] = client
	}
	return set
}
