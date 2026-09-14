package dashboard

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var hostTabNames = []string{"SERVICES", "CONTAINERS", "NETWORK", "PROCESSES", "STORAGE"}
var hostTabIcons = []iconRole{iconServices, iconContainers, iconNetwork, iconProcesses, iconStorage}
var suiteButtonWidths = []int{15, 15, 14, 15, 14}

// tview centres a button label inside its cell, so a button sized to its exact
// label leaves no gap and the next label reads as part of this one. The extra
// cells on each side keep the rail legible as separate controls.
func controlButtonWidth(label string) int {
	return tview.TaggedStringWidth(label) + 2
}

func setSuiteLabels(buttons []*tview.Button, width int, nerd bool) {
	for i, button := range buttons {
		name := hostTabNames[i]
		if width < 100 {
			name = []string{"SVC", "CTR", "NET", "PROC", "DISK"}[i]
		}
		button.SetLabel(fmt.Sprintf("%d %s %s", i+1, iconFor(nerd, hostTabIcons[i]), name))
	}
}

func (w *workspace) openHostTab(tab int) {
	switch tab {
	case 0, 1:
		w.switchMode(tab)
	case 2:
		w.networkPage()
	case 3, 4:
		w.hostPage(tab)
	}
}

// hostNavigation is the suite rail every non-main page shares with the main
// dashboard: five numbered suites on the left, a spacer, and the controls that
// apply everywhere on the right. Matching the main rail's geometry is what
// keeps the top bar from reshuffling when the user changes suite.
func (w *workspace) hostNavigation(active int, choose func(int)) (*tview.Flex, []*tview.Button, *tview.Button) {
	p := w.palette()
	bar := tview.NewFlex()
	bar.SetBackgroundColor(tcell.GetColor(p.background))
	var buttons []*tview.Button
	for i, label := range hostTabNames {
		button := tview.NewButton(label).SetSelectedFunc(func() { choose(i) })
		fg, bg := p.muted, p.background
		if i == active {
			fg, bg = p.background, p.accent
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(i == active))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
		bar.AddItem(button, suiteButtonWidths[i], 0, false)
		buttons = append(buttons, button)
	}
	setSuiteLabels(buttons, 120, w.settings.NerdIcons)
	bar.AddItem(tview.NewBox().SetBackgroundColor(tcell.GetColor(p.background)), 0, 1, false)
	deckLabel := "0 " + w.iconLabel(iconDeck, "Deck")
	deck := tview.NewButton(deckLabel).SetSelectedFunc(w.controlDeck)
	deck.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.muted)).Background(tcell.GetColor(p.background)))
	deck.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	bar.AddItem(deck, controlButtonWidth(deckLabel), 0, false)
	return bar, buttons, deck
}

type hostRow struct {
	key    string
	cells  []string
	detail string
	// rich is the selection band's presentation of the same facts: trusted
	// colour tags around already-escaped values. detail stays plain because
	// the full-details view and the exported report reuse it verbatim.
	rich    string
	tint    map[int]string
	pid     int
	warning bool
}

// A card shows either a one-row meter above a note, or a two-row trend chart.
// Both fill the same three inner rows, so the row of cards keeps one height.
type hostCard struct {
	title, headline, visual, note string
	chart                         bool
}
type hostPage struct {
	*tview.Flex
	w                               *workspace
	ctx                             context.Context
	cancel                          context.CancelFunc
	tab                             int
	name                            string
	header, detail, status, footer  *tview.TextView
	navigation                      *tview.Flex
	table                           *tview.Table
	search                          *tview.InputField
	summary                         *tview.Flex
	cards                           [3]*tview.TextView
	rows                            []hostRow
	processes                       []hostProcess
	mounts                          []hostMount
	deleted                         []deletedFile
	sort                            int
	tree, deletedView, busy, paused bool
	signalling                      bool
	generation                      int
	selected                        string
	query                           [2]string
	at                              [2]time.Time
	lastPoll                        time.Time
	note                            [2]string
	failure                         [2]string
	modeButtons                     [5]*tview.Button
	viewButtons                     [2]*tview.Button
	deckButton                      *tview.Button
	width, height                   int
}

// Each host suite carries its own pair of views. Network already exposes its
// views on a rail; processes and storage kept theirs behind a single toggle
// key, which left two different tools looking like the same table twice.
func (h *hostPage) viewLabels() []string {
	if h.tab == 3 {
		return []string{"f " + h.w.icon(iconProcesses) + " Flat", "t " + h.w.icon(iconDependencies) + " Tree"}
	}
	return []string{"m " + h.w.icon(iconStorage) + " Filesystems", "d " + h.w.icon(iconChanged) + " Deleted but open"}
}

// The rail selects a view directly. The original toggle keys still work, so
// existing muscle memory and the documented shortcuts are unaffected.
func (h *hostPage) selectView(view int) {
	if h.tab == 3 {
		if h.tree != (view == 1) {
			h.switchTree()
		}
		return
	}
	if h.deletedView != (view == 1) {
		h.switchStorage()
	}
}

func (h *hostPage) currentView() int {
	if h.tab == 3 {
		if h.tree {
			return 1
		}
		return 0
	}
	return h.view()
}

// The selection band is named for what it actually shows, not "SELECTED".
func (h *hostPage) bandTitle() string {
	if h.tab == 3 {
		return h.w.iconLabel(iconProcesses, "PROCESS VITALS · Enter live activity")
	}
	if h.deletedView {
		return h.w.iconLabel(iconChanged, "OPEN HANDLE · Enter full record")
	}
	return h.w.iconLabel(iconStorage, "CAPACITY · Enter full record")
}

