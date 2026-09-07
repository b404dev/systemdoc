package dashboard

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type workspace struct {
	app                             *tview.Application
	pages                           *tview.Pages
	root                            *tview.Flex
	header, summary, detail, footer *tview.TextView
	table                           *tview.Table
	search                          *tview.InputField
	mode, tab, theme                int
	user, ready                     bool
	items                           [2][]workload
	selected                        [2]string
	filters                         [2]string
	visible                         []workload
	generation                      int
	inventoryJobs                   [2]*inventoryJob
	savedQuickFilters               [2]int
	activeOnlyButton                *tview.Button
	detailCache                     map[detailKey]detailEntry
	detailPending                   bool
	logHistoryGeneration            int
	ctx                             context.Context
	detailCancel                    context.CancelFunc
	settings                        settings
	configError                     error
	operations                      []*operation
	operationCancel                 context.CancelFunc
	projects                        []project
	loading                         bool
	refreshDetail                   bool
	lastWidth, lastHeight           int
	layoutDetail                    bool
	logDrawer                       *tview.TextView
	inspectorLog, drawerLog         logSnapshot
	inspectorLogStyle               atomic.Pointer[logStyle]
	drawerLogStyle                  atomic.Pointer[logStyle]
	drawerOpen, drawerPaused        bool
	drawerKey                       string
	drawerCancel                    context.CancelFunc
	drawerGeneration                int
	commandBar, tabBar, telemetry   *tview.Flex
	selectionCard                   *tview.TextView
	loadingIndicator                *tview.TextView
	footerBar                       *tview.Flex
	loadingAnimation                atomic.Bool
	cards                           [4]*tview.TextView
	modeButtons                     [2]*tview.Button
	tabButtons                      [5]*tview.Button
	toolbarButtons                  []*tview.Button
	quickFilter, sortMode, zoom     int
	fleetHistory                    [2][]fleetSample
	lastRefresh                     [2]time.Time
	backendError                    [2]bool
	body                            *tview.Flex
	inspector                       *tview.Flex
	favoriteOnly                    bool
	logPaused                       bool
	logQuery                        string
	paused                          bool
	metrics                         map[string][]metricSample
	activity                        []activityEvent
	commandHistory                  []string
}

type Options struct {
	User       bool
	Docker     bool
	Filter     string
	ActiveOnly bool
}

func Run(options Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Pin the CLI context so an external `docker context use` cannot retarget actions mid-session.
	if os.Getenv("DOCKER_CONTEXT") == "" && os.Getenv("DOCKER_HOST") == "" {
		if output, err := command(ctx, "docker", "context", "show"); err == nil {
			os.Setenv("DOCKER_CONTEXT", strings.TrimSpace(output))
		}
	}
	w := newWorkspace(ctx, options.User)
	if options.Docker {
		w.mode = 1
	}
	w.filters[w.mode] = options.Filter
	w.search.SetText(options.Filter)
	w.settings, w.configError = readSettings()
	if w.settings.SystemdActiveOnly {
		w.savedQuickFilters[0] = 1
	}
	if options.ActiveOnly {
		w.savedQuickFilters[w.mode] = 1
	}
	w.quickFilter = w.savedQuickFilters[w.mode]
	w.theme = themeIndex(w.settings.Theme)
	w.applyTheme()
	w.app.SetAfterDrawFunc(func(tcell.Screen) {
		if !w.ready {
			w.ready = true
			w.startInventory(w.mode)
			w.startInventory(1 - w.mode)
			go w.refreshLoop()
			go func() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(250 * time.Millisecond):
					w.queue(func() {
						if w.loading && w.settings.Splash {
							w.splash()
						}
					})
				}
			}()
		}
	})
	return w.app.Run()
}

func textView() *tview.TextView { return tview.NewTextView().SetDynamicColors(false) }

