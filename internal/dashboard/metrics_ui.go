package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"systemdoc/internal/promapi"
	"systemdoc/internal/textutil"
)

const (
	metricsTab      = 5
	metricsPageName = "metrics"
)

type metricsView int

const (
	metricsPanelsView metricsView = iota
	metricsTargetsView
	metricsSetupView
)

var metricsViewLabels = []string{"p PANELS", "t TARGETS", "s SETUP"}

// metricsPage is the sixth suite: Prometheus trends, scrape targets and setup.
type metricsPage struct {
	*tview.Flex
	w            *workspace
	ctx          context.Context
	cancel       context.CancelFunc
	header       *tview.TextView
	navigation   *tview.Flex
	modeButtons  [6]*tview.Button
	deckButton   *tview.Button
	viewBar      *tview.Flex
	viewButtons  [3]*tview.Button
	grid         *tview.Flex
	cards        []*tview.TextView
	table        *tview.Table
	setup        *tview.TextView
	detail       *tview.TextView
	status       *tview.TextView
	footer       *tview.TextView
	view         metricsView
	state        metricsState
	endpoint     promEndpoint
	build        promapi.BuildInfo
	runtime      promapi.RuntimeInfo
	families     []promapi.Family
	names        map[string]bool
	instances    map[string][]string
	nodes        []string
	targets      []promapi.Target
	interval     time.Duration
	detectedAt   time.Time
	panels       []*metricsPanel
	selected     int
	firstRow     int
	columns      int
	visibleRows  int
	cardHeight   int
	bars         bool
	busy, paused bool
	generation   int
	pollFailures int
	lastPoll     time.Time
	sampledAt    time.Time
	failure      string
	autoRouted   bool
	install      []promapi.InstallStep
	installStep  int
	focus        *tview.TextView // full-screen view of the selected panel, nil when closed
	hidden       int             // panels the server had no data for
	width        int
	height       int
}

// metricsAxisWidth is the room a card keeps for the chart's axis labels.
const metricsAxisWidth = 9

func (w *workspace) metricsPage() *metricsPage {
	if _, page := w.pages.GetFrontPage(); page != nil {
		if m, ok := page.(*metricsPage); ok {
			m.focusPrimary()
			return m
		}
	}
	ctx, cancel := context.WithCancel(w.ctx)
	m := &metricsPage{Flex: tview.NewFlex().SetDirection(tview.FlexRow), w: w, ctx: ctx, cancel: cancel, columns: 1, visibleRows: 1, cardHeight: 6}
	p := w.palette()
	background, surface := tcell.GetColor(p.background), tcell.GetColor(p.surface)
	m.SetBackgroundColor(background)
	m.header = textView().SetDynamicColors(true).SetTextColor(tcell.GetColor(p.accent))
	m.status = textView().SetDynamicColors(true).SetWrap(false).SetTextColor(tcell.GetColor(p.muted))
	m.footer = textView().SetTextColor(tcell.GetColor(p.muted))
	for _, v := range []*tview.TextView{m.header, m.status, m.footer} {
		v.SetBackgroundColor(background)
	}
	hue := func(p palette) string { return panelHue(p, metricsTab) }
	m.detail = textView().SetDynamicColors(true).SetWrap(true).SetScrollable(true).SetTextColor(tcell.GetColor(p.text))
	m.detail.SetBackgroundColor(surface).SetBorder(true).SetTitle(" TARGET · Enter full record ").SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	w.illuminate(m.detail.Box, m.detail.HasFocus, hue)
	m.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	m.table.SetBackgroundColor(background).SetBorder(true).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	m.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(p.accent)).Foreground(background).Bold(true))
	m.table.SetSelectionChangedFunc(func(int, int) { m.renderTargetDetail() })
	m.table.SetSelectedFunc(func(int, int) { m.inspectTarget() })
	w.illuminate(m.table.Box, m.table.HasFocus, hue)
	m.setup = textView().SetDynamicColors(true).SetWrap(true).SetScrollable(true).SetTextColor(tcell.GetColor(p.text))
	m.setup.SetBackgroundColor(surface).SetBorder(true).SetTitle(" SETUP · j/k choose a step · Enter reviews and runs it ").SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	w.illuminate(m.setup.Box, m.setup.HasFocus, hue)
	m.grid = tview.NewFlex().SetDirection(tview.FlexRow)
	m.grid.SetBackgroundColor(background)
	nav, modeButtons, deck := w.hostNavigation(metricsTab, func(target int) {
		if target == metricsTab {
			m.focusPrimary()
		} else {
			m.close()
			w.openHostTab(target)
		}
	})
	copy(m.modeButtons[:], modeButtons)
	m.deckButton, m.navigation = deck, nav
	m.viewBar = tview.NewFlex()
	m.viewBar.SetBackgroundColor(background)
	for i, label := range metricsViewLabels {
		view := metricsView(i)
		button := tview.NewButton(label).SetSelectedFunc(func() { m.switchView(view) })
		m.viewButtons[i] = button
		m.viewBar.AddItem(button, controlButtonWidth(label)+2, 0, false)
	}
	m.viewBar.AddItem(tview.NewBox().SetBackgroundColor(background), 0, 1, false)
	m.AddItem(m.header, 1, 0, false).AddItem(nav, 1, 0, false).AddItem(m.viewBar, 1, 0, false).
		AddItem(m.grid, 0, 1, true).AddItem(m.table, 0, 0, false).AddItem(m.setup, 0, 0, false).
		AddItem(m.detail, 0, 0, false).AddItem(m.status, 1, 0, false).AddItem(m.footer, 1, 0, false)
	m.SetInputCapture(m.input)
	w.pages.AddPage(metricsPageName, m, true, true)
	m.render()
	m.refresh()
	go m.detectInstall()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.queueQuiet(func() {
					if ctx.Err() != nil {
						return
					}
					if front, _ := w.pages.GetFrontPage(); (front == metricsPageName || front == "metrics-focus") && !m.paused && pollDue(m.lastPoll, time.Now(), m.pollSeconds()) {
						m.refresh()
					}
				})
			}
		}
	}()
	return m
}

