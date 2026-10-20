package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

var Version = "dev"

const (
	minTerminalWidth  = 60
	minTerminalHeight = 20
)

func (m *ResponsiveTUIModel) Init() tea.Cmd {
	diskMounts, _ := m.gops.GetDiskMounts()
	m.diskMounts = diskMounts

	cmds := []tea.Cmd{tick(), m.fetchData(), m.fetchTemperatureData()}

	if m.colorManager != nil {
		cmds = append(cmds, m.listenForColorChanges())
	}

	if m.keybindManager != nil {
		cmds = append(cmds, m.listenForKeybindChanges())
	}

	return tea.Batch(cmds...)
}

func (m *ResponsiveTUIModel) listenForColorChanges() tea.Cmd {
	return func() tea.Msg {
		<-m.colorManager.ColorChanges()
		return colorUpdateMsg{}
	}
}

func (m *ResponsiveTUIModel) listenForKeybindChanges() tea.Cmd {
	return func() tea.Msg {
		<-m.keybindManager.Changes()
		return keybindUpdateMsg{}
	}
}

func (m *ResponsiveTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		return m, tea.Batch(m.scheduledFetches()...)

	case fetchDataMsg:
		m.applyFetchData(msg)
		return m, nil

	case fetchNetworkMsg:
		m.applyNetworkData(msg)
		return m, nil

	case fetchDiskMsg:
		m.applyDiskData(msg)
		return m, nil

	case fetchTempMsg:
		if msg.err == nil {
			m.systemTemperatures = msg.temps
		}
		return m, nil

	case processKillResultMsg:
		m.killResultMsg = msg.message
		m.killResultTime = time.Now()
		m.fetchGeneration++
		return m, m.fetchData()

	case colorUpdateMsg:
		m.refreshColorCache()
		m.updateTableStyles()
		return m, m.listenForColorChanges()

	case keybindUpdateMsg:
		if m.keybindManager != nil {
			m.keybinds = m.keybindManager.Resolve()
		}
		return m, m.listenForKeybindChanges()
	}

	return m, nil
}

func (m *ResponsiveTUIModel) scheduledFetches() []tea.Cmd {
	cmds := []tea.Cmd{tick()}
	now := time.Now()

	if now.Sub(m.lastUpdate) >= time.Second {
		cmds = append(cmds, m.fetchData())
	}

	if now.Sub(m.lastNetworkUpdate) >= 2*time.Second {
		m.lastNetworkUpdate = now
		cmds = append(cmds, m.fetchNetworkData())
	}

	if now.Sub(m.lastDiskUpdate) >= 2*time.Second {
		m.lastDiskUpdate = now
		cmds = append(cmds, m.fetchDiskData())
	}

	if now.Sub(m.lastTempUpdate) >= 10*time.Second {
		m.lastTempUpdate = now
		cmds = append(cmds, m.fetchTemperatureData())
	}

	return cmds
}

func (m *ResponsiveTUIModel) applyFetchData(msg fetchDataMsg) {
	if msg.generation != m.fetchGeneration {
		return
	}

	m.metrics = msg.metrics
	m.err = msg.err
	m.cpuCursor = msg.cpuCursor
	m.procCursor = msg.procCursor
	m.lastUpdate = time.Now()

	if m.metrics != nil {
		m.metrics.DiskMounts = m.diskMounts
		if m.metrics.CPU != nil {
			m.cpuHistory = append(m.cpuHistory, m.metrics.CPU.Usage)
			if len(m.cpuHistory) > maxCPUHistory {
				m.cpuHistory = m.cpuHistory[1:]
			}
		}
	}

	m.updateProcessTable()
}

func (m *ResponsiveTUIModel) applyNetworkData(msg fetchNetworkMsg) {
	if msg.rates == nil || len(msg.rates.Interfaces) == 0 {
		return
	}
	m.networkCursor = msg.rates.Cursor

	best := m.selectBestNetworkInterface(msg.rates.Interfaces)
	if best == nil {
		return
	}
	m.selectedInterfaceName = best.Interface

	m.networkHistory = append(m.networkHistory, NetworkSample{
		rxBytes: best.RxTotal,
		txBytes: best.TxTotal,
		rxRate:  best.RxRate,
		txRate:  best.TxRate,
	})
	if len(m.networkHistory) > maxNetHistory {
		m.networkHistory = m.networkHistory[1:]
	}
}