func (h *hostPage) view() int {
	if h.deletedView {
		return 1
	}
	return 0
}

func (h *hostPage) Draw(screen tcell.Screen) {
	_, _, width, height := h.GetRect()
	if h.width != width || mastheadHeight(h.width, h.height) != mastheadHeight(width, height) {
		h.width, h.height = width, height
		setSuiteLabels(h.modeButtons[:], width, h.w.settings.NerdIcons)
		for i, button := range h.modeButtons {
			buttonWidth := suiteButtonWidths[i]
			if width < 100 {
				buttonWidth = max(6, width/5)
			}
			h.navigation.ResizeItem(button, buttonWidth, 0)
		}
		deckWidth := controlButtonWidth(h.deckButton.GetLabel())
		if width < 140 {
			deckWidth = 0
		}
		h.navigation.ResizeItem(h.deckButton, deckWidth, 0)
		h.render()
	}
	h.height = height
	headerHeight, cardsHeight, detailHeight := mastheadHeight(width, height), 0, 7
	if height >= 28 {
		cardsHeight = 5
	}
	if height < 22 {
		detailHeight = 0
	}
	h.ResizeItem(h.header, headerHeight, 0)
	h.header.SetBorderPadding(0, 0, 1, 0)
	h.ResizeItem(h.summary, cardsHeight, 0)
	h.ResizeItem(h.detail, detailHeight, 0)
	h.Flex.Draw(screen)
	p := h.w.palette()
	rails := map[*tview.Box]string{}
	for i, card := range h.cards {
		rails[card.Box] = []string{p.accent, p.warning, p.success}[i]
	}
	paintSurfaces(screen, h.Flex, p, h.header, h.table, rails)
}

func (w *workspace) hostPage(tab int) {
	name := "processes"
	if tab == 4 {
		name = "storage"
	}
	if _, page := w.pages.GetFrontPage(); page != nil {
		if h, ok := page.(*hostPage); ok && h.tab == tab {
			w.app.SetFocus(h.table)
			return
		}
	}
	ctx, cancel := context.WithCancel(w.ctx)
	h := &hostPage{Flex: tview.NewFlex().SetDirection(tview.FlexRow), w: w, ctx: ctx, cancel: cancel, tab: tab, name: name}
	p := w.palette()
	h.SetBackgroundColor(tcell.GetColor(p.background))
	h.header = textView().SetDynamicColors(true).SetTextColor(tcell.GetColor(p.accent))
	h.detail = textView().SetDynamicColors(true).SetWrap(true).SetScrollable(true).SetTextColor(tcell.GetColor(p.text))
	h.status = textView().SetDynamicColors(true).SetWrap(false).SetTextColor(tcell.GetColor(p.muted))
	h.footer = textView().SetTextColor(tcell.GetColor(p.muted))
	for _, v := range []*tview.TextView{h.header, h.status, h.footer} {
		v.SetBackgroundColor(tcell.GetColor(p.background))
	}
	detailTitle := " SELECTED · Enter full details "
	if h.tab == 3 {
		detailTitle = " SELECTED · Enter live activity "
	}
	h.detail.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetTitle(detailTitle).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	h.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	h.table.SetBackgroundColor(tcell.GetColor(p.background)).SetBorder(true).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	h.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(p.accent)).Foreground(tcell.GetColor(p.background)).Bold(true))
	w.illuminate(h.table.Box, h.table.HasFocus, func(p palette) string { return panelHue(p, tab) })
	w.illuminate(h.detail.Box, h.detail.HasFocus, func(p palette) string { return panelHue(p, tab) })
	h.summary = tview.NewFlex()
	h.summary.SetBackgroundColor(tcell.GetColor(p.background))
	for i := range h.cards {
		card := textView().SetDynamicColors(true).SetWrap(false)
		card.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderAttributes(tcell.AttrBold)
		h.cards[i] = card
		h.summary.AddItem(card, 0, 1, false)
		w.illuminatePanel(card.Box, nil, func(p palette) string { return []string{panelHue(p, tab), p.warning, p.success}[i] }, true)
	}
	nav, modeButtons, deck := w.hostNavigation(tab, func(target int) {
		if target == h.tab {
			w.app.SetFocus(h.table)
		} else {
			h.close()
			w.openHostTab(target)
		}
	})
	copy(h.modeButtons[:], modeButtons)
	h.deckButton = deck
	h.navigation = nav
	viewBar := tview.NewFlex()
	viewBar.SetBackgroundColor(tcell.GetColor(p.background))
	for i, label := range h.viewLabels() {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { h.selectView(i) })
		h.viewButtons[i] = button
		// Sized to the label, not to an equal share: two views stretched
		// across the full width read as banners rather than as controls.
		viewBar.AddItem(button, controlButtonWidth(label)+2, 0, false)
	}
	viewBar.AddItem(tview.NewBox().SetBackgroundColor(tcell.GetColor(p.background)), 0, 1, false)
	h.search = tview.NewInputField().SetLabel(" / " + w.iconLabel(iconSearch, "Filter "))
	h.search.SetBackgroundColor(tcell.GetColor(p.background))
	h.search.SetFieldBackgroundColor(tcell.GetColor(p.surface)).SetFieldTextColor(tcell.GetColor(p.text)).SetLabelColor(tcell.GetColor(p.accent))
	h.search.SetChangedFunc(func(query string) { h.query[h.view()] = query; h.render() })
	h.search.SetDoneFunc(func(tcell.Key) { w.app.SetFocus(h.table) })
	h.AddItem(h.header, 1, 0, false).AddItem(nav, 1, 0, false).AddItem(h.summary, 5, 0, false).AddItem(viewBar, 1, 0, false).AddItem(h.search, 1, 0, false).AddItem(h.table, 0, 1, true).AddItem(h.detail, 7, 0, false).AddItem(h.status, 2, 0, false).AddItem(h.footer, 1, 0, false)
	h.table.SetSelectionChangedFunc(func(row, col int) { h.selectRow(row) })
	h.table.SetSelectedFunc(func(row, col int) { h.inspect() })
	h.SetInputCapture(h.input)
	w.pages.AddPage(name, h, true, true)
	h.render()
	h.refresh()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Deciding whether a poll is due changes nothing on screen;
				// refresh() draws when its results arrive.
				w.queueQuiet(func() {
					if ctx.Err() == nil {
						if front, _ := w.pages.GetFrontPage(); front == name && !h.paused && pollDue(h.lastPoll, time.Now(), h.pollSeconds()) {
							h.refresh()
						}
					}
				})
			}
		}
	}()
}

