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

var tabNames = []string{"Overview", "Logs", "Config", "Metrics", "Dependencies"}

func (w *workspace) inspectorTabName() string {
	if w.mode == 0 && usesLaunchd() && w.tab == 4 {
		return "Runtime"
	}
	if w.mode == 1 && w.tab == 4 {
		return "Connections"
	}
	if w.mode == 1 && w.tab == 2 {
		if isPod(w.current()) {
			return "Manifest"
		}
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

type workloadSignal struct {
	at          time.Time
	cpu, memory float64
	state       string
}

func available(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func needsAttention(item workload) bool {
	return item.State == "failed" || item.State == "restarting" || strings.Contains(item.Detail, "unhealthy") || strings.HasPrefix(item.Detail, "not ready")
}

func isActive(item workload) bool { return item.State == "active" || item.State == "running" }

func stateColour(item workload, p palette) string {
	if needsAttention(item) {
		return p.error
	}
	if isActive(item) {
		return p.success
	}
	if item.State == "activating" || item.State == "deactivating" || item.State == "paused" || item.State == "pending" || item.State == "terminating" {
		return p.warning
	}
	return p.muted
}

func stateSymbol(item workload, nerd bool) string {
	if needsAttention(item) {
		return iconFor(nerd, iconAttention)
	}
	if isActive(item) {
		return iconFor(nerd, iconHealthy)
	}
	return iconFor(nerd, iconIdle)
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

func (w *workspace) sampleWorkloads(mode int, items []workload) {
	if w.workloadHistory == nil {
		w.workloadHistory = map[string][]workloadSignal{}
	}
	now := time.Now()
	for _, item := range items {
		cpu, cpuOK := cpuValue(item.CPU)
		memory, memoryOK := memoryValue(item.Memory)
		if !cpuOK {
			cpu = -1
		}
		if !memoryOK {
			memory = -1
		}
		key := fmt.Sprintf("%d/%t/%s", mode, w.user, item.ID)
		history := append(w.workloadHistory[key], workloadSignal{at: now, cpu: cpu, memory: memory, state: item.State})
		if len(history) > 60 {
			history = history[len(history)-60:]
		}
		w.workloadHistory[key] = history
	}
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

// enterMode records a mode change made by the command palette or another
// route that bypasses switchMode. It recomputes the loading flag from the
// new mode's own job and schedules inventory when none is due, so a job
// still running for the previous mode cannot leave the new one marked as
// loading with the refresh loop silently paused.
func (w *workspace) enterMode(mode int) {
	w.mode = mode
	w.loading = w.inventoryJobs[mode] != nil && w.lastRefresh[mode].IsZero()
	if w.lastRefresh[mode].IsZero() || time.Since(w.lastRefresh[mode]) >= time.Duration(w.settings.RefreshSeconds)*time.Second {
		w.startInventory(mode)
	}
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

func (w *workspace) resizePanes(delta int) {
	ratio := w.settings.PaneRatio
	if ratio < 30 || ratio > 70 {
		ratio = 46
	}
	w.settings.PaneRatio = max(30, min(70, ratio+delta))
	w.lastWidth = 0
	w.redrawRows()
}

func (w *workspace) buildDashboard() {
	w.header.SetDynamicColors(true)
	w.footer.SetDynamicColors(true)
	w.logDrawer = textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	w.logDrawer.SetBorder(true).SetTitle(" " + w.iconLabel(iconLogs, "LIVE LOGS · L closes "))
	w.pauseLogsOnScroll(w.logDrawer, true)
	w.pauseLogsOnScroll(w.detail, false)
	w.selectionCard = textView().SetDynamicColors(true).SetWrap(false)
	w.selectionCard.SetBorder(true).SetTitle(" " + w.iconLabel(iconEye, "SELECTED WORKLOAD "))
	w.storyline = textView().SetDynamicColors(true).SetWrap(false)
	w.storyline.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick && w.storyline.InRect(event.Position()) {
			w.storylineView()
			return tview.MouseConsumed, nil
		}
		return action, event
	})
	w.loadingIndicator = textView().SetWrap(false).SetTextAlign(tview.AlignCenter)
	w.pollButton = tview.NewButton("").SetSelectedFunc(w.pollingDialog)
	w.footerBar = tview.NewFlex().AddItem(w.footer, 0, 1, false).AddItem(w.pollButton, 13, 0, false).AddItem(w.loadingIndicator, 2, 0, false)
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
	initialSuiteLabels := hostTabNames
	for i, label := range initialSuiteLabels[:2] {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { w.switchMode(i) })
		w.modeButtons[i] = button
		w.commandBar.AddItem(button, suiteButtonWidths[i], 0, false)
	}
	network := tview.NewButton(initialSuiteLabels[2]).SetSelectedFunc(w.networkPage)
	w.toolbarButtons = append(w.toolbarButtons, network)
	w.commandBar.AddItem(network, suiteButtonWidths[2], 0, false)
	for i, label := range initialSuiteLabels[3:] {
		button := tview.NewButton(label).SetSelectedFunc(func() { w.hostPage(i + 3) })
		w.toolbarButtons = append(w.toolbarButtons, button)
		w.commandBar.AddItem(button, suiteButtonWidths[i+3], 0, false)
	}
	w.commandBar.AddItem(tview.NewBox(), 0, 1, false)
	w.activeOnlyButton = tview.NewButton("i " + w.iconLabel(iconFilter, "Active")).SetSelectedFunc(func() {
		w.toggleActiveOnly()
		if front, _ := w.pages.GetFrontPage(); front == "main" {
			w.app.SetFocus(w.table)
		}
	})
	w.commandBar.AddItem(w.activeOnlyButton, 15, 0, false)
	for _, control := range []struct {
		label string
		run   func()
	}{
		{"0 " + w.iconLabel(iconDeck, "Deck"), w.controlDeck},
		{"V " + w.iconLabel(iconViews, "Views"), w.savedViews},
		{"a " + w.iconLabel(iconActions, "Actions"), w.actions},
		{"t " + w.iconLabel(iconThemes, "Themes"), w.themeDialog},
		{"z " + w.iconLabel(iconExpand, "Expand"), w.toggleZoom},
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
		w.workspaceButtons = append(w.workspaceButtons, button)
		w.commandBar.AddItem(button, controlButtonWidth(control.label), 0, false)
	}
	w.tabBar = tview.NewFlex()
	for i, label := range []string{
		"o " + w.iconLabel(iconOverview, "Overview"),
		"l " + w.iconLabel(iconLogs, "Logs"),
		"c " + w.iconLabel(iconConfig, "Config"),
		"r " + w.iconLabel(iconResources, "Metrics"),
		"d " + w.iconLabel(iconDependencies, "Deps"),
	} {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { w.selectTab(i) })
		w.tabButtons[i] = button
		w.tabBar.AddItem(button, 0, 1, false)
	}
	w.inspector = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.selectionCard, 4, 0, false).
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
	suiteButtons := append(w.modeButtons[:], w.toolbarButtons[:3]...)
	setSuiteLabels(suiteButtons, width, w.settings.NerdIcons)
	for i, button := range suiteButtons {
		buttonWidth := suiteButtonWidths[i]
		if width < 100 {
			buttonWidth = max(6, width/5)
		}
		w.commandBar.ResizeItem(button, buttonWidth, 0)
	}
	headerHeight := mastheadHeight(width, height)
	w.header.SetBorderPadding(0, 0, 1, 0)
	w.root.AddItem(w.header, headerHeight, 0, false).AddItem(w.commandBar, 1, 0, false)
	activeWidth := 15
	if width < 150 {
		activeWidth = 0
	}
	w.commandBar.ResizeItem(w.activeOnlyButton, activeWidth, 0)
	for _, button := range w.workspaceButtons {
		buttonWidth := controlButtonWidth(button.GetLabel())
		if width < 140 {
			buttonWidth = 0
		}
		w.commandBar.ResizeItem(button, buttonWidth, 0)
	}
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
		w.body.SetDirection(tview.FlexColumn).AddItem(w.table, 0, w.settings.PaneRatio, true).AddItem(w.inspector, 0, 100-w.settings.PaneRatio, false)
	case height >= 36:
		w.body.SetDirection(tview.FlexRow).AddItem(w.table, 0, w.settings.PaneRatio, true).AddItem(w.inspector, 0, 100-w.settings.PaneRatio, false)
	case focusDetail:
		w.body.AddItem(w.inspector, 0, 1, true)
	default:
		w.body.AddItem(w.table, 0, 1, true)
	}
	w.inspector.ResizeItem(w.selectionCard, 4, 0)
	if height < 22 {
		w.inspector.ResizeItem(w.selectionCard, 0, 0)
	}
	w.root.AddItem(w.body, 0, 1, true)
	if w.drawerOpen && w.zoom == 0 {
		w.root.AddItem(w.logDrawer, max(4, min(8, height/4)), 0, false)
	}
	if height >= 24 && w.zoom == 0 {
		w.root.AddItem(w.storyline, 1, 0, false)
	}
	w.root.AddItem(w.footerBar, 1, 0, false)
	w.updateDashboard()
}

