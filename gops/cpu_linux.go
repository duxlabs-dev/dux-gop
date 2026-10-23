//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func getCPUTemperatureCached() float64 {
	if cpuTracker.tempPath != "" {
		tempBytes, err := os.ReadFile(cpuTracker.tempPath)
		if err != nil {
			cpuTracker.tempPath = ""
			return getCPUTemperatureCached()
		}
		temp, err := strconv.Atoi(strings.TrimSpace(string(tempBytes)))
		if err != nil {
			cpuTracker.tempPath = ""
			return getCPUTemperatureCached()
		}
		return float64(temp) / 1000.0
	}

	hwmonPath := "/sys/class/hwmon"
	entries, err := os.ReadDir(hwmonPath)
	if err != nil {
		return getACPITZFallback()
	}

	for _, entry := range entries {
		// CPU temp sensors are never on i2c; skip to avoid blocking on a held bus (e.g. OpenRGB).
		if target, err := os.Readlink(filepath.Join(hwmonPath, entry.Name())); err == nil && strings.Contains(target, "/i2c-") {
			continue
		}

		nameBytes, err := os.ReadFile(filepath.Join(hwmonPath, entry.Name(), "name"))
		if err != nil {
			continue
		}

		switch strings.TrimSpace(string(nameBytes)) {
		case "coretemp", "k10temp", "k8temp", "cpu_thermal", "zenpower":
		default:
			continue
		}

		temp, tempPath, ok := readCoretempInput(filepath.Join(hwmonPath, entry.Name()))
		if !ok {
			continue
		}
		cpuTracker.tempPath = tempPath
		return temp
	}

	return getACPITZFallback()
}

// readCoretempInput reads a CPU temperature from a coretemp hwmon directory,
// preferring the package sensor (temp1_input). Older CPUs (e.g. Nehalem i7 9xx)
// lack it and expose only per-core inputs, so fall back to the hottest core.
func readCoretempInput(dir string) (float64, string, bool) {
	pkgPath := filepath.Join(dir, "temp1_input")
	if temp, ok := readMilliCelsius(pkgPath); ok {
		return temp, pkgPath, true
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
	var bestTemp float64
	var bestPath string
	for _, path := range matches {
		temp, ok := readMilliCelsius(path)
		if !ok || (bestPath != "" && temp <= bestTemp) {
			continue
		}
		bestTemp, bestPath = temp, path
	}
	if bestPath == "" {
		return 0, "", false
	}
	return bestTemp, bestPath, true
}

func readMilliCelsius(path string) (float64, bool) {
	temp, err := strconv.Atoi(readTrimmed(path))
	if err != nil {
		return 0, false
	}
	return float64(temp) / 1000.0, true
}

func getACPITZFallback() float64 {
	thermalPath := thermalRoot
	thermalEntries, err := os.ReadDir(thermalPath)
	if err != nil {
		return 0
	}
	return getMaxACPITZTemperature(thermalPath, thermalEntries, 20, 100, true)
}

func getCurrentCPUFreq() float64 {
	cpuinfoBytes, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(cpuinfoBytes), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				freq, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					return freq
				}
			}
		}
	}

	freqBytes, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")
	if err == nil {
		freq, err := strconv.Atoi(strings.TrimSpace(string(freqBytes)))
		if err == nil {
			return float64(freq) / 1000.0
		}
	}

	return 0
}

func getMaxACPITZTemperature(thermalPath string, thermalEntries []os.DirEntry, minTemp, maxTemp float64, isCPU bool) float64 {
	var highestTemp float64

	for _, entry := range thermalEntries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}

		thermalType, err := readThermalType(thermalPath, entry.Name())
		if err != nil {
			continue
		}

		if thermalType != "acpitz" {
			continue
		}

		temp, tempPath, err := readThermalTemp(thermalPath, entry.Name())
		if err != nil {
			continue
		}

		if temp < minTemp || temp > maxTemp {
			continue
		}

		if temp > highestTemp {
			highestTemp = temp
			if isCPU {
				cpuTracker.tempPath = tempPath
			}
		}
	}

	return highestTemp
}

func readThermalType(thermalPath, entryName string) (string, error) {
	typePath := filepath.Join(thermalPath, entryName, "type")
	typeBytes, err := os.ReadFile(typePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(typeBytes)), nil
}

// cpuUsageFromProvider on Linux uses the tick-ratio approach, which is accurate
// because /proc/stat always includes all cores (even idle ones).
func cpuUsageFromProvider(_ CPUInfoProvider, cursorTotal, currentTotal []float64, _ float64, _ int) (float64, []float64) {
	return calculateCPUPercentage(cursorTotal, currentTotal), nil
}

func primeCPUPercent() {}

func readThermalTemp(thermalPath, entryName string) (float64, string, error) {
	tempPath := filepath.Join(thermalPath, entryName, "temp")
	tempBytes, err := os.ReadFile(tempPath)
	if err != nil {
		return 0, "", err
	}

	temp, err := strconv.Atoi(strings.TrimSpace(string(tempBytes)))
	if err != nil {
		return 0, "", err
	}

	tempC := float64(temp) / 1000.0
	return tempC, tempPath, nil
}
