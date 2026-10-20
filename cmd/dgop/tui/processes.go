package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

func (m *ResponsiveTUIModel) activeSearchQuery() string {
	if m.searchActive {
		return m.searchInput
	}
	return m.searchQuery
}

func (m *ResponsiveTUIModel) visibleProcesses() []*models.ProcessInfo {
	if m.metrics == nil {
		return nil
	}

	query := strings.ToLower(m.activeSearchQuery())
	if query == "" {
		return m.metrics.Processes
	}

	filtered := make([]*models.ProcessInfo, 0, len(m.metrics.Processes))
	for _, proc := range m.metrics.Processes {
		if strings.Contains(strings.ToLower(proc.Command), query) ||
			strings.Contains(strings.ToLower(proc.FullCommand), query) {
			filtered = append(filtered, proc)
		}
	}
	return filtered
}

func (m *ResponsiveTUIModel) moveProcessCursor(delta int) {
	oldCursor := m.processTable.Cursor()
	if delta < 0 {
		m.processTable.MoveUp(-delta)
	} else {
		m.processTable.MoveDown(delta)
	}

	newCursor := m.processTable.Cursor()
	if newCursor == oldCursor {
		return
	}

	visible := m.visibleProcesses()
	if newCursor >= len(visible) {
		return
	}
	m.selectedPID = visible[newCursor].PID
}

func (m *ResponsiveTUIModel) updateProcessTable() {
	if m.metrics == nil {
		return
	}

	processes := m.visibleProcesses()

	columns := m.processTable.Columns()
	numCols := len(columns)
	commandWidth := 30
	fullCommandWidth := 0

	switch {
	case numCols == 6:
		commandWidth = columns[4].Width
		fullCommandWidth = columns[5].Width
	case numCols > 4:
		commandWidth = columns[4].Width
	}

	rows := make([]table.Row, 0, len(processes))
	selectedIndex := -1

	for i, proc := range processes {
		if m.selectedPID > 0 && proc.PID == m.selectedPID {
			selectedIndex = i
		}

		row := table.Row{
			strconv.Itoa(int(proc.PID)),
			truncate(proc.Username, 12),
			fmt.Sprintf("%.1f", proc.CPU),
			fmt.Sprintf("%.1f%% %s", proc.MemoryPercent, formatKB(proc.MemoryKB)),
			truncate(proc.Command, commandWidth),
		}
		if numCols == 6 {
			row = append(row, truncate(proc.FullCommand, fullCommandWidth))
		}
		rows = append(rows, row)
	}

	m.processTable.SetRows(rows)

	if len(rows) == 0 {
		m.processTable.SetCursor(0)
		return
	}

	switch {
	case selectedIndex >= 0:
		m.processTable.SetCursor(selectedIndex)
	case m.selectedPID == -1:
		m.processTable.SetCursor(0)
	case m.processTable.Cursor() >= len(rows):
		m.processTable.SetCursor(len(rows) - 1)
	}
}

func (m *ResponsiveTUIModel) sortProcessesLocally() {
	if m.metrics == nil || len(m.metrics.Processes) == 0 {
		return
	}

	processes := m.metrics.Processes
	switch m.sortBy {
	case gops.SortByCPU:
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].CPU > processes[j].CPU
		})
	case gops.SortByMemory:
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].MemoryKB > processes[j].MemoryKB
		})
	case gops.SortByName:
		sort.Slice(processes, func(i, j int) bool {
			return strings.ToLower(processes[i].Command) < strings.ToLower(processes[j].Command)
		})
	case gops.SortByPID:
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].PID < processes[j].PID
		})
	}
}

func (m *ResponsiveTUIModel) renderProcessPanel(width, height int) string {
	style := m.panelStyle(width, height)

	sortIndicator := ""
	switch m.sortBy {
	case gops.SortByCPU:
		sortIndicator = " ↓CPU"
	case gops.SortByMemory:
		sortIndicator = " ↓MEM"
	case gops.SortByName:
		sortIndicator = " ↓NAME"
	case gops.SortByPID:
		sortIndicator = " ↓PID"
	}

	groupIndicator := ""
	if m.mergeChildren {
		groupIndicator = " [grouped]"
	}

	searchIndicator := ""
	if !m.searchActive && m.searchQuery != "" {
		searchIndicator = " /" + m.searchQuery
	}

	title := fmt.Sprintf("PROCESSES (%d)%s%s%s", len(m.visibleProcesses()), sortIndicator, groupIndicator, searchIndicator)

	tableHeight := max(
		// borders + title line
		height-3, 3)

	m.updateProcessColumnWidthsForPanel(width - 4)
	m.processTable.SetHeight(tableHeight)

	return style.Render(m.titleStyle().Render(title) + "\n" + m.processTable.View())
}