func newWorkspace(ctx context.Context, user bool) *workspace {
	w := &workspace{app: tview.NewApplication(), ctx: ctx, user: user, sortMode: 1, settings: settings{RefreshSeconds: 5}}
	w.header, w.summary, w.detail, w.footer = textView(), textView(), textView(), textView()
	w.detail.SetDynamicColors(true).SetScrollable(true).SetWrap(w.settings.WrapLogs).SetBorder(true).SetTitle(" Inspector ")
	w.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	w.table.SetBorder(true).SetTitle(" Workloads ")
	w.search = tview.NewInputField().SetLabel(" / Filter  ").SetFieldWidth(0)
	w.search.SetChangedFunc(w.filterChanged)
	w.search.SetDoneFunc(func(tcell.Key) { w.app.SetFocus(w.table) })
	w.table.SetSelectionChangedFunc(func(row, _ int) {
		if row > 0 && row <= len(w.visible) {
			w.selected[w.mode] = w.visible[row-1].ID
			w.updateSelectionCard()
			w.syncLogDrawer()
			w.showDetail()
		}
	})
	w.table.SetSelectedFunc(func(int, int) { w.app.SetFocus(w.detail) })
	w.buildDashboard()
	w.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		p := w.palette()
		border, focus := tcell.GetColor(p.accent), tcell.GetColor(p.text)
		w.table.SetBorderColor(border)
		w.detail.SetBorderColor(border)
		w.logDrawer.SetBorderColor(border)
		// Drawing holds the application lock; inspect widgets directly here.
		if w.table.HasFocus() {
			w.table.SetBorderColor(focus)
		} else if w.detail.HasFocus() {
			w.detail.SetBorderColor(focus)
		} else if w.logDrawer.HasFocus() {
			w.logDrawer.SetBorderColor(focus)
		}
		width, height := screen.Size()
		w.layoutDashboard(width, height)
		return false
	})
	w.pages = tview.NewPages().AddPage("main", &luminousDashboard{Flex: w.root, w: w}, true, true)
	w.app.SetRoot(w.pages, true).EnableMouse(true).EnablePaste(true).SetInputCapture(w.input)
	w.applyTheme()
	w.summary.SetText(" Connecting to workloads…")
	return w
}

func (w *workspace) applyTheme() {
	w.updateLogStyles()
	p := w.palette()
	bg, fg, accent := tcell.GetColor(p.background), tcell.GetColor(p.text), tcell.GetColor(p.accent)
	tview.Styles = tview.Theme{PrimitiveBackgroundColor: bg, ContrastBackgroundColor: tcell.GetColor(p.surface), MoreContrastBackgroundColor: accent, BorderColor: accent, TitleColor: accent, GraphicsColor: accent, PrimaryTextColor: fg, SecondaryTextColor: accent, TertiaryTextColor: tcell.GetColor(p.muted), InverseTextColor: bg, ContrastSecondaryTextColor: fg}
	w.root.SetBackgroundColor(bg)
	for _, view := range []*tview.TextView{w.header, w.summary, w.detail, w.footer} {
		view.SetBackgroundColor(bg)
		view.SetTextColor(fg)
	}
	w.detail.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorderColor(accent)
	w.table.SetBackgroundColor(bg).SetBorderColor(accent)
	w.table.SetSelectedStyle(tcell.StyleDefault.Background(accent).Foreground(bg))
	w.search.SetBackgroundColor(bg)
	w.search.SetLabelStyle(tcell.StyleDefault.Foreground(accent).Background(bg))
	w.search.SetLabelColor(accent).SetFieldBackgroundColor(tcell.GetColor(p.surface)).SetFieldTextColor(fg)
	w.header.SetTextColor(accent)
	w.footer.SetTextColor(tcell.GetColor(p.muted))
	w.styleDashboard()
	w.chrome()
	w.renderTable()
}

