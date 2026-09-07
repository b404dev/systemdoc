package dashboard

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var tabNames = []string{"Overview", "Logs", "Config", "Resources", "Dependencies"}

func (w *workspace) inspectorTabName() string {
	if w.mode == 1 && w.tab == 4 {
		return "Connections"
	}
	if w.mode == 1 && w.tab == 2 {
		return "Inspect JSON"
	}
	return tabNames[w.tab]
}

var quickFilterNames = []string{"All states", "Active", "Needs attention"}
var sortNames = []string{"Name ↑", "Attention first", "CPU ↓", "Memory ↓"}

// fleetSample describes observed workloads, never the whole host's utilisation.
type fleetSample struct {
	active, attention, cpuCount, memoryCount int
	cpu, memory                              float64
}

func available(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func needsAttention(item workload) bool {
	return item.State == "failed" || item.State == "restarting" || strings.Contains(item.Detail, "unhealthy")
}

func isActive(item workload) bool { return item.State == "active" || item.State == "running" }

func stateColour(item workload, p palette) string {
	if needsAttention(item) {
		return p.error
	}
	if isActive(item) {
		return p.success
	}
	if item.State == "activating" || item.State == "deactivating" || item.State == "paused" {
		return p.warning
	}
	return p.muted
}

func stateSymbol(item workload) string {
	if needsAttention(item) {
		return "!"
	}
	if isActive(item) {
		return "●"
	}
	return "○"
}

func cpuValue(value string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	return n, err == nil && n >= 0
}

// memoryValue accepts the CLI display units and returns MiB for sorting/aggregation.
func memoryValue(value string) (float64, bool) {
	used, _, _ := strings.Cut(value, "/")
	used = strings.TrimSpace(used)
	units := []struct {
		suffix string
		scale  float64
	}{
		{"GiB", 1024}, {"MiB", 1}, {"KiB", 1.0 / 1024},
		{"GB", 1e9 / (1024 * 1024)}, {"MB", 1e6 / (1024 * 1024)}, {"kB", 1e3 / (1024 * 1024)}, {"B", 1.0 / (1024 * 1024)},
	}
	for _, unit := range units {
		if strings.HasSuffix(used, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(used, unit.suffix)), 64)
			return n * unit.scale, err == nil && n >= 0
		}
	}
	return 0, false
}

func fleetTotals(items []workload) fleetSample {
	var result fleetSample
	for _, item := range items {
		if isActive(item) {
			result.active++
		}
		if needsAttention(item) {
			result.attention++
		}
		if n, ok := cpuValue(item.CPU); ok {
			result.cpu += n
			result.cpuCount++
		}
		if n, ok := memoryValue(item.Memory); ok {
			result.memory += n
			result.memoryCount++
		}
	}
	return result
}

func (w *workspace) sampleFleet() {
	history := append(w.fleetHistory[w.mode], fleetTotals(w.items[w.mode]))
	if len(history) > 32 {
		history = history[len(history)-32:]
	}
	w.fleetHistory[w.mode] = history
}

func (w *workspace) matchesQuickFilter(item workload) bool {
	switch w.quickFilter {
	case 1:
		return isActive(item)
	case 2:
		return needsAttention(item)
	default:
		return true
	}
}

func (w *workspace) sortWorkloads() {
	sort.SliceStable(w.visible, func(i, j int) bool {
		a, b := w.visible[i], w.visible[j]
		switch w.sortMode {
		case 1:
			if needsAttention(a) != needsAttention(b) {
				return needsAttention(a)
			}
		case 2, 3:
			av, aok := cpuValue(a.CPU)
			bv, bok := cpuValue(b.CPU)
			if w.sortMode == 3 {
				av, aok = memoryValue(a.Memory)
				bv, bok = memoryValue(b.Memory)
			}
			if aok != bok {
				return aok
			}
			if av != bv {
				return av > bv
			}
		}
		return a.Name < b.Name
	})
}