func (h *hostPage) close() { h.cancel(); h.w.pages.RemovePage(h.name); h.w.app.SetFocus(h.w.table) }

func (h *hostPage) pollSeconds() int {
	seconds := h.w.settings.RefreshSeconds
	if h.tab == 3 && seconds < 15 {
		return 15
	}
	return seconds
}

func (h *hostPage) input(e *tcell.EventKey) *tcell.EventKey {
	w := h.w
	if h.tab == 3 && e.Key() == tcell.KeyF9 {
		h.processActions()
		return nil
	}
	if e.Key() == tcell.KeyEscape {
		if h.search.HasFocus() || h.detail.HasFocus() {
			w.app.SetFocus(h.table)
		} else {
			h.close()
		}
		return nil
	}
	if e.Key() == tcell.KeyTab {
		_, _, _, height := h.GetRect()
		if h.search.HasFocus() {
			w.app.SetFocus(h.table)
		} else if h.table.HasFocus() && height >= 22 {
			w.app.SetFocus(h.detail)
		} else {
			w.app.SetFocus(h.search)
		}
		return nil
	}
	if h.search.HasFocus() {
		return e
	}
	if h.tab == 3 && e.Key() == tcell.KeyDelete {
		h.processActions()
		return nil
	}
	switch e.Rune() {
	case '0':
		w.controlDeck()
	case ',':
		w.pollingDialog()
	case '1', '2', '3', '4', '5':
		target := int(e.Rune() - '1')
		if target != h.tab {
			h.close()
			w.openHostTab(target)
		}
		return nil
	case '/':
		w.app.SetFocus(h.search)
	case 'r':
		h.refresh()
	case 'P':
		h.paused = !h.paused
		h.updateStatus()
	case 'S':
		h.sort = (h.sort + 1) % 3
		h.render()
	case 'G':
		w.cycleGraphMode()
		h.render()
	case 't':
		if h.tab == 3 {
			h.switchTree()
		}
	case 'f':
		if h.tab == 3 && h.tree {
			h.switchTree()
		}
	case 'd':
		if h.tab == 4 && !h.deletedView {
			h.switchStorage()
		}
	case 'm':
		if h.tab == 4 && h.deletedView {
			h.switchStorage()
		}
	case 'n':
		if h.tab == 3 {
			h.ports()
		}
	case 'K':
		if h.tab == 3 {
			h.processActions()
		}
	case 's':
		if h.tab == 3 {
			h.service()
		}
	case 'T':
		if h.tab == 3 {
			h.sysdigForProcess()
		}
	case 'e':
		w.reviewSnapshot(h.report())
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, 0)
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, 0)
	default:
		return e
	}
	return nil
}

func (h *hostPage) switchTree() {
	h.tree = !h.tree
	h.render()
}

func (h *hostPage) switchStorage() {
	h.deletedView = !h.deletedView
	h.generation++
	h.selected = ""
	h.search.SetChangedFunc(nil).SetText(h.query[h.view()]).SetChangedFunc(func(query string) { h.query[h.view()] = query; h.render() })
	h.render()
	h.refresh()
}

func (h *hostPage) refresh() {
	if h.busy || h.ctx.Err() != nil {
		return
	}
	h.busy = true
	h.lastPoll = time.Now()
	h.updateStatus()
	view, generation, platform := h.view(), h.generation, servicePlatform
	go func() {
		var processes []hostProcess
		var mounts []hostMount
		var deleted []deletedFile
		var note string
		var err error
		if h.tab == 3 {
			processes, err = collectProcesses(h.ctx)
		} else if view == 1 {
			deleted, note, err = collectDeletedFiles(h.ctx)
		} else {
			mounts, note, err = collectMounts(h.ctx, platform)
		}
		if h.ctx.Err() != nil {
			return
		}
		h.w.queue(func() {
			if h.ctx.Err() != nil {
				return
			}
			h.busy = false
			if generation != h.generation {
				h.refresh()
				return
			}
			if err != nil {
				h.failure[view] = err.Error()
			} else {
				h.failure[view] = ""
				h.at[view] = time.Now()
				h.note[view] = note
				if h.tab == 3 {
					h.processes = processes
				} else if view == 1 {
					h.deleted = deleted
				} else {
					h.mounts = mounts
				}
			}
			h.render()
		})
	}()
}

