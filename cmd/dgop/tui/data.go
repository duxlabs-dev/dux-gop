package tui

import (
	"context"
	"fmt"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

type fetchDataMsg struct {
	metrics    *models.SystemMetrics
	err        error
	generation int
	cpuCursor  string
	procCursor string
}

type fetchNetworkMsg struct {
	rates *models.NetworkRateResponse
	err   error
}

type fetchDiskMsg struct {
	rates *models.DiskRateResponse
	err   error
}

type fetchTempMsg struct {
	temps []models.TemperatureSensor
	err   error
}

type processKillResultMsg struct {
	message string
}

func killProcess(pid int32, force bool) tea.Cmd {
	return func() tea.Msg {
		sig := syscall.SIGTERM
		sigName := "SIGTERM"
		if force {
			sig = syscall.SIGKILL
			sigName = "SIGKILL"
		}

		if err := syscall.Kill(int(pid), sig); err != nil {
			return processKillResultMsg{message: fmt.Sprintf("Failed to kill PID %d: %v", pid, err)}
		}
		return processKillResultMsg{message: fmt.Sprintf("Sent %s to PID %d", sigName, pid)}
	}
}

func (m *ResponsiveTUIModel) fetchData() tea.Cmd {
	params := gops.MetaParams{
		SortBy:        m.sortBy,
		EnableCPU:     true,
		MergeChildren: m.mergeChildren,
		CPUCursor:     m.cpuCursor,
		ProcCursor:    m.procCursor,
	}
	generation := m.fetchGeneration

	return func() tea.Msg {
		modules := []string{"cpu", "memory", "system", "processes"}
		metrics, err := m.gops.GetMeta(context.Background(), modules, params)
		if err != nil {
			return fetchDataMsg{err: err, generation: generation}
		}

		cpuCursor := ""
		if metrics.CPU != nil {
			cpuCursor = metrics.CPU.Cursor
		}

		return fetchDataMsg{
			metrics: &models.SystemMetrics{
				CPU:       metrics.CPU,
				Memory:    metrics.Memory,
				System:    metrics.System,
				Network:   metrics.Network,
				Disk:      metrics.Disk,
				Processes: metrics.Processes,
			},
			generation: generation,
			cpuCursor:  cpuCursor,
			procCursor: metrics.Cursor,
		}
	}
}

func (m *ResponsiveTUIModel) fetchNetworkData() tea.Cmd {
	cursor := m.networkCursor
	return func() tea.Msg {
		rates, err := m.gops.GetNetworkRates(cursor)
		return fetchNetworkMsg{rates: rates, err: err}
	}
}

func (m *ResponsiveTUIModel) fetchDiskData() tea.Cmd {
	cursor := m.diskCursor
	return func() tea.Msg {
		rates, err := m.gops.GetDiskRates(cursor)
		return fetchDiskMsg{rates: rates, err: err}
	}
}

func (m *ResponsiveTUIModel) fetchTemperatureData() tea.Cmd {
	return func() tea.Msg {
		temps, err := m.gops.GetSystemTemperatures()
		return fetchTempMsg{temps: temps, err: err}
	}
}
