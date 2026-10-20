package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/config"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

const (
	maxNetHistory  = 60
	maxDiskHistory = 60
	maxCPUHistory  = 300
)

type NetworkSample struct {
	rxBytes uint64
	txBytes uint64
	rxRate  float64
	txRate  float64
}

type DiskSample struct {
	readRate  float64
	writeRate float64
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type colorUpdateMsg struct{}
type keybindUpdateMsg struct{}

func defaultResolvedKeybinds() map[string]models.KeyAction {
	out := make(map[string]models.KeyAction)
	for action, keys := range models.DefaultKeybinds() {
		for _, k := range keys {
			out[k] = action
		}
	}
	return out
}

type ResponsiveTUIModel struct {
	gops           *gops.GopsUtil
	colorManager   *config.ColorManager
	keybindManager *config.KeybindManager
	keybinds       map[string]models.KeyAction
	metrics        *models.SystemMetrics
	width          int
	height         int
	err            error
	lastUpdate     time.Time

	processTable table.Model

	hardware   *models.SystemHardware
	diskMounts []*models.DiskMountInfo

	networkHistory        []NetworkSample
	networkCursor         string
	lastNetworkUpdate     time.Time
	selectedInterfaceName string

	diskHistory    []DiskSample
	diskCursor     string
	lastDiskUpdate time.Time

	cpuHistory []float64
	cpuCursor  string
	procCursor string

	systemTemperatures []models.TemperatureSensor
	lastTempUpdate     time.Time

	sortBy          gops.ProcSortBy
	ready           bool
	showDetails     bool
	selectedPID     int32
	fetchGeneration int

	distroLogo  []string
	distroColor string

	hideCPUCores   bool
	summarizeCores bool
	mergeChildren  bool

	cachedColors      *models.ColorPalette
	cachedNetDownChar string
	cachedNetUpChar   string
	lastTableWidth    int

	killConfirmPID       int32
	killConfirmSelection int // 0=kill, 1=force kill
	killResultMsg        string
	killResultTime       time.Time

	searchActive bool
	searchInput  string
	searchQuery  string
}

func (m *ResponsiveTUIModel) Cleanup() {
	if m.colorManager != nil {
		m.colorManager.Close()
	}

	if m.keybindManager == nil {
		return
	}

	if err := m.keybindManager.Close(); err != nil {
		log.Warnf("Error closing keybind manager: %v", err)
	}
}

func (m *ResponsiveTUIModel) action(key string) models.KeyAction {
	if m.keybinds == nil {
		return ""
	}
	return m.keybinds[key]
}

func (m *ResponsiveTUIModel) hint(action models.KeyAction) string {
	if m.keybindManager != nil {
		return m.keybindManager.PrimaryKey(action)
	}
	if keys := models.DefaultKeybinds()[action]; len(keys) > 0 {
		return keys[0]
	}
	return ""
}