func (h *hostPage) render() {
	p := h.w.palette()
	title := "Process explorer"
	if h.tab == 4 {
		title = "Disk & storage"
	}
	h.header.SetText(h.w.masthead(title, "", hostReadout(p, h.w.hostUsage)+"   "+h.w.signalsHint(p), h.width, h.height))
	// The suite's signature hue lights its frame, its selection band and its
	// view rail, so each panel is recognisable before a word is read.
	hue := panelHue(p, h.tab)
	h.table.SetBorderColor(tcell.GetColor(hue)).SetTitleColor(tcell.GetColor(hue))
	h.detail.SetBorderColor(tcell.GetColor(hue)).SetTitleColor(tcell.GetColor(hue))
	h.detail.SetTitle(" " + h.bandTitle() + " ")
	current := h.currentView()
	for i, button := range h.viewButtons {
		if button == nil {
			continue
		}
		fg, bg := p.muted, p.background
		if i == current {
			fg, bg = p.background, hue
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(i == current))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(hue)).Bold(true))
	}
	var headers []string
	var cards [3]hostCard
	if h.tab == 3 {
		headers, cards = h.processRows()
	} else if h.deletedView {
		headers, cards = h.deletedRows()
	} else {
		headers, cards = h.mountRows()
	}
	for i, c := range cards {
		colour := []string{hue, p.warning, p.success}[i]
		h.cards[i].SetTitle(" " + c.title + " ").SetTitleColor(tcell.GetColor(colour))
		if c.chart {
			h.cards[i].SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s", colour, tview.Escape(c.headline), gradientText(c.visual, colour, p.accent)))
			continue
		}
		h.cards[i].SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s\n[%s]%s[-]", colour, tview.Escape(c.headline), gradientText(c.visual, colour, p.accent), p.muted, tview.Escape(c.note)))
	}
	h.table.SetSelectionChangedFunc(nil)
	h.table.Clear()
	columns := len(headers)
	if h.width > 0 && h.width < 100 {
		columns = min(columns, 4)
	}
	if h.width > 0 && h.width < 55 {
		columns = min(columns, 2)
	}
	for col, label := range headers[:columns] {
		h.table.SetCell(0, col, tview.NewTableCell(label).SetSelectable(false).SetTextColor(tcell.GetColor(p.accent)).SetAttributes(tcell.AttrBold))
	}
	selected := 1
	for i, row := range h.rows {
		for col, value := range row.cells[:columns] {
			if col == 0 {
				value = " " + value
			}
			cell := tview.NewTableCell(tview.Escape(clean(value))).SetTextColor(tcell.GetColor(p.text)).SetMaxWidth(16)
			if col == 0 {
				cell.SetExpansion(1).SetMaxWidth(max(12, h.width/2))
			}
			if row.warning {
				cell.SetTextColor(tcell.GetColor(p.warning))
			}
			if colour, ok := row.tint[col]; ok {
				cell.SetTextColor(tcell.GetColor(colour))
			}
			h.table.SetCell(i+1, col, cell)
		}
		if row.key == h.selected {
			selected = i + 1
		}
	}
	h.table.Select(selected, 0)
	h.table.SetSelectionChangedFunc(func(row, col int) { h.selectRow(row) })
	h.selectRow(selected)
	h.updateStatus()
}

func (h *hostPage) selectRow(row int) {
	previous := h.selected
	y, x := h.detail.GetScrollOffset()
	if row < 1 || row > len(h.rows) {
		h.selected = ""
		h.detail.SetText("No matching rows. Clear the filter or press r to refresh.")
		return
	}
	r := h.rows[row-1]
	h.selected = r.key
	if r.rich != "" {
		h.detail.SetText(r.rich)
	} else {
		h.detail.SetText(tview.Escape(r.detail))
	}
	if previous == h.selected {
		h.detail.ScrollTo(y, x)
	} else {
		h.detail.ScrollToBeginning()
	}
}

func (h *hostPage) current() *hostRow {
	for i := range h.rows {
		if h.rows[i].key == h.selected {
			return &h.rows[i]
		}
	}
	return nil
}

// Enter on a process opens live activity; storage rows keep the full record.
func (h *hostPage) inspect() {
	if h.tab == 3 {
		h.openProcessActivity()
		return
	}
	h.inspectRecord()
}

func (h *hostPage) inspectRecord() {
	r := h.current()
	if r == nil {
		return
	}
	h.w.hostText("Details", r.detail)
}

func (w *workspace) hostText(title, body string) {
	focus := w.app.GetFocus()
	p := w.palette()
	v := textView().SetWrap(true).SetScrollable(true).SetText(clean(body)).SetTextColor(tcell.GetColor(p.text))
	v.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetTitle(" " + title + " · Esc returns ").SetTitleColor(tcell.GetColor(p.accent))
	v.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.pages.RemovePage("host-detail")
			w.app.SetFocus(focus)
			return nil
		}
		return e
	})
	w.pages.AddPage("host-detail", centered(v, 120, 30), true, true)
}

func (h *hostPage) ports() {
	r := h.current()
	if r == nil {
		return
	}
	pid := r.pid
	h.close()
	h.w.networkPage()
	_, page := h.w.pages.GetFrontPage()
	n := page.(*networkPage)
	n.switchView(1)
	n.search.SetText("pid:" + strconv.Itoa(pid))
}

