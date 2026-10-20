package gops_handler

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-gop/api/server"
)

type HandlerGroup struct {
	srv *server.Server
}

func RegisterHandlers(server *server.Server, grp *huma.Group) {
	handlers := &HandlerGroup{
		srv: server,
	}

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "all",
			Summary:     "Get All Metrics",
			Description: "Get all system metrics including CPU, memory, network, and processes",
			Path:        "/all",
			Method:      http.MethodGet,
		},
		handlers.All,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "cpu",
			Summary:     "Get CPU Info",
			Description: "Get information about the CPUs",
			Path:        "/cpu",
			Method:      http.MethodGet,
		},
		handlers.Cpu,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "memory",
			Summary:     "Get Memory Info",
			Description: "Get information about the system memory",
			Path:        "/memory",
			Method:      http.MethodGet,
		},
		handlers.Memory,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "network",
			Summary:     "Get Network Info",
			Description: "Get information about the network interfaces",
			Path:        "/network",
			Method:      http.MethodGet,
		},
		handlers.Network,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "net-rate",
			Summary:     "Get Network Rates",
			Description: "Get network transfer rates with cursor-based sampling for accurate rate calculations",
			Path:        "/net-rate",
			Method:      http.MethodGet,
		},
		handlers.NetRate,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "disk-rate",
			Summary:     "Get Disk I/O Rates",
			Description: "Get disk I/O rates with cursor-based sampling for accurate rate calculations",
			Path:        "/disk-rate",
			Method:      http.MethodGet,
		},
		handlers.DiskRate,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "system",
			Summary:     "Get System Info",
			Description: "Get general system information",
			Path:        "/system",
			Method:      http.MethodGet,
		},
		handlers.System,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "processes",
			Summary:     "Get Processes",
			Description: "Get a list of running processes",
			Path:        "/processes",
			Method:      http.MethodGet,
		},
		handlers.Processes,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "disks",
			Summary:     "Get Disk Info",
			Description: "Get information about the system disks",
			Path:        "/disk",
			Method:      http.MethodGet,
		},
		handlers.Disk,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "disk-mounts",
			Summary:     "Get Disk Mounts",
			Description: "Get information about the disk mounts",
			Path:        "/disk/mounts",
			Method:      http.MethodGet,
		},
		handlers.DiskMounts,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "hardware",
			Summary:     "Get Hardware Info",
			Description: "Get system hardware information including BIOS, motherboard, and CPU",
			Path:        "/hardware",
			Method:      http.MethodGet,
		},
		handlers.SystemHardware,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "gpu",
			Summary:     "Get GPU Info",
			Description: "Get information about GPUs and graphics cards",
			Path:        "/gpu",
			Method:      http.MethodGet,
		},
		handlers.GPU,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "gpu-temp",
			Summary:     "Get GPU Temperature",
			Description: "Get temperature for a specific GPU by PCI ID",
			Path:        "/gpu/temp",
			Method:      http.MethodGet,
		},
		handlers.GPUTemp,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "sensors",
			Summary:     "Get Sensors",
			Description: "Get hardware sensor readings",
			Path:        "/sensors",
			Method:      http.MethodGet,
		},
		handlers.Sensors,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "process-detail",
			Summary:     "Get Process Detail",
			Description: "Get the on-demand details of one process: lifetime I/O, swap, open files, systemd unit and terminal",
			Path:        "/process/{pid}",
			Method:      http.MethodGet,
		},
		handlers.ProcessDetail,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "memory-modules",
			Summary:     "Get Memory Modules",
			Description: "Get information about the installed memory modules",
			Path:        "/memory/modules",
			Method:      http.MethodGet,
		},
		handlers.MemoryModules,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "meta",
			Summary:     "Get Dynamic Metrics",
			Description: "Get system metrics for specified modules (e.g., cpu,memory,network)",
			Path:        "/meta",
			Method:      http.MethodGet,
		},
		handlers.Meta,
	)

	huma.Register(
		grp,
		huma.Operation{
			OperationID: "modules",
			Summary:     "List Available Modules",
			Description: "Get a list of all available modules for the meta endpoint",
			Path:        "/modules",
			Method:      http.MethodGet,
		},
		handlers.Modules,
	)
}