func (m *metricsPage) close() {
	m.cancel()
	m.w.pages.RemovePage(metricsPageName)
	m.w.app.SetFocus(m.w.table)
}

func (m *metricsPage) suiteIndex() int { return metricsTab }

func (m *metricsPage) focusPrimary() {
	switch m.view {
	case metricsTargetsView:
		m.w.app.SetFocus(m.table)
	case metricsSetupView:
		m.w.app.SetFocus(m.setup)
	default:
		if card := m.selectedCard(); card != nil {
			m.w.app.SetFocus(card)
		} else {
			m.w.app.SetFocus(m.grid)
		}
	}
}

// pollSeconds floors polling because every poll re-reads a window per panel.
func (m *metricsPage) pollSeconds() int { return max(metricsPollFloor, m.w.settings.RefreshSeconds) }

func (m *metricsPage) Draw(screen tcell.Screen) {
	_, _, width, height := m.GetRect()
	if m.width != width || m.height != height {
		m.width, m.height = width, height
		setSuiteLabels(m.modeButtons[:], width, m.w.settings.NerdIcons)
		for i, button := range m.modeButtons {
			m.navigation.ResizeItem(button, suiteButtonWidth(i, width), 0)
		}
		deckWidth := controlButtonWidth(m.deckButton.GetLabel())
		if width < 140 {
			deckWidth = 0
		}
		m.navigation.ResizeItem(m.deckButton, deckWidth, 0)
		m.ResizeItem(m.header, mastheadHeight(width, height), 0)
		m.header.SetBorderPadding(0, 0, 1, 0)
		m.layout()
		m.render()
	}
	m.Flex.Draw(screen)
	p := m.w.palette()
	rails := map[*tview.Box]string{}
	for _, card := range m.cards {
		rails[card.Box] = panelHue(p, metricsTab)
	}
	paintSurfaces(screen, m.Flex, p, m.header, m.table, rails, m.w.limitedColours)
}