func (w *workspace) chooseSort() {
	var choices []choice
	for i, name := range sortNames {
		i := i
		choices = append(choices, choice{name, "Sort the list; keep the selected workload", func() { w.sortMode = i; w.renderTable() }})
	}
	w.choose("sort", " Sort workloads ", choices)
}

func (w *workspace) switchMode(mode int) {
	if mode == w.mode {
		w.app.SetFocus(w.table)
		return
	}
	w.savedQuickFilters[w.mode] = w.quickFilter
	w.mode, w.quickFilter = mode, w.savedQuickFilters[mode]
	w.generation++
	if w.detailCancel != nil {
		w.detailCancel()
	}
	w.detailPending = false
	// Replace the filter text without relying on tview firing a change callback
	// when both modes have the same search string.
	w.search.SetChangedFunc(nil).SetText(w.filters[mode]).SetChangedFunc(w.filterChanged)
	w.loading = w.inventoryJobs[mode] != nil && w.lastRefresh[mode].IsZero()
	w.renderTable()
	w.chrome()
	if w.lastRefresh[mode].IsZero() || time.Since(w.lastRefresh[mode]) >= time.Duration(w.settings.RefreshSeconds)*time.Second {
		w.startInventory(mode)
	}
	w.app.SetFocus(w.table)
}

func (w *workspace) selectTab(tab int) {
	w.tab = tab
	w.chrome()
	w.showDetail()
	w.app.SetFocus(w.detail)
}

func (w *workspace) toggleZoom() {
	if w.zoom != 0 {
		w.zoom = 0
	} else if w.logDrawer.HasFocus() {
		w.zoom = 3
	} else if w.detail.HasFocus() {
		w.zoom = 2
	} else {
		w.zoom = 1
	}
	w.lastWidth = 0
	w.redrawRows()
}

func (w *workspace) buildDashboard() {
	w.header.SetDynamicColors(true)
	w.footer.SetDynamicColors(true)
	w.logDrawer = textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	w.logDrawer.SetBorder(true).SetTitle(" LIVE LOGS · L closes ")
	w.pauseLogsOnScroll(w.logDrawer, true)
	w.pauseLogsOnScroll(w.detail, false)
	w.selectionCard = textView().SetDynamicColors(true).SetWrap(false)
	w.selectionCard.SetBorder(true).SetTitle(" SELECTED WORKLOAD ")
	w.loadingIndicator = textView().SetWrap(false).SetTextAlign(tview.AlignCenter)
	w.footerBar = tview.NewFlex().AddItem(w.footer, 0, 1, false).AddItem(w.loadingIndicator, 2, 0, false)
	w.telemetry = tview.NewFlex()
	for i := range w.cards {
		card := textView().SetDynamicColors(true).SetWrap(false)
		card.SetBorder(true)
		w.cards[i] = card
		w.telemetry.AddItem(card, 0, 1, false)
		if i < 2 {
			filter := i + 1
			card.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
				if action == tview.MouseLeftClick && card.InRect(event.Position()) {
					if w.quickFilter == filter {
						w.setQuickFilter(0)
					} else {
						w.setQuickFilter(filter)
					}
					return tview.MouseConsumed, nil
				}
				return action, event
			})
		}
	}
	w.commandBar = tview.NewFlex()
	for i, label := range []string{"1 Services", "2 Containers"} {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { w.switchMode(i) })
		w.modeButtons[i] = button
		w.commandBar.AddItem(button, 14, 0, false)
	}
	w.commandBar.AddItem(tview.NewBox(), 0, 1, false)
	w.activeOnlyButton = tview.NewButton("i Active only").SetSelectedFunc(func() {
		w.toggleActiveOnly()
		if front, _ := w.pages.GetFrontPage(); front == "main" {
			w.app.SetFocus(w.table)
		}
	})
	w.commandBar.AddItem(w.activeOnlyButton, 15, 0, false)
	for _, control := range []struct {
		label string
		width int
		run   func()
	}{
		{"a Actions", 11, w.actions}, {"t Themes", 10, w.themeDialog}, {"z Expand", 10, w.toggleZoom},
	} {
		button := tview.NewButton(control.label).SetSelectedFunc(control.run)
		button.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftClick && button.InRect(event.Position()) {
				control.run()
				return tview.MouseConsumed, nil
			}
			return action, event
		})
		w.toolbarButtons = append(w.toolbarButtons, button)
		w.commandBar.AddItem(button, control.width, 0, false)
	}
	w.tabBar = tview.NewFlex()
	for i, label := range []string{"o Overview", "l Logs", "c Config", "r Metrics", "d Deps"} {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { w.selectTab(i) })
		w.tabButtons[i] = button
		w.tabBar.AddItem(button, 0, 1, false)
	}
	restart := tview.NewButton("R Restart selected workload").SetSelectedFunc(func() { w.confirmAction("restart") })
	w.toolbarButtons = append(w.toolbarButtons, restart)
	w.inspector = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.selectionCard, 5, 0, false).AddItem(restart, 1, 0, false).
		AddItem(w.tabBar, 1, 0, false).AddItem(w.detail, 0, 1, true)
	w.body = tview.NewFlex()
	w.root = tview.NewFlex().SetDirection(tview.FlexRow)
	accent := func(p palette) string { return p.accent }
	w.illuminate(w.table.Box, w.table.HasFocus, accent)
	w.illuminate(w.detail.Box, w.detail.HasFocus, accent)
	w.illuminate(w.logDrawer.Box, w.logDrawer.HasFocus, accent)
	w.illuminate(w.selectionCard.Box, nil, accent)
	for i, card := range w.cards {
		w.illuminate(card.Box, func() bool { return i < 2 && w.quickFilter == i+1 }, func(p palette) string {
			return []string{p.success, p.error, p.accent, p.warning}[i]
		})
	}
	w.layoutDashboard(120, 30)
}

