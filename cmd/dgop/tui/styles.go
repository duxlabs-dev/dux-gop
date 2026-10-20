package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-gop/models"
)

func (m *ResponsiveTUIModel) getColors() *models.ColorPalette {
	if m.cachedColors != nil {
		return m.cachedColors
	}
	m.refreshColorCache()
	return m.cachedColors
}

func (m *ResponsiveTUIModel) refreshColorCache() {
	if m.colorManager != nil {
		m.cachedColors = m.colorManager.GetPalette()
	} else {
		m.cachedColors = models.DefaultColorPalette()
	}
	m.cachedNetDownChar = lipgloss.NewStyle().Foreground(lipgloss.Color(m.cachedColors.Charts.NetworkDownload)).Render("█")
	m.cachedNetUpChar = lipgloss.NewStyle().Foreground(lipgloss.Color(m.cachedColors.Charts.NetworkUpload)).Render("▓")
}

func (m *ResponsiveTUIModel) panelStyle(width, height int) lipgloss.Style {
	colors := m.getColors()
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colors.UI.BorderPrimary)).
		Padding(0, 1).
		Width(width).
		MaxHeight(height)
}

func (m *ResponsiveTUIModel) titleStyle() lipgloss.Style {
	colors := m.getColors()
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colors.UI.TextAccent))
}

func (m *ResponsiveTUIModel) headerStyle() lipgloss.Style {
	colors := m.getColors()
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colors.UI.HeaderText)).
		Background(lipgloss.Color(colors.UI.HeaderBackground)).
		Bold(true).
		Width(m.width).
		Padding(0, 2)
}

func (m *ResponsiveTUIModel) footerStyle() lipgloss.Style {
	colors := m.getColors()
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colors.UI.FooterText)).
		Background(lipgloss.Color(colors.UI.FooterBackground)).
		Width(m.width).
		Padding(0, 2)
}

func (m *ResponsiveTUIModel) getProgressBarColor(usage float64, colorType string) string {
	colors := m.getColors()

	switch colorType {
	case "cpu":
		return pickByThreshold(usage, 80, 60, colors.ProgressBars.CPUHigh, colors.ProgressBars.CPUMedium, colors.ProgressBars.CPULow)
	case "memory":
		return pickByThreshold(usage, 80, 60, colors.ProgressBars.MemoryHigh, colors.ProgressBars.MemoryMedium, colors.ProgressBars.MemoryLow)
	case "disk":
		return pickByThreshold(usage, 90, 70, colors.ProgressBars.DiskHigh, colors.ProgressBars.DiskMedium, colors.ProgressBars.DiskLow)
	default:
		return colors.ProgressBars.MemoryLow
	}
}

func pickByThreshold(value, high, medium float64, highColor, mediumColor, lowColor string) string {
	switch {
	case value > high:
		return highColor
	case value > medium:
		return mediumColor
	default:
		return lowColor
	}
}

func (m *ResponsiveTUIModel) getTemperatureColor(temp float64) string {
	colors := m.getColors()

	switch {
	case temp > 85:
		return colors.Temperature.Danger
	case temp > 70:
		return colors.Temperature.Hot
	case temp > 50:
		return colors.Temperature.Warm
	default:
		return colors.Temperature.Cold
	}
}
