package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  "Display the current version of dgop.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dgop version %s\n", Version)
	},
}

var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Get all system metrics",
	Long:  "Display system information including CPU, memory, disk, network, and process data.",
}

var cpuCmd = &cobra.Command{
	Use:   "cpu",
	Short: "Get CPU information",
	Long:  "Display CPU information including usage percentages and core details.",
}

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Get memory information",
	Long:  "Display memory usage information including RAM and swap statistics.",
}

var networkCmd = &cobra.Command{
	Use:   "network",
	Short: "Get network interface information",
	Long:  "Display network interface statistics including throughput and connection data.",
}

var netRateCmd = &cobra.Command{
	Use:   "net-rate",
	Short: "Get network transfer rates",
	Long:  "Display network transfer rates with cursor-based sampling for accurate rate calculations.",
}

var diskRateCmd = &cobra.Command{
	Use:   "disk-rate",
	Short: "Get disk I/O rates",
	Long:  "Display disk I/O rates with cursor-based sampling for accurate rate calculations.",
}

var diskCmd = &cobra.Command{
	Use:   "disk",
	Short: "Get disk information",
	Long:  "Display disk usage statistics and mount information.",
}

var processesCmd = &cobra.Command{
	Use:   "processes",
	Short: "Get running processes",
	Long:  "Display information about running processes with sorting and filtering options.",
}

var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "Get general system information",
	Long:  "Display general system information including load averages and boot time.",
}

var hardwareCmd = &cobra.Command{
	Use:   "hardware",
	Short: "Get hardware information",
	Long:  "Display system hardware information including BIOS, motherboard, and CPU data.",
}

var gpuCmd = &cobra.Command{
	Use:   "gpu",
	Short: "Get GPU information",
	Long:  "Display information about GPUs and graphics cards.",
}

var gpuTempCmd = &cobra.Command{
	Use:   "gpu-temp",
	Short: "Get GPU temperature",
	Long:  "Get temperature for a specific GPU by PCI ID (e.g., --pci-id 10de:2684).",
}

var metaCmd = &cobra.Command{
	Use:   "meta",
	Short: "Get dynamic system metrics",
	Long:  "Display system metrics for specified modules (e.g., --modules cpu,memory,network).",
}

var modulesCmd = &cobra.Command{
	Use:   "modules",
	Short: "List available modules",
	Long:  "Display all available modules for the meta command.",
}

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Launch interactive system monitor",
	Long:  "Launch an interactive system monitor for real-time system monitoring.",
}

func runAllCommand(gopsUtil *gops.GopsUtil) error {
	enableCPU := !disableProcCPU
	sortBy := parseProcessSortBy(procSortBy, disableProcCPU)

	metrics, err := gopsUtil.GetAllMetricsWithCursors(sortBy, procLimit, enableCPU, cpuCursor, procCursor, mergeChildren)
	if err != nil {
		return fmt.Errorf("failed to get system metrics: %w", err)
	}

	if jsonOutput {
		return outputJSON(metrics)
	}

	displayAllMetrics(metrics)
	return nil
}

func runCpuCommand(gopsUtil *gops.GopsUtil) error {
	cpuInfo, err := gopsUtil.GetCPUInfoWithCursor(cpuCursor)
	if err != nil {
		return fmt.Errorf("failed to get CPU info: %w", err)
	}

	if jsonOutput {
		return outputJSON(cpuInfo)
	}

	displayCPUInfo(cpuInfo)
	return nil
}

func runMemoryCommand(gopsUtil *gops.GopsUtil) error {
	memInfo, err := gopsUtil.GetMemoryInfo()
	if err != nil {
		return fmt.Errorf("failed to get memory info: %w", err)
	}

	if jsonOutput {
		return outputJSON(memInfo)
	}

	displayMemoryInfo(memInfo)
	return nil
}