func (m *ResponsiveTUIModel) applyDiskData(msg fetchDiskMsg) {
	if msg.rates == nil || len(msg.rates.Disks) == 0 {
		return
	}
	m.diskCursor = msg.rates.Cursor

	var sample DiskSample
	for _, disk := range msg.rates.Disks {
		sample.readRate += disk.ReadRate
		sample.writeRate += disk.WriteRate
	}

	m.diskHistory = append(m.diskHistory, sample)
	if len(m.diskHistory) > maxDiskHistory {
		m.diskHistory = m.diskHistory[1:]
	}
}

func (m *ResponsiveTUIModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	if m.killConfirmPID > 0 {
		return m, m.handleKillConfirmKey(m.action(key))
	}

	if m.searchActive {
		return m, m.handleSearchKey(msg, key)
	}

	return m.handleGlobalKey(msg, m.action(key))
}

func (m *ResponsiveTUIModel) handleKillConfirmKey(act models.KeyAction) tea.Cmd {
	switch act {
	case models.ActionCancel, models.ActionQuit:
		m.killConfirmPID = 0
		m.killConfirmSelection = 0
	case models.ActionSelectLeft:
		if m.killConfirmSelection > 0 {
			m.killConfirmSelection--
		}
	case models.ActionSelectRight:
		if m.killConfirmSelection < 1 {
			m.killConfirmSelection++
		}
	case models.ActionConfirm:
		pid := m.killConfirmPID
		force := m.killConfirmSelection == 1
		m.killConfirmPID = 0
		m.killConfirmSelection = 0
		return killProcess(pid, force)
	}
	return nil
}

func (m *ResponsiveTUIModel) handleSearchKey(msg tea.KeyMsg, key string) tea.Cmd {
	switch {
	case key == "esc" || key == "escape":
		m.searchActive = false
		m.searchInput = ""
	case key == "enter":
		m.searchActive = false
		m.searchQuery = m.searchInput
		m.searchInput = ""
	case key == "up":
		m.moveProcessCursor(-1)
	case key == "down":
		m.moveProcessCursor(1)
	case key == "backspace":
		if m.searchInput == "" {
			m.searchActive = false
			break
		}
		runes := []rune(m.searchInput)
		m.searchInput = string(runes[:len(runes)-1])
	case msg.Type == tea.KeyRunes, msg.Type == tea.KeySpace:
		m.searchInput += string(msg.Runes)
	}

	m.updateProcessTable()
	return nil
}

func (m *ResponsiveTUIModel) handleGlobalKey(msg tea.KeyMsg, act models.KeyAction) (tea.Model, tea.Cmd) {
	if sortBy, ok := sortForAction(act); ok {
		return m, m.applySort(sortBy)
	}

	switch act {
	case models.ActionQuit:
		return m, tea.Quit
	case models.ActionRefresh:
		m.fetchGeneration++
		return m, m.fetchData()
	case models.ActionDetails:
		m.showDetails = !m.showDetails
	case models.ActionSearch:
		m.searchActive = true
		m.searchInput = ""
		m.updateProcessTable()
	case models.ActionCancel:
		if m.searchQuery == "" {
			return m, nil
		}
		m.searchQuery = ""
		m.updateProcessTable()
	case models.ActionKill:
		m.beginKillConfirm()
	case models.ActionGroup:
		m.mergeChildren = !m.mergeChildren
		m.fetchGeneration++
		return m, m.fetchData()
	case models.ActionNavUp:
		m.moveProcessCursor(-1)
	case models.ActionNavDown:
		m.moveProcessCursor(1)
	default:
		var cmd tea.Cmd
		m.processTable, cmd = m.processTable.Update(msg)
		return m, cmd
	}

	return m, nil
}

func sortForAction(act models.KeyAction) (gops.ProcSortBy, bool) {
	switch act {
	case models.ActionSortCPU:
		return gops.SortByCPU, true
	case models.ActionSortMemory:
		return gops.SortByMemory, true
	case models.ActionSortName:
		return gops.SortByName, true
	case models.ActionSortPID:
		return gops.SortByPID, true
	default:
		return "", false
	}
}

func (m *ResponsiveTUIModel) applySort(sortBy gops.ProcSortBy) tea.Cmd {
	if m.sortBy == sortBy {
		return nil
	}

	m.sortBy = sortBy
	m.fetchGeneration++
	m.sortProcessesLocally()
	m.updateProcessTable()
	return m.fetchData()
}

func (m *ResponsiveTUIModel) beginKillConfirm() {
	visible := m.visibleProcesses()
	idx := m.processTable.Cursor()
	if idx >= len(visible) {
		return
	}
	m.killConfirmPID = visible[idx].PID
	m.killConfirmSelection = 0
}

