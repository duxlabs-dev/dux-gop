//go:build !linux

package gops

import (
	"sort"

	"github.com/duxlabs-dev/dux-gop/models"
)

// GetSensors maps the platform temperature sensors; fans and power are Linux-only.
func (self *GopsUtil) GetSensors(_ []string) (*models.SensorsInfo, error) {
	temps, err := self.hostProvider.SensorsTemperatures()
	if err != nil {
		return nil, err
	}

	sensors := []models.Sensor{}
	for _, temp := range temps {
		s := models.Sensor{
			Chip:  temp.SensorKey,
			Label: temp.SensorKey,
			Kind:  "temperature",
			Value: temp.Temperature,
		}
		if temp.High > 0 {
			s.Max = &temp.High
		}
		if temp.Critical > 0 {
			s.Critical = &temp.Critical
		}
		sensors = append(sensors, s)
	}
	sort.SliceStable(sensors, func(i, j int) bool { return sensors[i].Label < sensors[j].Label })
	return &models.SensorsInfo{Sensors: sensors}, nil
}