func runNetworkCommand(gopsUtil *gops.GopsUtil) error {
	networkInfo, err := gopsUtil.GetNetworkInfo()
	if err != nil {
		return fmt.Errorf("failed to get network info: %w", err)
	}

	if jsonOutput {
		return outputJSON(networkInfo)
	}

	displayNetworkInfo(networkInfo)
	return nil
}

func runDiskCommand(gopsUtil *gops.GopsUtil) error {
	diskInfo, err := gopsUtil.GetDiskInfo()
	if err != nil {
		return fmt.Errorf("failed to get disk info: %w", err)
	}

	diskMounts, err := gopsUtil.GetDiskMounts()
	if err != nil {
		return fmt.Errorf("failed to get disk mounts: %w", err)
	}

	if jsonOutput {
		data := struct {
			Disk   []*models.DiskInfo      `json:"disk"`
			Mounts []*models.DiskMountInfo `json:"mounts"`
		}{
			Disk:   diskInfo,
			Mounts: diskMounts,
		}
		return outputJSON(data)
	}

	displayDiskInfo(diskInfo, diskMounts)
	return nil
}

func runProcessesCommand(gopsUtil *gops.GopsUtil) error {
	enableCPU := !disableProcCPU
	sortBy := parseProcessSortBy(procSortBy, disableProcCPU)

	result, err := gopsUtil.GetProcessesWithOptions(gops.ProcessOptions{
		SortBy:        sortBy,
		Limit:         procLimit,
		EnableCPU:     enableCPU,
		EnableGPU:     enableProcGPU,
		GPUPciIds:     metaGPUPciIds,
		Cursor:        procCursor,
		MergeChildren: mergeChildren,
	})
	if err != nil {
		return fmt.Errorf("failed to get processes: %w", err)
	}

	if jsonOutput {
		return outputJSON(result)
	}

	displayProcesses(result.Processes)
	return nil
}

func runSystemCommand(gopsUtil *gops.GopsUtil) error {
	systemInfo, err := gopsUtil.GetSystemInfo()
	if err != nil {
		return fmt.Errorf("failed to get system info: %w", err)
	}

	if jsonOutput {
		return outputJSON(systemInfo)
	}

	displaySystemInfo(systemInfo)
	return nil
}

func runHardwareCommand(gopsUtil *gops.GopsUtil) error {
	hardwareInfo, err := gopsUtil.GetSystemHardware()
	if err != nil {
		return fmt.Errorf("failed to get hardware info: %w", err)
	}

	if jsonOutput {
		return outputJSON(hardwareInfo)
	}

	displayHardwareInfo(hardwareInfo)
	return nil
}

func runGPUCommand(gopsUtil *gops.GopsUtil) error {
	gpuInfo, err := gopsUtil.GetGPUInfo()
	if err != nil {
		return fmt.Errorf("failed to get GPU info: %w", err)
	}

	if jsonOutput {
		return outputJSON(gpuInfo)
	}

	displayGPUInfo(gpuInfo)
	return nil
}

func runGPUTempCommand(gopsUtil *gops.GopsUtil) error {
	gpuTempInfo, err := gopsUtil.GetGPUTemp(gpuPciId)
	if err != nil {
		return fmt.Errorf("failed to get GPU temperature: %w", err)
	}

	if jsonOutput {
		return outputJSON(gpuTempInfo)
	}

	displayGPUTempInfo(gpuTempInfo)
	return nil
}

func runMetaCommand(gopsUtil *gops.GopsUtil) error {
	params := gops.MetaParams{
		SortBy:         parseProcessSortBy(procSortBy, disableProcCPU),
		ProcLimit:      procLimit,
		EnableCPU:      !disableProcCPU,
		EnableGPU:      enableProcGPU,
		MergeChildren:  mergeChildren,
		GPUPciIds:      metaGPUPciIds,
		GPUCursor:      gpuCursor,
		CPUCursor:      cpuCursor,
		ProcCursor:     procCursor,
		NetRateCursor:  netRateCursor,
		DiskRateCursor: diskRateCursor,
	}

	metaInfo, err := gopsUtil.GetMeta(context.Background(), metaModules, params)
	if err != nil {
		return fmt.Errorf("failed to get meta info: %w", err)
	}

	if jsonOutput {
		return outputJSON(metaInfo)
	}

	displayMetaInfo(metaInfo)
	return nil
}