// layout sizes the grid for the terminal: up to three cards across and as
// many rows as fit, each card tall enough for a chart.
func (m *metricsPage) layout() {
	m.columns = 1
	if m.width >= 150 {
		m.columns = 3
	} else if m.width >= 110 {
		m.columns = 2
	}
	chartRows := 2
	switch {
	case m.height >= 40:
		chartRows = 5
	case m.height >= 28:
		chartRows = 3
	}
	m.cardHeight = chartRows + 4
	body := m.height - mastheadHeight(m.width, m.height) - 4
	detailHeight := 0
	if m.view == metricsTargetsView && m.height >= 22 {
		detailHeight = 7
	}
	m.visibleRows = max(1, (body-detailHeight)/m.cardHeight)
	m.ResizeItem(m.detail, detailHeight, 0)
	m.ResizeItem(m.grid, 0, boolInt(m.view == metricsPanelsView))
	m.ResizeItem(m.table, 0, boolInt(m.view == metricsTargetsView))
	m.ResizeItem(m.setup, 0, boolInt(m.view == metricsSetupView))
	m.rebuildGrid()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// chartWidth is how many samples a chart can draw: a card's inner width
// less the axis, or the whole terminal in the full-screen view.
func (m *metricsPage) chartWidth() int {
	if m.width == 0 {
		return 40
	}
	if m.focus != nil {
		return max(8, min(240, m.width-metricsAxisWidth-4))
	}
	return max(8, min(120, m.width/m.columns-metricsAxisWidth-4))
}

// visiblePanels are the cards on screen, or the one panel shown full screen;
// only they are polled.
func (m *metricsPage) visiblePanels() []*metricsPanel {
	if m.focus != nil && m.selected < len(m.panels) {
		return m.panels[m.selected : m.selected+1]
	}
	perPage := m.columns * m.visibleRows
	start := min(m.firstRow*m.columns, len(m.panels))
	return m.panels[start:min(start+perPage, len(m.panels))]
}

// rebuildGrid lays the visible cards out in rows, reusing card widgets.
func (m *metricsPage) rebuildGrid() {
	p := m.w.palette()
	visible := m.visiblePanels()
	for len(m.cards) < len(visible) {
		i := len(m.cards)
		card := textView().SetDynamicColors(true).SetWrap(false)
		card.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderAttributes(tcell.AttrBold)
		m.w.illuminatePanel(card.Box, card.HasFocus, func(p palette) string { return panelHue(p, metricsTab) }, true)
		card.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftClick {
				m.selected = m.firstRow*m.columns + i
				m.focusPrimary()
				m.render()
			}
			return action, event
		})
		m.cards = append(m.cards, card)
	}
	m.cards = m.cards[:len(visible)]
	m.grid.Clear()
	for row := 0; row*m.columns < len(visible); row++ {
		line := tview.NewFlex()
		line.SetBackgroundColor(tcell.GetColor(p.background))
		for col := 0; col < m.columns; col++ {
			if i := row*m.columns + col; i < len(visible) {
				line.AddItem(m.cards[i], 0, 1, false)
			} else {
				line.AddItem(tview.NewBox().SetBackgroundColor(tcell.GetColor(p.background)), 0, 1, false)
			}
		}
		m.grid.AddItem(line, m.cardHeight, 0, false)
	}
	m.grid.AddItem(tview.NewBox().SetBackgroundColor(tcell.GetColor(p.background)), 0, 1, false)
}

func (m *metricsPage) selectedCard() *tview.TextView {
	i := m.selected - m.firstRow*m.columns
	if m.view != metricsPanelsView || i < 0 || i >= len(m.cards) {
		return nil
	}
	return m.cards[i]
}

// render redraws everything from the page state; it never touches the network.
func (m *metricsPage) render() {
	p := m.w.palette()
	m.header.SetText(m.w.masthead("Metrics", "", hostReadout(p, m.w.hostUsage, m.width)+"   "+m.w.signalsHint(p), m.width, m.height))
	for i, button := range m.viewButtons {
		fg, bg := p.muted, p.background
		if metricsView(i) == m.view {
			fg, bg = p.background, panelHue(p, metricsTab)
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(metricsView(i) == m.view))
	}
	switch m.view {
	case metricsPanelsView:
		m.renderPanels()
	case metricsTargetsView:
		m.renderTargets()
	case metricsSetupView:
		m.renderSetup()
	}
	if m.focus != nil {
		m.renderFocus()
	}
	m.updateStatus()
	m.updateFooter()
}

func (m *metricsPage) renderPanels() {
	visible := m.visiblePanels()
	if len(m.cards) != len(visible) {
		m.rebuildGrid()
	}
	for i, panel := range visible {
		m.renderCard(m.cards[i], panel)
	}
}

