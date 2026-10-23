//go:build linux

package gops

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

type drmEngine struct {
	BusyNs      uint64
	HasBusy     bool
	Cycles      uint64
	TotalCycles uint64
	Capacity    uint64
}

type drmClient struct {
	Driver    string
	PDev      string
	ID        uint64
	Engines   map[string]drmEngine
	VRAMBytes uint64
	GTTBytes  uint64
}

type drmClientKey struct {
	PDev string
	ID   uint64
}

type drmClientSet map[drmClientKey]drmClient

func (self drmClient) key() drmClientKey {
	return drmClientKey{PDev: self.PDev, ID: self.ID}
}

func drmDriverReportsEngines(driver string) bool {
	return driver == "amdgpu" || driver == "i915" || driver == "xe"
}

func parseDRMFdinfo(text string) (drmClient, bool) {
	client := drmClient{Engines: map[string]drmEngine{}}
	hasID := false
	var vram, legacyVRAM, gtt, legacyGTT uint64
	hasVRAM, hasGTT := false, false

	engine := func(name string) drmEngine {
		e := client.Engines[name]
		if e.Capacity == 0 {
			e.Capacity = 1
		}
		return e
	}

	for line := range strings.SplitSeq(text, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "drm-driver":
			client.Driver = value
			continue
		case "drm-pdev":
			client.PDev = value
			continue
		case "drm-client-id":
			id, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return drmClient{}, false
			}
			client.ID, hasID = id, true
			continue
		}

		n, ok := parseDRMValue(value)
		if !ok {
			continue
		}

		switch {
		case strings.HasPrefix(key, "drm-engine-capacity-"):
			name := strings.TrimPrefix(key, "drm-engine-capacity-")
			e := engine(name)
			if n > 0 {
				e.Capacity = n
			}
			client.Engines[name] = e
		case strings.HasPrefix(key, "drm-engine-"):
			name := strings.TrimPrefix(key, "drm-engine-")
			e := engine(name)
			e.BusyNs, e.HasBusy = n, true
			client.Engines[name] = e
		case strings.HasPrefix(key, "drm-total-cycles-"):
			name := strings.TrimPrefix(key, "drm-total-cycles-")
			e := engine(name)
			e.TotalCycles = n
			client.Engines[name] = e
		case strings.HasPrefix(key, "drm-cycles-"):
			name := strings.TrimPrefix(key, "drm-cycles-")
			e := engine(name)
			e.Cycles = n
			client.Engines[name] = e
		case strings.HasPrefix(key, "drm-resident-vram"), strings.HasPrefix(key, "drm-resident-local"):
			vram += n
			hasVRAM = true
		case key == "drm-memory-vram":
			legacyVRAM = n
		case key == "drm-resident-gtt":
			gtt, hasGTT = n, true
		case key == "drm-memory-gtt":
			legacyGTT = n
		}
	}

	if !hasID {
		return drmClient{}, false
	}
	if hasVRAM {
		client.VRAMBytes = vram
	} else {
		client.VRAMBytes = legacyVRAM
	}
	if hasGTT {
		client.GTTBytes = gtt
	} else {
		client.GTTBytes = legacyGTT
	}
	return client, true
}

func parseDRMValue(value string) (uint64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if len(fields) == 1 {
		return n, true
	}
	switch fields[1] {
	case "KiB":
		n <<= 10
	case "MiB":
		n <<= 20
	case "GiB":
		n <<= 30
	}
	return n, true
}

// drmEngineUtilization returns busy percent per engine between two samples,
// summed over clients and normalized by engine capacity. A client or engine
// missing from prev counts from zero only if its counter fits in the interval.
func drmEngineUtilization(prev, cur drmClientSet, elapsed time.Duration) map[string]float64 {
	result := map[string]float64{}
	if elapsed <= 0 {
		return result
	}

	for key, client := range cur {
		prevClient, hadClient := prev[key]
		for name, e := range client.Engines {
			p, hadEngine := prevClient.Engines[name]
			hadEngine = hadClient && hadEngine
			capacity := float64(max(e.Capacity, 1))

			var busy float64
			switch {
			case e.HasBusy:
				switch {
				case hadEngine && e.BusyNs >= p.BusyNs:
					busy = float64(e.BusyNs-p.BusyNs) / float64(elapsed.Nanoseconds())
				case !hadEngine && float64(e.BusyNs) <= float64(elapsed.Nanoseconds())*capacity:
					busy = float64(e.BusyNs) / float64(elapsed.Nanoseconds())
				}
			case e.TotalCycles > 0:
				// Total cycles is a device clock, so a new client has no baseline.
				if hadEngine && e.Cycles >= p.Cycles && e.TotalCycles > p.TotalCycles {
					busy = float64(e.Cycles-p.Cycles) / float64(e.TotalCycles-p.TotalCycles)
				}
			}
			result[name] += busy * 100 / capacity
		}
	}
	return result
}

// videoEnginePrefixes name the encode/decode engines: amdgpu enc, dec, jpeg
// and vcn; i915 video and video-enhance; xe vcs and vecs.
var videoEnginePrefixes = []string{"enc", "dec", "jpeg", "vcn", "video", "vcs", "vecs"}

func maxVideoEngineUtilization(engines map[string]float64) float64 {
	var best float64
	for name, v := range engines {
		if slices.ContainsFunc(videoEnginePrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			best = max(best, v)
		}
	}
	return min(best, 100)
}

func maxEngineUtilization(engines map[string]float64) float64 {
	var best float64
	for _, v := range engines {
		best = max(best, v)
	}
	return min(best, 100)
}
