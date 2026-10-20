package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/duxlabs-dev/dux-gop/cmd/dgop/tui"
	"github.com/duxlabs-dev/dux-gop/gops"
)

func runTUIWithOptions(gopsUtil *gops.GopsUtil, hideCPUCores, summarizeCores bool) error {
	tui.Version = Version
	model := tui.NewResponsiveTUIModelWithOptions(gopsUtil, hideCPUCores, summarizeCores)
	defer model.Cleanup()

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)

	_, err := p.Run()
	return err
}
