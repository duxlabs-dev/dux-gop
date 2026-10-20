# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Dux GOP is a system monitoring tool with CLI and REST API, designed to provide comprehensive system metrics. It's written in Go and compiles to a single binary that provides both CLI commands and a REST API server.

## Development Commands

### Building
```bash
# Standard build
make

# Development build (with debug info)
make dev

# Build and install system-wide
make && sudo make install
```

### Testing
```bash
# Run all tests
make test

# Run tests for specific package
go test -v ./api/middleware/...
```

### Code Quality
```bash
# Format code
make fmt

# Run go vet
make vet

# Update dependencies
make deps
```

### Running the Application
```bash
# Run locally without building
go run ./cmd/dgop [command]

# After building
./bin/dgop [command]

# Start API server
./bin/dgop server
```

## Architecture

### Core Components

1. **CLI Entry Point** (`cmd/dgop/`)
   - `main.go`: Sets up Cobra commands and initializes GopsUtil
   - `commands.go`: Implements individual CLI commands (cpu, memory, network, etc.)
   - `server.go`: Starts the REST API server

2. **Data Collection** (`gops/`)
   - `gops.go`: Main utility class that orchestrates all metric collection
   - Individual collectors: `cpu.go`, `memory.go`, `disk.go`, `net.go`, `processes.go`, `system.go`, `hardware.go`
   - Each collector uses `github.com/shirou/gopsutil/v4` for system information

3. **API Layer** (`api/`)
   - `api/gops/`: HTTP handlers that wrap the gops collectors
   - `api/server/`: Server implementation
   - `api/middleware/`: Request logging, recovery, and request ID middleware
   - Uses Huma v2 framework for OpenAPI-compliant REST API

4. **Data Models** (`models/`)
   - Defines structures for all system metrics (CPU, Memory, Disk, Network, Process, etc.)
   - Shared between CLI output and API responses

### Key Design Patterns

- **Single Responsibility**: Each file in `gops/` handles one type of metric
- **Dependency Injection**: GopsUtil is passed to commands and API handlers
- **Process Sampling**: CPU and process metrics support sampling for performance optimization
- **Modular Commands**: Meta command allows combining multiple metrics dynamically

### API Server Details

- Default port: 63484
- OpenAPI docs available at `/docs`
- All endpoints under `/gops/` prefix
- Supports JSON output and query parameters for filtering/sorting

### Important Implementation Notes

1. **Process CPU Calculation**: Can be disabled with `--no-cpu` flag for faster results
2. **GPU Temperature**: Requires `nvidia-smi` for NVIDIA GPUs, uses PCI IDs for identification
3. **CGO Disabled**: Built with `CGO_ENABLED=0` for maximum portability
4. **Error Handling**: Non-critical errors are logged but don't stop execution (graceful degradation)
5. **Sampling Support**: CPU and process commands support `--sample` flag for testing with mock data