func (w *workspace) styleDashboard() {
	p := w.palette()
	for _, flex := range []*tview.Flex{w.commandBar, w.tabBar, w.telemetry, w.body, w.inspector, w.footerBar} {
		flex.SetBackgroundColor(tcell.GetColor(p.background))
	}
	for _, view := range append(w.cards[:], w.selectionCard, w.logDrawer, w.storyline) {
		view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
		view.SetTextColor(tcell.GetColor(p.text))
	}
	w.table.SetBorderAttributes(tcell.AttrBold)
	w.detail.SetBorderAttributes(tcell.AttrBold)
	w.commandBar.GetItem(5).(*tview.Box).SetBackgroundColor(tcell.GetColor(p.background))
	w.table.SetTitle(" " + w.iconLabel(iconEye, "WORKLOADS · S sort · Enter inspect ")).SetTitleColor(tcell.GetColor(p.accent))
	w.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(p.accent)).Foreground(tcell.GetColor(p.background)).Bold(true))
	w.header.SetBackgroundColor(tcell.GetColor(p.background))
	w.footer.SetBackgroundColor(tcell.GetColor(p.background))
	w.loadingIndicator.SetBackgroundColor(tcell.GetColor(p.background))
	w.loadingIndicator.SetTextColor(tcell.GetColor(p.muted))
	w.pollButton.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.muted)).Background(tcell.GetColor(p.background)))
	w.pollButton.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	w.updatePollButton()
	w.summary.SetTextColor(tcell.GetColor(p.muted))
	w.styleNavigation()
}

