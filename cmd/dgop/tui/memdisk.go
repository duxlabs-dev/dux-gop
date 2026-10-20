package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-gop/models"
)

const (
	maxMountsShown  = 3
	maxSensorsShown = 6
)

func (m *ResponsiveTUIModel) renderMemDiskPanel(width, height int) string {
	var lines []string
	lines = append(lines, m.memorySection(width)...)
	lines = append(lines, "")
	lines = append(lines, m.diskSection(width)...)
	lines = append(lines, m.sensorSection()...)

	return m.panelStyle(width, height).Render(fitHeight(strings.Join(lines, "\n"), height-2))
}

func (m *ResponsiveTUIModel) memorySection(width int) []string {
	lines := []string{m.titleStyle().Render("MEMORY")}

	if m.metrics == nil || m.metrics.Memory == nil {
		return append(lines, "Loading memory info...")
	}

	mem := m.metrics.Memory
	barWidth := max(width-15, 8)

	bar := m.renderProgressBar(mem.Used, mem.Total, barWidth, "memory")
	lines = append(lines, fmt.Sprintf("%s %.1f%%", bar, mem.UsedPercent))
	lines = append(lines, fmt.Sprintf("%s/%s used", formatKB(mem.Used), formatKB(mem.Total)))
	lines = append(lines, fmt.Sprintf("avail %s cache %s buff %s",
		formatKB(mem.Available), formatKB(mem.Cached+mem.SReclaimable), formatKB(mem.Buffers)))

	if mem.SwapTotal == 0 {
		return lines
	}

	swapUsed := mem.SwapTotal - mem.SwapFree
	swapBar := m.renderProgressBar(swapUsed, mem.SwapTotal, barWidth, "memory")
	swapPercent := float64(swapUsed) / float64(mem.SwapTotal) * 100
	lines = append(lines, fmt.Sprintf("%s %.1f%%", swapBar, swapPercent))
	lines = append(lines, fmt.Sprintf("%s/%s swap", formatKB(swapUsed), formatKB(mem.SwapTotal)))
	return lines
}

func (m *ResponsiveTUIModel) diskSection(width int) []string {
	lines := []string{m.titleStyle().Render("DISK")}

	if m.metrics == nil || len(m.metrics.DiskMounts) == 0 {
		return append(lines, "Loading...")
	}

	shown := 0
	for _, mount := range m.metrics.DiskMounts {
		if shown >= maxMountsShown {
			break
		}
		if isPseudoMount(mount) {
			continue
		}
		lines = append(lines, m.renderMountLines(mount, width)...)
		shown++
	}

	return append(lines, m.diskRateLines(width)...)
}

func isPseudoMount(mount *models.DiskMountInfo) bool {
	if mount.Device == "tmpfs" || mount.Device == "devtmpfs" {
		return true
	}
	for _, prefix := range []string{"/dev", "/proc", "/sys", "/run"} {
		if strings.HasPrefix(mount.Mount, prefix) {
			return true
		}
	}
	return false
}

func (m *ResponsiveTUIModel) renderMountLines(mount *models.DiskMountInfo, width int) []string {
	name := fmt.Sprintf("%s → %s", truncate(mount.Device, 15), mount.Mount)
	if lipgloss.Width(name) > width-8 {
		name = fmt.Sprintf("%s → %s", truncate(mount.Device, 8), mount.Mount)
	}
	if lipgloss.Width(name) > width-8 {
		name = mount.Mount
	}

	percent, _ := strconv.ParseFloat(strings.TrimSuffix(mount.Percent, "%"), 64)
	barWidth := max(width-20, 10)

	bar := m.renderProgressBar(uint64(percent*100), 10000, barWidth, "disk")
	return []string{name, fmt.Sprintf("%s %s/%s", bar, mount.Used, mount.Size)}
}

func (m *ResponsiveTUIModel) diskRateLines(width int) []string {
	if len(m.diskHistory) < 2 {
		return nil
	}

	chartWidth := max(width-18, 10)

	reads := make([]float64, len(m.diskHistory))
	writes := make([]float64, len(m.diskHistory))
	var maxRate float64
	for i, sample := range m.diskHistory {
		reads[i] = sample.readRate
		writes[i] = sample.writeRate
		maxRate = max(maxRate, sample.readRate, sample.writeRate)
	}

	colors := m.getColors()
	readStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Charts.DiskRead))
	writeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Charts.DiskWrite))

	latest := m.diskHistory[len(m.diskHistory)-1]
	return []string{
		"",
		fmt.Sprintf("R %8s/s %s", formatBytes(uint64(latest.readRate)), readStyle.Render(sparkline(reads, chartWidth, maxRate))),
		fmt.Sprintf("W %8s/s %s", formatBytes(uint64(latest.writeRate)), writeStyle.Render(sparkline(writes, chartWidth, maxRate))),
	}
}

func (m *ResponsiveTUIModel) sensorSection() []string {
	if len(m.systemTemperatures) == 0 {
		return nil
	}

	lines := []string{"", m.titleStyle().Render("SENSORS")}

	sensors := m.systemTemperatures
	if len(sensors) > maxSensorsShown {
		sensors = sensors[:maxSensorsShown]
	}

	for _, sensor := range sensors {
		tempStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.getTemperatureColor(sensor.Temperature)))
		temp := tempStyle.Render(fmt.Sprintf("%.0f°C", sensor.Temperature))
		lines = append(lines, fmt.Sprintf("%s: %s", truncate(sensor.Name, 20), temp))
	}

	return lines
}
