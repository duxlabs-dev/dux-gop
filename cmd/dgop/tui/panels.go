package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-gop/models"
)

const cpuChartHeight = 2

func (m *ResponsiveTUIModel) renderCPUPanel(width, height int) string {
	style := m.panelStyle(width, height)
	innerHeight := height - 2

	if m.metrics == nil || m.metrics.CPU == nil {
		return style.Render(fitHeight("Loading CPU info...", innerHeight))
	}

	cpu := m.metrics.CPU
	var content strings.Builder

	content.WriteString(m.renderCPUTitleLine(cpu, width))
	content.WriteByte('\n')
	content.WriteString(m.renderCPUUsageLine(cpu, width))
	content.WriteByte('\n')

	for _, line := range m.renderCPUHistory(width) {
		content.WriteString(line)
		content.WriteByte('\n')
	}

	if len(cpu.CoreUsage) > 0 && !m.hideCPUCores {
		if m.summarizeCores {
			m.renderSummarizedCores(&content, cpu, width)
		} else {
			m.renderDetailedCores(&content, cpu, width)
		}
	}

	if m.metrics.System != nil {
		fmt.Fprintf(&content, "Load: %s | Tasks: %d | Threads: %d",
			m.metrics.System.LoadAvg, m.metrics.System.Processes, m.metrics.System.Threads)
	}

	return style.Render(fitHeight(content.String(), innerHeight))
}

func (m *ResponsiveTUIModel) renderCPUTitleLine(cpu *models.CPUInfo, width int) string {
	name := truncate(cpu.Model, width-10)
	freq := fmt.Sprintf("%.0fMHz", cpu.Frequency)

	spaces := max(width-5-lipgloss.Width(name)-len(freq), 1)

	return m.titleStyle().Render(name) + strings.Repeat(" ", spaces) + freq
}

func (m *ResponsiveTUIModel) renderCPUUsageLine(cpu *models.CPUInfo, width int) string {
	barWidth := max(width-15, 8)

	bar := m.renderProgressBar(uint64(cpu.Usage*100), 10000, barWidth, "cpu")
	return fmt.Sprintf("%s %3.0f%% %.0f°C", bar, cpu.Usage, cpu.Temperature)
}

func (m *ResponsiveTUIModel) renderCPUHistory(width int) []string {
	chartWidth := width - 4
	if chartWidth < 10 || len(m.cpuHistory) < 2 {
		return nil
	}

	colors := m.getColors()
	chartStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Charts.CPUCoreLow))

	rows := blockChart(m.cpuHistory, chartWidth, cpuChartHeight, 100)
	for i, row := range rows {
		rows[i] = chartStyle.Render(row)
	}
	return rows
}

func (m *ResponsiveTUIModel) renderDetailedCores(content *strings.Builder, cpu *models.CPUInfo, width int) {
	columnWidth := (width - 4) / 3
	barWidth := max(columnWidth-8, 6)

	for i, usage := range cpu.CoreUsage {
		bar := m.renderProgressBar(uint64(usage*100), 10000, barWidth, "cpu")
		fmt.Fprintf(content, "C%02d%s%3.0f%%", i, bar, usage)

		if i%3 == 2 || i == len(cpu.CoreUsage)-1 {
			content.WriteString("\n")
			continue
		}
		content.WriteString(" ")
	}
}

func summarizedGroupSize(cores int) int {
	if cores > 64 {
		return 16
	}
	return 8
}

func (m *ResponsiveTUIModel) renderSummarizedCores(content *strings.Builder, cpu *models.CPUInfo, width int) {
	totalCores := len(cpu.CoreUsage)
	groupSize := summarizedGroupSize(totalCores)

	barWidth := max(width-4-25, 10)

	for start := 0; start < totalCores; start += groupSize {
		end := min(start+groupSize, totalCores)

		var avgUsage, maxUsage float64
		activeCount := 0
		for _, usage := range cpu.CoreUsage[start:end] {
			avgUsage += usage
			maxUsage = max(maxUsage, usage)
			if usage > 1.0 {
				activeCount++
			}
		}
		avgUsage /= float64(end - start)

		bar := m.renderProgressBar(uint64(avgUsage*100), 10000, barWidth, "cpu")
		fmt.Fprintf(content, "C%02d-%02d %s %3.0f%% avg (max:%3.0f%% active:%d)\n",
			start, end-1, bar, avgUsage, maxUsage, activeCount)
	}

	var totalAvg float64
	totalActive := 0
	for _, usage := range cpu.CoreUsage {
		totalAvg += usage
		if usage > 1.0 {
			totalActive++
		}
	}
	totalAvg /= float64(totalCores)

	fmt.Fprintf(content, "Total: %d cores, %d active (>1%%), %.1f%% average\n",
		totalCores, totalActive, totalAvg)
}