// renderCard fills one card: a title, the latest value, the trend and a caption.
func (m *metricsPage) renderCard(card *tview.TextView, panel *metricsPanel) {
	p := m.w.palette()
	hue := panelHue(p, metricsTab)
	family := strings.ToUpper(panel.family)
	if panel.pinned {
		family = "PIN"
	}
	card.SetTitle(" " + tview.Escape(family+" · "+panel.spec.Title) + " ").SetTitleColor(tcell.GetColor(hue))
	chartWidth, chartRows := m.chartWidth(), m.cardHeight-4
	var lines []string
	if panel.failure != "" {
		lines = append(lines, "["+p.error+"]query failed[-]", "["+p.muted+"]"+tview.Escape(ellipsize(panel.failure, chartWidth))+"[-]")
	} else if len(panel.series) == 0 {
		lines = append(lines, "["+p.muted+"]"+panelEmptyText(panel)+"[-]", "["+p.muted+"]"+tview.Escape(ellipsize(panel.spec.Note, chartWidth))+"[-]")
	} else {
		lead := panel.series[0]
		lines = append(lines, m.headline(panel, lead, chartWidth))
		if panel.spec.Range {
			for _, row := range axisChart(lead.values, panel.spec.Unit, trendStyle{width: chartWidth + metricsAxisWidth, rows: chartRows, fixedMax: panel.ceiling, mode: m.w.settings.GraphMode, bars: m.bars}) {
				lines = append(lines, gradientText(row, hue, p.accent))
			}
			lines = append(lines, "["+p.muted+"]"+tview.Escape(m.cardCaption(panel, chartWidth))+"[-]")
		} else {
			lines = append(lines, m.renderBars(panel, chartRows+1, chartWidth+metricsAxisWidth)...)
		}
	}
	card.SetText(strings.Join(lines, "\n"))
}

// headline is a panel's latest value, coloured by pressure when the value
// has a ceiling, with the series name and the direction of change.
func (m *metricsPage) headline(panel *metricsPanel, lead metricsSeries, width int) string {
	p := m.w.palette()
	colour := panelHue(p, metricsTab)
	if panel.ceiling > 0 && lead.latest >= 0 {
		colour = pressureHue(p, int(lead.latest/panel.ceiling*100))
	}
	text := "[" + colour + "::b]" + formatMetric(lead.latest, panel.spec.Unit) + "[-::-]"
	if name := seriesName(lead.label, panel.spec.Legend); name != "" {
		text += " [" + p.text + "]" + tview.Escape(ellipsize(name, width/2)) + "[-]"
	}
	if panel.spec.Range {
		text += " [" + p.muted + "]" + signalDirection(lead.values) + "[-]"
	}
	return text
}

// seriesName shows a bare numeric label with its legend key ("cpu 6") and
// a metric-name legend without its exporter prefix ("load15").
func seriesName(label, legend string) string {
	label = textutil.Clean(label)
	switch {
	case label == "":
		return ""
	case legend == "__name__":
		if _, short, ok := strings.Cut(label, "_"); ok {
			return short
		}
	case !strings.Contains(legend, "/") && strings.Trim(label, "0123456789") == "":
		return legend + " " + label
	}
	return label
}

// panelEmptyText distinguishes a panel that has not been polled yet from one
// the server answered with nothing.
func panelEmptyText(panel *metricsPanel) string {
	if panel.newest.IsZero() {
		return "loading …"
	}
	return "no data"
}

// cardCaption names the window and the other series a chart does not draw.
func (m *metricsPage) cardCaption(panel *metricsPanel, width int) string {
	caption := trendCaption(panel.newest, panel.step, knownSpan(panel.series[0].values), width)
	if len(panel.series) > 1 {
		caption += " · +" + fmt.Sprint(len(panel.series)-1) + " more"
	}
	return ellipsize(caption, width)
}

// renderBars draws one meter per series for instant-query panels.
func (m *metricsPage) renderBars(panel *metricsPanel, rows, width int) []string {
	p := m.w.palette()
	var lines []string
	labelWidth := min(18, width/3)
	for i, series := range panel.series {
		if i >= rows {
			lines[rows-1] = "[" + p.muted + "]+" + fmt.Sprint(len(panel.series)-rows+1) + " more[-]"
			break
		}
		value := formatMetric(series.latest, panel.spec.Unit)
		label := ellipsize(seriesName(series.label, panel.spec.Legend), labelWidth)
		meterWidth := width - labelWidth - displayWidth(value) - 3
		colour := panelHue(p, metricsTab)
		if panel.ceiling > 0 && series.latest >= 0 {
			colour = pressureHue(p, int(series.latest/panel.ceiling*100))
		}
		line := fmt.Sprintf("%-*s ", labelWidth, tview.Escape(label))
		if meterWidth >= 4 && panel.ceiling > 0 {
			line += "[" + colour + "]" + signalMeter(max(0, series.latest), panel.ceiling, meterWidth, m.w.settings.GraphMode) + "[-] "
		}
		lines = append(lines, line+"["+colour+"]"+value+"[-]")
	}
	return lines
}

