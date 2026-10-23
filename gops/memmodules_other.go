//go:build !linux

package gops

import "github.com/duxlabs-dev/dux-gop/models"

func (self *GopsUtil) GetMemoryModules() (*models.MemoryModulesInfo, error) {
	return &models.MemoryModulesInfo{Modules: []models.MemoryModule{}}, nil
}
