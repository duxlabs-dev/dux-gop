package tui

import (
	"fmt"
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
)

func (m *ResponsiveTUIModel) renderNetworkPanel(width, height int) string {
	style := m.panelStyle(width, height)
	innerHeight := height - 2

	title := "NETWORK"
	switch {
	case m.selectedInterfaceName != "":
		title = strings.ToUpper(m.selectedInterfaceName)
	case m.metrics != nil && len(m.metrics.Network) > 0:
		title = m.metrics.Network[0].Name
	}

	var content strings.Builder
	content.WriteString(m.titleStyle().Render(title))
	content.WriteByte('\n')

	if len(m.networkHistory) == 0 {
		content.WriteString("Loading...")
		return style.Render(fitHeight(content.String(), innerHeight))
	}

	latest := m.networkHistory[len(m.networkHistory)-1]
	fmt.Fprintf(&content, "↓%s/s ↑%s/s\n", formatBytes(uint64(latest.rxRate)), formatBytes(uint64(latest.txRate)))

	chartHeight := innerHeight - 3
	if chartHeight > 0 {
		content.WriteString(m.renderSplitNetworkGraph(m.networkHistory, width-2, chartHeight))
	}

	totals := fmt.Sprintf("RX: %s TX: %s", formatBytes(latest.rxBytes), formatBytes(latest.txBytes))
	content.WriteByte('\n')
	content.WriteString(truncate(totals, width-4))

	return style.Render(fitHeight(content.String(), innerHeight))
}

func (m *ResponsiveTUIModel) renderSplitNetworkGraph(history []NetworkSample, width, height int) string {
	if len(history) == 0 || width < 10 || height < 3 {
		return strings.Repeat("─", width) + "\n"
	}

	var maxRxRate, maxTxRate float64
	for _, sample := range history {
		maxRxRate = max(maxRxRate, sample.rxRate)
		maxTxRate = max(maxTxRate, sample.txRate)
	}

	if maxRxRate == 0 && maxTxRate == 0 {
		return strings.Repeat("─", width) + "\n"
	}

	maxRxRate = max(maxRxRate, 1024)
	maxTxRate = max(maxTxRate, 1024)

	centerLine := height / 2
	downRows := centerLine
	upRows := height - centerLine - 1

	samplesPerCol := 1
	if len(history) > width {
		samplesPerCol = (len(history) + width - 1) / width
	}

	downChar := m.cachedNetDownChar
	upChar := m.cachedNetUpChar
	if downChar == "" || upChar == "" {
		m.getColors()
		downChar = m.cachedNetDownChar
		upChar = m.cachedNetUpChar
	}

	var result strings.Builder
	result.Grow(width * height * 4)

	for row := range height {
		if row > 0 {
			result.WriteString("\n")
		}
		if row == centerLine {
			result.WriteString(strings.Repeat("─", width))
			continue
		}

		for col := range width {
			avgRx, avgTx, ok := averageRates(history, col*samplesPerCol, samplesPerCol)
			if !ok {
				result.WriteString(" ")
				continue
			}

			if row < centerLine {
				downloadHeight := int(avgRx / maxRxRate * float64(downRows))
				if downloadHeight >= downRows-row {
					result.WriteString(downChar)
					continue
				}
				result.WriteString(" ")
				continue
			}

			uploadHeight := int(avgTx / maxTxRate * float64(upRows))
			if uploadHeight >= row-centerLine {
				result.WriteString(upChar)
				continue
			}
			result.WriteString(" ")
		}
	}

	return result.String()
}

func averageRates(history []NetworkSample, start, count int) (rx, tx float64, ok bool) {
	if start >= len(history) {
		return 0, 0, false
	}

	sampled := 0
	for i := start; i < start+count && i < len(history); i++ {
		rx += history[i].rxRate
		tx += history[i].txRate
		sampled++
	}
	return rx / float64(sampled), tx / float64(sampled), true
}

func (m *ResponsiveTUIModel) selectBestNetworkInterface(interfaces []*models.NetworkRateInfo) *models.NetworkRateInfo {
	if len(interfaces) == 0 {
		return nil
	}

	var candidates []*models.NetworkRateInfo
	for _, iface := range interfaces {
		if isVirtualInterface(iface.Interface) {
			continue
		}
		candidates = append(candidates, iface)
	}

	if len(candidates) == 0 {
		for _, iface := range interfaces {
			if iface.Interface != "lo" {
				return iface
			}
		}
		return interfaces[0]
	}

	var best *models.NetworkRateInfo
	var bestScore uint64
	for _, iface := range candidates {
		score := iface.RxTotal + iface.TxTotal
		if currentActivity := uint64(iface.RxRate + iface.TxRate); currentActivity > 0 {
			score += currentActivity * 1000
		}
		if best == nil || score > bestScore {
			best = iface
			bestScore = score
		}
	}

	return best
}

func isVirtualInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, prefix := range []string{"docker", "br-", "veth"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