func (w *workspace) chrome() {
	host, _ := os.Hostname()
	contextLabel := "system"
	if w.user {
		contextLabel = "user"
	}
	if w.mode == 1 {
		contextLabel = os.Getenv("DOCKER_CONTEXT")
		if contextLabel == "" {
			contextLabel = "Docker CLI endpoint"
		}
	}

	brand := "SYSTEMDOC"
	if w.settings.NerdIcons {
		brand = "\uf17c " + brand
		if w.mode == 1 {
			brand = "\uf308 SYSTEMDOC"
		}
	}
	p := w.palette()
	wordmark := gradientText("SYSTEMDOC", p.accent, p.glow)
	if brand != "SYSTEMDOC" {
		wordmark = strings.TrimSuffix(brand, "SYSTEMDOC") + wordmark
	}
	w.header.SetText(fmt.Sprintf(" [%s]◈ [::b]%s[-::-]  [%s]WORKLOAD CONTROL[-]   [%s]%s / %s[-]", p.accent, wordmark, p.muted, p.text, tview.Escape(clean(host)), tview.Escape(clean(contextLabel))))
	w.styleNavigation()
	w.footer.SetText(fmt.Sprintf(" [%s::b]/[-::-] search  [%s::b]i[-::-] active  A states  S sort  z expand  [%s::b]L[-::-] logs  [%s::b]R[-::-] restart  a actions  ? help  q quit", p.accent, p.accent, p.accent, p.warning))
	w.updateDashboard()

}

func (w *workspace) queue(update func()) {
	if w.ctx.Err() == nil {
		w.app.QueueUpdateDraw(update)
	}
}

func (w *workspace) renderTable() {
	w.table.SetSelectionChangedFunc(nil)
	w.table.Clear()
	w.visible = nil
	filter := strings.ToLower(w.filters[w.mode])
	for _, item := range w.items[w.mode] {
		if matchesFilter(item, filter) && w.matchesQuickFilter(item) && (!w.favoriteOnly || w.isFavorite(item.ID)) {
			w.visible = append(w.visible, item)
		}
	}
	w.sortWorkloads()
	p := w.palette()
	headers := []string{"WORKLOAD", "STATE", "CPU", "MEMORY"}
	if w.mode == 1 {
		headers[0] = "CONTAINER"
	}
	if w.zoom == 1 {
		if w.mode == 0 {
			headers = append(headers, "BOOT", "SUBSTATE", "DESCRIPTION")
		} else {
			headers = append(headers, "PROJECT", "HEALTH / STATUS", "IMAGE")
		}
	}
	for col, name := range headers {
		cell := tview.NewTableCell(name).SetSelectable(false).SetTextColor(tcell.GetColor(p.accent)).SetAttributes(tcell.AttrBold)
		if col == 0 {
			cell.SetExpansion(1)
		}
		w.table.SetCell(0, col, cell)
	}
	selection := 1
	for i, item := range w.visible {
		name := item.Name
		if w.recentlyChanged(item.ID) {
			name = "› " + name
		}
		if w.isFavorite(item.ID) {
			name = "★ " + name
		}
		name = "  " + name
		values := []string{name, stateSymbol(item) + " " + item.State, available(item.CPU), available(item.Memory)}
		if w.zoom == 1 {
			context := available(item.Enablement)
			if w.mode == 1 {
				context = item.Project
			}
			values = append(values, context, item.Detail, item.Description)
		}
		for col, value := range values {
			colour := p.text
			if col == 1 {
				colour = stateColour(item, p)
			}
			if col == 2 || col == 3 {
				colour = p.accent
			}
			cell := tview.NewTableCell(tview.Escape(clean(value))).SetTextColor(tcell.GetColor(colour))
			if col == 0 {
				cell.SetExpansion(1).SetMaxWidth(64)
			} else if col == 6 {
				cell.SetExpansion(2).SetMaxWidth(80)
			} else {
				cell.SetMaxWidth(14)
			}
			cell.SetBackgroundColor(tcell.GetColor(p.background))
			w.table.SetCell(i+1, col, cell)
		}
		if item.ID == w.selected[w.mode] {
			selection = i + 1
		}
	}
	w.summary.SetText(fmt.Sprintf(" %d / %d workloads  ·  %s  ·  %s", len(w.visible), len(w.items[w.mode]), quickFilterNames[w.quickFilter], sortNames[w.sortMode]))
	w.table.Select(selection, 0)
	w.table.SetSelectionChangedFunc(func(row, _ int) {
		if row > 0 && row <= len(w.visible) {
			w.selected[w.mode] = w.visible[row-1].ID
			w.updateSelectionCard()
			w.syncLogDrawer()
			w.showDetail()
		}
	})
	if len(w.visible) > 0 {
		w.selected[w.mode] = w.visible[selection-1].ID
		if !w.refreshDetail {
			w.showDetail()
		}
	} else {
		w.generation++
		w.detailPending = false
		w.inspectorLog = logSnapshot{}
		if w.detailCancel != nil {
			w.detailCancel()
			w.detailCancel = nil
		}
		w.detail.SetText("No matching workloads.\n\nClear the filter or refresh. Check backend access if no workloads are available.")
	}
	w.updateDashboard()
	w.syncLogDrawer()
}

