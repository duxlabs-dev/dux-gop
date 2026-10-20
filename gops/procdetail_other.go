//go:build !linux

package gops

import (
	"errors"
	"fmt"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
)

// GetProcessDetail fills what gopsutil offers off Linux: storage totals and
// open files.
func (self *GopsUtil) GetProcessDetail(pid int32) (*models.ProcessDetail, error) {
	p, err := self.procProvider.NewProcess(pid)
	if errors.Is(err, process.ErrorProcessNotRunning) {
		return nil, fmt.Errorf("process %d: %w", pid, ErrProcessNotFound)
	}
	if err != nil {
		return nil, err
	}
	d := &models.ProcessDetail{PID: pid}
	if read, write, ok := processDiskBytes(p); ok {
		d.ReadTotal, d.WriteTotal = &read, &write
	}
	if n, err := p.NumFDs(); err == nil {
		d.OpenFiles = new(int(n))
	}
	return d, nil
}
