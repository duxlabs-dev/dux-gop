package gops

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/host"
)

func (self *GopsUtil) GetSystemHardware() (*models.SystemHardware, error) {
	info := &models.SystemHardware{}

	cpuTracker.mu.Lock()
	self.loadCPUStatic()
	info.CPU = models.CPUBasic{Count: cpuTracker.cpuCount, Model: cpuTracker.cpuModel}
	cpuTracker.mu.Unlock()

	biosInfo := getBIOSInfo()
	info.BIOS = biosInfo

	hostInfo, err := host.Info()
	if err != nil {
		return nil, err
	}

	info.Kernel = hostInfo.KernelVersion
	info.Hostname = hostInfo.Hostname
	info.Arch = hostInfo.KernelArch
	info.Distro = getDistroName()
	if comm, err := readFile("/proc/1/comm"); err == nil {
		info.Init = strings.TrimSpace(comm)
	}

	return info, nil
}

func (self *GopsUtil) GetGPUInfo() (*models.GPUInfo, error) {
	entries, err := self.detectGPUEntries()
	if err != nil {
		return nil, err
	}

	return &models.GPUInfo{GPUs: gpusFromEntries(entries)}, nil
}

func (self *GopsUtil) GetGPUInfoWithTemp(ids []string) (*models.GPUInfo, error) {
	return self.GetGPUInfoWithStats(ids, "")
}

// GetGPUInfoWithStats samples live metrics only for the GPUs named in ids (PCI
// ID or bus address), so cards nobody asked about are never woken. The cursor
// from the previous call enables metrics derived from counter deltas.
func (self *GopsUtil) GetGPUInfoWithStats(ids []string, cursor string) (*models.GPUInfo, error) {
	entries, err := self.detectGPUEntries()
	if err != nil {
		return nil, err
	}

	gpus := gpusFromEntries(entries)
	info := &models.GPUInfo{GPUs: gpus}
	if len(ids) == 0 {
		return info, nil
	}

	var targets []gpuEntry
	var indexes []int
	for i, gpu := range gpus {
		if gpuRequested(gpu, ids) {
			targets = append(targets, entries[i])
			indexes = append(indexes, i)
		}
	}
	if len(targets) == 0 {
		return info, nil
	}

	samples, next := self.sampleGPUs(targets, decodeGPUCursor(cursor), time.Now())
	for i, sample := range samples {
		sample.applyTo(&gpus[indexes[i]])
	}
	info.Cursor = encodeGPUCursor(next)
	return info, nil
}

func (self *GopsUtil) GetGPUTemp(pciId string) (*models.GPUTempInfo, error) {
	if pciId == "" {
		return nil, fmt.Errorf("pciId is required")
	}

	entries, err := self.detectGPUEntries()
	if err != nil {
		return nil, err
	}

	for i, gpu := range gpusFromEntries(entries) {
		if !gpuRequested(gpu, []string{pciId}) {
			continue
		}
		samples, _ := self.sampleGPUs(entries[i:i+1], gpuCursor{}, time.Now())
		return &models.GPUTempInfo{
			Driver:      entries[i].Driver,
			Hwmon:       samples[0].Hwmon,
			Temperature: samples[0].Temperature,
		}, nil
	}
	return nil, fmt.Errorf("GPU with PCI ID %s not found", pciId)
}

type gpuEntry struct {
	Priority int
	Driver   string
	Vendor   string
	RawLine  string
	BusId    string
}

func inferVendorFromId(vendorId, driver string) string {
	switch vendorId {
	case "10de":
		return "NVIDIA"
	case "1002":
		return "AMD"
	case "8086":
		return "Intel"
	default:
		return inferVendor(driver, "")
	}
}