// layoutDashboard only changes geometry. Draw callbacks must not call application
// methods that acquire its lock (including GetFocus and SetFocus).
func (w *workspace) layoutDashboard(width, height int) {
	focusDetail := w.detail.HasFocus()
	if width == w.lastWidth && height == w.lastHeight && focusDetail == w.layoutDetail {
		return
	}
	w.lastWidth, w.lastHeight, w.layoutDetail = width, height, focusDetail
	w.root.Clear()
	headerHeight := 1
	if height >= 28 {
		headerHeight = 3
	}
	w.header.SetBorderPadding((headerHeight-1)/2, 0, 1, 0)
	w.root.AddItem(w.header, headerHeight, 0, false).AddItem(w.commandBar, 1, 0, false)
	if height >= 28 && w.zoom == 0 && (!w.drawerOpen || height >= 36) {
		w.root.AddItem(w.telemetry, 5, 0, false)
	}
	w.root.AddItem(w.summary, 1, 0, false).AddItem(w.search, 1, 0, false)
	w.body.Clear()
	switch {
	case w.zoom == 3:
		w.body.AddItem(w.logDrawer, 0, 1, true)
	case w.zoom == 1:
		w.body.AddItem(w.table, 0, 1, true)
	case w.zoom == 2:
		w.body.AddItem(w.inspector, 0, 1, true)
	case w.settings.Layout == "side-by-side" || (w.settings.Layout != "stacked" && width >= 110):
		w.body.SetDirection(tview.FlexColumn).AddItem(w.table, 0, 5, true).AddItem(w.inspector, 0, 6, false)
	case height >= 36:
		w.body.SetDirection(tview.FlexRow).AddItem(w.table, 0, 1, true).AddItem(w.inspector, 0, 2, false)
	case focusDetail:
		w.body.AddItem(w.inspector, 0, 1, true)
	default:
		w.body.AddItem(w.table, 0, 1, true)
	}
	w.inspector.ResizeItem(w.selectionCard, 5, 0)
	if height < 22 {
		w.inspector.ResizeItem(w.selectionCard, 3, 0)
	}
	w.root.AddItem(w.body, 0, 1, true)
	if w.drawerOpen && w.zoom == 0 {
		w.root.AddItem(w.logDrawer, max(4, min(8, height/4)), 0, false)
	}
	w.root.AddItem(w.footerBar, 1, 0, false)
	w.updateDashboard()
}

