package gops

import (
	"strings"
	"time"
)

// processGPUSample is one scan of per-process GPU activity.
//
// Counters hold DRM fdinfo counters keyed "pdev/client/engine/kind", where kind
// is "ns" (drm-engine), "cyc" (drm-cycles) or "tot" (drm-total-cycles).
// Capacity uses the same keys as the "ns" counters. Direct holds usage a
// driver already reports as a percentage (nvidia-smi pmon), keyed by GPU.
type processGPUSample struct {
	TimeMs   int64
	Clients  map[int32]bool
	Counters map[int32]map[string]uint64
	Capacity map[int32]map[string]uint64
	Direct   map[int32]map[string]float64
	MemoryKB map[int32]uint64
}

func newProcessGPUSample(now time.Time) processGPUSample {
	return processGPUSample{
		TimeMs:   now.UnixMilli(),
		Clients:  map[int32]bool{},
		Counters: map[int32]map[string]uint64{},
		Capacity: map[int32]map[string]uint64{},
		Direct:   map[int32]map[string]float64{},
		MemoryKB: map[int32]uint64{},
	}
}

func (s processGPUSample) setCounter(pid int32, key string, value uint64) {
	if s.Counters[pid] == nil {
		s.Counters[pid] = map[string]uint64{}
	}
	s.Counters[pid][key] = value
}

// processGPUUsage returns each client process's busiest GPU in percent. Engines
// and clients of one process on the same GPU add up, capped at 100. Without a
// previous scan only directly reported usage is known.
func processGPUUsage(prev, cur processGPUSample) map[int32]float64 {
	usage := map[int32]float64{}
	elapsedNs := float64(cur.TimeMs-prev.TimeMs) * 1e6
	haveBaseline := prev.TimeMs > 0 && elapsedNs > 0

	for pid := range cur.Clients {
		perGPU := map[string]float64{}
		for gpu, pct := range cur.Direct[pid] {
			perGPU[gpu] += pct
		}

		if haveBaseline {
			counters := cur.Counters[pid]
			for key, value := range counters {
				gpu, kind := splitGPUCounterKey(key)
				delta := counterDelta(prev.Counters[pid][key], value)
				switch kind {
				case "ns":
					capacity := cur.Capacity[pid][key]
					if capacity == 0 {
						capacity = 1
					}
					perGPU[gpu] += 100 * float64(delta) / (elapsedNs * float64(capacity))
				case "cyc":
					totalKey := strings.TrimSuffix(key, "/cyc") + "/tot"
					total := counterDelta(prev.Counters[pid][totalKey], counters[totalKey])
					if total > 0 {
						perGPU[gpu] += 100 * float64(delta) / float64(total)
					}
				}
			}
		} else if len(cur.Direct[pid]) == 0 {
			continue
		}

		busiest := 0.0
		for _, pct := range perGPU {
			busiest = max(busiest, min(100, pct))
		}
		usage[pid] = busiest
	}
	return usage
}

// The DRM usage-stats ABI allows counters to dip briefly; a reader keeps the
// larger previous value until they catch up.
func counterDelta(prev, cur uint64) uint64 {
	if cur <= prev {
		return 0
	}
	return cur - prev
}

// nextGPUCounters carries the larger of the previous and current value forward.
func nextGPUCounters(prev, cur processGPUSample) map[int32]map[string]uint64 {
	next := map[int32]map[string]uint64{}
	for pid, counters := range cur.Counters {
		next[pid] = make(map[string]uint64, len(counters))
		for key, value := range counters {
			next[pid][key] = max(value, prev.Counters[pid][key])
		}
	}
	return next
}

// splitGPUCounterKey splits "pdev/client/engine/kind" into the GPU and kind.
func splitGPUCounterKey(key string) (gpu, kind string) {
	parts := strings.Split(key, "/")
	if len(parts) != 4 {
		return "", ""
	}
	return parts[0], parts[3]
}