func runModulesCommand(gopsUtil *gops.GopsUtil) error {
	modulesInfo, err := gopsUtil.GetModules()
	if err != nil {
		return fmt.Errorf("failed to get modules info: %w", err)
	}

	if jsonOutput {
		return outputJSON(modulesInfo)
	}

	displayModulesInfo(modulesInfo)
	return nil
}

// Display functions for pretty printing

func displayAllMetrics(metrics *models.SystemMetrics) {
	fmt.Println(titleStyle.Render("SYSTEM METRICS"))
	fmt.Println()

	if metrics.System != nil {
		displaySystemInfo(metrics.System)
		fmt.Println()
	}

	if metrics.CPU != nil {
		displayCPUInfo(metrics.CPU)
		fmt.Println()
	}

	if metrics.Memory != nil {
		displayMemoryInfo(metrics.Memory)
		fmt.Println()
	}

	if len(metrics.Network) > 0 {
		displayNetworkInfo(metrics.Network)
		fmt.Println()
	}

	if len(metrics.Disk) > 0 || len(metrics.DiskMounts) > 0 {
		displayDiskInfo(metrics.Disk, metrics.DiskMounts)
		fmt.Println()
	}

	if len(metrics.Processes) > 0 {
		displayProcesses(metrics.Processes)
	}
}

func displaySystemInfo(info *models.SystemInfo) {
	fmt.Println(titleStyle.Render("SYSTEM"))

	rows := [][]string{
		{"Load Average:", info.LoadAvg},
		{"Processes:", strconv.Itoa(info.Processes)},
		{"Threads:", strconv.Itoa(info.Threads)},
		{"Boot Time:", info.BootTime},
	}

	printTable(rows)
}

func displayCPUInfo(cpu *models.CPUInfo) {
	fmt.Println(titleStyle.Render("CPU"))

	rows := [][]string{
		{"Count:", strconv.Itoa(cpu.Count)},
		{"Model:", cpu.Model},
		{"Frequency:", fmt.Sprintf("%.2f MHz", cpu.Frequency)},
		{"Temperature:", fmt.Sprintf("%.2f°C", cpu.Temperature)},
		{"Usage:", fmt.Sprintf("%.1f%%", cpu.Usage)},
	}

	if len(cpu.CoreUsage) > 0 {
		coreUsageStr := ""
		for i, usage := range cpu.CoreUsage {
			if i > 0 && i%4 == 0 {
				coreUsageStr += "\n              "
			}
			coreUsageStr += fmt.Sprintf("%d: %5.1f%%  ", i, usage)
		}
		rows = append(rows, []string{"Core Usage:", coreUsageStr})
	}

	printTable(rows)
}

func displayMemoryInfo(mem *models.MemoryInfo) {
	fmt.Println(titleStyle.Render("MEMORY"))

	toGB := func(kb uint64) float64 { return float64(kb) / 1024 / 1024 }

	rows := [][]string{
		{"Total:", fmt.Sprintf("%.2f GB", toGB(mem.Total))},
		{"Used:", fmt.Sprintf("%.2f GB (%.1f%%)", toGB(mem.Used), mem.UsedPercent)},
		{"Available:", fmt.Sprintf("%.2f GB", toGB(mem.Available))},
		{"Free:", fmt.Sprintf("%.2f GB", toGB(mem.Free))},
		{"Buffers:", fmt.Sprintf("%.2f GB", toGB(mem.Buffers))},
		{"Cached:", fmt.Sprintf("%.2f GB", toGB(mem.Cached))},
	}

	if mem.SwapTotal > 0 {
		rows = append(rows, []string{"Swap Total:", fmt.Sprintf("%.2f GB", toGB(mem.SwapTotal))})
		rows = append(rows, []string{"Swap Used:", fmt.Sprintf("%.2f GB", toGB(mem.SwapTotal-mem.SwapFree))})
	}

	printTable(rows)
}