func (w *workspace) styleDashboard() {
	p := w.palette()
	for _, flex := range []*tview.Flex{w.commandBar, w.tabBar, w.telemetry, w.body, w.inspector, w.footerBar} {
		flex.SetBackgroundColor(tcell.GetColor(p.background))
	}
	for _, view := range append(w.cards[:], w.selectionCard, w.logDrawer) {
		view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
		view.SetTextColor(tcell.GetColor(p.text))
	}
	w.table.SetBorderAttributes(tcell.AttrBold)
	w.detail.SetBorderAttributes(tcell.AttrBold)
	w.commandBar.GetItem(2).(*tview.Box).SetBackgroundColor(tcell.GetColor(p.background))
	w.table.SetTitle(" WORKLOADS · S sort · Enter inspect ").SetTitleColor(tcell.GetColor(p.accent))
	w.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(p.accent)).Foreground(tcell.GetColor(p.background)).Bold(true))
	w.header.SetBackgroundColor(tcell.GetColor(p.background))
	w.footer.SetBackgroundColor(tcell.GetColor(p.background))
	w.loadingIndicator.SetBackgroundColor(tcell.GetColor(p.background))
	w.loadingIndicator.SetTextColor(tcell.GetColor(p.muted))
	w.summary.SetTextColor(tcell.GetColor(p.muted))
	w.styleNavigation()
}

func (w *workspace) styleNavigation() {
	p := w.palette()
	style := func(button *tview.Button, selected bool) {
		fg, bg := p.muted, p.background
		if selected {
			fg, bg = p.background, p.accent
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(selected))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	}
	style(w.activeOnlyButton, w.quickFilter == 1)
	for i, button := range w.modeButtons {
		style(button, i == w.mode)
	}
	for i, button := range w.tabButtons {
		if i == 4 {
			label := "d Deps"
			if w.mode == 1 {
				label = "d Connect"
			}
			button.SetLabel(label)
		}
		style(button, i == w.tab)
	}
	for _, button := range w.toolbarButtons {
		style(button, false)
	}
}

func meter(value, maximum float64, width int) string {
	filled := 0
	if maximum > 0 {
		filled = int(value / maximum * float64(width))
	}
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("━", filled) + strings.Repeat("·", width-filled)
}

