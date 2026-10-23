//go:build linux

package gops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

var dmiUdevData = "/run/udev/data/+dmi:id"

// GetMemoryModules reads the DMI memory devices that systemd's udev rules
// export without root as MEMORY_DEVICE_<n>_* properties.
// The result, failure included, is read once per GopsUtil, since modules
// don't change at runtime.
func (self *GopsUtil) GetMemoryModules() (*models.MemoryModulesInfo, error) {
	self.memModules.Lock()
	defer self.memModules.Unlock()
	if !self.memModules.read {
		self.memModules.info, self.memModules.err = readMemoryModules(dmiUdevData)
		self.memModules.read = true
	}
	if self.memModules.err != nil {
		return nil, self.memModules.err
	}
	info := *self.memModules.info
	info.Modules = slices.Clone(info.Modules)
	return &info, nil
}

func readMemoryModules(udevData string) (*models.MemoryModulesInfo, error) {
	text, err := os.ReadFile(udevData)
	if err != nil {
		if text, err = udevadmDMIProperties(); err != nil {
			return nil, fmt.Errorf("memory modules: %w", err)
		}
	}
	return parseMemoryModules(string(text)), nil
}

func udevadmDMIProperties() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "udevadm", "info", "-q", "property", "-p", "/devices/virtual/dmi/id").Output()
}

// parseMemoryModules accepts the udev database format (E:KEY=VALUE) and plain
// udevadm output (KEY=VALUE).
func parseMemoryModules(text string) *models.MemoryModulesInfo {
	const prefix = "MEMORY_DEVICE_"
	devices := map[int]map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "E:")
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		idxStr, kv, ok := strings.Cut(rest, "_")
		if !ok {
			continue
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if devices[idx] == nil {
			devices[idx] = map[string]string{}
		}
		devices[idx][key] = val
	}

	indices := make([]int, 0, len(devices))
	for i := range devices {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	info := &models.MemoryModulesInfo{Slots: len(indices), Modules: []models.MemoryModule{}}
	for _, i := range indices {
		d := devices[i]
		size, _ := strconv.ParseUint(d["SIZE"], 10, 64)
		if size == 0 || d["PRESENT"] == "0" {
			continue
		}
		speed, _ := strconv.ParseUint(d["SPEED_MTS"], 10, 32)
		configured, _ := strconv.ParseUint(d["CONFIGURED_SPEED_MTS"], 10, 32)
		info.Modules = append(info.Modules, models.MemoryModule{
			Locator:         dmiValue(d["LOCATOR"]),
			Size:            size / 1024,
			Type:            dmiValue(d["TYPE"]),
			Speed:           uint32(speed),
			ConfiguredSpeed: uint32(configured),
			Manufacturer:    dmiValue(d["MANUFACTURER"]),
			PartNumber:      dmiValue(d["PART_NUMBER"]),
			FormFactor:      dmiValue(d["FORM_FACTOR"]),
		})
	}
	return info
}

func dmiValue(v string) string {
	v = strings.TrimSpace(v)
	switch v {
	case "Unknown", "Not Specified", "Undefined":
		return ""
	}
	return v
}