func (h *hostPage) service() {
	r := h.current()
	if r == nil {
		return
	}
	pid := r.pid
	unit := ""
	for _, item := range h.w.items[0] {
		if item.PID == pid && pid > 0 {
			unit = item.ID
			break
		}
	}
	if unit == "" && !usesLaunchd() {
		if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid)); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				parts := strings.Split(line, "/")
				part := parts[len(parts)-1]
				if strings.HasSuffix(part, ".service") {
					unit = part
				}
			}
		}
	}
	if unit == "" {
		h.w.message("Service unavailable", "No service association was found in the current scope. This process may be standalone, inaccessible, or already exited.")
		return
	}
	found := false
	for _, item := range h.w.items[0] {
		if item.ID == unit {
			found = true
			break
		}
	}
	if !found {
		h.w.message("Service outside current inventory", "Observed service: "+unit+"\n\nSwitch Services to the appropriate system/user scope and refresh to inspect this service and its logs.")
		return
	}
	h.close()
	h.w.switchMode(0)
	h.w.quickFilter = 0
	h.w.favoriteOnly = false
	h.w.tab = 0
	h.w.search.SetText(unit)
	h.w.selected[0] = unit
	h.w.renderTable()
	h.w.showDetail()
}

type processSignal struct {
	name, description string
	signal            syscall.Signal
}

var processSignals = []processSignal{
	{"Terminate · SIGTERM", "Ask the process to shut down cleanly", syscall.SIGTERM},
	{"Interrupt · SIGINT", "Send the same interrupt as Ctrl-C", syscall.SIGINT},
	{"Hang up · SIGHUP", "Request reload/reopen where the process supports it", syscall.SIGHUP},
	{"Suspend · SIGSTOP", "Pause execution until SIGCONT is sent", syscall.SIGSTOP},
	{"Resume · SIGCONT", "Continue a suspended process", syscall.SIGCONT},
	{"Force kill · SIGKILL", "Stop immediately; cleanup handlers cannot run", syscall.SIGKILL},
}

func processTargetError(p hostProcess) error {
	switch {
	case p.PID <= 1:
		return fmt.Errorf("PID %d is protected", p.PID)
	case p.PID == os.Getpid():
		return fmt.Errorf("Systemdoc cannot signal itself")
	case strings.TrimSpace(p.Command) == "":
		return fmt.Errorf("the selected process has no command identity")
	}
	return nil
}

func (h *hostPage) selectedProcess() (hostProcess, error) {
	r := h.current()
	if r == nil {
		return hostProcess{}, fmt.Errorf("select a process first")
	}
	for _, process := range h.processes {
		if process.PID == r.pid {
			return process, processTargetError(process)
		}
	}
	return hostProcess{}, fmt.Errorf("PID %d is no longer in the current snapshot", r.pid)
}

func (h *hostPage) processActions() {
	process, err := h.selectedProcess()
	if err != nil {
		h.w.message("Process action unavailable", err.Error())
		return
	}
	choices := make([]choice, 0, len(processSignals))
	for _, option := range processSignals {
		option := option
		choices = append(choices, choice{option.name, option.description, func() {
			detail := fmt.Sprintf("PID %d · %s\nUser: %s · state: %s\n\nCommand observed in the current snapshot:\n%s\n\nSystemdoc will verify that this PID still has the same command immediately before sending %s. If the PID disappeared or was reused, the action is refused.", process.PID, option.name, process.User, process.State, process.Command, option.name)
			h.w.confirm(option.name+" · PID "+strconv.Itoa(process.PID), detail, func() { h.sendProcessSignal(process, option) })
		}})
	}
	h.w.choose("process-actions", fmt.Sprintf("Process actions · PID %d · %s", process.PID, process.User), choices)
}

func verifyAndSignalProcess(ctx context.Context, process hostProcess, signal syscall.Signal) error {
	if err := processTargetError(process); err != nil {
		return err
	}
	raw, diagnostic, err := networkCommand(ctx, "ps", "-ww", "-p", strconv.Itoa(process.PID), "-o", "args=")
	if err != nil {
		return fmt.Errorf("could not revalidate PID %d: %w %s", process.PID, err, diagnostic)
	}
	observed := strings.TrimSpace(clean(raw))
	if observed == "" {
		return fmt.Errorf("PID %d exited before the signal was sent", process.PID)
	}
	if observed != strings.TrimSpace(clean(process.Command)) {
		return fmt.Errorf("PID %d changed identity; expected %q, now %q", process.PID, process.Command, observed)
	}
	target, err := os.FindProcess(process.PID)
	if err != nil {
		return fmt.Errorf("find PID %d: %w", process.PID, err)
	}
	if err := target.Signal(signal); err != nil {
		return fmt.Errorf("signal PID %d: %w", process.PID, err)
	}
	return nil
}

func (h *hostPage) sendProcessSignal(process hostProcess, option processSignal) {
	if h.signalling {
		h.w.message("Process action running", "Wait for the current signal attempt to finish.")
		return
	}
	h.signalling = true
	op := &operation{Target: fmt.Sprintf("PID %d", process.PID), Command: option.name + " · " + process.Command, Status: "validating process identity", Started: time.Now()}
	h.w.operations = append(h.w.operations, op)
	if len(h.w.operations) > 50 {
		h.w.operations = h.w.operations[len(h.w.operations)-50:]
	}
	h.updateStatus()
	go func() {
		err := verifyAndSignalProcess(h.ctx, process, option.signal)
		h.w.queue(func() {
			h.signalling = false
			if err != nil {
				op.Status = "refused or failed"
				op.Output = err.Error()
				h.w.message("Process signal not sent", err.Error())
			} else {
				op.Status = "signal sent · awaiting next observation"
				op.Output = fmt.Sprintf("%s sent to PID %d after command identity validation.", option.name, process.PID)
				h.status.SetText(" Signal sent to PID " + strconv.Itoa(process.PID) + " · refreshing process snapshot")
			}
			h.refresh()
		})
	}()
}

