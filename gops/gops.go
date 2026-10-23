package gops

import (
	"time"

	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/sensors"
)

type GopsUtil struct {
	cpuProvider  CPUInfoProvider
	memProvider  MemoryInfoProvider
	diskProvider DiskInfoProvider
	netProvider  NetworkInfoProvider
	procProvider ProcessInfoProvider
	hostProvider HostInfoProvider
	loadProvider LoadInfoProvider
	fs           FileSystem

	pss pssCache

	detectGPUEntries func() ([]gpuEntry, error)
	sampleGPUs       func([]gpuEntry, gpuCursor, time.Time) ([]gpuSample, gpuCursor)
	platformSources
}

func NewGopsUtil() *GopsUtil {
	return withDefaultSources(&GopsUtil{
		cpuProvider:  &DefaultCPUInfoProvider{},
		memProvider:  &DefaultMemoryInfoProvider{},
		diskProvider: &DefaultDiskInfoProvider{},
		netProvider:  &DefaultNetworkInfoProvider{},
		procProvider: &DefaultProcessInfoProvider{},
		hostProvider: &DefaultHostInfoProvider{},
		loadProvider: &DefaultLoadInfoProvider{},
		fs:           &DefaultFileSystem{},
	})
}

// NewGopsUtilWithProviders creates a GopsUtil with custom providers (for testing)
func NewGopsUtilWithProviders(
	cpu CPUInfoProvider,
	mem MemoryInfoProvider,
	disk DiskInfoProvider,
	net NetworkInfoProvider,
	proc ProcessInfoProvider,
	host HostInfoProvider,
	load LoadInfoProvider,
	fs FileSystem,
) *GopsUtil {
	return withDefaultSources(&GopsUtil{
		cpuProvider:  cpu,
		memProvider:  mem,
		diskProvider: disk,
		netProvider:  net,
		procProvider: proc,
		hostProvider: host,
		loadProvider: load,
		fs:           fs,
	})
}

func withDefaultSources(g *GopsUtil) *GopsUtil {
	// The system module reads boot time on every call; it never changes.
	host.EnableBootTimeCache(true)
	g.detectGPUEntries = detectGPUEntries
	g.sampleGPUs = g.defaultGPUSampler()
	g.platformSources = defaultPlatformSources()
	return g
}

// Close stops the background socket listener that process-net starts. It is
// idempotent; a short-lived GopsUtil may skip it.
func (self *GopsUtil) Close() { self.closePlatform() }

func (self *GopsUtil) GetAllMetrics(procSortBy ProcSortBy, procLimit int, enableProcessCPU bool, mergeChildren bool) (*models.SystemMetrics, error) {
	return self.GetAllMetricsWithCursors(procSortBy, procLimit, enableProcessCPU, "", "", mergeChildren)
}

func (self *GopsUtil) GetAllMetricsWithCursors(procSortBy ProcSortBy, procLimit int, enableProcessCPU bool, cpuCursor string, procCursor string, mergeChildren bool) (*models.SystemMetrics, error) {
	cpuInfo, err := self.GetCPUInfoWithCursor(cpuCursor)
	if err != nil {
		log.Errorf("Failed to get CPU info: %v", err)
	}

	memInfo, err := self.GetMemoryInfo()
	if err != nil {
		log.Errorf("Failed to get memory info: %v", err)
	}

	networkInfo, err := self.GetNetworkInfo()
	if err != nil {
		log.Errorf("Failed to get network info: %v", err)
	}

	diskInfo, err := self.GetDiskInfo()
	if err != nil {
		log.Errorf("Failed to get disk info: %v", err)
	}

	diskMounts, err := self.GetDiskMounts()
	if err != nil {
		log.Errorf("Failed to get disk mounts: %v", err)
	}

	processResult, err := self.GetProcessesWithCursor(procSortBy, procLimit, enableProcessCPU, procCursor, mergeChildren)
	if err != nil {
		log.Errorf("Failed to get processes: %v", err)
	}

	systemInfo, err := self.GetSystemInfo()
	if err != nil {
		log.Errorf("Failed to get system info: %v", err)
	}

	var processes []*models.ProcessInfo
	if processResult != nil {
		processes = processResult.Processes
	}

	return &models.SystemMetrics{
		Memory:     memInfo,
		CPU:        cpuInfo,
		Network:    networkInfo,
		Disk:       diskInfo,
		Processes:  processes,
		System:     systemInfo,
		DiskMounts: diskMounts,
	}, nil
}

// GetSystemTemperatures returns system temperature sensors
func (self *GopsUtil) GetSystemTemperatures() ([]models.TemperatureSensor, error) {
	temps, err := sensors.SensorsTemperatures()
	if err != nil {
		return nil, err
	}

	var sensors []models.TemperatureSensor
	for _, temp := range temps {
		sensor := models.TemperatureSensor{
			Name:        temp.SensorKey,
			Temperature: temp.Temperature,
			High:        temp.High,
			Critical:    temp.Critical,
		}
		sensors = append(sensors, sensor)
	}

	return sensors, nil
}
