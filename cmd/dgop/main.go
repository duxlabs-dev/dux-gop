package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/spf13/cobra"
)

var (
	Version        = "dev"
	jsonOutput     bool
	procSortBy     string
	procLimit      int
	disableProcCPU bool
	enableProcGPU  bool
	mergeChildren  bool
	metaModules    []string
	gpuPciId       string
	metaGPUPciIds  []string
	gpuCursor      string
	cpuCursor      string
	procCursor     string
	netRateCursor  string
	diskRateCursor string
	hideCPUCores   bool
	summarizeCores bool
)

var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("0")).
	Background(lipgloss.Color("6")).
	PaddingLeft(1).
	PaddingRight(1)

var keyStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("6"))

var valueStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("7"))

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("6"))

func printHeader() {
	header := `
 ██████╗  █████╗ ███╗   ██╗██╗  ██╗
 ██╔══██╗██╔══██╗████╗  ██║██║ ██╔╝
 ██║  ██║███████║██╔██╗ ██║█████╔╝ 
 ██║  ██║██╔══██║██║╚██╗██║██╔═██╗ 
 ██████╔╝██║  ██║██║ ╚████║██║  ██╗
 ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝`
	fmt.Println(headerStyle.Render(header))
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&disableProcCPU, "no-cpu", false, "Disable CPU calculation for faster process listing")

	allCmd.Flags().StringVar(&procSortBy, "sort", "cpu", "Sort processes by (cpu, memory, name, pid)")
	allCmd.Flags().IntVar(&procLimit, "limit", 0, "Limit number of processes (0 = no limit)")
	allCmd.Flags().StringVar(&cpuCursor, "cpu-cursor", "", "CPU cursor from previous request")
	allCmd.Flags().StringVar(&procCursor, "proc-cursor", "", "Process cursor from previous request")
	allCmd.Flags().BoolVar(&mergeChildren, "merge-children", true, "Merge child processes with same executable")

	cpuCmd.Flags().StringVar(&cpuCursor, "cursor", "", "Cursor from previous CPU request")

	netRateCmd.Flags().StringVar(&netRateCursor, "cursor", "", "Cursor from previous network rate request")

	diskRateCmd.Flags().StringVar(&diskRateCursor, "cursor", "", "Cursor from previous disk rate request")

	processesCmd.Flags().StringVar(&procSortBy, "sort", "cpu", "Sort processes by (cpu, memory, name, pid, gpu)")
	processesCmd.Flags().IntVar(&procLimit, "limit", 0, "Limit number of processes (0 = no limit)")
	processesCmd.Flags().StringVar(&procCursor, "cursor", "", "Cursor from previous process request")
	processesCmd.Flags().BoolVar(&mergeChildren, "merge-children", true, "Merge child processes with same executable")
	processesCmd.Flags().BoolVar(&enableProcGPU, "proc-gpu", false, "Sample per-process GPU usage of the --gpu-pci-ids GPUs (implied by --sort gpu)")
	processesCmd.Flags().StringSliceVar(&metaGPUPciIds, "gpu-pci-ids", []string{}, "PCI IDs or bus addresses of GPUs to sample per-process usage for (e.g., 10de:2684,0000:03:00.0)")

	metaCmd.Flags().StringSliceVar(&metaModules, "modules", []string{"all"}, "Modules to include (cpu,memory,network,etc)")
	metaCmd.Flags().StringVar(&procSortBy, "sort", "cpu", "Sort processes by (cpu, memory, name, pid, gpu)")
	metaCmd.Flags().IntVar(&procLimit, "limit", 0, "Limit number of processes (0 = no limit)")
	metaCmd.Flags().StringSliceVar(&metaGPUPciIds, "gpu-pci-ids", []string{}, "PCI IDs or bus addresses of GPUs to sample (e.g., 10de:2684,0000:03:00.0)")
	metaCmd.Flags().BoolVar(&enableProcGPU, "proc-gpu", false, "Sample per-process GPU usage of the --gpu-pci-ids GPUs (implied by --sort gpu)")
	metaCmd.Flags().StringVar(&gpuCursor, "gpu-cursor", "", "GPU cursor from previous request")
	metaCmd.Flags().StringVar(&cpuCursor, "cpu-cursor", "", "CPU cursor from previous request")
	metaCmd.Flags().StringVar(&procCursor, "proc-cursor", "", "Process cursor from previous request")
	metaCmd.Flags().StringVar(&netRateCursor, "net-rate-cursor", "", "Network rate cursor from previous request")
	metaCmd.Flags().StringVar(&diskRateCursor, "disk-rate-cursor", "", "Disk rate cursor from previous request")
	metaCmd.Flags().BoolVar(&mergeChildren, "merge-children", true, "Merge child processes with same executable")

	gpuTempCmd.Flags().StringVar(&gpuPciId, "pci-id", "", "PCI ID of GPU to get temperature (e.g., 10de:2684)")
	gpuTempCmd.MarkFlagRequired("pci-id")

	topCmd.Flags().BoolVar(&hideCPUCores, "hide-cpu-cores", false, "Hide individual CPU core display in TUI")
	topCmd.Flags().BoolVar(&summarizeCores, "summarize-cores", false, "Show summarized CPU core groups instead of individual cores")
}

