//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
)

const cpuSysRoot = "/sys/devices/system/cpu"

var cpuDirRe = regexp.MustCompile(`^cpu(\d+)$`)

// cpuDirs maps logical CPU number to its sysfs directory, and returns the highest number.
func cpuDirs(root string) (map[int]string, int) {
	entries, _ := os.ReadDir(root)
	dirs := map[int]string{}
	highest := -1
	for _, e := range entries {
		m := cpuDirRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		dirs[n] = filepath.Join(root, e.Name())
		highest = max(highest, n)
	}
	return dirs, highest
}

func readCPUDynamic(root string) cpuDynamic {
	dirs, highest := cpuDirs(root)
	var d cpuDynamic
	for n := 0; n <= highest; n++ {
		dir, ok := dirs[n]
		if !ok {
			if d.coreMHz != nil {
				d.coreMHz = append(d.coreMHz, 0)
			}
			continue
		}
		freq := filepath.Join(dir, "cpufreq")
		khz, ok := readInt(filepath.Join(freq, "scaling_cur_freq"))
		if ok && d.coreMHz == nil {
			d.coreMHz = make([]float64, n, highest+1)
		}
		if d.coreMHz != nil {
			d.coreMHz = append(d.coreMHz, float64(khz)/1000)
		}
		if d.governor == "" {
			d.governor = readTrimmed(filepath.Join(freq, "scaling_governor"))
		}
		if d.epp == "" {
			d.epp = readTrimmed(filepath.Join(freq, "energy_performance_preference"))
		}
	}
	return d
}

func readCPUStatic(root string) cpuStatic {
	dirs, _ := cpuDirs(root)
	var s cpuStatic
	cores := map[[2]int64]struct{}{}
	packages := map[int64]struct{}{}
	seen := map[string]struct{}{}
	cache := models.CPUCache{}
	var haveCache bool

	for _, dir := range dirs {
		freq := filepath.Join(dir, "cpufreq")
		if khz, ok := readInt(filepath.Join(freq, "cpuinfo_min_freq")); ok {
			if mhz := float64(khz) / 1000; s.minMHz == 0 || mhz < s.minMHz {
				s.minMHz = mhz
			}
		}
		if khz, ok := readInt(filepath.Join(freq, "cpuinfo_max_freq")); ok {
			s.maxMHz = max(s.maxMHz, float64(khz)/1000)
		}
		if mhz, ok := readInt(filepath.Join(dir, "acpi_cppc", "nominal_freq")); ok {
			s.baseMHz = max(s.baseMHz, float64(mhz))
		} else if khz, ok := readInt(filepath.Join(freq, "base_frequency")); ok {
			s.baseMHz = max(s.baseMHz, float64(khz)/1000)
		}

		pkg, ok1 := readInt(filepath.Join(dir, "topology", "physical_package_id"))
		core, ok2 := readInt(filepath.Join(dir, "topology", "core_id"))
		if ok1 && ok2 {
			cores[[2]int64{pkg, core}] = struct{}{}
		}
		if ok1 {
			packages[pkg] = struct{}{}
		}

		idx, _ := filepath.Glob(filepath.Join(dir, "cache", "index*"))
		for _, ix := range idx {
			level, ok := readInt(filepath.Join(ix, "level"))
			typ := readTrimmed(filepath.Join(ix, "type"))
			shared := readTrimmed(filepath.Join(ix, "shared_cpu_list"))
			sizeStr := readTrimmed(filepath.Join(ix, "size"))
			if !ok || sizeStr == "" {
				continue
			}
			key := strconv.FormatInt(level, 10) + "/" + typ + "/" + shared
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			size := parseCacheSize(sizeStr)
			switch {
			case level == 1 && typ == "Data":
				cache.L1d += size
			case level == 1 && typ == "Instruction":
				cache.L1i += size
			case level == 2:
				cache.L2 += size
			case level == 3:
				cache.L3 += size
			default:
				continue
			}
			haveCache = true
		}
	}

	s.physicalCores = len(cores)
	s.sockets = len(packages)
	if haveCache {
		s.cache = &cache
	}
	return s
}

// parseCacheSize converts sysfs sizes such as "32768K" or "1M" to bytes.
func parseCacheSize(s string) uint64 {
	mult := uint64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult = 1 << 10
	case strings.HasSuffix(s, "M"):
		mult = 1 << 20
	case strings.HasSuffix(s, "G"):
		mult = 1 << 30
	}
	n, err := strconv.ParseUint(strings.TrimRight(s, "KMG"), 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}

func readCPUStaticLive() cpuStatic   { return readCPUStatic(cpuSysRoot) }
func readCPUDynamicLive() cpuDynamic { return readCPUDynamic(cpuSysRoot) }
