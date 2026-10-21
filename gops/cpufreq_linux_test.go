//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSys(t *testing.T, root, rel, value string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(value+"\n"), 0o644))
}

func writeCache(t *testing.T, root string, cpu, idx int, level, typ, size, shared string) {
	base := filepath.Join("cpu"+strconv.Itoa(cpu), "cache", "index"+strconv.Itoa(idx))
	writeSys(t, root, filepath.Join(base, "level"), level)
	writeSys(t, root, filepath.Join(base, "type"), typ)
	writeSys(t, root, filepath.Join(base, "size"), size)
	writeSys(t, root, filepath.Join(base, "shared_cpu_list"), shared)
}

func TestReadCPUFreqFixture(t *testing.T) {
	root := t.TempDir()
	cores := []string{"0", "0", "1", "1"}
	for i, core := range cores {
		c := "cpu" + strconv.Itoa(i)
		writeSys(t, root, c+"/topology/physical_package_id", "0")
		writeSys(t, root, c+"/topology/core_id", core)
		if i == 3 {
			continue // offline: no cpufreq dir
		}
		writeSys(t, root, c+"/cpufreq/scaling_cur_freq", "3000000")
		writeSys(t, root, c+"/cpufreq/cpuinfo_min_freq", "400000")
		writeSys(t, root, c+"/cpufreq/cpuinfo_max_freq", "5000000")
		writeSys(t, root, c+"/cpufreq/scaling_governor", "powersave")
		writeSys(t, root, c+"/cpufreq/energy_performance_preference", "balance_performance")
		writeSys(t, root, c+"/acpi_cppc/nominal_freq", "3701")
	}
	writeSys(t, root, "cpu1/cpufreq/scaling_cur_freq", "1500000")
	shared := map[int]string{0: "0-1", 1: "0-1", 2: "2-3", 3: "2-3"}
	for i := 0; i < 4; i++ {
		writeCache(t, root, i, 0, "1", "Data", "32K", shared[i])
		writeCache(t, root, i, 1, "1", "Instruction", "32K", shared[i])
		writeCache(t, root, i, 2, "2", "Unified", "1024K", shared[i])
		writeCache(t, root, i, 3, "3", "Unified", "32768K", "0-3")
	}

	d := readCPUDynamic(root)
	assert.Equal(t, []float64{3000, 1500, 3000, 0}, d.coreMHz)
	assert.Equal(t, "powersave", d.governor)
	assert.Equal(t, "balance_performance", d.epp)

	s := readCPUStatic(root)
	assert.Equal(t, 2, s.physicalCores)
	assert.Equal(t, 1, s.sockets)
	assert.Equal(t, 3701.0, s.baseMHz)
	assert.Equal(t, 400.0, s.minMHz)
	assert.Equal(t, 5000.0, s.maxMHz)
	require.NotNil(t, s.cache)
	assert.Equal(t, uint64(2*32<<10), s.cache.L1d)
	assert.Equal(t, uint64(2*32<<10), s.cache.L1i)
	assert.Equal(t, uint64(2<<20), s.cache.L2)
	assert.Equal(t, uint64(32<<20), s.cache.L3)
}

func TestGetCPUInfoDoesNotShareCache(t *testing.T) {
	cpuTracker.mu.Lock()
	cpuTracker.dynamic.coreMHz = []float64{1000}
	cpuTracker.static.cache = &models.CPUCache{L1d: 1}
	cpuTracker.modelCached, cpuTracker.freqLastRead = true, time.Now()
	cpuTracker.mu.Unlock()
	t.Cleanup(func() {
		cpuTracker.mu.Lock()
		cpuTracker.modelCached, cpuTracker.freqLastRead = false, time.Time{}
		cpuTracker.mu.Unlock()
	})

	info, err := NewGopsUtil().GetCPUInfo()
	require.NoError(t, err)
	info.CoreFrequencies[0], info.Cache.L1d = 0, 0

	info, err = NewGopsUtil().GetCPUInfo()
	require.NoError(t, err)
	assert.Equal(t, []float64{1000}, info.CoreFrequencies)
	assert.Equal(t, uint64(1), info.Cache.L1d)
}