func (h *hostPage) updateStatus() {
	v := h.view()
	state := "waiting for first sample"
	if !h.at[v].IsZero() {
		state = "sampled " + h.at[v].Format("15:04:05")
	}
	if h.busy {
		state += " · refreshing"
	}
	if h.paused {
		state += " · PAUSED"
	}
	if h.signalling {
		state += " · VALIDATING SIGNAL TARGET"
	}
	note := "Host only · permissions may hide details"
	if h.tab == 3 {
		note += " · ps CPU is an OS estimate; RSS may include shared pages"
	} else if h.deletedView {
		note += " · file sizes are logical, not reclaimable disk blocks"
	} else {
		note += " · shared pools and duplicate mounts are not additive"
	}
	if h.note[v] != "" {
		note += " · " + h.note[v]
	}
	if h.failure[v] != "" {
		note = "Refresh failed; previous sample retained: " + h.failure[v]
	}
	colour := h.w.palette().muted
	if h.failure[v] != "" {
		colour = h.w.palette().error
	}
	h.status.SetText(fmt.Sprintf(" %d rows · %s · poll %ds\n [%s]%s[-]", len(h.rows), state, h.pollSeconds(), colour, tview.Escape(clean(strings.ReplaceAll(note, "\n", " · ")))))
}

func (h *hostPage) report() string {
	host, _ := os.Hostname()
	var out strings.Builder
	sampled := "not yet sampled"
	if !h.at[h.view()].IsZero() {
		sampled = h.at[h.view()].Format(time.RFC3339)
	}
	fmt.Fprintf(&out, "SYSTEMDOC %s SNAPSHOT\nHost: %s\nSampled: %s\nView: %s\nFilter: %s\n%s\n\n", strings.ToUpper(h.name), host, sampled, h.table.GetTitle(), h.query[h.view()], h.status.GetText(true))
	for _, r := range h.rows {
		out.WriteString(r.detail + "\n\n")
	}
	return clean(out.String())
}