var rootCmd = &cobra.Command{
	Use: "dgop",
	Run: func(cmd *cobra.Command, args []string) {
		gopsUtil := gops.NewGopsUtil()
		if err := runTUIWithOptions(gopsUtil, hideCPUCores, summarizeCores); err != nil {
			log.Fatal(err)
		}
	},
}

func main() {
	log.SetEnvPrefix("DGOP")

	gopsUtil := gops.NewGopsUtil()

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cmd.SetContext(cmd.Context())
	}

	setupCommands(gopsUtil)

	if err := rootCmd.Execute(); err != nil {
		log.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}

func setupCommands(gopsUtil *gops.GopsUtil) {
	rootCmd.AddCommand(helpCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(allCmd)
	rootCmd.AddCommand(cpuCmd)
	rootCmd.AddCommand(memoryCmd)
	rootCmd.AddCommand(networkCmd)
	rootCmd.AddCommand(diskCmd)
	rootCmd.AddCommand(processesCmd)
	rootCmd.AddCommand(systemCmd)
	rootCmd.AddCommand(hardwareCmd)
	rootCmd.AddCommand(gpuCmd)
	rootCmd.AddCommand(gpuTempCmd)
	rootCmd.AddCommand(metaCmd)
	rootCmd.AddCommand(modulesCmd)
	rootCmd.AddCommand(netRateCmd)
	rootCmd.AddCommand(diskRateCmd)
	rootCmd.AddCommand(topCmd)
	rootCmd.AddCommand(serverCmd)

	// Set gopsUtil for all commands
	allCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runAllCommand(gopsUtil)
	}

	cpuCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runCpuCommand(gopsUtil)
	}

	memoryCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runMemoryCommand(gopsUtil)
	}

	networkCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runNetworkCommand(gopsUtil)
	}

	netRateCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runNetRateCommand(gopsUtil)
	}

	diskRateCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runDiskRateCommand(gopsUtil)
	}

	diskCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runDiskCommand(gopsUtil)
	}

	processesCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runProcessesCommand(gopsUtil)
	}

	systemCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runSystemCommand(gopsUtil)
	}

	hardwareCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runHardwareCommand(gopsUtil)
	}

	gpuCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runGPUCommand(gopsUtil)
	}

	gpuTempCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runGPUTempCommand(gopsUtil)
	}

	metaCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runMetaCommand(gopsUtil)
	}

	modulesCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runModulesCommand(gopsUtil)
	}

	topCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runTopCommand(gopsUtil)
	}

	helpCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runHelpCommand(gopsUtil)
	}
}

func outputJSON(data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	fmt.Println(string(jsonData))
	return nil
}

func parseProcessSortBy(sortBy string, cpuDisabled bool) gops.ProcSortBy {
	// If CPU is disabled and user chose CPU sort, default to memory
	if cpuDisabled && sortBy == "cpu" {
		return gops.SortByMemory
	}

	switch sortBy {
	case "cpu":
		return gops.SortByCPU
	case "memory":
		return gops.SortByMemory
	case "name":
		return gops.SortByName
	case "pid":
		return gops.SortByPID
	case "gpu":
		return gops.SortByGPU
	default:
		// Default behavior: CPU if enabled, memory if CPU disabled
		if cpuDisabled {
			return gops.SortByMemory
		}
		return gops.SortByCPU
	}
}