func (m *ResponsiveTUIModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	if m.width < minTerminalWidth || m.height < minTerminalHeight {
		return m.renderTooSmall()
	}

	return m.renderLayout()
}

func (m *ResponsiveTUIModel) renderTooSmall() string {
	msg := fmt.Sprintf("Terminal too small: %dx%d (need %dx%d)", m.width, m.height, minTerminalWidth, minTerminalHeight)
	if m.width <= 0 || m.height <= 0 {
		return msg
	}
	if lipgloss.Width(msg) > m.width {
		msg = msg[:m.width]
	}
	topPad := (m.height - 1) / 2
	return strings.Repeat("\n", topPad) + msg
}

func (m *ResponsiveTUIModel) renderLayout() string {
	header := m.renderHeader()
	footer := m.renderFooter()

	availableHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(footer), 8)

	return header + "\n" + m.renderMainContentWithHeight(availableHeight) + "\n" + footer
}

type panelSpec struct{ min, max, weight int }

func allocCapped(total int, specs []panelSpec, floor int, shrinkOrder []int) []int {
	out := make([]int, len(specs))
	sum := 0
	for i, s := range specs {
		out[i] = s.min
		sum += s.min
	}

	for sum > total {
		shrunk := false
		for _, i := range shrinkOrder {
			if out[i] <= floor {
				continue
			}
			out[i]--
			sum--
			shrunk = true
			if sum == total {
				break
			}
		}
		if !shrunk {
			break
		}
	}

	rem := total - sum
	for rem > 0 {
		progressed := false
		for i, s := range specs {
			if out[i] >= s.max || s.weight <= 0 {
				continue
			}
			out[i]++
			rem--
			progressed = true
			if rem == 0 {
				break
			}
		}
		if !progressed {
			break
		}
	}

	return out
}

func (m *ResponsiveTUIModel) minSystemLines() int {
	lines := max(len(m.distroLogo), len(m.systemInfoLines()))
	if lines < 7 {
		lines = 7
	}
	return lines
}

func (m *ResponsiveTUIModel) minCPULines(width int) int {
	lines := 2 // title + usage bar

	if len(m.cpuHistory) >= 2 && width-4 >= 10 {
		lines += cpuChartHeight
	}

	if m.metrics != nil && m.metrics.CPU != nil && !m.hideCPUCores {
		cores := len(m.metrics.CPU.CoreUsage)
		switch {
		case cores == 0:
		case m.summarizeCores:
			groupSize := summarizedGroupSize(cores)
			lines += (cores+groupSize-1)/groupSize + 1
		default:
			lines += (cores + 2) / 3
		}
	}

	if m.metrics != nil && m.metrics.System != nil {
		lines++
	}

	return lines
}

func (m *ResponsiveTUIModel) minMemDiskLines() int {
	lines := 4 // MEMORY header + bar + used + breakdown
	if m.metrics != nil && m.metrics.Memory != nil && m.metrics.Memory.SwapTotal > 0 {
		lines += 2
	}

	lines += 2 + 4 // blank + DISK header + 2 mounts

	if len(m.diskHistory) >= 2 {
		lines += 3 // blank + read + write sparklines
	}

	if len(m.systemTemperatures) > 0 {
		sensors := min(len(m.systemTemperatures), maxSensorsShown)
		lines += 2 + sensors
	}

	return lines
}

func (m *ResponsiveTUIModel) minNetworkLines() int {
	return 12
}