func (m *metricsPage) renderTargets() {
	p := m.w.palette()
	m.table.Clear()
	title := " TARGETS"
	if m.build.Version != "" {
		title += " · prometheus " + m.build.Version
	}
	up := 0
	for _, target := range m.targets {
		if target.Health == "up" {
			up++
		}
	}
	if len(m.targets) > 0 {
		title += fmt.Sprintf(" · up %d/%d", up, len(m.targets))
	}
	if !m.runtime.StartTime.IsZero() {
		title += " · config "
		if m.runtime.ReloadConfigSuccess {
			title += "OK"
		} else {
			title += "FAILED"
		}
	}
	m.table.SetTitle(tview.Escape(title) + " ")
	headers := []string{"STATE", "JOB", "INSTANCE", "LAST SCRAPE", "DURATION", "ERROR"}
	if m.width < 110 {
		headers = []string{"STATE", "JOB", "INSTANCE", "ERROR"}
	}
	for col, name := range headers {
		m.table.SetCell(0, col, tview.NewTableCell(name).SetTextColor(tcell.GetColor(p.muted)).SetSelectable(false).SetAttributes(tcell.AttrBold))
	}
	rows := append([]promapi.Target(nil), m.targets...)
	sortTargets(rows)
	for i, target := range rows {
		colour := p.muted
		switch target.Health {
		case "up":
			colour = p.success
		case "down":
			colour = p.error
		}
		cells := map[string]string{
			"STATE":       strings.ToUpper(target.Health),
			"JOB":         target.Job,
			"INSTANCE":    target.Instance,
			"LAST SCRAPE": agoText(target.LastScrape),
			"DURATION":    formatMetric(target.ScrapeDuration.Seconds(), promapi.UnitSeconds),
			"ERROR":       ellipsize(target.LastError, 40),
		}
		for col, name := range headers {
			cell := tview.NewTableCell(tview.Escape(cells[name])).SetTextColor(tcell.GetColor(p.text))
			if name == "STATE" || name == "ERROR" && target.LastError != "" {
				cell.SetTextColor(tcell.GetColor(colour))
			}
			if name == "DURATION" && target.ScrapeDuration > 5*time.Second {
				cell.SetTextColor(tcell.GetColor(p.warning))
			}
			m.table.SetCell(i+1, col, cell)
		}
	}
	if row, _ := m.table.GetSelection(); row < 1 && len(rows) > 0 {
		m.table.Select(1, 0)
	}
	m.renderTargetDetail()
}

// sortTargets lists down targets first; Targets already sorted by job and instance.
func sortTargets(targets []promapi.Target) {
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].Health == "down" && targets[j].Health != "down" })
}

func agoText(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return formatSeconds(time.Since(at).Seconds()) + " ago"
}

func (m *metricsPage) currentTarget() (promapi.Target, bool) {
	rows := append([]promapi.Target(nil), m.targets...)
	sortTargets(rows)
	row, _ := m.table.GetSelection()
	if row < 1 || row > len(rows) {
		return promapi.Target{}, false
	}
	return rows[row-1], true
}

func (m *metricsPage) renderTargetDetail() {
	p := m.w.palette()
	target, ok := m.currentTarget()
	if !ok {
		m.detail.SetText("[" + p.muted + "]No scrape targets reported.[-]")
		return
	}
	m.detail.SetText(targetRecord(target, p))
}

func targetRecord(target promapi.Target, p palette) string {
	var out strings.Builder
	line := func(key, value string) {
		fmt.Fprintf(&out, "[%s]%-10s[-] %s\n", p.muted, key, tview.Escape(value))
	}
	line("url", target.ScrapeURL)
	line("health", target.Health)
	line("scraped", agoText(target.LastScrape)+" · "+formatMetric(target.ScrapeDuration.Seconds(), promapi.UnitSeconds)+" · every "+shortDuration(target.ScrapeInterval))
	if target.LastError != "" {
		line("error", target.LastError)
	}
	var labels []string
	for key, value := range target.Labels {
		labels = append(labels, key+"="+value)
	}
	sort.Strings(labels)
	line("labels", strings.Join(labels, " "))
	return strings.TrimRight(out.String(), "\n")
}

