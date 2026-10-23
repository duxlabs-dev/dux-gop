package gops

import (
	"io/fs"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"
)

// FileSystem provides an interface for file system operations to enable testing
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Stat(name string) (fs.FileInfo, error)
}

// CommandExecutor provides an interface for executing external commands
type CommandExecutor interface {
	Execute(name string, args ...string) ([]byte, error)
}

// CPUInfoProvider provides an interface for CPU information
type CPUInfoProvider interface {
	Info() ([]cpu.InfoStat, error)
	Counts(logical bool) (int, error)
	Times(perCPU bool) ([]cpu.TimesStat, error)
	Percent(interval time.Duration, perCPU bool) ([]float64, error)
}

// MemoryInfoProvider provides an interface for memory information
type MemoryInfoProvider interface {
	VirtualMemory() (*mem.VirtualMemoryStat, error)
	SwapMemory() (*mem.SwapMemoryStat, error)
}

// DiskInfoProvider provides an interface for disk information
type DiskInfoProvider interface {
	IOCounters() (map[string]disk.IOCountersStat, error)
	Partitions(all bool) ([]disk.PartitionStat, error)
	Usage(path string) (*disk.UsageStat, error)
}

// NetworkInfoProvider provides an interface for network information
type NetworkInfoProvider interface {
	IOCounters(pernic bool) ([]net.IOCountersStat, error)
	Interfaces() ([]net.InterfaceStat, error)
}

// ProcessInfoProvider provides an interface for process information
type ProcessInfoProvider interface {
	Processes() ([]*process.Process, error)
	NewProcess(pid int32) (*process.Process, error)
}

// HostInfoProvider provides an interface for host information
type HostInfoProvider interface {
	Info() (*host.InfoStat, error)
	SensorsTemperatures() ([]sensors.TemperatureStat, error)
}

// LoadInfoProvider provides an interface for load average information
type LoadInfoProvider interface {
	Avg() (*load.AvgStat, error)
	Misc() (*load.MiscStat, error)
}

// DefaultFileSystem implements FileSystem using standard os package
type DefaultFileSystem struct{}

func (d *DefaultFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (d *DefaultFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(name)
}

func (d *DefaultFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

// DefaultCPUInfoProvider implements CPUInfoProvider using gopsutil
type DefaultCPUInfoProvider struct{}

func (d *DefaultCPUInfoProvider) Info() ([]cpu.InfoStat, error) {
	return cpu.Info()
}

func (d *DefaultCPUInfoProvider) Counts(logical bool) (int, error) {
	return cpu.Counts(logical)
}

func (d *DefaultCPUInfoProvider) Times(perCPU bool) ([]cpu.TimesStat, error) {
	return cpu.Times(perCPU)
}

func (d *DefaultCPUInfoProvider) Percent(interval time.Duration, perCPU bool) ([]float64, error) {
	return cpu.Percent(interval, perCPU)
}

// DefaultMemoryInfoProvider implements MemoryInfoProvider using gopsutil
type DefaultMemoryInfoProvider struct{}

func (d *DefaultMemoryInfoProvider) VirtualMemory() (*mem.VirtualMemoryStat, error) {
	return mem.VirtualMemory()
}

func (d *DefaultMemoryInfoProvider) SwapMemory() (*mem.SwapMemoryStat, error) {
	return mem.SwapMemory()
}

// DefaultDiskInfoProvider implements DiskInfoProvider using gopsutil
type DefaultDiskInfoProvider struct{}

func (d *DefaultDiskInfoProvider) IOCounters() (map[string]disk.IOCountersStat, error) {
	return disk.IOCounters()
}

func (d *DefaultDiskInfoProvider) Partitions(all bool) ([]disk.PartitionStat, error) {
	return disk.Partitions(all)
}

func (d *DefaultDiskInfoProvider) Usage(path string) (*disk.UsageStat, error) {
	return disk.Usage(path)
}

// DefaultNetworkInfoProvider implements NetworkInfoProvider using gopsutil
type DefaultNetworkInfoProvider struct{}

func (d *DefaultNetworkInfoProvider) IOCounters(pernic bool) ([]net.IOCountersStat, error) {
	return net.IOCounters(pernic)
}

func (d *DefaultNetworkInfoProvider) Interfaces() ([]net.InterfaceStat, error) {
	return net.Interfaces()
}

// DefaultProcessInfoProvider implements ProcessInfoProvider using gopsutil
type DefaultProcessInfoProvider struct{}

func (d *DefaultProcessInfoProvider) Processes() ([]*process.Process, error) {
	return listProcesses()
}

func (d *DefaultProcessInfoProvider) NewProcess(pid int32) (*process.Process, error) {
	return process.NewProcess(pid)
}

// DefaultHostInfoProvider implements HostInfoProvider using gopsutil
type DefaultHostInfoProvider struct{}

func (d *DefaultHostInfoProvider) Info() (*host.InfoStat, error) {
	return host.Info()
}

func (d *DefaultHostInfoProvider) SensorsTemperatures() ([]sensors.TemperatureStat, error) {
	return sensors.SensorsTemperatures()
}

// DefaultLoadInfoProvider implements LoadInfoProvider using gopsutil
type DefaultLoadInfoProvider struct{}

func (d *DefaultLoadInfoProvider) Avg() (*load.AvgStat, error) {
	return load.Avg()
}

func (d *DefaultLoadInfoProvider) Misc() (*load.MiscStat, error) {
	return load.Misc()
}