func (w *workspace) styleNavigation() {
	p := w.palette()
	w.activeOnlyButton.SetLabel("i " + w.iconLabel(iconFilter, "Active"))
	workspaceLabels := []string{
		"0 " + w.iconLabel(iconDeck, "Deck"),
		"V " + w.iconLabel(iconViews, "Views"),
		"a " + w.iconLabel(iconActions, "Actions"),
		"t " + w.iconLabel(iconThemes, "Themes"),
		"z " + w.iconLabel(iconExpand, "Expand"),
	}
	for i, button := range w.workspaceButtons {
		button.SetLabel(workspaceLabels[i])
	}
	tabLabels := []string{
		"o " + w.iconLabel(iconOverview, "Overview"),
		"l " + w.iconLabel(iconLogs, "Logs"),
		"c " + w.iconLabel(iconConfig, "Config"),
		"r " + w.iconLabel(iconResources, "Metrics"),
		"d " + w.iconLabel(iconDependencies, "Deps"),
	}
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
		button.SetLabel(tabLabels[i])
		if i == 4 {
			label := "d " + w.iconLabel(iconDependencies, "Deps")
			if w.mode == 0 && usesLaunchd() {
				label = "d " + w.iconLabel(iconProcesses, "Runtime")
			}
			if w.mode == 1 {
				label = "d " + w.iconLabel(iconNetwork, "Connect")
			}
			button.SetLabel(label)
		}
		style(button, i == w.tab)
	}
	for _, button := range w.toolbarButtons {
		style(button, false)
	}
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
	// Each card takes a quarter of the width and its rail frame keeps two
	// cells on either side, so the trail must fit width/4-4 or the newest
	// samples, the ones on the right, are clipped off the card.
	chartWidth := max(1, w.lastWidth/4-4)
	if w.lastWidth == 0 {
		chartWidth = 24
	}
	// Host cards are drawn on a fixed 0–100 scale so the fill height always
	// means the same share of the machine, whatever the recent range was.
	hostCPUTrend := signalArea(trimHistory(w.hostCPUHistory, chartWidth), 100, w.settings.GraphMode)
	hostMemoryTrend := signalArea(trimHistory(w.hostMemoryHistory, chartWidth), 100, w.settings.GraphMode)
	activeHistory, attentionHistory := []float64{}, []float64{}
	for _, sample := range w.fleetHistory[w.mode] {
		activeHistory = append(activeHistory, float64(sample.active))
		attentionHistory = append(attentionHistory, float64(sample.attention))
	}
	activeVisual := signalArea(activeHistory, float64(max(1, len(w.items[w.mode]))), w.settings.GraphMode)
	attentionVisual := signalArea(attentionHistory, float64(max(1, len(w.items[w.mode]))), w.settings.GraphMode)
	values := []struct{ title, colour, headline, visual string }{
		{" " + w.iconLabel(iconHealthy, "ACTIVE · click to filter "), p.success, fmt.Sprintf("%d active · %s", totals.active, signalDelta(activeHistory)), activeVisual},
		{" " + w.iconLabel(iconAttention, "ATTENTION · click to filter "), p.error, fmt.Sprintf("%d failed · %s", totals.attention, signalDelta(attentionHistory)), attentionVisual},
		{" " + w.iconLabel(iconResources, "HOST CPU "), p.accent, severityHeadline(p, p.accent, hostCPUHeadline(w.hostUsage, cpu), w.hostUsage.cpuPercent, w.hostUsage.cpuOK), hostCPUTrend},
		{" " + w.iconLabel(iconProcesses, "HOST MEMORY "), p.glow, severityHeadline(p, p.glow, hostMemoryHeadline(w.hostUsage, memory), w.hostUsage.memPercent, w.hostUsage.memOK), hostMemoryTrend},
	}
	for i, value := range values {
		card := w.cards[i]
		card.SetTitle(value.title).SetTitleColor(tcell.GetColor(value.colour))
		visual := value.visual
		if i < 2 || len(w.hostCPUHistory) >= 2 {
			visual = gradientText(visual, value.colour, p.accent)
		}
		card.SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s", value.colour, value.headline, visual))
		border := value.colour
		if i < 2 && w.quickFilter == i+1 {
			border = p.text
		}
		card.SetBorderColor(tcell.GetColor(border))
	}
	shortStatus := "waiting"
	if w.limitedColours {
		shortStatus = "256 colours · see troubleshooting"
	}
	if !w.lastRefresh[w.mode].IsZero() {
		shortStatus = "sampled " + w.lastRefresh[w.mode].Format("15:04:05")
		if w.limitedColours {
			shortStatus += " · 256 colours"
		}
	}
	if w.paused {
		shortStatus = "PAUSED"
	}
	if w.backendError[w.mode] {
		w.summary.SetText(" Backend unavailable · last observation retained · a actions to retry")
	} else {
		filter := quickFilterNames[w.quickFilter]
		if w.favoriteOnly {
			filter = w.icon(iconFavorite) + " " + filter
		}
		line := fmt.Sprintf(" %d/%d · %s · %s · %s · %s %d  %s %d", len(w.visible), len(w.items[w.mode]), shortStatus, filter, sortNames[w.sortMode], w.icon(iconHealthy), totals.active, w.icon(iconAttention), totals.attention)
		if note := w.inventoryNote[w.mode]; note != "" {
			// A failed accounting call is said out loud; blank resource
			// columns must never look like an idle machine.
			line += " · " + ellipsize("accounting unavailable · "+clean(note), max(20, w.lastWidth-displayWidth(line)-3))
		}
		w.summary.SetText(line)
	}
	w.updateLoadingIndicator()
	w.updateSelectionCard()
	w.updateStoryline()
	w.updateHeader()
}

