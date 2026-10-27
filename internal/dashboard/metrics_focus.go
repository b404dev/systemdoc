package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The full-screen view shows one panel with a chart across the whole
// terminal and every series listed under it. Left and right flick between
// panels; the page keeps polling the shown panel at full width.

func (m *metricsPage) expand() {
	if m.selected >= len(m.panels) || m.focus != nil {
		return
	}
	p := m.w.palette()
	view := textView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderAttributes(tcell.AttrBold).SetTitleColor(tcell.GetColor(p.accent))
	view.SetInputCapture(m.focusInput)
	m.w.illuminatePanel(view.Box, func() bool { return true }, func(p palette) string { return panelHue(p, metricsTab) }, true)
	m.focus = view
	m.w.pages.AddPage("metrics-focus", view, true, true)
	m.w.app.SetFocus(view)
	m.repoll()
	m.refresh()
	m.renderFocus()
}

func (m *metricsPage) closeFocus() {
	if m.focus == nil {
		return
	}
	m.focus = nil
	m.w.pages.RemovePage("metrics-focus")
	m.repoll()
	m.rebuildGrid()
	m.render()
	m.focusPrimary()
}

// repoll asks for fresh samples at the current chart width right away.
func (m *metricsPage) repoll() {
	m.generation++
	m.busy = false
	m.lastPoll = time.Time{}
}

func (m *metricsPage) focusInput(e *tcell.EventKey) *tcell.EventKey {
	switch e.Key() {
	case tcell.KeyEscape, tcell.KeyEnter:
		m.closeFocus()
		return nil
	case tcell.KeyLeft, tcell.KeyUp:
		m.flick(-1)
		return nil
	case tcell.KeyRight, tcell.KeyDown:
		m.flick(1)
		return nil
	case tcell.KeyHome:
		m.flick(-len(m.panels))
		return nil
	case tcell.KeyEnd:
		m.flick(len(m.panels))
		return nil
	}
	switch e.Rune() {
	case 'h', 'k':
		m.flick(-1)
	case 'l', 'j':
		m.flick(1)
	case 'G':
		m.w.cycleGraphMode()
		m.render()
	case 'b':
		m.bars = !m.bars
		m.render()
	case 'r':
		m.repoll()
		m.refresh()
	case 'q':
		m.closeFocus()
	default:
		return e
	}
	return nil
}

// flick moves to another panel without leaving the full-screen view.
func (m *metricsPage) flick(delta int) {
	if len(m.panels) == 0 {
		return
	}
	m.selected = max(0, min(len(m.panels)-1, m.selected+delta))
	m.repoll()
	m.refresh()
	m.renderFocus()
}

func (m *metricsPage) renderFocus() {
	if m.focus == nil || m.selected >= len(m.panels) {
		return
	}
	p := m.w.palette()
	hue := panelHue(p, metricsTab)
	panel := m.panels[m.selected]
	family := strings.ToUpper(panel.family)
	if panel.pinned {
		family = "PIN"
	}
	m.focus.SetTitle(fmt.Sprintf(" %s · %s · %d of %d · ← → flick · Esc back ", tview.Escape(family), tview.Escape(panel.spec.Title), m.selected+1, len(m.panels)))
	width, chartWidth := m.width-4, m.chartWidth()
	chartRows := max(3, m.height-12)
	var lines []string
	switch {
	case panel.failure != "":
		lines = append(lines, "["+p.error+"]query failed: "+tview.Escape(panel.failure)+"[-]")
	case len(panel.series) == 0:
		lines = append(lines, "["+p.muted+"]"+panelEmptyText(panel)+"[-]")
	case panel.spec.Range:
		lead := panel.series[0]
		lines = append(lines, m.headline(panel, lead, width)+"   ["+p.muted+"]"+tview.Escape(panel.spec.Note)+"[-]", "")
		for _, row := range axisChart(lead.values, panel.spec.Unit, trendStyle{width: chartWidth + metricsAxisWidth, rows: chartRows, fixedMax: panel.ceiling, mode: m.w.settings.GraphMode, bars: m.bars}) {
			lines = append(lines, gradientText(row, hue, p.accent))
		}
		lines = append(lines, "["+p.muted+"]"+tview.Escape(m.cardCaption(panel, width))+"[-]", "")
		lines = append(lines, m.seriesTable(panel, width)...)
	default:
		lines = append(lines, m.headline(panel, panel.series[0], width)+"   ["+p.muted+"]"+tview.Escape(panel.spec.Note)+"[-]", "")
		lines = append(lines, m.renderBars(panel, len(panel.series), width)...)
	}
	lines = append(lines, "", "["+p.muted+"]"+tview.Escape(panel.spec.Expr(m.selectors()[panel.family]))+"[-]")
	m.focus.SetText(strings.Join(lines, "\n"))
}

// seriesTable lists every series with its latest value and a one-row trend.
func (m *metricsPage) seriesTable(panel *metricsPanel, width int) []string {
	p := m.w.palette()
	if len(panel.series) < 2 {
		return nil
	}
	labelWidth := min(28, width/3)
	sparkWidth := max(8, width-labelWidth-14)
	lines := []string{"[" + p.muted + "]SERIES[-]"}
	for _, series := range panel.series {
		name := ellipsize(seriesName(series.label, panel.spec.Legend), labelWidth)
		spark := signalChart(trimHistory(series.values, sparkWidth), m.w.settings.GraphMode)
		lines = append(lines, fmt.Sprintf("%-*s %10s  %s", labelWidth, tview.Escape(name), formatMetric(series.latest, panel.spec.Unit), gradientText(spark, panelHue(p, metricsTab), p.accent)))
	}
	return lines
}
