//go:build linux

package gops

import (
	"sort"

	"github.com/duxlabs-dev/dux-gop/models"
)

// GetSensors returns every hwmon temperature, fan and power input.
func (self *GopsUtil) GetSensors() (*models.SensorsInfo, error) {
	return sensorsFromChips(self.readHwmonChips(hwmonRoot)), nil
}

func sensorsFromChips(chips []hwmonChip) *models.SensorsInfo {
	sensors := []models.Sensor{}
	for _, chip := range chips {
		for _, r := range chip.Readings {
			if r.Kind == sensorFrequency {
				continue
			}
			sensors = append(sensors, models.Sensor{
				Chip:     chip.Name,
				Device:   chip.Device,
				Label:    r.Label,
				Kind:     r.Kind,
				Value:    r.Value,
				Max:      r.Max,
				Critical: r.Critical,
			})
		}
	}
	sort.SliceStable(sensors, func(i, j int) bool {
		a, b := sensors[i], sensors[j]
		if a.Chip != b.Chip {
			return a.Chip < b.Chip
		}
		if a.Device != b.Device {
			return a.Device < b.Device
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Label < b.Label
	})
	return &models.SensorsInfo{Sensors: sensors}
}