func (w *workspace) updateDashboard() {
	p := w.palette()
	totals := fleetTotals(w.items[w.mode])
	cpu, memory := "—", "—"
	if totals.cpuCount > 0 {
		cpu = fmt.Sprintf("%.1f%%", totals.cpu)
	}
	if totals.memoryCount > 0 {
		memory = fmt.Sprintf("%.1f MiB", totals.memory)
	}
	cpuHistory, memoryHistory := []float64{}, []float64{}
	for _, sample := range w.fleetHistory[w.mode] {
		c, m := -1.0, -1.0
		if sample.cpuCount > 0 {
			c = sample.cpu
		}
		if sample.memoryCount > 0 {
			m = sample.memory
		}
		cpuHistory, memoryHistory = append(cpuHistory, c), append(memoryHistory, m)
	}
	chartWidth := max(1, w.lastWidth/4-2)
	if w.lastWidth == 0 {
		chartWidth = 24
	}
	if len(cpuHistory) > chartWidth {
		cpuHistory = cpuHistory[len(cpuHistory)-chartWidth:]
		memoryHistory = memoryHistory[len(memoryHistory)-chartWidth:]
	}
	cpuTrend, memoryTrend := sparkline(cpuHistory), sparkline(memoryHistory)
	if len(cpuHistory) < 2 {
		cpuTrend = "collecting samples"
		memoryTrend = "collecting samples"
	}
	values := []struct{ title, colour, text string }{
		{" ACTIVE · click to filter ", p.success, fmt.Sprintf("%d active / %d listed\n%s\n%s", totals.active, len(w.items[w.mode]), meter(float64(totals.active), float64(len(w.items[w.mode])), 20), quickFilterNames[w.quickFilter])},
		{" ATTENTION · filter ", p.error, fmt.Sprintf("%d need attention\n%s\nFailures / unhealthy", totals.attention, meter(float64(totals.attention), float64(len(w.items[w.mode])), 20))},
		{" WORKLOAD CPU ", p.accent, fmt.Sprintf("%s · %d reporting\n%s\n100%% = one logical CPU", cpu, totals.cpuCount, cpuTrend)},
		{" TRACKED MEMORY ", p.warning, fmt.Sprintf("%s · %d reporting\n%s\nTrend · automatic scale", memory, totals.memoryCount, memoryTrend)},
	}
	for i, value := range values {
		card := w.cards[i]
		card.SetTitle(value.title).SetTitleColor(tcell.GetColor(value.colour))
		lines := strings.Split(value.text, "\n")
		visual := lines[1]
		if i < 2 || len(cpuHistory) >= 2 {
			visual = gradientText(visual, value.colour, p.accent)
		}
		card.SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s\n[%s]%s[-]", value.colour, lines[0], visual, p.muted, lines[2]))
		border := value.colour
		if i < 2 && w.quickFilter == i+1 {
			border = p.text
		}
		card.SetBorderColor(tcell.GetColor(border))
	}
	shortStatus := "waiting"
	if !w.lastRefresh[w.mode].IsZero() {
		shortStatus = "sampled " + w.lastRefresh[w.mode].Format("15:04:05")
	}
	if w.paused {
		shortStatus = "PAUSED"
	}
	if w.backendError[w.mode] {
		w.summary.SetText(" Backend unavailable · last observation retained · a actions to retry")
	} else {
		filter := quickFilterNames[w.quickFilter]
		if w.favoriteOnly {
			filter = "★ " + filter
		}
		w.summary.SetText(fmt.Sprintf(" %d/%d · %s · %s · %s · ● %d  ! %d", len(w.visible), len(w.items[w.mode]), shortStatus, filter, sortNames[w.sortMode], totals.active, totals.attention))
	}
	w.updateLoadingIndicator()
	w.updateSelectionCard()
}

func (w *workspace) updateSelectionCard() {
	if w.selectionCard == nil {
		return
	}
	item := w.current()
	p := w.palette()
	if item.ID == "" {
		w.selectionCard.SetTitle(" SELECTED WORKLOAD ")
		w.selectionCard.SetText("\n Select a workload to inspect its state, resources and logs.")
		return
	}
	context := "boot " + available(item.Enablement) + " · load " + available(item.LoadState)
	if w.mode == 1 {
		context = "project " + item.Project
	}
	w.selectionCard.SetTitle(" " + tview.Escape(clean(item.Name)) + " ").SetTitleColor(tcell.GetColor(p.accent))
	description := item.Description
	if item.Aliases != "" {
		description += " · aliases: " + item.Aliases
	}
	state := item.State
	if w.mode == 0 && !item.UnitFileOnly && item.Detail != "" {
		state += " / " + item.Detail
	}
	w.selectionCard.SetText(fmt.Sprintf(" [%s::b]%s %s[-::-]  [%s]%s[-]\n %s\n [%s]CPU[-] %s   [%s]MEM[-] %s", stateColour(item, p), stateSymbol(item), tview.Escape(clean(state)), p.muted, tview.Escape(clean(context)), tview.Escape(clean(description)), p.accent, tview.Escape(available(item.CPU)), p.accent, tview.Escape(available(item.Memory))))
}

func (w *workspace) recentlyChanged(id string) bool {
	for i := len(w.activity) - 1; i >= 0; i-- {
		event := w.activity[i]
		if time.Since(event.at) > 15*time.Second {
			break
		}
		if event.id == id && event.mode == w.mode && event.user == w.user {
			return true
		}
	}
	return false
}

// Presentation changes should not restart the selected workload's inspector.
func (w *workspace) redrawRows() {
	previous := w.refreshDetail
	w.refreshDetail = true
	w.renderTable()
	w.refreshDetail = previous
}