func displayNetworkInfo(interfaces []*models.NetworkInfo) {
	fmt.Println(titleStyle.Render("NETWORK"))

	for i, iface := range interfaces {
		if i > 0 {
			fmt.Println()
		}

		fmt.Println(keyStyle.Render(fmt.Sprintf("Interface: %s", iface.Name)))

		rows := [][]string{
			{"Bytes Received:", formatBytes(iface.Rx)},
			{"Bytes Sent:", formatBytes(iface.Tx)},
		}

		printTable(rows)
	}
}

func displayDiskInfo(disks []*models.DiskInfo, mounts []*models.DiskMountInfo) {
	fmt.Println(titleStyle.Render("DISK"))

	if len(disks) > 0 {
		fmt.Println(keyStyle.Render("I/O Statistics:"))
		for i, disk := range disks {
			if i > 0 {
				fmt.Println()
			}

			fmt.Println(keyStyle.Render(fmt.Sprintf("Device: %s", disk.Name)))

			rows := [][]string{
				{"Read:", formatBytes(disk.Read)},
				{"Write:", formatBytes(disk.Write)},
			}

			printTable(rows)
		}
	}

	if len(mounts) > 0 {
		fmt.Println()
		fmt.Println(keyStyle.Render("Mount Points:"))

		for _, mount := range mounts {
			fmt.Printf("  %s → %s (%s) [%s used, %s available]\n",
				valueStyle.Render(mount.Device),
				valueStyle.Render(mount.Mount),
				valueStyle.Render(mount.FSType),
				valueStyle.Render(mount.Used+" ("+mount.Percent+")"),
				valueStyle.Render(mount.Avail))
		}
	}
}

func displayProcesses(processes []*models.ProcessInfo) {
	fmt.Println(titleStyle.Render(fmt.Sprintf("PROCESSES (%d)", len(processes))))

	showGPU := false
	for _, proc := range processes {
		if proc.GPU != nil {
			showGPU = true
			break
		}
	}

	header := fmt.Sprintf("%-8s %-8s %-20s %-8s %-8s ", "PID", "PPID", "COMMAND", "CPU%", "MEM%")
	if showGPU {
		header += fmt.Sprintf("%-8s ", "GPU%")
	}
	fmt.Println(keyStyle.Render(header + "FULL COMMAND"))
	fmt.Println(strings.Repeat("─", 80))

	for _, proc := range processes {
		row := fmt.Sprintf("%-8d %-8d %-20s %-8.1f %-8.1f ",
			proc.PID,
			proc.PPID,
			truncateString(proc.Command, 20),
			proc.CPU,
			proc.MemoryPercent)
		if showGPU {
			gpu := "-"
			if proc.GPU != nil {
				gpu = fmt.Sprintf("%.1f", *proc.GPU)
			}
			row += fmt.Sprintf("%-8s ", gpu)
		}
		fmt.Println(valueStyle.Render(row + truncateString(proc.FullCommand, 30)))
	}
}

// Helper functions