func (w *workspace) updateSelectionCard() {
	if w.selectionCard == nil {
		return
	}
	item := w.current()
	p := w.palette()
	if item.ID == "" {
		w.selectionCard.SetTitle(" " + w.iconLabel(iconEye, "SELECTED WORKLOAD "))
		w.selectionCard.SetText(" Select a workload to inspect state, resources and logs.")
		return
	}
	context := "boot " + available(item.Enablement)
	if w.mode == 0 && !usesLaunchd() && item.Restarts > 0 {
		// A unit that keeps being restarted by its manager is a signal in
		// itself, even while its state reads active.
		context += fmt.Sprintf(" · %d restarts", item.Restarts)
	}
	if w.mode == 0 && usesLaunchd() {
		context = "override " + available(item.Enablement) + " · " + launchDomain(w.user)
	}
	if w.mode == 1 {
		context = "project " + item.Project
		if isPod(item) {
			context = "namespace " + item.Project
			if item.Owner != "" {
				context += " · " + item.Owner
			}
		}
	}
	w.selectionCard.SetTitle(" " + tview.Escape(clean(item.Name)) + " ").SetTitleColor(tcell.GetColor(p.accent))
	state := item.State
	if w.mode == 0 && !item.UnitFileOnly && item.Detail != "" {
		state += " / " + item.Detail
	}
	key := fmt.Sprintf("%d/%t/%s", w.mode, w.user, item.ID)
	history := w.workloadHistory[key]
	cpuHistory, memoryHistory := []float64{}, []float64{}
	for _, sample := range history {
		cpuHistory = append(cpuHistory, sample.cpu)
		memoryHistory = append(memoryHistory, sample.memory)
	}
	trend := "collecting workload history"
	if len(history) >= 2 {
		width := w.selectionChartWidth()
		trend = fmt.Sprintf("CPU %s  %s   MEM %s  %s", signalChart(trimHistory(cpuHistory, width), w.settings.GraphMode), signalDelta(cpuHistory), signalChart(trimHistory(memoryHistory, width), w.settings.GraphMode), signalDelta(memoryHistory))
	}
	// The workload's CPU share takes the severity ramp; memory has no honest
	// per-workload ceiling to grade against, so it stays in the identity hue.
	cpuHue := p.accent
	if cpuShare, ok := cpuValue(item.CPU); ok {
		cpuHue = pressureHue(p, int(cpuShare))
	}
	w.selectionCard.SetText(fmt.Sprintf(" [%s::b]%s %s[-::-]  [%s]%s[-]  [%s]CPU[-] [%s::b]%s[-::-]  [%s]MEM[-] %s\n [%s]%s[-]", stateColour(item, p), stateSymbol(item, w.settings.NerdIcons), tview.Escape(clean(state)), p.muted, tview.Escape(clean(context)), p.muted, cpuHue, tview.Escape(available(item.CPU)), p.muted, tview.Escape(available(item.Memory)), p.muted, tview.Escape(trend)))
}

