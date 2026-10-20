//go:build darwin

package gops

import "github.com/duxlabs-dev/dux-gop/models"

func (self *GopsUtil) GetMemoryInfo() (*models.MemoryInfo, error) {
	v, err := self.memProvider.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &models.MemoryInfo{
		Total:       v.Total / 1024,
		Used:        v.Used / 1024,
		UsedPercent: v.UsedPercent,
		Free:        v.Free / 1024,
		Available:   v.Available / 1024,
		SwapTotal:   v.SwapTotal / 1024,
		SwapFree:    v.SwapFree / 1024,
	}, nil
}