func (m *metricsPage) inspectTarget() {
	if target, ok := m.currentTarget(); ok {
		m.w.message("Target · "+target.Job+" · "+target.Instance, targetRecord(target, m.w.palette()))
	}
}

func (m *metricsPage) switchView(view metricsView) {
	m.view = view
	m.generation++
	m.busy = false
	m.layout()
	m.render()
	m.focusPrimary()
	m.lastPoll = time.Time{}
}

// focusFamily jumps to an exporter's first panel, or to the targets view.
func (m *metricsPage) focusFamily(family string) {
	if family == "targets" {
		m.switchView(metricsTargetsView)
		return
	}
	for i, panel := range m.panels {
		if panel.family == family {
			m.selectPanel(i)
			return
		}
	}
	m.failure = ""
	m.render()
}

func (m *metricsPage) selectPanel(i int) {
	if len(m.panels) == 0 {
		return
	}
	m.selected = max(0, min(len(m.panels)-1, i))
	row := m.selected / m.columns
	if row < m.firstRow {
		m.firstRow = row
	} else if row >= m.firstRow+m.visibleRows {
		m.firstRow = row - m.visibleRows + 1
	}
	m.rebuildGrid()
	m.render()
	m.focusPrimary()
}

func (m *metricsPage) input(e *tcell.EventKey) *tcell.EventKey {
	w := m.w
	switch e.Key() {
	case tcell.KeyEscape:
		if m.view == metricsTargetsView && w.app.GetFocus() == m.detail {
			w.app.SetFocus(m.table)
			return nil
		}
		m.close()
		return nil
	case tcell.KeyTab:
		if m.view == metricsTargetsView {
			if w.app.GetFocus() == m.table {
				w.app.SetFocus(m.detail)
			} else {
				w.app.SetFocus(m.table)
			}
			return nil
		}
	case tcell.KeyEnter:
		switch m.view {
		case metricsPanelsView:
			m.expand()
		case metricsSetupView:
			m.runSelectedStep()
		default:
			return e
		}
		return nil
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight, tcell.KeyHome, tcell.KeyEnd:
		if m.view == metricsPanelsView {
			m.movePanel(e.Key())
			return nil
		}
		if m.view == metricsSetupView && (e.Key() == tcell.KeyUp || e.Key() == tcell.KeyDown) {
			m.moveStep(e.Key())
			return nil
		}
		return e
	}
	switch e.Rune() {
	case '0':
		w.controlDeck()
	case ',':
		w.pollingDialog()
	case '1', '2', '3', '4', '5':
		m.close()
		w.openHostTab(int(e.Rune() - '1'))
	case '6':
		m.focusPrimary()
	case 'p':
		m.switchView(metricsPanelsView)
	case 't':
		m.switchView(metricsTargetsView)
	case 's':
		m.switchView(metricsSetupView)
	case 'a':
		m.addMetric()
	case 'd':
		m.unpinSelected()
	case 'b':
		m.bars = !m.bars
		m.render()
	case 'r':
		retryPrometheusProbe()
		m.detectedAt = time.Time{}
		m.refresh()
	case 'P':
		m.paused = !m.paused
		m.updateStatus()
	case 'G':
		w.cycleGraphMode()
		m.render()
	case 'i':
		m.chooseInstance()
	case 'e':
		w.reviewSnapshot(m.report())
	case 'j', 'k', 'h', 'l':
		keys := map[rune]tcell.Key{'j': tcell.KeyDown, 'k': tcell.KeyUp, 'h': tcell.KeyLeft, 'l': tcell.KeyRight}
		return m.input(tcell.NewEventKey(keys[e.Rune()], 0, tcell.ModNone))
	default:
		return e
	}
	return nil
}

func (m *metricsPage) movePanel(key tcell.Key) {
	step := map[tcell.Key]int{tcell.KeyUp: -m.columns, tcell.KeyDown: m.columns, tcell.KeyLeft: -1, tcell.KeyRight: 1}[key]
	switch key {
	case tcell.KeyHome:
		m.selectPanel(0)
	case tcell.KeyEnd:
		m.selectPanel(len(m.panels) - 1)
	default:
		m.selectPanel(m.selected + step)
	}
}