func (w *workspace) showDetail() {
	if w.refreshDetail && (w.tab == 1 || w.detailPending) {
		return
	}
	w.detail.SetTitle(" " + w.inspectorTabName() + " ")
	w.detail.SetWrap(w.settings.WrapLogs || (w.mode == 1 && (w.tab == 0 || w.tab == 4)))
	if w.detailCancel != nil {
		w.detailCancel()
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.detailCancel = cancel
	w.detailPending = false
	w.generation++
	generation := w.generation
	var selected workload
	for _, item := range w.visible {
		if item.ID == w.selected[w.mode] {
			selected = item
			break
		}
	}
	if selected.ID == "" {
		return
	}
	if w.mode == 0 && isTemplate(selected) && w.tab != 2 {
		text, _ := inspect(ctx, w.mode, w.user, selected, w.tab)
		w.detail.SetText(richOutput(text, w.tab, w.palette()))
		return
	}
	if !w.refreshDetail {
		w.detail.ScrollToBeginning()
	}

	key := detailKey{mode: w.mode, user: w.user, id: selected.ID, tab: w.tab}
	cached, exists := w.detailCache[key]
	if exists {
		w.detail.SetText(richOutput(cached.text, w.tab, w.palette()))
		if time.Since(cached.at) < time.Duration(w.settings.RefreshSeconds)*time.Second {
			return
		}
	} else if !w.refreshDetail {
		w.detail.SetText(tview.Escape("Loading " + selected.Name + "…"))
	}
	preserve := w.refreshDetail
	mode, tab, user := w.mode, w.tab, w.user
	if tab == 1 {
		w.inspectorLog = logSnapshot{}
		w.logPaused = false
		go w.streamLogs(ctx, mode, user, selected, generation)
		return
	}
	w.detailPending = true
	p := w.palette()
	go func() {
		// Brief debounce avoids spawning a process for every rapidly traversed row.
		select {
		case <-ctx.Done():
			return
		case <-time.After(120 * time.Millisecond):
		}
		output, err := inspect(ctx, mode, user, selected, tab)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			output = err.Error()
		}
		formatted := richOutput(output, tab, p)
		w.queue(func() {
			if generation == w.generation {
				w.detailPending = false
				if err == nil {
					w.cacheDetail(key, output)
				}
				if tab == 3 && mode == 0 && err == nil {
					output = w.resourceOutput(selected.ID, output)
				}
				if tab == 3 && mode == 1 && err == nil {
					output = dockerResourceOutput(output)
				}
				if tab == 3 || p != w.palette() {
					formatted = richOutput(output, tab, w.palette())
				}
				// Preserve where the reader is now, not where they were when
				// the asynchronous backend request started.
				row, col := w.detail.GetScrollOffset()
				w.detail.SetText(formatted)
				if preserve {
					w.detail.ScrollTo(row, col)
				}
			}
		})
	}()
}

