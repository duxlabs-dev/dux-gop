package server

import (
	"context"

	"github.com/duxlabs-dev/dux-gop/config"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

// Collector is the part of gops.GopsUtil the handlers call.
type Collector interface {
	GetAllMetrics(sortBy gops.ProcSortBy, limit int, enableCPU bool, mergeChildren bool) (*models.SystemMetrics, error)
	GetCPUInfoWithCursor(cursor string) (*models.CPUInfo, error)
	GetDiskInfo() ([]*models.DiskInfo, error)
	GetDiskMounts() ([]*models.DiskMountInfo, error)
	GetDiskRates(cursor string) (*models.DiskRateResponse, error)
	GetGPUInfo() (*models.GPUInfo, error)
	GetGPUTemp(pciId string) (*models.GPUTempInfo, error)
	GetMemoryInfo() (*models.MemoryInfo, error)
	GetMemoryModules() (*models.MemoryModulesInfo, error)
	GetMeta(ctx context.Context, modules []string, params gops.MetaParams) (*models.MetaInfo, error)
	GetModules() (*models.ModulesInfo, error)
	GetNetworkInfo() ([]*models.NetworkInfo, error)
	GetNetworkRatesWithOptions(cursor string, allInterfaces bool) (*models.NetworkRateResponse, error)
	GetProcessDetail(pid int32) (*models.ProcessDetail, error)
	GetProcessesWithOptions(opts gops.ProcessOptions) (*models.ProcessListResponse, error)
	GetSensors(gpuPciIds []string) (*models.SensorsInfo, error)
	GetSystemHardware() (*models.SystemHardware, error)
	GetSystemInfo() (*models.SystemInfo, error)
}

type Server struct {
	Cfg  *config.Config
	Gops Collector
}