// chooseInstance lets the user pick which scraped host a family shows.
func (m *metricsPage) chooseInstance() {
	if m.selected >= len(m.panels) {
		return
	}
	panel := m.panels[m.selected]
	candidates := m.instances[panel.family]
	if panel.family == "pve" {
		candidates = m.nodes
	}
	if len(candidates) < 2 {
		m.w.message("One host", "Prometheus reports a single "+panel.family+" instance; nothing to choose.")
		return
	}
	var choices []choice
	for _, candidate := range candidates {
		candidate := candidate
		choices = append(choices, choice{candidate, "Show " + panel.family + " panels for this host", func() {
			next := m.w.settings
			next.PrometheusInstances = map[string]string{}
			for k, v := range m.w.settings.PrometheusInstances {
				next.PrometheusInstances[k] = v
			}
			next.PrometheusInstances[m.endpoint.URL+"|"+panel.family] = candidate
			if m.w.persistViewSettings(next) {
				m.generation++
				m.busy = false
				m.refresh()
			}
		}})
	}
	m.w.choose("metrics-instance", "Choose host · "+panel.family, choices)
}

func (m *metricsPage) updateStatus() {
	p := m.w.palette()
	series := 0
	for _, panel := range m.panels {
		series += len(panel.series)
	}
	state := "waiting for first sample"
	if !m.sampledAt.IsZero() {
		state = "sampled " + m.sampledAt.Format("15:04:05")
	}
	if m.busy {
		state += " · refreshing"
	}
	if m.paused {
		state += " · PAUSED"
	}
	source := "no endpoint"
	if m.endpoint.URL != "" {
		source = m.endpoint.URL + " (" + m.endpoint.Source + ")"
	}
	lead := fmt.Sprintf(" %d panels · %d series · %s · poll %ds · %s · ", len(m.panels), series, state, m.pollSeconds(), source)
	note, colour := m.stateNote(), p.muted
	if m.failure != "" {
		note, colour = "Prometheus unreachable; previous sample retained: "+m.failure, p.error
	}
	note = ellipsize(textutil.Clean(strings.ReplaceAll(note, "\n", " · ")), max(0, m.width-displayWidth(lead)-1))
	m.status.SetText(fmt.Sprintf("%s[%s]%s[-]", tview.Escape(lead), colour, tview.Escape(note)))
}

func (m *metricsPage) stateNote() string {
	switch m.state {
	case metricsUnconfigured:
		return "No Prometheus configured or detected · s setup"
	case metricsProbing:
		return "Probing " + m.endpoint.URL
	case metricsNoFamilies:
		return "Reachable, but no node_exporter, cAdvisor or Proxmox series yet · s setup"
	}
	var names []string
	for _, family := range m.families {
		names = append(names, family.Title)
	}
	note := strings.Join(names, " · ")
	if pins := len(m.w.settings.PrometheusMetrics); pins > 0 {
		note += fmt.Sprintf(" · %d pinned", pins)
	}
	if m.hidden > 0 {
		note += fmt.Sprintf(" · %d without data hidden", m.hidden)
	}
	if note == "" {
		return "Ready"
	}
	return note
}

func (m *metricsPage) updateFooter() {
	text := " p panels · t targets · s setup · a pin · d unpin · Enter full screen · i host · b bars · r refresh · P pause · G graphics · , polling · e export · Esc back"
	if m.width < 100 {
		text = " p/t/s views · Enter full screen · i host · b bars · r · G · Esc"
	}
	m.footer.SetText(text)
}

// report is the plain-text export of the current page.
func (m *metricsPage) report() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Prometheus metrics · %s · %s\n\n", m.endpoint.URL, time.Now().Format(time.RFC3339))
	for _, panel := range m.panels {
		fmt.Fprintf(&out, "%s · %s\n", strings.ToUpper(panel.family), panel.spec.Title)
		for _, series := range panel.series {
			fmt.Fprintf(&out, "  %-32s %s\n", series.label, formatMetric(series.latest, panel.spec.Unit))
		}
	}
	out.WriteString("\nTARGETS\n")
	for _, target := range m.targets {
		fmt.Fprintf(&out, "  %-6s %-16s %-28s %s\n", target.Health, target.Job, target.Instance, target.LastError)
	}
	return textutil.Clean(out.String())
}
