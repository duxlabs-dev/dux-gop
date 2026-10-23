//go:build linux

package gops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/duxlabs-dev/dux-gop/models"
)

func getBIOSInfo() models.BIOSInfo {
	dmip := "/sys/class/dmi/id"
	if _, err := os.Stat(dmip); os.IsNotExist(err) {
		dmip = "/sys/devices/virtual/dmi/id"
	}

	biosInfo := models.BIOSInfo{}

	if vendor, err := readFile(filepath.Join(dmip, "board_vendor")); err == nil {
		biosInfo.Vendor = strings.TrimSpace(vendor)
	} else {
		biosInfo.Vendor = "Unknown"
	}

	var boardName string
	if name, err := readFile(filepath.Join(dmip, "board_name")); err == nil {
		boardName = strings.TrimSpace(name)
	}

	switch {
	case biosInfo.Vendor != "Unknown" && boardName != "":
		biosInfo.Motherboard = biosInfo.Vendor + " " + boardName
	case boardName != "":
		biosInfo.Motherboard = boardName
	default:
		biosInfo.Motherboard = "Unknown"
	}

	if version, err := readFile(filepath.Join(dmip, "bios_version")); err == nil {
		biosInfo.Version = strings.TrimSpace(version)
	} else {
		biosInfo.Version = "Unknown"
	}

	if date, err := readFile(filepath.Join(dmip, "bios_date")); err == nil {
		biosInfo.Date = strings.TrimSpace(date)
	}

	return biosInfo
}

func getDistroName() string {
	content, err := readFile("/etc/os-release")
	if err != nil {
		return "Unknown"
	}

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			distro := strings.TrimPrefix(line, "PRETTY_NAME=")
			distro = strings.Trim(distro, "\"")
			return distro
		}
	}

	return "Unknown"
}

var (
	scanGPUEntriesOnce sync.Once
	scannedGPUEntries  []gpuEntry
	scanGPUEntriesErr  error
)

// Scanning parses pci.ids for every device, so it runs once per process.
func detectGPUEntries() ([]gpuEntry, error) {
	scanGPUEntriesOnce.Do(func() {
		scannedGPUEntries, scanGPUEntriesErr = scanGPUEntries()
	})
	return scannedGPUEntries, scanGPUEntriesErr
}

func scanGPUEntries() ([]gpuEntry, error) {
	devices, err := filepath.Glob(filepath.Join(pciDevicesRoot, "*"))
	if err != nil {
		return nil, err
	}

	var gpuEntries []gpuEntry
	for _, devicePath := range devices {
		classBytes, err := os.ReadFile(filepath.Join(devicePath, "class"))
		if err != nil {
			continue
		}

		class := strings.TrimSpace(string(classBytes))
		if !strings.HasPrefix(class, "0x03") {
			continue
		}

		vendorBytes, err := os.ReadFile(filepath.Join(devicePath, "vendor"))
		if err != nil {
			continue
		}

		deviceBytes, err := os.ReadFile(filepath.Join(devicePath, "device"))
		if err != nil {
			continue
		}

		vendorId := strings.TrimSpace(strings.TrimPrefix(string(vendorBytes), "0x"))
		deviceId := strings.TrimSpace(strings.TrimPrefix(string(deviceBytes), "0x"))
		bdf := filepath.Base(devicePath)
		driver := getGPUDriver(bdf)
		displayName := lookupPCIDevice(vendorId, deviceId)
		vendor := inferVendorFromId(vendorId, driver)
		priority := getPriority(driver, bdf)
		pciId := fmt.Sprintf("%s:%s", vendorId, deviceId)
		rawLine := fmt.Sprintf("%s Display controller: %s [%s]", bdf, displayName, pciId)

		gpuEntries = append(gpuEntries, gpuEntry{
			Priority: priority,
			Driver:   driver,
			Vendor:   vendor,
			RawLine:  rawLine,
			BusId:    bdf,
		})
	}

	sort.Slice(gpuEntries, func(i, j int) bool {
		if gpuEntries[i].Priority != gpuEntries[j].Priority {
			return gpuEntries[i].Priority > gpuEntries[j].Priority
		}
		return gpuEntries[i].Driver < gpuEntries[j].Driver
	})

	return gpuEntries, nil
}

func lookupPCIDevice(vendorId, deviceId string) string {
	pciIdsPaths := []string{
		"/usr/share/hwdata/pci.ids",
		"/usr/share/misc/pci.ids",
		"/var/lib/pciutils/pci.ids",
	}

	var pciIdsPath string
	for _, path := range pciIdsPaths {
		if _, err := os.Stat(path); err == nil {
			pciIdsPath = path
			break
		}
	}

	if pciIdsPath == "" {
		return fmt.Sprintf("GPU %s:%s", vendorId, deviceId)
	}

	content, err := os.ReadFile(pciIdsPath)
	if err != nil {
		return fmt.Sprintf("GPU %s:%s", vendorId, deviceId)
	}

	inVendor := false
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "#") || len(strings.TrimSpace(line)) == 0 {
			continue
		}

		if !strings.HasPrefix(line, "\t") {
			inVendor = strings.HasPrefix(line, vendorId)
			continue
		}

		if !inVendor || strings.HasPrefix(line, "\t\t") {
			continue
		}

		fields := strings.SplitN(strings.TrimPrefix(line, "\t"), " ", 2)
		if len(fields) >= 2 && fields[0] == deviceId {
			return strings.TrimSpace(fields[1])
		}
	}

	return fmt.Sprintf("GPU %s:%s", vendorId, deviceId)
}

func getGPUDriver(bdf string) string {
	driverPath := filepath.Join(pciDevicesRoot, bdf, "driver")
	if link, err := os.Readlink(driverPath); err == nil {
		return filepath.Base(link)
	}
	return ""
}

func getMaxACPITZTemperatureForGPU(thermalPath string, thermalEntries []os.DirEntry, minTemp, maxTemp float64) float64 {
	var highestTemp float64

	for _, entry := range thermalEntries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}

		thermalType, err := readThermalTypeForGPU(thermalPath, entry.Name())
		if err != nil {
			continue
		}

		if thermalType != "acpitz" {
			continue
		}

		temp, err := readThermalTempForGPU(thermalPath, entry.Name())
		if err != nil {
			continue
		}

		if temp < minTemp || temp > maxTemp {
			continue
		}

		if temp > highestTemp {
			highestTemp = temp
		}
	}

	return highestTemp
}

func readThermalTypeForGPU(thermalPath, entryName string) (string, error) {
	typePath := filepath.Join(thermalPath, entryName, "type")
	typeBytes, err := os.ReadFile(typePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(typeBytes)), nil
}

func readThermalTempForGPU(thermalPath, entryName string) (float64, error) {
	tempPath := filepath.Join(thermalPath, entryName, "temp")
	tempBytes, err := os.ReadFile(tempPath)
	if err != nil {
		return 0, err
	}

	temp, err := strconv.Atoi(strings.TrimSpace(string(tempBytes)))
	if err != nil {
		return 0, err
	}

	return float64(temp) / 1000.0, nil
}