func printTable(rows [][]string) {
	for _, row := range rows {
		if len(row) >= 2 {
			fmt.Printf("  %s %s\n", keyStyle.Render(row[0]), valueStyle.Render(row[1]))
		}
	}
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func displayHardwareInfo(hardware *models.SystemHardware) {
	fmt.Println(titleStyle.Render("HARDWARE"))

	rows := [][]string{
		{"Kernel:", hardware.Kernel},
		{"Distro:", hardware.Distro},
		{"Hostname:", hardware.Hostname},
		{"Architecture:", hardware.Arch},
		{"CPU Count:", strconv.Itoa(hardware.CPU.Count)},
		{"CPU Model:", hardware.CPU.Model},
		{"Motherboard:", hardware.BIOS.Motherboard},
		{"BIOS Version:", hardware.BIOS.Version},
		{"BIOS Date:", hardware.BIOS.Date},
	}

	printTable(rows)
}

func displayGPUInfo(gpuInfo *models.GPUInfo) {
	fmt.Println(titleStyle.Render("GPU"))

	if len(gpuInfo.GPUs) == 0 {
		fmt.Println(valueStyle.Render("  No GPUs detected"))
		return
	}

	for i, gpu := range gpuInfo.GPUs {
		if i > 0 {
			fmt.Println()
		}

		fmt.Println(keyStyle.Render(fmt.Sprintf("GPU %d:", i+1)))

		rows := [][]string{
			{"Vendor:", gpu.Vendor},
			{"Driver:", gpu.Driver},
			{"Name:", gpu.DisplayName},
			{"Full Name:", gpu.FullName},
			{"PCI ID:", gpu.PciId},
			{"Temperature:", fmt.Sprintf("%.1f°C", gpu.Temperature)},
		}
		if gpu.BusId != "" {
			rows = append(rows, []string{"Bus ID:", gpu.BusId})
		}
		if gpu.State != "" {
			rows = append(rows, []string{"State:", gpu.State})
		}
		if gpu.Usage != nil {
			rows = append(rows, []string{"Usage:", fmt.Sprintf("%.0f%%", *gpu.Usage)})
		}
		if gpu.MemoryUsed != nil && gpu.MemoryTotal != nil {
			rows = append(rows, []string{"Memory:", formatBytes(*gpu.MemoryUsed*1024) + " / " + formatBytes(*gpu.MemoryTotal*1024)})
		}
		if gpu.Power != nil {
			rows = append(rows, []string{"Power:", fmt.Sprintf("%.1f W", *gpu.Power)})
		}
		if gpu.CoreClock != nil {
			rows = append(rows, []string{"Core Clock:", fmt.Sprintf("%.0f MHz", *gpu.CoreClock)})
		}
		if gpu.MemoryClock != nil {
			rows = append(rows, []string{"Memory Clock:", fmt.Sprintf("%.0f MHz", *gpu.MemoryClock)})
		}

		printTable(rows)
	}
}

func displayMetaInfo(meta *models.MetaInfo) {
	fmt.Println(titleStyle.Render("META METRICS"))
	fmt.Println()

	if meta.CPU != nil {
		displayCPUInfo(meta.CPU)
		fmt.Println()
	}

	if meta.Memory != nil {
		displayMemoryInfo(meta.Memory)
		fmt.Println()
	}

	if meta.System != nil {
		displaySystemInfo(meta.System)
		fmt.Println()
	}

	if meta.Hardware != nil {
		displayHardwareInfo(meta.Hardware)
		fmt.Println()
	}

	if meta.GPU != nil {
		displayGPUInfo(meta.GPU)
		fmt.Println()
	}

	if len(meta.Network) > 0 {
		displayNetworkInfo(meta.Network)
		fmt.Println()
	}

	if meta.NetRate != nil {
		displayNetworkRates(meta.NetRate)
		fmt.Println()
	}

	if len(meta.Disk) > 0 || len(meta.DiskMounts) > 0 {
		displayDiskInfo(meta.Disk, meta.DiskMounts)
		fmt.Println()
	}

	if meta.DiskRate != nil {
		displayDiskRates(meta.DiskRate)
		fmt.Println()
	}

	if len(meta.Processes) > 0 {
		displayProcesses(meta.Processes)
	}
}

func displayModulesInfo(modules *models.ModulesInfo) {
	fmt.Println(titleStyle.Render("AVAILABLE MODULES"))

	for i, module := range modules.Available {
		fmt.Printf("  %s%s", valueStyle.Render(module), keyStyle.Render(", "))
		if (i+1)%5 == 0 {
			fmt.Println()
		}
	}
	fmt.Println()
}

func displayGPUTempInfo(gpuTemp *models.GPUTempInfo) {
	fmt.Println(titleStyle.Render("GPU TEMPERATURE"))

	rows := [][]string{
		{"Driver:", gpuTemp.Driver},
		{"Hwmon:", gpuTemp.Hwmon},
		{"Temperature:", fmt.Sprintf("%.1f°C", gpuTemp.Temperature)},
	}

	printTable(rows)
}

func displayNetworkRates(netRates *models.NetworkRateResponse) {
	fmt.Println(titleStyle.Render("NETWORK RATES"))

	if len(netRates.Interfaces) == 0 {
		fmt.Println(valueStyle.Render("  No network interfaces found"))
		return
	}

	for i, iface := range netRates.Interfaces {
		if i > 0 {
			fmt.Println()
		}

		fmt.Println(keyStyle.Render(fmt.Sprintf("Interface: %s", iface.Interface)))

		rows := [][]string{
			{"RX Rate:", formatRate(iface.RxRate)},
			{"TX Rate:", formatRate(iface.TxRate)},
			{"RX Total:", formatBytes(iface.RxTotal)},
			{"TX Total:", formatBytes(iface.TxTotal)},
		}

		printTable(rows)
	}

	fmt.Printf("\nCursor: %s\n", netRates.Cursor)
}

func displayDiskRates(diskRates *models.DiskRateResponse) {
	fmt.Println(titleStyle.Render("DISK I/O RATES"))

	if len(diskRates.Disks) == 0 {
		fmt.Println(valueStyle.Render("  No disk devices found"))
		return
	}

	for i, disk := range diskRates.Disks {
		if i > 0 {
			fmt.Println()
		}

		fmt.Println(keyStyle.Render(fmt.Sprintf("Device: %s", disk.Device)))

		rows := [][]string{
			{"Read Rate:", formatRate(disk.ReadRate)},
			{"Write Rate:", formatRate(disk.WriteRate)},
			{"Read Total:", formatBytes(disk.ReadTotal)},
			{"Write Total:", formatBytes(disk.WriteTotal)},
			{"Read Count:", fmt.Sprintf("%d", disk.ReadCount)},
			{"Write Count:", fmt.Sprintf("%d", disk.WriteCount)},
		}

		printTable(rows)
	}

	fmt.Printf("\nCursor: %s\n", diskRates.Cursor)
}

func formatRate(bytesPerSecond float64) string {
	return fmt.Sprintf("%s/s", formatBytesFloat(bytesPerSecond))
}

func formatBytesFloat(bytes float64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%.2f B", bytes)
	}
	div, exp := float64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", bytes/div, "KMGTPE"[exp])
}

func runNetRateCommand(gopsUtil *gops.GopsUtil) error {
	netRateInfo, err := gopsUtil.GetNetworkRates(netRateCursor)
	if err != nil {
		return fmt.Errorf("failed to get network rates: %w", err)
	}

	if jsonOutput {
		return outputJSON(netRateInfo)
	}

	displayNetworkRates(netRateInfo)
	return nil
}

func runDiskRateCommand(gopsUtil *gops.GopsUtil) error {
	diskRateInfo, err := gopsUtil.GetDiskRates(diskRateCursor)
	if err != nil {
		return fmt.Errorf("failed to get disk rates: %w", err)
	}

	if jsonOutput {
		return outputJSON(diskRateInfo)
	}

	displayDiskRates(diskRateInfo)
	return nil
}

func runTopCommand(gopsUtil *gops.GopsUtil) error {
	return runTUIWithOptions(gopsUtil, hideCPUCores, summarizeCores)
}