// selectionChartWidth is how many samples fit on the identity band's trend
// line once its labels and deltas have taken their share. Without the trim
// the full 60-sample history overran the band and the memory trail was
// never visible.
func (w *workspace) selectionChartWidth() int {
	width := w.lastWidth
	if w.zoom != 2 && (w.settings.Layout == "side-by-side" || (w.settings.Layout != "stacked" && width >= 110)) {
		width = width * (100 - w.settings.PaneRatio) / 100
	}
	// " CPU " + delta + "   MEM " + delta is about 34 cells; the rail frame takes 4.
	return max(4, (width-38)/2)
}

// severityHeadline colours the leading host figure by the severity ramp and
// hands the rest of the line back to the card's own hue. Card headlines are
// trusted text, so the tags are safe; the figure itself is a formatted number.
func severityHeadline(p palette, cardHue, headline string, percent float64, ok bool) string {
	if !ok {
		return headline
	}
	share, rest, found := strings.Cut(headline, " · ")
	if !found {
		return headline
	}
	return fmt.Sprintf("[%s]%s[%s] · %s", pressureHue(p, int(percent)), share, cardHue, rest)
}

func (w *workspace) cycleGraphMode() {
	current := graphMode(w.settings.GraphMode)
	for i, mode := range graphModes {
		if mode == current {
			w.settings.GraphMode = graphModes[(i+1)%len(graphModes)]
			break
		}
	}
	w.updateDashboard()
	w.savePreferences()
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