func clean(value string) string {
	value = ansiSequence.ReplaceAllString(value, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 && !(r >= 128 && r <= 159) {
			return r
		}
		return -1
	}, value)
}

func (w *workspace) input(event *tcell.EventKey) *tcell.EventKey {
	front, _ := w.pages.GetFrontPage()
	if front != "main" {
		return event
	}
	if w.app.GetFocus() == w.search {
		return event
	}
	if event.Key() == tcell.KeyEscape {
		w.zoom = 0
		w.lastWidth = 0
		w.redrawRows()
		w.app.SetFocus(w.table)
		return nil
	}
	if event.Key() == tcell.KeyTab {
		w.zoom = 0
		w.lastWidth = 0
		w.redrawRows()
		if w.app.GetFocus() == w.table {
			w.app.SetFocus(w.detail)
		} else if w.detail.HasFocus() && w.drawerOpen {
			w.app.SetFocus(w.logDrawer)
		} else {
			w.app.SetFocus(w.table)
		}
		return nil
	}
	if w.logDrawer.HasFocus() {
		if event.Rune() == ' ' {
			w.drawerPaused = !w.drawerPaused
			if w.drawerPaused {
				w.logDrawer.SetTitle(" LOGS · paused · g follows · L closes ")
			} else {
				w.renderDrawerLogs()
			}
			return nil
		}
		if event.Rune() == 'g' {
			w.drawerPaused = false
			w.renderDrawerLogs()
			return nil
		}
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyPgUp || event.Rune() == 'k' {
			w.drawerPaused = true
			w.logDrawer.SetTitle(" LOGS · paused · g follows · L closes ")
		}
	}
	if w.app.GetFocus() == w.detail && w.tab == 1 && (event.Key() == tcell.KeyUp || event.Key() == tcell.KeyPgUp || event.Rune() == 'k') {
		w.logPaused = true
		w.detail.SetTitle(" Logs · paused · g resumes ")
	}
	switch event.Rune() {
	case ' ':
		if w.tab == 1 {
			w.logPaused = !w.logPaused
			if w.logPaused {
				w.detail.SetTitle(" Logs · paused · g resumes ")
			} else {
				w.renderInspectorLogs()
			}
		}
	case 'g':
		w.logPaused = false
		if w.tab == 1 {
			w.renderInspectorLogs()
		} else {
			w.detail.ScrollToEnd()
		}
	case 'q':
		if w.operationCancel != nil {
			w.confirm("Quit while an operation is running?", "Local observation stops; backend jobs may continue.", func() { w.operationCancel(); w.app.Stop() })
		} else {
			w.app.Stop()
		}
	case '1', '2':
		w.switchMode(int(event.Rune() - '1'))
	case 'z':
		w.toggleZoom()
	case 'V':
		w.savedViews()
	case 'T':
		w.timers()
	case 'E':
		w.snapshotDialog()
	case 'S':
		w.chooseSort()
	case 'A':
		w.setQuickFilter((w.quickFilter + 1) % len(quickFilterNames))
	case 'i':
		w.toggleActiveOnly()
	case '/':
		w.app.SetFocus(w.search)
	case 'L':
		w.toggleLogDrawer()
	case 'l':
		w.selectTab(1)
	case 'c':
		w.selectTab(2)
	case 'R':
		w.confirmAction("restart")
	case 'r':
		w.selectTab(3)
	case 'v':
		w.activityView()
	case 'd':
		w.selectTab(4)
	case 'o':
		w.selectTab(0)
	case 't':
		w.themeDialog()
	case 'P':
		w.paused = !w.paused
		w.updateDashboard()
		if w.paused {
			w.footer.SetText(" Inventory refresh paused · P resumes")
		} else {
			w.load()
		}
	case 's':
		if w.tab == 1 || w.logDrawer.HasFocus() {
			w.logSearch()
		}
	case 'e':
		w.exportView()
	case 'p':
		w.projectDialog()
	case 'h':
		if w.tab == 1 || w.logDrawer.HasFocus() {
			w.logHistory()
		}
	case 'H':
		w.operationHistory()
	case 'f':
		w.toggleFavorite()
	case 'F':
		w.favoriteOnly = !w.favoriteOnly
		w.renderTable()
	case 'u':
		if w.mode == 0 {
			w.user = !w.user
			w.items[0] = nil
			w.fleetHistory[0] = nil
			w.lastRefresh[0] = time.Time{}
			w.selected[0] = ""
			w.renderTable()
			w.chrome()
			w.load()
		}
	case ':':
		w.commandDialog()
	case 'a':
		w.actions()
	case '?':
		w.message("Welcome to Systemdoc", "Two modes. One workspace.\n\n1 / 2 switch modes · / filters names, states, projects\nEnter focuses inspector · Tab switches panes\nl logs · h retained log history · L live log drawer · c configuration · r resources · o overview\nR restart selected workload · t previews themes · a actions · q quits\nS sort · i active only · A cycle state filter · z expand focused pane · Escape restores\nClick Active / Attention cards to filter; click again to clear\n\nLive data refreshes automatically.\nV saved views · T systemd timers · E troubleshooting snapshot\ns richer log search · p Compose projects · H operation history\nf favorite · F favorites only · u system/user scope\n: supported commands · Actions includes lifecycle and native tools.")
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	default:
		return event
	}
	return nil
}