func gpusFromEntries(entries []gpuEntry) []models.GPU {
	gpus := make([]models.GPU, 0, len(entries))
	for _, entry := range entries {
		displayName, pciId := parseGPUInfo(entry.RawLine)
		gpus = append(gpus, models.GPU{
			Driver:      entry.Driver,
			Vendor:      entry.Vendor,
			DisplayName: displayName,
			FullName:    buildFullName(entry.Vendor, displayName),
			PciId:       pciId,
			RawLine:     entry.RawLine,
			Hwmon:       "unknown",
			BusId:       entry.BusId,
		})
	}
	return gpus
}

func inferVendor(driver, line string) string {
	switch driver {
	case "nvidia", "nouveau":
		return "NVIDIA"
	case "amdgpu", "radeon":
		return "AMD"
	case "i915", "xe":
		return "Intel"
	}

	lineLower := strings.ToLower(line)
	switch {
	case strings.Contains(lineLower, "nvidia"):
		return "NVIDIA"
	case strings.Contains(lineLower, "amd"), strings.Contains(lineLower, "ati"):
		return "AMD"
	case strings.Contains(lineLower, "intel"):
		return "Intel"
	case strings.Contains(lineLower, "apple"):
		return "Apple"
	}

	return "Unknown"
}

func getPriority(driver, bdf string) int {
	switch driver {
	case "nvidia":
		return 3
	case "amdgpu", "radeon":
		parts := strings.Split(bdf, ":")
		if len(parts) >= 3 {
			deviceFunc := parts[2]
			if strings.HasPrefix(deviceFunc, "00.") {
				return 1
			}
		}
		return 2
	case "i915", "xe":
		return 0
	default:
		return 0
	}
}

var (
	gpuPciIDRe        = regexp.MustCompile(`\[([0-9a-f]{4}:[0-9a-f]{4})\]`)
	gpuClassPrefixRe  = regexp.MustCompile(`^[^:]+: `)
	gpuPciIDTailRe    = regexp.MustCompile(`\[[0-9a-f]{4}:[0-9a-f]{4}\].*$`)
	gpuAfterBracketRe = regexp.MustCompile(`\]\s*([^\[]+)$`)
	gpuLastBracketRe  = regexp.MustCompile(`\[([^\]]+)\]([^\[]*$)`)
)

func parseGPUInfo(rawLine string) (displayName, pciId string) {
	if rawLine == "" {
		return "Unknown", ""
	}

	if match := gpuPciIDRe.FindStringSubmatch(rawLine); len(match) > 1 {
		pciId = match[1]
	}

	s := gpuClassPrefixRe.ReplaceAllString(rawLine, "")
	s = gpuPciIDTailRe.ReplaceAllString(s, "")

	if match := gpuAfterBracketRe.FindStringSubmatch(s); len(match) > 1 && strings.TrimSpace(match[1]) != "" {
		displayName = strings.TrimSpace(match[1])
	} else if match := gpuLastBracketRe.FindStringSubmatch(s); len(match) > 1 {
		displayName = match[1]
	} else {
		displayName = s
	}

	displayName = removeVendorPrefixes(displayName)

	if displayName == "" {
		displayName = "Unknown"
	}

	return displayName, pciId
}

func removeVendorPrefixes(name string) string {
	prefixes := []string{
		"NVIDIA Corporation ",
		"NVIDIA ",
		"Advanced Micro Devices, Inc. ",
		"AMD/ATI ",
		"AMD ",
		"ATI ",
		"Intel Corporation ",
		"Intel ",
		"Apple ",
	}

	result := name
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(result), strings.ToLower(prefix)) {
			result = result[len(prefix):]
			break
		}
	}

	return strings.TrimSpace(result)
}

func buildFullName(vendor, displayName string) string {
	if displayName == "Unknown" {
		return displayName
	}

	switch vendor {
	case "NVIDIA":
		return "NVIDIA " + displayName
	case "AMD":
		return "AMD " + displayName
	case "Intel":
		return "Intel " + displayName
	case "Apple":
		return "Apple " + displayName
	default:
		return displayName
	}
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
