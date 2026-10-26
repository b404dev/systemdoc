package dashboard

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"systemdoc/internal/hostgpu"
)

func gpuHistoryKey(gpu hostgpu.Device, index int) string {
	if gpu.ID != "" {
		return gpu.ID + ":" + gpu.Name
	}
	return fmt.Sprintf("%d:%s", index, gpu.Name)
}

// Each device uses the same headline and two-row history as CPU and memory.
// Additional devices scroll within the card instead of expanding the dashboard.
func (w *workspace) renderGPUCard() {
	w.renderGPUCardInto(w.cards[4], w.lastWidth, len(w.cards))
}

func (w *workspace) renderGPUCardInto(card *tview.TextView, terminalWidth, cardCount int) {
	p := w.palette()
	card.SetTitle(" " + w.iconLabel(iconResources, "HOST GPU ")).SetTitleColor(tcell.GetColor(p.accent)).SetBorderColor(tcell.GetColor(p.accent))
	gpus := w.hostUsage.gpus
	if len(gpus) == 0 {
		card.SetText(fmt.Sprintf("[%s]Usage unavailable\nDriver metrics needed[-]", p.muted))
		return
	}
	width := max(1, terminalWidth/cardCount-4)
	if terminalWidth == 0 {
		width = 20
	}
	var rows []string
	for i, gpu := range gpus {
		busy := fmt.Sprintf("[%s]—[-]", p.muted)
		if gpu.Busy >= 0 {
			busy = fmt.Sprintf("[%s::b]%.0f%%[-::-]", pressureHue(p, int(gpu.Busy)), gpu.Busy)
		}
		temp, memory := "—°C", "—/—"
		if gpu.Temperature >= 0 {
			temp = fmt.Sprintf("%.0f°C", gpu.Temperature)
		}
		if gpu.MemoryUsed >= 0 && gpu.MemoryTotal > 0 && gpu.MemoryUsed <= gpu.MemoryTotal {
			memory = compactGPUBytes(gpu.MemoryUsed) + "/" + compactGPUBytes(gpu.MemoryTotal)
		}
		label := ""
		if len(gpus) > 1 {
			label = fmt.Sprintf("%d ", i)
		}
		history := w.hostGPUHistory[gpuHistoryKey(gpu, i)]
		trend := signalArea(trimHistory(history, width), 100, w.settings.GraphMode)
		if len(history) > 0 {
			trend = gradientText(trend, p.accent, p.glow)
		}
		rows = append(rows, fmt.Sprintf("[%s]%s[-]%s [%s]%s %s[-]\n%s", p.muted, label, busy, p.text, memory, temp, trend))
	}
	if len(gpus) == 1 && terminalWidth >= 160 {
		card.SetTitle(" " + w.iconLabel(iconResources, "HOST GPU · ") + tview.Escape(gpus[0].Name) + " ")
	}
	if len(gpus) > 1 {
		card.SetTitle(fmt.Sprintf(" %s· %d ", w.iconLabel(iconResources, "HOST GPU "), len(gpus)))
	}
	card.SetText(strings.Join(rows, "\n"))
}

func compactGPUBytes(value int64) string {
	number, unit, _ := strings.Cut(hostBytes(value, false), " ")
	return strings.TrimSuffix(number, ".0") + strings.TrimSuffix(unit, "iB")
}