func (h *hostPage) processRows() ([]string, [3]hostCard) {
	h.rows = nil
	// The loop below binds p to a process, so the palette keeps its own name.
	pal, glyphs, hue := h.w.palette(), h.w.settings.GraphMode, panelHue(h.w.palette(), h.tab)
	memoryCeiling := int64(0)
	for _, process := range h.processes {
		memoryCeiling = max(memoryCeiling, process.RSS)
	}
	filtered := []hostProcess{}
	running, zombies := 0, 0
	cpu := 0.0
	rss := int64(0)
	for _, p := range h.processes {
		if strings.HasPrefix(p.State, "R") {
			running++
		}
		if strings.HasPrefix(p.State, "Z") {
			zombies++
		}
		cpu += max(0, p.CPU)
		rss += max(int64(0), p.RSS)
		if matchesProcess(p, h.query[0]) {
			filtered = append(filtered, p)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if h.sort == 0 && a.CPU != b.CPU {
			return a.CPU > b.CPU
		}
		if h.sort == 1 && a.RSS != b.RSS {
			return a.RSS > b.RSS
		}
		return a.PID < b.PID
	})
	branches := make([]processBranch, len(filtered))
	for i, p := range filtered {
		branches[i] = processBranch{Process: p}
	}
	if h.tree {
		branches = processTree(filtered)
	}
	parents := map[int]string{}
	children := map[int][]string{}
	for _, p := range h.processes {
		parents[p.PID] = p.Command
		children[p.PPID] = append(children[p.PPID], strconv.Itoa(p.PID))
	}
	for _, b := range branches {
		p := b.Process
		command := p.Command
		if h.tree && b.Depth > 0 {
			command = strings.Repeat("  ", min(b.Depth, 8)) + "└ " + command
		}
		cpuText := "—"
		if p.CPU >= 0 {
			cpuText = fmt.Sprintf("%.1f%%", p.CPU)
		}
		detail := fmt.Sprintf("PID %d · %s · %s\nCPU %s · RSS %s · elapsed %s\nParent %d · %s\nChildren: %s\nCommand: %s\n\nEnter live activity (threads, switches, I/O, files, journal) · T sysdig runtime tracing · K process actions · n ports · s service (current scope). Signals require review and PID identity validation.", p.PID, p.User, p.State, cpuText, hostBytes(p.RSS, true), p.Elapsed, p.PPID, available(parents[p.PPID]), available(strings.Join(children[p.PID], ", ")), p.Command)
		cpuCell := cpuText
		if h.width >= 100 && p.CPU >= 0 {
			cpuCell = cellGauge(cpuText, p.CPU, 100, 6, glyphs)
		}
		// A zombie is a state to notice, not a level of resource pressure.
		stateHue := pal.success
		stateText := "running"
		if !strings.HasPrefix(p.State, "R") {
			stateHue, stateText = pal.muted, "sleeping"
		}
		if strings.HasPrefix(p.State, "Z") {
			stateHue, stateText = pal.error, "zombie · parent has not reaped it"
		}
		cpuGauge := fmt.Sprintf("[%s]CPU     no reading[-]", pal.muted)
		if p.CPU >= 0 {
			cpuGauge = gaugeLine("CPU", p.CPU, 100, 20, hueOf(pal, pressureHue(pal, int(p.CPU))), pal, glyphs, cpuText, "100% = one logical CPU")
		}
		memoryGauge := fmt.Sprintf("[%s]MEMORY  no reading[-]", pal.muted)
		if p.RSS >= 0 && memoryCeiling > 0 {
			memoryGauge = gaugeLine("MEMORY", float64(p.RSS), float64(memoryCeiling), 20, hueOf(pal, hue), pal, glyphs, hostBytes(p.RSS, true), "RSS · largest observed process sets the scale")
		}
		rich := fmt.Sprintf("[%s::b]%s[-::-]\n%s\n%s\n[%s]PID[-] [%s::b]%d[-::-]  [%s]%s[-]  [%s::b] %s [-::-]  [%s]elapsed %s · parent %d %s[-]",
			hue, tview.Escape(clean(p.Command)), cpuGauge, memoryGauge,
			pal.muted, hue, p.PID, pal.muted, tview.Escape(clean(p.User)), stateHue, tview.Escape(clean(stateText)),
			pal.muted, tview.Escape(clean(p.Elapsed)), p.PPID, tview.Escape(clean(available(parents[p.PPID]))))
		h.rows = append(h.rows, hostRow{key: strconv.Itoa(p.PID), cells: []string{command, cpuCell, hostBytes(p.RSS, true), strconv.Itoa(p.PID), p.User, p.State, strconv.Itoa(p.PPID), p.Elapsed}, detail: detail, rich: rich, tint: map[int]string{1: pressureHue(pal, int(max(0, p.CPU)))}, pid: p.PID, warning: strings.HasPrefix(p.State, "Z")})
	}
	mode := []string{"CPU", "MEMORY", "PID"}[h.sort]
	if h.tree {
		mode = "PARENT TREE"
	}
	h.table.SetTitle(" PROCESSES · " + mode + " · F9/K signal · S sort ")
	h.footer.SetText(" Enter activity · F9/K signals · T sysdig · / filter · S sort · f flat · t tree · n ports · s service · G graphics · r refresh · P pause · e export · Esc back")
	// The two resource cards plot the machine's own recent utilisation, which
	// is a real series; the summed ps figures beside them are a snapshot and
	// are named as such so the two are never read as the same measurement.
	chartWidth := 20
	if h.width > 0 {
		chartWidth = max(8, h.width/3-8)
	}
	usage := h.w.hostUsage
	cpuTrend := signalArea(trimHistory(h.w.hostCPUHistory, chartWidth), 100, glyphs)
	memoryTrend := signalArea(trimHistory(h.w.hostMemoryHistory, chartWidth), 100, glyphs)
	cpuHeadline, memoryHeadline := "— host", "— host"
	if usage.cpuOK {
		cpuHeadline = fmt.Sprintf("%.0f%% host", usage.cpuPercent)
	}
	if usage.memOK {
		memoryHeadline = fmt.Sprintf("%.0f%% host · %s", usage.memPercent, hostBytesPair(usage.memUsed, usage.memTotal))
	}
	return []string{"COMMAND", "CPU", "RSS", "PID", "USER", "STATE", "PPID", "ELAPSED"}, [3]hostCard{
		{title: "PROCESSES", headline: fmt.Sprintf("%d observed", len(h.processes)), visual: signalMeter(float64(running), float64(len(h.processes)), 20, glyphs), note: fmt.Sprintf("%d running · %d zombies", running, zombies)},
		{title: "HOST CPU", headline: fmt.Sprintf("%s · %.1f%% summed", cpuHeadline, cpu), visual: cpuTrend, chart: true},
		{title: "HOST MEMORY", headline: fmt.Sprintf("%s · %s RSS", memoryHeadline, hostBytes(rss, true)), visual: memoryTrend, chart: true},
	}
}