func (m *ResponsiveTUIModel) renderMainContentWithHeight(availableHeight int) string {
	leftWidth := m.width * 40 / 100
	spacer := 1
	rightWidth := max(m.width-leftWidth-spacer-4, 10)

	leftPanels := 3
	rightPanels := 2
	if m.showDetails {
		rightPanels = 3
	}

	leftInnerTotal := availableHeight - leftPanels*2
	rightInnerTotal := availableHeight - rightPanels*2
	if leftInnerTotal < 3 {
		leftInnerTotal = 3
	}
	if rightInnerTotal < 3 {
		rightInnerTotal = 3
	}

	sysMin := m.minSystemLines()
	memDiskMin := m.minMemDiskLines()
	netMin := m.minNetworkLines()

	leftSpecs := []panelSpec{
		{sysMin, sysMin, 0},
		{memDiskMin, 999, 3},
		{netMin, netMin + 8, 5},
	}
	leftInner := allocCapped(leftInnerTotal, leftSpecs, 3, []int{2, 1, 0})
	leftHeights := []int{leftInner[0] + 2, leftInner[1] + 2, leftInner[2] + 2}

	cpuMin := m.minCPULines(rightWidth)

	var rightHeights []int
	if m.showDetails {
		rightSpecs := []panelSpec{
			{cpuMin, cpuMin, 0},
			{6, 999, 3},
			{5, 24, 1},
		}
		rightInner := allocCapped(rightInnerTotal, rightSpecs, 3, []int{2, 1, 0})
		rightHeights = []int{rightInner[0] + 2, rightInner[1] + 2, rightInner[2] + 2}
	} else {
		rightSpecs := []panelSpec{
			{cpuMin, cpuMin, 0},
			{6, 999, 5},
		}
		rightInner := allocCapped(rightInnerTotal, rightSpecs, 3, []int{1, 0})
		rightHeights = []int{rightInner[0] + 2, rightInner[1] + 2}
	}

	leftColumn := lipgloss.JoinVertical(lipgloss.Left,
		m.renderSystemInfoPanel(leftWidth, leftHeights[0]),
		m.renderMemDiskPanel(leftWidth, leftHeights[1]),
		m.renderNetworkPanel(leftWidth, leftHeights[2]),
	)

	rightColumn := m.renderCPUPanel(rightWidth, rightHeights[0])
	rightColumn = lipgloss.JoinVertical(lipgloss.Left, rightColumn, m.renderProcessPanel(rightWidth, rightHeights[1]))
	if m.showDetails {
		rightColumn = lipgloss.JoinVertical(lipgloss.Left, rightColumn, m.renderProcessDetailsPanel(rightWidth, rightHeights[2]))
	}

	spacerCol := lipgloss.NewStyle().Width(spacer).Render(" ")
	return lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, spacerCol, rightColumn)
}

func (m *ResponsiveTUIModel) renderHeader() string {
	title := fmt.Sprintf("dgop %s", Version)
	clock := time.Now().Format("15:04:05")

	spaces := max(m.width-len(title)-len(clock)-4, 0)

	return m.headerStyle().Render(title + strings.Repeat(" ", spaces) + clock)
}

func (m *ResponsiveTUIModel) renderFooter() string {
	style := m.footerStyle()

	if m.killConfirmPID > 0 {
		return style.Render(m.killConfirmFooter())
	}

	if m.searchActive {
		return style.Render(fmt.Sprintf("Search: /%s█  [enter] apply  [esc] cancel  [↑↓] navigate", m.searchInput))
	}

	if m.killResultMsg != "" && time.Since(m.killResultTime) < 3*time.Second {
		return style.Render(m.killResultMsg)
	}
	m.killResultMsg = ""

	if m.err != nil {
		colors := m.getColors()
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Status.Error))
		return style.Render(errStyle.Render(fmt.Sprintf("error: %v", m.err)))
	}

	groupStatus := ""
	if m.mergeChildren {
		groupStatus = "*"
	}
	k := m.hint
	controls := fmt.Sprintf("Controls: [%s]uit [%s]efresh [%s]etails [%s]group%s [%s] kill [%s] search | Sort: [%s]cpu [%s]mem [%s]name [%s]pid | %s%s Navigate",
		k(models.ActionQuit), k(models.ActionRefresh), k(models.ActionDetails), k(models.ActionGroup), groupStatus, k(models.ActionKill), k(models.ActionSearch),
		k(models.ActionSortCPU), k(models.ActionSortMemory), k(models.ActionSortName), k(models.ActionSortPID),
		k(models.ActionNavUp), k(models.ActionNavDown))
	return style.Render(controls)
}

func (m *ResponsiveTUIModel) killConfirmFooter() string {
	colors := m.getColors()

	procName := ""
	if m.metrics != nil {
		for _, p := range m.metrics.Processes {
			if p.PID == m.killConfirmPID {
				procName = p.Command
				break
			}
		}
	}

	selected := lipgloss.NewStyle().
		Background(lipgloss.Color(colors.UI.SelectionBackground)).
		Foreground(lipgloss.Color(colors.UI.SelectionText)).
		Padding(0, 1)
	normal := lipgloss.NewStyle().Padding(0, 1)

	var parts []string
	for i, opt := range []string{"Kill (SIGTERM)", "Force Kill (SIGKILL)"} {
		if i == m.killConfirmSelection {
			parts = append(parts, selected.Render(opt))
			continue
		}
		parts = append(parts, normal.Render(opt))
	}

	return fmt.Sprintf("Kill PID %d (%s)?  %s  ESC cancel", m.killConfirmPID, procName, strings.Join(parts, " "))
}
