//go:build freebsd

package gops

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/duxlabs-dev/dux-gop/models"
)

func kenvString(name string) string {
	out, err := exec.Command("kenv", "-q", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// smbios.* kenv variables are set by the loader; names per stand/libsa/smbios.c.
func getBIOSInfo() models.BIOSInfo {
	biosInfo := models.BIOSInfo{
		Vendor:  kenvString("smbios.planar.maker"),
		Version: kenvString("smbios.bios.version"),
		Date:    kenvString("smbios.bios.reldate"),
	}

	if biosInfo.Vendor == "" {
		biosInfo.Vendor = "Unknown"
	}
	if biosInfo.Version == "" {
		biosInfo.Version = "Unknown"
	}

	boardName := kenvString("smbios.planar.product")
	switch {
	case biosInfo.Vendor != "Unknown" && boardName != "":
		biosInfo.Motherboard = biosInfo.Vendor + " " + boardName
	case boardName != "":
		biosInfo.Motherboard = boardName
	default:
		biosInfo.Motherboard = "Unknown"
	}

	return biosInfo
}

func getDistroName() string {
	// Generated at boot since FreeBSD 13.0; os-release(5).
	content, err := readFile("/var/run/os-release")
	if err != nil {
		return fallbackDistroName()
	}

	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
	}

	return fallbackDistroName()
}

func fallbackDistroName() string {
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil {
		return "FreeBSD"
	}
	return "FreeBSD " + release
}

func detectGPUEntries() ([]gpuEntry, error) {
	out, err := exec.Command("pciconf", "-lv").Output()
	if err != nil {
		return nil, err
	}

	var gpuEntries []gpuEntry
	var driver, selector, vendorId, deviceId, deviceName string
	isDisplay := false

	flush := func() {
		if !isDisplay {
			return
		}

		displayName := deviceName
		if displayName == "" {
			displayName = fmt.Sprintf("GPU %s:%s", vendorId, deviceId)
		}

		pciId := fmt.Sprintf("%s:%s", vendorId, deviceId)
		gpuEntries = append(gpuEntries, gpuEntry{
			Priority: getPriority(driver, selector),
			Driver:   driver,
			Vendor:   inferVendorFromId(vendorId, driver),
			RawLine:  fmt.Sprintf("%s Display controller: %s [%s]", selector, displayName, pciId),
		})
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			key, value, ok := parsePciconfCaption(line)
			if ok && key == "device" {
				deviceName = value
			}
			continue
		}

		flush()
		driver, selector, vendorId, deviceId, isDisplay = parsePciconfHeader(line)
		deviceName = ""
	}
	flush()

	sort.Slice(gpuEntries, func(i, j int) bool {
		if gpuEntries[i].Priority != gpuEntries[j].Priority {
			return gpuEntries[i].Priority > gpuEntries[j].Priority
		}
		return gpuEntries[i].Driver < gpuEntries[j].Driver
	})

	return gpuEntries, nil
}

// Header format per pciconf(8): dev@selector:\tclass=0x.. vendor=0x.. device=0x..
func parsePciconfHeader(line string) (driver, selector, vendorId, deviceId string, isDisplay bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 || !strings.Contains(fields[0], "@") {
		return "", "", "", "", false
	}

	name, sel, _ := strings.Cut(fields[0], "@")
	selector = strings.TrimSuffix(sel, ":")
	driver = strings.TrimRight(name, "0123456789")
	switch driver {
	case "none", "vgapci":
		driver = ""
	}

	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "class":
			// PCI base class 0x03 is display, per PCI Code and ID Assignment spec.
			isDisplay = strings.HasPrefix(value, "0x03")
		case "vendor":
			vendorId = strings.TrimPrefix(value, "0x")
		case "device":
			deviceId = strings.TrimPrefix(value, "0x")
		}
	}

	return driver, selector, vendorId, deviceId, isDisplay
}

func parsePciconfCaption(line string) (key, value string, ok bool) {
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), "'"), true
}

func getNvidiaTemperature() (float64, string) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return 0, "unknown"
	}

	tempStr := strings.TrimSpace(string(output))
	lines := strings.Split(tempStr, "\n")
	if len(lines) > 0 && lines[0] != "" {
		if temp, err := strconv.ParseFloat(lines[0], 64); err == nil {
			return temp, "nvidia"
		}
	}

	return 0, "unknown"
}

func sampleGPUs(entries []gpuEntry, _ gpuCursor, now time.Time) ([]gpuSample, gpuCursor) {
	samples := make([]gpuSample, len(entries))
	for i, entry := range entries {
		samples[i].Hwmon = "unknown"
		if entry.Driver == "nvidia" {
			samples[i].Temperature, samples[i].Hwmon = getNvidiaTemperature()
		}
	}
	return samples, newGPUCursor(now)
}