func (w *workspace) message(title, body string) {
	focus := w.app.GetFocus()
	modal := tview.NewModal().SetText(tview.Escape(title + "\n\n" + body)).AddButtons([]string{"Close"})
	modal.SetDoneFunc(func(int, string) { w.pages.RemovePage("dialog"); w.app.SetFocus(focus) })
	w.pages.AddPage("dialog", modal, true, true)
}

func (w *workspace) themeDialog() {
	original := w.theme
	content := w.detail.GetText(true)
	if content == "" {
		content = "Your selected workload will appear here."
	}
	content += "\n\nColour vocabulary\nERROR · failure\nWARNING · needs attention\nSuccess · healthy\n12:34:56 Muted timestamps"
	list := tview.NewList().ShowSecondaryText(false)
	preview := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	preview.SetBorder(true).SetTitle(" Live colour preview ")
	for _, theme := range themes {
		list.AddItem(theme.name, "", 0, nil)
	}
	list.SetBorder(true).SetTitle(" Themes · Enter saves ")
	refresh := func() {
		p := w.palette()
		list.SetBackgroundColor(tcell.GetColor(p.background))
		list.SetMainTextColor(tcell.GetColor(p.text))
		list.SetSelectedBackgroundColor(tcell.GetColor(p.accent))
		list.SetSelectedTextColor(tcell.GetColor(p.background))
		list.SetBorderColor(tcell.GetColor(p.muted))
		preview.SetBackgroundColor(tcell.GetColor(p.surface))
		preview.SetBorderColor(tcell.GetColor(p.accent))
		preview.SetText(richOutput(content, w.tab, p))
	}
	list.SetCurrentItem(original)
	list.SetChangedFunc(func(index int, _, _ string, _ rune) { w.theme = index; w.applyTheme(); refresh() })
	close := func() { w.pages.RemovePage("theme"); w.app.SetFocus(w.table) }
	list.SetSelectedFunc(func(int, string, string, rune) { w.settings.Theme = w.palette().name; close(); w.savePreferences() })
	panel := tview.NewFlex().AddItem(list, 28, 0, true).AddItem(preview, 0, 1, false)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			w.theme = original
			w.applyTheme()
			close()
			return nil
		}
		return event
	})
	refresh()
	w.pages.AddPage("theme", centered(panel, 108, 24), true, true)
}