func (h *hostPage) mountRows() ([]string, [3]hostCard) {
	h.rows = nil
	p, mode, hue := h.w.palette(), h.w.settings.GraphMode, panelHue(h.w.palette(), h.tab)
	mounts := append([]hostMount(nil), h.mounts...)
	sort.SliceStable(mounts, func(i, j int) bool {
		a, b := mounts[i], mounts[j]
		if h.sort == 0 && a.Percent != b.Percent {
			return a.Percent > b.Percent
		}
		if h.sort == 1 && a.Free != b.Free {
			return a.Free < b.Free
		}
		return a.Path < b.Path
	})
	pressure, inodePressure, peak := 0, 0, 0
	for _, m := range mounts {
		if m.Percent >= 85 {
			pressure++
		}
		if m.InodePercent >= 85 {
			inodePressure++
		}
		peak = max(peak, m.Percent)
		if !matchesChoice(m.Path+" "+m.Source, h.query[0]) {
			continue
		}
		inode := "—"
		if m.InodePercent >= 0 {
			inode = fmt.Sprintf("%d%%", m.InodePercent)
		}
		free := hostBytes(m.Free, true)
		if m.Free < 0 {
			free = "-" + hostBytes(-m.Free, true)
		}
		detail := fmt.Sprintf("Mount: %s\nSource: %s\nCapacity %s · used %s · available %s · %d%% used\nInodes: %s used · %s used / %s total · %s free\n\nShared pools (APFS/Btrfs), bind mounts and reserved blocks affect accounting.\nPress d to inspect deleted files still held open.", m.Path, m.Source, hostBytes(m.Size, true), hostBytes(m.Used, true), free, m.Percent, inode, hostCount(m.InodeUsed), hostCount(m.Inodes), hostCount(m.InodeFree))
		spaceHue := pressureHue(p, m.Percent)
		usedCell := fmt.Sprintf("%d%%", m.Percent)
		if h.width >= 100 {
			usedCell = cellGauge(usedCell, float64(m.Percent), 100, 8, mode)
		}
		inodeGauge := fmt.Sprintf("[%s]INODES  %s[-]", p.muted, "unavailable on this filesystem")
		if m.InodePercent >= 0 {
			inodeGauge = gaugeLine("INODES", float64(m.InodePercent), 100, 26, hueOf(p, pressureHue(p, m.InodePercent)), p, mode, fmt.Sprintf("%d%%", m.InodePercent),
				fmt.Sprintf("%s used / %s total · %s free", hostCount(m.InodeUsed), hostCount(m.Inodes), hostCount(m.InodeFree)))
		}
		rich := fmt.Sprintf("[%s::b]%s[-::-]  [%s]%s[-]\n%s\n%s\n[%s]Shared pools, bind mounts and reserved blocks affect accounting.[-]",
			hue, tview.Escape(clean(m.Path)), p.muted, tview.Escape(clean(m.Source)),
			gaugeLine("SPACE", float64(m.Percent), 100, 26, hueOf(p, spaceHue), p, mode, fmt.Sprintf("%d%%", m.Percent),
				fmt.Sprintf("%s used · %s free · %s total", hostBytes(m.Used, true), tview.Escape(free), hostBytes(m.Size, true))),
			inodeGauge, p.muted)
		h.rows = append(h.rows, hostRow{key: m.key(), cells: []string{m.Path, usedCell, free, hostBytes(m.Size, true), hostBytes(m.Used, true), inode, m.Source}, detail: detail, rich: rich, tint: map[int]string{1: spaceHue}, warning: m.Percent >= 85 || m.InodePercent >= 85})
	}
	h.table.SetTitle(" FILESYSTEMS · " + []string{"USAGE", "LEAST FREE", "MOUNT"}[h.sort] + " · S sort ")
	h.footer.SetText(" / filter mounts · S sort · m filesystems · d deleted files · G graphics · r refresh · P pause · e export · Esc back")
	return []string{"MOUNT", "USED", "AVAILABLE", "SIZE", "USED SPACE", "INODES", "SOURCE"}, [3]hostCard{
		{title: "MOUNTS", headline: fmt.Sprintf("%d filesystems", len(mounts)), visual: "Host mount inventory", note: "Shared capacity is not summed"},
		{title: "SPACE PRESSURE", headline: fmt.Sprintf("%d at 85%% or more", pressure), visual: signalMeter(float64(peak), 100, 20, mode), note: fmt.Sprintf("Highest observed usage: %d%%", peak)},
		{title: "INODE PRESSURE", headline: fmt.Sprintf("%d at 85%% or more", inodePressure), visual: "File count / metadata", note: "Unavailable counts shown as —"},
	}
}

func (h *hostPage) deletedRows() ([]string, [3]hostCard) {
	h.rows = nil
	files := append([]deletedFile(nil), h.deleted...)
	sort.SliceStable(files, func(i, j int) bool {
		if h.sort == 0 && files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		if h.sort == 1 && files[i].PID != files[j].PID {
			return files[i].PID < files[j].PID
		}
		return files[i].Path < files[j].Path
	})
	unique := map[string]bool{}
	owners := map[int]bool{}
	size := int64(0)
	for _, f := range files {
		key := f.Device + "/" + f.Inode
		if f.Device == "" || f.Inode == "" {
			key = f.key()
		}
		if !unique[key] {
			unique[key] = true
			size += max(int64(0), f.Size)
		}
		owners[f.PID] = true
		if !matchesChoice(fmt.Sprintf("%s %s %s %d", f.Path, f.Process, f.User, f.PID), h.query[1]) {
			continue
		}
		detail := fmt.Sprintf("Deleted file: %s\nOwner: %s · PID %d · UID %s · FD %s\nLogical size: %s\nDevice: %s · inode: %s\n\nThe process still holds this file open. Multiple handles can refer to one file.\nLogical size is not the amount of disk space that would be reclaimed.", f.Path, f.Process, f.PID, f.User, f.FD, hostBytes(f.Size, false), f.Device, f.Inode)
		h.rows = append(h.rows, hostRow{key: f.key(), cells: []string{f.Path, hostBytes(f.Size, false), f.Process, strconv.Itoa(f.PID), f.User, f.FD}, detail: detail, pid: f.PID})
	}
	h.table.SetTitle(" DELETED BUT OPEN · " + []string{"SIZE", "PID", "PATH"}[h.sort] + " · S sort ")
	h.footer.SetText(" / filter files · S sort · d filesystems · G graphics · r refresh · P pause · e export · Esc back")
	return []string{"PATH", "SIZE", "PROCESS", "PID", "UID", "FD"}, [3]hostCard{
		{title: "DELETED FILES", headline: fmt.Sprintf("%d observed files", len(unique)), visual: fmt.Sprintf("%d open handles", len(files)), note: "Permissions limit visibility"},
		{title: "KNOWN LOGICAL SIZE", headline: hostBytes(size, false), visual: "Deduplicated by device/inode", note: "Not reclaimable disk blocks"},
		{title: "OWNERS", headline: fmt.Sprintf("%d processes", len(owners)), visual: "Still holding files open", note: "Sampled by lsof +L1"},
	}
}
