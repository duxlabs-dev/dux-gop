package tui

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-gop/config"
	"github.com/duxlabs-dev/dux-gop/gops"
)

func NewResponsiveTUIModel(gopsUtil *gops.GopsUtil) *ResponsiveTUIModel {
	return NewResponsiveTUIModelWithOptions(gopsUtil, false, false)
}

func NewResponsiveTUIModelWithOptions(gopsUtil *gops.GopsUtil, hideCPUCores, summarizeCores bool) *ResponsiveTUIModel {
	colorManager, err := config.NewColorManager()
	if err != nil {
		colorManager = nil
	}
	keybindManager, err := config.NewKeybindManager()
	if err != nil {
		keybindManager = nil
	}

	model := &ResponsiveTUIModel{
		gops:           gopsUtil,
		colorManager:   colorManager,
		keybindManager: keybindManager,
		sortBy:         gops.SortByCPU,
		selectedPID:    -1,
		hideCPUCores:   hideCPUCores,
		summarizeCores: summarizeCores,
		mergeChildren:  true,
	}

	model.processTable = table.New(
		table.WithColumns([]table.Column{
			{Title: "PID", Width: 5},
			{Title: "USER", Width: 4},
			{Title: "CPU", Width: 3},
			{Title: "MEMORY", Width: 18},
			{Title: "COMMAND", Width: 53},
		}),
		table.WithHeight(20),
		table.WithFocused(true),
	)
	model.processTable.SetStyles(model.tableStyles())

	if keybindManager != nil {
		model.keybinds = keybindManager.Resolve()
	} else {
		model.keybinds = defaultResolvedKeybinds()
	}

	hardware, _ := gopsUtil.GetSystemHardware()
	model.hardware = hardware
	model.distroLogo, model.distroColor = getDistroInfo(hardware)

	return model
}

func (m *ResponsiveTUIModel) tableStyles() table.Styles {
	colors := m.getColors()

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colors.UI.BorderPrimary)).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(colors.UI.SelectionText)).
		Background(lipgloss.Color(colors.UI.SelectionBackground)).
		Bold(false)
	return s
}

func (m *ResponsiveTUIModel) updateTableStyles() {
	m.processTable.SetStyles(m.tableStyles())
}

func (m *ResponsiveTUIModel) renderProgressBar(used, total uint64, width int, colorType string) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	percentage := float64(used) / float64(total) * 100.0
	usedWidth := int(math.Round(float64(width) * float64(used) / float64(total)))
	if usedWidth == 0 && used > 0 {
		usedWidth = 1
	}
	if usedWidth > width {
		usedWidth = width
	}

	emptyChar := "░"
	if colorType == "cpu" {
		emptyChar = " "
	}
	bar := strings.Repeat("▓", usedWidth) + strings.Repeat(emptyChar, width-usedWidth)

	color := m.getProgressBarColor(percentage, colorType)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(bar)
}

func (m *ResponsiveTUIModel) systemInfoLines() []string {
	if m.hardware == nil {
		return nil
	}

	username := os.Getenv("USER")
	if username == "" {
		username = "user"
	}

	lines := []string{
		m.hardware.Distro,
		username + "@" + m.hardware.Hostname,
		strings.TrimSpace(m.hardware.Kernel + " " + m.hardware.Arch),
		m.hardware.BIOS.Motherboard,
		strings.TrimSpace(m.hardware.BIOS.Version + " " + m.hardware.BIOS.Date),
	}

	if m.metrics != nil && m.metrics.CPU != nil && len(m.metrics.CPU.CoreUsage) > 0 {
		lines = append(lines, fmt.Sprintf("%d threads", len(m.metrics.CPU.CoreUsage)))
	}
	if m.metrics != nil && m.metrics.System != nil && m.metrics.System.BootTime != "" {
		lines = append(lines, "Uptime: "+m.metrics.System.BootTime)
	}

	return lines
}

func (m *ResponsiveTUIModel) renderSystemInfoPanel(width, height int) string {
	innerWidth := width - 4
	innerHeight := height - 2

	info := m.systemInfoLines()
	logoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.distroColor))
	logoWidth := maxLineWidth(m.distroLogo)

	var content string
	if innerWidth < 35 || logoWidth+15 > innerWidth {
		content = m.renderSystemStacked(info, logoStyle, innerWidth)
	} else {
		content = m.renderSystemSideBySide(info, logoStyle, innerWidth, logoWidth)
	}

	return m.panelStyle(width, height).Render(fitHeight(content, innerHeight))
}

func (m *ResponsiveTUIModel) renderSystemSideBySide(info []string, logoStyle lipgloss.Style, innerWidth, logoWidth int) string {
	textWidth := max(innerWidth-logoWidth-2, 10)

	rows := max(len(info), len(m.distroLogo))
	lines := make([]string, 0, rows)
	for i := range rows {
		left := ""
		if i < len(info) {
			left = truncate(info[i], textWidth)
		}
		pad := strings.Repeat(" ", textWidth-lipgloss.Width(left))
		if i == 0 && left != "" {
			left = m.titleStyle().Render(left)
		}

		right := ""
		if i < len(m.distroLogo) {
			right = logoStyle.Render(m.distroLogo[i])
		}

		lines = append(lines, left+pad+"  "+right)
	}

	return strings.Join(lines, "\n")
}

func (m *ResponsiveTUIModel) renderSystemStacked(info []string, logoStyle lipgloss.Style, innerWidth int) string {
	var lines []string
	for i, line := range info {
		line = truncate(line, innerWidth)
		if i == 0 {
			line = m.titleStyle().Render(line)
		}
		lines = append(lines, line)
	}

	if len(lines) > 0 && len(m.distroLogo) > 0 {
		lines = append(lines, "")
	}

	for _, line := range m.distroLogo {
		pad := (innerWidth - lipgloss.Width(line)) / 2
		if pad > 0 {
			line = strings.Repeat(" ", pad) + line
		}
		lines = append(lines, logoStyle.Render(line))
	}

	return strings.Join(lines, "\n")
}

func maxLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	return width
}

func fitHeight(content string, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return strings.Join(lines[:height], "\n")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 2 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-2]) + ".."
}

func wrapText(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}

	var out strings.Builder
	line := ""
	for word := range strings.FieldsSeq(s) {
		switch {
		case line == "":
			line = word
		case len(line)+len(word)+1 > width:
			out.WriteString(line)
			out.WriteByte('\n')
			line = word
		default:
			line += " " + word
		}
	}
	out.WriteString(line)
	return out.String()
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatKB(kb uint64) string {
	return formatBytes(kb * 1024)
}