func (m *ResponsiveTUIModel) renderProcessDetailsPanel(width, height int) string {
	style := m.panelStyle(width, height)
	innerHeight := height - 2
	title := m.titleStyle().Render("PROCESS DETAILS")

	visible := m.visibleProcesses()
	if len(visible) == 0 {
		return style.Render(fitHeight(title+"\nLoading process data...", innerHeight))
	}

	idx := m.processTable.Cursor()
	if idx >= len(visible) {
		return style.Render(fitHeight(title+"\nNo process selected", innerHeight))
	}

	proc := visible[idx]
	var content strings.Builder
	content.WriteString(title)
	content.WriteByte('\n')
	fmt.Fprintf(&content, "PID: %d  PPID: %d  USER: %s\n", proc.PID, proc.PPID, proc.Username)
	fmt.Fprintf(&content, "CPU: %.1f%%  Memory: %.1f%% (%s)\n", proc.CPU, proc.MemoryPercent, formatKB(proc.MemoryKB))

	if proc.RSSKB > 0 || proc.PSSKB > 0 {
		fmt.Fprintf(&content, "RSS: %s  PSS: %s\n", formatKB(proc.RSSKB), formatKB(proc.PSSKB))
	}
	if proc.ChildCount > 0 {
		fmt.Fprintf(&content, "Children: %d\n", proc.ChildCount)
	}
	if proc.ExecutablePath != "" {
		fmt.Fprintf(&content, "Exe: %s\n", truncate(proc.ExecutablePath, width-10))
	}

	fmt.Fprintf(&content, "Command: %s\n", proc.Command)
	content.WriteString(wrapText("Full Command: "+proc.FullCommand, width-6))

	return style.Render(fitHeight(content.String(), innerHeight))
}

func (m *ResponsiveTUIModel) updateProcessColumnWidthsForPanel(totalWidth int) {
	if m.lastTableWidth == totalWidth {
		return
	}
	m.lastTableWidth = totalWidth

	bordersPadding := 16
	availableWidth := totalWidth - bordersPadding

	pidWidth := 5
	userWidth := 6
	cpuWidth := 5
	memWidth := 13

	fixedColumnsWidth := pidWidth + userWidth + cpuWidth + memWidth
	if availableWidth < fixedColumnsWidth+10 {
		memWidth = 11
		fixedColumnsWidth = pidWidth + userWidth + cpuWidth + memWidth
	}

	minCommandWidth := 15
	minFullCommandWidth := 20
	remainingWidth := availableWidth - fixedColumnsWidth

	var columns []table.Column
	switch {
	case remainingWidth >= minCommandWidth+minFullCommandWidth+2:
		commandWidth := minCommandWidth
		fullCommandWidth := remainingWidth - commandWidth
		if fullCommandWidth > 60 {
			fullCommandWidth = 60
			commandWidth = remainingWidth - fullCommandWidth
		}
		columns = []table.Column{
			{Title: "PID", Width: pidWidth},
			{Title: "USER", Width: userWidth},
			{Title: "CPU%", Width: cpuWidth},
			{Title: "MEM%", Width: memWidth},
			{Title: "COMMAND", Width: commandWidth},
			{Title: "FULL COMMAND", Width: fullCommandWidth},
		}
	default:
		commandWidth := min(max(remainingWidth, 8), 80)
		columns = []table.Column{
			{Title: "PID", Width: pidWidth},
			{Title: "USER", Width: userWidth},
			{Title: "CPU%", Width: cpuWidth},
			{Title: "MEM%", Width: memWidth},
			{Title: "COMMAND", Width: commandWidth},
		}
	}

	m.processTable.SetRows([]table.Row{})
	m.processTable.SetColumns(columns)
	m.processTable.UpdateViewport()
	m.updateProcessTable()
}
