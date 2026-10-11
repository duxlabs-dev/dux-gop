package gops

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
	"golang.org/x/sync/errgroup"
)

var availableModules = []string{
	"cpu",
	"memory",
	"network",
	"net-rate",
	"disk",
	"disk-rate",
	"diskmounts",
	"processes",
	"system",
	"hardware",
	"gpu",
	"gpu-temp",
}

func (self *GopsUtil) GetModules() (*models.ModulesInfo, error) {
	return &models.ModulesInfo{
		Available: availableModules,
	}, nil
}

type MetaParams struct {
	SortBy         ProcSortBy
	ProcLimit      int
	EnableCPU      bool
	EnableGPU      bool
	MergeChildren  bool
	GPUPciIds      []string
	GPUCursor      string
	CPUCursor      string
	ProcCursor     string
	NetRateCursor  string
	DiskRateCursor string
}

func (params MetaParams) processOptions() ProcessOptions {
	return ProcessOptions{
		SortBy:        params.SortBy,
		Limit:         params.ProcLimit,
		EnableCPU:     params.EnableCPU,
		EnableGPU:     params.EnableGPU,
		Cursor:        params.ProcCursor,
		MergeChildren: params.MergeChildren,
	}
}

func (self *GopsUtil) GetMeta(ctx context.Context, modules []string, params MetaParams) (*models.MetaInfo, error) {
	meta := &models.MetaInfo{}

	for _, module := range modules {
		switch strings.ToLower(module) {
		case "all":
			return self.loadAllModules(ctx, params)
		case "cpu":
			if cpu, err := self.GetCPUInfoWithCursor(params.CPUCursor); err == nil {
				meta.CPU = cpu
			}
		case "memory":
			if mem, err := self.GetMemoryInfo(); err == nil {
				meta.Memory = mem
			}
		case "network":
			if net, err := self.GetNetworkInfo(); err == nil {
				meta.Network = net
			}
		case "net-rate":
			if netRate, err := self.GetNetworkRates(params.NetRateCursor); err == nil {
				meta.NetRate = netRate
			}
		case "disk":
			if disk, err := self.GetDiskInfo(); err == nil {
				meta.Disk = disk
			}
		case "disk-rate":
			if diskRate, err := self.GetDiskRates(params.DiskRateCursor); err == nil {
				meta.DiskRate = diskRate
			}
		case "diskmounts":
			if mounts, err := self.GetDiskMounts(); err == nil {
				meta.DiskMounts = mounts
			}
		case "processes":
			if result, err := self.GetProcessesWithOptions(params.processOptions()); err == nil {
				meta.Processes = result.Processes
				meta.Cursor = result.Cursor
			}
		case "system":
			if sys, err := self.GetSystemInfo(); err == nil {
				meta.System = sys
			}
		case "hardware":
			if hw, err := self.GetSystemHardware(); err == nil {
				meta.Hardware = hw
			}
		case "gpu", "gpu-temp":
			if gpu, err := self.GetGPUInfoWithStats(params.GPUPciIds, params.GPUCursor); err == nil {
				meta.GPU = gpu
			}
		default:
			return nil, fmt.Errorf("unknown module: %s", module)
		}
	}

	return meta, nil
}

func (self *GopsUtil) loadAllModules(ctx context.Context, params MetaParams) (*models.MetaInfo, error) {
	meta := &models.MetaInfo{}
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cpu, err := self.GetCPUInfoWithCursor(params.CPUCursor)
		if err != nil {
			log.Warn("failed to get CPU info", "error", err)
			return nil
		}
		mu.Lock()
		meta.CPU = cpu
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		mem, err := self.GetMemoryInfo()
		if err != nil {
			log.Warn("failed to get memory info", "error", err)
			return nil
		}
		mu.Lock()
		meta.Memory = mem
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		net, err := self.GetNetworkInfo()
		if err != nil {
			log.Warn("failed to get network info", "error", err)
			return nil
		}
		mu.Lock()
		meta.Network = net
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		netRate, err := self.GetNetworkRates(params.NetRateCursor)
		if err != nil {
			log.Warn("failed to get network rates", "error", err)
			return nil
		}
		mu.Lock()
		meta.NetRate = netRate
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		disk, err := self.GetDiskInfo()
		if err != nil {
			log.Warn("failed to get disk info", "error", err)
			return nil
		}
		mu.Lock()
		meta.Disk = disk
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		diskRate, err := self.GetDiskRates(params.DiskRateCursor)
		if err != nil {
			log.Warn("failed to get disk rates", "error", err)
			return nil
		}
		mu.Lock()
		meta.DiskRate = diskRate
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		mounts, err := self.GetDiskMounts()
		if err != nil {
			log.Warn("failed to get disk mounts", "error", err)
			return nil
		}
		mu.Lock()
		meta.DiskMounts = mounts
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		procs, err := self.GetProcessesWithOptions(params.processOptions())
		if err != nil {
			log.Warn("failed to get processes", "error", err)
			return nil
		}
		mu.Lock()
		meta.Processes = procs.Processes
		meta.Cursor = procs.Cursor
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sys, err := self.GetSystemInfo()
		if err != nil {
			log.Warn("failed to get system info", "error", err)
			return nil
		}
		mu.Lock()
		meta.System = sys
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		hw, err := self.GetSystemHardware()
		if err != nil {
			log.Warn("failed to get hardware info", "error", err)
			return nil
		}
		mu.Lock()
		meta.Hardware = hw
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		gpu, err := self.GetGPUInfoWithStats(params.GPUPciIds, params.GPUCursor)
		if err != nil {
			log.Warn("failed to get GPU info", "error", err)
			return nil
		}
		mu.Lock()
		meta.GPU = gpu
		mu.Unlock()
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return meta, nil
}
