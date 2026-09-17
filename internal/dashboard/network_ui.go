package dashboard

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type networkPage struct {
	lastWidth, lastHeight int
	deckButton            *tview.Button
	*tview.Flex
	w                      *workspace
	ctx                    context.Context
	cancel                 context.CancelFunc
	table                  *tview.Table
	search                 *tview.InputField
	header, detail, status *tview.TextView
	suiteNavigation        *tview.Flex
	summary                *tview.Flex
	cards                  [3]*tview.TextView
	buttons                [4]*tview.Button
	modeButtons            [5]*tview.Button
	snapshot               networkSnapshot
	visible                []networkSocket
	interfaces             []networkInterface
	rates                  []networkRate
	visibleRates           []networkRate
	view                   int
	filters                [4]string
	selected               string
	busy, paused           bool
	lastPoll               time.Time
	rateInterval           time.Duration
	failure                [4]string
	rateHistory            map[string][]float64
	downloadHistory        []float64
	uploadHistory          []float64
}

func (n *networkPage) Draw(screen tcell.Screen) {
	_, _, width, height := n.GetRect()
	if width != n.lastWidth || mastheadHeight(n.lastWidth, n.lastHeight) != mastheadHeight(width, height) {
		n.lastWidth, n.lastHeight = width, height
		setSuiteLabels(n.modeButtons[:], width, n.w.settings.NerdIcons)
		for i, button := range n.modeButtons {
			buttonWidth := suiteButtonWidths[i]
			if width < 100 {
				buttonWidth = max(6, width/5)
			}
			n.suiteNavigation.ResizeItem(button, buttonWidth, 0)
		}
		deckWidth := controlButtonWidth(n.deckButton.GetLabel())
		if width < 140 {
			deckWidth = 0
		}
		n.suiteNavigation.ResizeItem(n.deckButton, deckWidth, 0)
		n.render()
	}
	n.lastHeight = height
	detailHeight := 8
	summaryHeight := 0
	chromeHeight := mastheadHeight(width, height)
	if height >= 28 {
		summaryHeight = 5
	}
	if height < 22 {
		detailHeight = 0
		summaryHeight = 0
	}
	n.ResizeItem(n.summary, summaryHeight, 0)
	n.ResizeItem(n.detail, detailHeight, 0)
	n.ResizeItem(n.header, chromeHeight, 0)
	n.header.SetBorderPadding(0, 0, 1, 0)
	n.Flex.Draw(screen)
	p := n.w.palette()
	rails := map[*tview.Box]string{}
	for i, card := range n.cards {
		rails[card.Box] = []string{p.success, p.accent, p.warning}[i]
	}
	paintSurfaces(screen, n.Flex, p, n.header, n.table, rails, n.w.limitedColours)
}

func (w *workspace) networkPage() {
	if _, page := w.pages.GetFrontPage(); page != nil {
		if n, ok := page.(*networkPage); ok {
			w.app.SetFocus(n.table)
			return
		}
	}
	ctx, cancel := context.WithCancel(w.ctx)
	n := &networkPage{Flex: tview.NewFlex().SetDirection(tview.FlexRow), w: w, ctx: ctx, cancel: cancel, rateHistory: map[string][]float64{}}
	p := w.palette()
	n.header = textView().SetDynamicColors(true).SetTextColor(tcell.GetColor(p.accent))
	n.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	n.table.SetBorder(true).SetTitle(" TCP / UDP sockets ").SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	n.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.GetColor(p.accent)).Foreground(tcell.GetColor(p.background)).Bold(true))
	n.search = tview.NewInputField().SetLabel(" / " + w.iconLabel(iconSearch, "Filter "))
	n.search.SetFieldBackgroundColor(tcell.GetColor(p.surface)).SetFieldTextColor(tcell.GetColor(p.text)).SetLabelColor(tcell.GetColor(p.accent))
	n.search.SetChangedFunc(func(query string) { n.filters[n.view] = query; n.render() })
	n.search.SetDoneFunc(func(tcell.Key) { w.app.SetFocus(n.table) })
	n.detail = textView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	n.detail.SetBorder(true).SetTitle(" Selected socket · Enter process details ").SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	n.status = textView().SetDynamicColors(true).SetWrap(true).SetTextColor(tcell.GetColor(p.muted))
	n.summary = tview.NewFlex()
	for i, title := range []string{"LISTENING", "CONNECTIONS", "HOST SURFACE"} {
		card := textView().SetDynamicColors(true).SetWrap(false)
		card.SetBorder(true).SetTitle(" " + title + " ").SetBorderAttributes(tcell.AttrBold)
		card.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorderColor(tcell.GetColor(p.muted))
		n.cards[i] = card
		n.summary.AddItem(card, 0, 1, false)
	}
	navigation := tview.NewFlex()
	for i, label := range []string{"l " + w.icon(iconNetwork) + " Ports", "c " + w.icon(iconConstellation) + " Connections", "i " + w.icon(iconServices) + " Interfaces", "s " + w.icon(iconResources) + " Speed"} {
		i := i
		button := tview.NewButton(label).SetSelectedFunc(func() { n.switchView(i) })
		n.buttons[i] = button
		navigation.AddItem(button, 0, 1, false)
	}
	footer := textView().SetTextColor(tcell.GetColor(p.muted)).SetText(" / filter · l ports · c connections · i interfaces · s speed · G graphics · r refresh · P pause · e export · Esc back")
	for _, v := range []*tview.TextView{n.header, n.detail, n.status, footer} {
		v.SetBackgroundColor(tcell.GetColor(p.background))
	}
	n.table.SetBackgroundColor(tcell.GetColor(p.background))
	n.summary.SetBackgroundColor(tcell.GetColor(p.background))
	n.SetBackgroundColor(tcell.GetColor(p.background))
	w.illuminate(n.table.Box, n.table.HasFocus, func(p palette) string { return p.accent })
	w.illuminate(n.detail.Box, n.detail.HasFocus, func(p palette) string { return p.accent })
	for i, card := range n.cards {
		w.illuminatePanel(card.Box, nil, func(p palette) string { return []string{p.success, p.accent, p.warning}[i] }, true)
	}
	commandBar, modes, deck := w.hostNavigation(2, func(target int) {
		if target == 2 {
			w.app.SetFocus(n.table)
		} else {
			n.close()
			w.openHostTab(target)
		}
	})
	copy(n.modeButtons[:], modes)
	n.deckButton = deck
	n.suiteNavigation = commandBar
	n.AddItem(n.header, 1, 0, false).AddItem(commandBar, 1, 0, false).AddItem(n.summary, 5, 0, false).AddItem(navigation, 1, 0, false).AddItem(n.search, 1, 0, false).AddItem(n.table, 0, 1, true).AddItem(n.detail, 8, 0, false).AddItem(n.status, 2, 0, false).AddItem(footer, 1, 0, false)
	n.table.SetSelectionChangedFunc(func(row, col int) { n.updateDetail(row) })
	n.table.SetSelectedFunc(func(row, col int) {
		if n.view >= 2 {
			count := len(n.interfaces)
			if n.view == 3 {
				count = len(n.visibleRates)
			}
			if row > 0 && row <= count {
				w.message("Interface details", n.detail.GetText(true))
			}
			return
		}
		if row > 0 && row <= len(n.visible) {
			n.processDetails(n.visible[row-1])
		}
	})
	n.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			if n.search.HasFocus() || n.detail.HasFocus() {
				w.app.SetFocus(n.table)
			} else {
				n.close()
			}
			return nil
		}
		if e.Key() == tcell.KeyTab {
			if n.search.HasFocus() {
				w.app.SetFocus(n.table)
			} else if n.table.HasFocus() {
				_, _, _, height := n.GetRect()
				if height < 22 {
					w.app.SetFocus(n.search)
				} else {
					w.app.SetFocus(n.detail)
				}
			} else {
				w.app.SetFocus(n.search)
			}
			return nil
		}
		if n.search.HasFocus() {
			return e
		}
		switch e.Rune() {
		case '0':
			w.controlDeck()
		case ',':
			w.pollingDialog()
		case '/':
			w.app.SetFocus(n.search)
		case 'l':
			n.switchView(0)
		case 'c':
			n.switchView(1)
		case 'i':
			n.switchView(2)
		case 's':
			n.switchView(3)
		case 'G':
			w.cycleGraphMode()
			n.render()
		case 'r':
			n.refresh()
		case 'P':
			n.paused = !n.paused
			n.updateStatus()
		case 'e':
			w.reviewSnapshot(n.report())
		case '1', '2', '4', '5':
			n.close()
			w.openHostTab(int(e.Rune() - '1'))
		case 'p':
			row, _ := n.table.GetSelection()
			if n.view < 2 && row > 0 && row <= len(n.visible) && n.visible[row-1].PID > 0 {
				pid := n.visible[row-1].PID
				n.close()
				w.hostPage(3)
				_, page := w.pages.GetFrontPage()
				page.(*hostPage).search.SetText("pid:" + strconv.Itoa(pid))
			}
		case 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, 0)
		case 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, 0)
		default:
			return e
		}
		return nil
	})
	w.pages.AddPage("network", n, true, true)
	n.render()
	n.refresh()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// As on the host pages: the tick decides, refresh() draws.
				w.queueQuiet(func() {
					if ctx.Err() != nil {
						return
					}
					front, _ := w.pages.GetFrontPage()
					if front == "network" && !n.paused && pollDue(n.lastPoll, time.Now(), w.settings.RefreshSeconds) {
						n.refresh()
					}
				})
			}
		}
	}()
}
func (n *networkPage) close() {
	n.cancel()
	n.w.pages.RemovePage("network")
	n.w.app.SetFocus(n.w.table)
}
func (n *networkPage) switchView(view int) {
	n.view = view
	n.selected = ""
	n.search.SetChangedFunc(nil).SetText(n.filters[view]).SetChangedFunc(func(query string) { n.filters[n.view] = query; n.render() })
	n.render()
	n.w.app.SetFocus(n.table)
	n.refresh()
}
func (n *networkPage) refresh() {
	if n.busy || n.ctx.Err() != nil {
		return
	}
	n.busy = true
	n.lastPoll = time.Now()
	n.updateStatus()
	platform, view := servicePlatform, n.view
	go func() {
		snapshot, err := collectNetworkView(n.ctx, platform, view)
		if n.ctx.Err() != nil {
			return
		}
		n.w.queue(func() {
			if n.ctx.Err() != nil {
				return
			}
			n.busy = false
			if err != nil {
				n.failure[view] = err.Error()
				if view < 2 && len(snapshot.Interfaces) > 0 {
					n.snapshot.Interfaces = snapshot.Interfaces
					n.snapshot.InterfaceAt = snapshot.InterfaceAt
				}
			} else {
				n.failure[view] = ""
				switch view {
				case 3:
					n.rateInterval = snapshot.CounterAt.Sub(n.snapshot.CounterAt)
					n.rates = networkRates(n.snapshot.Counters, n.snapshot.CounterAt, snapshot.Counters, snapshot.CounterAt)
					n.recordRateHistory()
					n.snapshot.Counters = snapshot.Counters
					n.snapshot.CounterAt = snapshot.CounterAt
					n.snapshot.CounterSource = snapshot.CounterSource
				case 2:
					n.snapshot.Interfaces = snapshot.Interfaces
					n.snapshot.InterfaceAt = snapshot.InterfaceAt
				default:
					n.snapshot.Sockets = snapshot.Sockets
					n.snapshot.Source = snapshot.Source
					n.snapshot.Note = snapshot.Note
					n.snapshot.At = snapshot.At
					if len(snapshot.Interfaces) > 0 {
						n.snapshot.Interfaces = snapshot.Interfaces
						n.snapshot.InterfaceAt = snapshot.InterfaceAt
					}
				}
			}
			n.render()
			if n.view != view {
				n.refresh()
			}
		})
	}()
}

func appendSignal(history []float64, value float64) []float64 {
	history = append(history, value)
	if len(history) > 60 {
		history = history[len(history)-60:]
	}
	return history
}

func (n *networkPage) recordRateHistory() {
	download, upload := 0.0, 0.0
	// Docker's veth names come and go with every container, so a trail whose
	// interface has vanished is dropped rather than kept for the session.
	present := make(map[string]bool, len(n.rates))
	for _, rate := range n.rates {
		present[rate.Name] = true
	}
	for name := range n.rateHistory {
		if !present[name] {
			delete(n.rateHistory, name)
		}
	}
	for _, rate := range n.rates {
		if !rate.Ready {
			continue
		}
		download += rate.Receive
		upload += rate.Transmit
		n.rateHistory[rate.Name] = appendSignal(n.rateHistory[rate.Name], rate.Receive+rate.Transmit)
	}
	if download+upload > 0 {
		n.downloadHistory = appendSignal(n.downloadHistory, download)
		n.uploadHistory = appendSignal(n.uploadHistory, upload)
	}
}
func (n *networkPage) render() {
	p := n.w.palette()
	n.header.SetText(n.w.masthead("Host network", "", hostReadout(p, n.w.hostUsage)+"   "+n.w.signalsHint(p), n.lastWidth, n.lastHeight))
	for i, button := range n.modeButtons {
		fg, bg := p.muted, p.background
		if i == 2 {
			fg, bg = p.background, p.accent
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(i == 2))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	}
	for i, card := range n.cards {
		card.SetBackgroundColor(tcell.GetColor(p.surface))
		card.SetBorderColor(tcell.GetColor([]string{p.success, p.accent, p.warning}[i]))
	}
	for i, button := range n.buttons {
		fg, bg := p.muted, p.background
		if i == n.view {
			fg, bg = p.background, p.accent
		}
		button.SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(i == n.view))
		button.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	}
	n.table.SetSelectionChangedFunc(nil)
	n.table.Clear()
	n.visible = nil
	n.interfaces = nil
	n.visibleRates = nil
	width := n.lastWidth
	if width == 0 {
		width = 120
	}
	headers := []string{"PROTO", "LOCAL ADDRESS", "PROCESS", "PID", "STATE", "USER", "REMOTE ADDRESS"}
	if width < 110 {
		headers = headers[:4]
	}
	if width < 60 {
		headers = headers[1:]
	}
	if n.view == 2 {
		headers = []string{"INTERFACE", "ADDRESSES", "FLAGS", "MTU"}
		if width < 100 {
			headers = []string{"INTERFACE", "ADDRESSES"}
		}
		n.table.SetTitle(" Host interfaces ")
		n.table.SetTitleColor(tcell.GetColor(p.accent))
		n.detail.SetTitle(" Selected interface ")
	} else if n.view == 3 {
		headers = []string{"INTERFACE", "DOWNLOAD", "UPLOAD", "TREND", "TOTAL RX", "TOTAL TX"}
		if width < 100 {
			headers = headers[:3]
		}
		n.table.SetTitle(" LIVE INTERFACE SPEED · rates between polls ")
		n.table.SetTitleColor(tcell.GetColor(p.accent))
		n.detail.SetTitle(" Selected interface speed ")
	} else {
		n.table.SetTitle(" TCP / UDP · port order ")
		n.table.SetTitleColor(tcell.GetColor(p.accent))
		n.detail.SetTitle(" " + n.w.iconLabel(iconNetwork, "SOCKET EXPOSURE · Enter process details") + " ")
	}
	n.detail.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent))
	n.updateSummary()
	for col, title := range headers {
		n.table.SetCell(0, col, tview.NewTableCell(title).SetSelectable(false).SetTextColor(tcell.GetColor(p.accent)).SetAttributes(tcell.AttrBold))
	}
	selectedRow := 1
	add := func(values []string, key string) {
		row := n.table.GetRowCount()
		for col, value := range values {
			if col == 0 {
				value = " " + value // Leave room for the shared selection marker.
			}
			cell := tview.NewTableCell(tview.Escape(clean(value))).SetTextColor(tcell.GetColor(p.text)).SetBackgroundColor(tcell.GetColor(p.background)).SetMaxWidth(12)
			if headers[col] == "LOCAL ADDRESS" || headers[col] == "REMOTE ADDRESS" || headers[col] == "ADDRESSES" {
				cell.SetExpansion(2).SetMaxWidth(max(14, min(34, width/3)))
			}
			if headers[col] == "PROCESS" {
				cell.SetExpansion(1).SetMaxWidth(max(10, min(20, width/4)))
			}
			n.table.SetCell(row, col, cell)
		}
		if key == n.selected {
			selectedRow = row
		}
	}
	if n.view == 2 {
		for _, iface := range n.snapshot.Interfaces {
			if !matchesChoice(iface.Name+" "+iface.Addresses+" "+iface.Flags, n.filters[2]) {
				continue
			}
			n.interfaces = append(n.interfaces, iface)
			values := []string{iface.Name, available(iface.Addresses), iface.Flags, strconv.Itoa(iface.MTU)}
			add(values[:len(headers)], iface.Name)
		}
	} else if n.view == 3 {
		for _, rate := range n.rates {
			if !matchesChoice(rate.Name, n.filters[3]) {
				continue
			}
			n.visibleRates = append(n.visibleRates, rate)
			receive, transmit := "collecting…", "collecting…"
			if rate.Ready {
				receive, transmit = networkBytes(rate.Receive)+"/s", networkBytes(rate.Transmit)+"/s"
			}
			trend := signalChart(n.rateHistory[rate.Name], n.w.settings.GraphMode)
			if trend == "" {
				trend = "baseline"
			}
			values := []string{rate.Name, receive, transmit, trend, networkBytes(float64(rate.ReceiveTotal)), networkBytes(float64(rate.SendTotal))}
			add(values[:len(headers)], rate.Name)
			row := n.table.GetRowCount() - 1
			if cell := n.table.GetCell(row, 1); cell != nil {
				cell.SetTextColor(tcell.GetColor(p.success)).SetAttributes(tcell.AttrBold)
			}
			if cell := n.table.GetCell(row, 2); cell != nil {
				cell.SetTextColor(tcell.GetColor(p.accent)).SetAttributes(tcell.AttrBold)
			}
		}
	} else {
		for _, s := range n.snapshot.Sockets {
			if n.view == 0 && !s.bound() || !matchesNetworkSocket(s, n.filters[n.view]) {
				continue
			}
			n.visible = append(n.visible, s)
			pid := "—"
			if s.PID > 0 {
				pid = strconv.Itoa(s.PID)
			}
			user := s.User
			if user == "" && s.UID != "" {
				user = "uid:" + s.UID
			}
			values := []string{s.Protocol, s.Local, available(s.Process), pid, s.State, available(user), available(s.Remote)}
			if width < 110 {
				values = values[:4]
			}
			if width < 60 {
				values = values[1:]
			}
			add(values, s.key())
			row := n.table.GetRowCount() - 1
			if row > 0 {
				protoCol := 0
				stateCol := 4
				if width < 60 {
					stateCol = 3
				}
				if width >= 60 {
					if cell := n.table.GetCell(row, protoCol); cell != nil {
						if s.Protocol == "UDP" {
							cell.SetTextColor(tcell.GetColor(p.warning))
						} else {
							cell.SetTextColor(tcell.GetColor(p.accent))
						}
					}
					if cell := n.table.GetCell(row, stateCol); cell != nil {
						colour := p.muted
						if s.bound() {
							colour = p.success
						} else if strings.Contains(s.State, "CLOSE") {
							colour = p.warning
						} else if strings.Contains(s.State, "FAIL") {
							colour = p.error
						} else if s.State == "ESTABLISHED" {
							colour = p.accent
						}
						cell.SetTextColor(tcell.GetColor(colour)).SetAttributes(tcell.AttrBold)
					}
				}
			}
		}
	}
	n.table.Select(selectedRow, 0)
	n.table.SetSelectionChangedFunc(func(row, col int) { n.updateDetail(row) })
	n.updateDetail(selectedRow)
	n.updateStatus()
}
func (n *networkPage) updateDetail(row int) {
	oldKey := n.selected
	scroll, col := n.detail.GetScrollOffset()
	if n.view == 2 {
		if row < 1 || row > len(n.interfaces) {
			n.selected = ""
			n.detail.SetText("No matching interfaces.")
			return
		}
		i := n.interfaces[row-1]
		n.selected = i.Name
		n.detail.SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s\n%s\n%s\n%s", n.w.palette().accent, tview.Escape(i.Name), networkField("Addresses", available(i.Addresses), n.w.palette().accent), networkField("Flags", i.Flags, n.w.palette().muted), networkField("MTU", strconv.Itoa(i.MTU), n.w.palette().warning), networkField("Hardware", available(i.Hardware), n.w.palette().muted)))
	} else if n.view == 3 {
		if row < 1 || row > len(n.visibleRates) {
			n.selected = ""
			n.detail.SetText("No interface speed samples. Press r to sample again.\n\nThe first counter read establishes a baseline; rates appear after the next poll.")
			return
		}
		rate := n.visibleRates[row-1]
		n.selected = rate.Name
		receive, transmit := "collecting baseline", "collecting baseline"
		if rate.Ready {
			receive, transmit = networkBytes(rate.Receive)+"/s", networkBytes(rate.Transmit)+"/s"
		}
		p := n.w.palette()
		trend := signalChart(n.rateHistory[rate.Name], n.w.settings.GraphMode)
		n.detail.SetText(fmt.Sprintf("[%s::b]%s[-::-]\n[%s]DOWNLOAD[-] %s    [%s]UPLOAD[-] %s\n[%s]THROUGHPUT[-] %s  ·  %s glyphs\nTotal received %s    Total sent %s\n\nRates are calculated from host interface byte counters sampled %.1fs apart. Loopback and virtual interfaces are included; this is throughput, not negotiated link capacity.", p.accent, tview.Escape(rate.Name), p.success, receive, p.accent, transmit, p.warning, trend, graphMode(n.w.settings.GraphMode), networkBytes(float64(rate.ReceiveTotal)), networkBytes(float64(rate.SendTotal)), n.speedInterval()))
	} else {
		if row < 1 || row > len(n.visible) {
			n.selected = ""
			if n.failure[n.view] != "" {
				n.detail.SetText("Socket lookup failed:\n" + clean(n.failure[n.view]) + "\n\nInterfaces are available separately. Press r to retry.")
				return
			}
			n.detail.SetText("No matching sockets. Try Connections or clear the filter.\nFilters: port:3000  localport:8080  process:node  pid:123  proto:tcp  state:listen\nHost permissions can hide owners or sockets. Docker VM/container namespaces are separate.")
			return
		}
		s := n.visible[row-1]
		n.selected = s.key()
		pid := "unavailable"
		if s.PID > 0 {
			pid = strconv.Itoa(s.PID)
		}
		p := n.w.palette()
		n.detail.SetText(fmt.Sprintf("[%s::b]%s  %s[-::-]  %s\n%s\n%s\n%s\n%s\n%s\n[%s]%s · a bind address is not a firewall rule[-]", p.accent, tview.Escape(s.Protocol), tview.Escape(s.State), tview.Escape(available(s.Process)), exposureChip(p, s.Local, s.State), networkField("Local", s.Local, p.muted)+"    "+networkField("Remote", available(s.Remote), p.muted), networkField("Process", available(s.Process), p.accent)+"    "+networkField("PID", pid, p.muted)+"    "+networkField("User", available(s.User), p.muted)+"    "+networkField("UID", available(s.UID), p.muted), networkField("FD", available(s.FD), p.muted)+"    "+networkField("Inode", available(s.Inode), p.muted)+"    "+networkField("Family", available(s.Family), p.muted), networkField("Cgroup", available(s.Cgroup), p.muted), p.muted, tview.Escape(socketBinding(s.Local))))
	}
	if n.selected == oldKey {
		n.detail.ScrollTo(scroll, col)
	} else {
		n.detail.ScrollToBeginning()
	}
}

func (n *networkPage) updateSummary() {
	p := n.w.palette()
	listeners, connections, udp, owners := 0, 0, 0, 0
	for _, s := range n.snapshot.Sockets {
		if s.bound() {
			listeners++
		} else {
			connections++
		}
		if s.Protocol == "UDP" && s.bound() {
			udp++
		}
		if s.PID > 0 {
			owners++
		}
	}
	interfaces := len(n.snapshot.Interfaces)
	values := []struct {
		title, colour, headline, visual, note string
	}{
		{" LISTENING · bound sockets ", p.success, fmt.Sprintf("%d bound sockets", listeners), fmt.Sprintf("%d TCP  ·  %d UDP", listeners-udp, udp), "TCP listeners / UDP endpoints"},
		{" CONNECTIONS · active flows ", p.accent, fmt.Sprintf("%d active flows", connections), "TCP + UDP", "sampled host connections"},
		{" HOST SURFACE · attribution ", p.warning, fmt.Sprintf("%d owners  ·  %d interfaces", owners, interfaces), "process + host links", "permissions may hide owners"},
	}
	if n.view == 3 {
		download, upload := 0.0, 0.0
		ready, busiest := 0, networkRate{}
		for _, rate := range n.rates {
			if !rate.Ready {
				continue
			}
			ready++
			download += rate.Receive
			upload += rate.Transmit
			if rate.Receive+rate.Transmit > busiest.Receive+busiest.Transmit {
				busiest = rate
			}
		}
		downText, upText, busyText := "collecting baseline", "collecting baseline", "waiting for next poll"
		if ready > 0 {
			downText = networkBytes(download) + "/s"
			upText = networkBytes(upload) + "/s"
			busyText = busiest.Name + "  ·  " + networkBytes(busiest.Receive+busiest.Transmit) + "/s"
		}
		downVisual, upVisual := signalMeter(download, max(download, upload), 20, n.w.settings.GraphMode), signalMeter(upload, max(download, upload), 20, n.w.settings.GraphMode)
		if len(n.downloadHistory) >= 2 {
			downVisual = signalChart(n.downloadHistory, n.w.settings.GraphMode)
			upVisual = signalChart(n.uploadHistory, n.w.settings.GraphMode)
		}
		values = []struct {
			title, colour, headline, visual, note string
		}{
			{" DOWNLOAD · observed interfaces ", p.success, downText, downVisual, fmt.Sprintf("%d interfaces · %s", ready, signalDirection(n.downloadHistory))},
			{" UPLOAD · observed interfaces ", p.accent, upText, upVisual, "live polls · " + signalDirection(n.uploadHistory)},
			{" BUSIEST INTERFACE ", p.warning, busyText, "RX + TX throughput", "virtual paths may overlap"},
		}
	}
	for i, value := range values {
		n.cards[i].SetTitle(value.title).SetTitleColor(tcell.GetColor(value.colour))
		n.cards[i].SetText(fmt.Sprintf("[%s::b]%s[-::-]\n%s\n[%s]%s[-]", value.colour, tview.Escape(value.headline), gradientText(value.visual, value.colour, p.accent), p.muted, value.note))
	}
}

func networkField(label, value, colour string) string {
	return fmt.Sprintf("[%s]%s[-] %s", colour, label, tview.Escape(clean(value)))
}

func networkBytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func (n *networkPage) speedInterval() float64 {
	if n.rateInterval <= 0 {
		return 0
	}
	return n.rateInterval.Seconds()
}

func (n *networkPage) updateStatus() {
	count := len(n.visible)
	if n.view == 2 {
		count = len(n.interfaces)
	} else if n.view == 3 {
		count = len(n.visibleRates)
	}
	at := n.snapshot.At
	if n.view == 2 {
		at = n.snapshot.InterfaceAt
	} else if n.view == 3 {
		at = n.snapshot.CounterAt
	}
	state := "waiting"
	if !at.IsZero() {
		state = "sampled " + at.Format("15:04:05")
	}
	if n.busy {
		state += " · refreshing"
	}
	if n.paused {
		state += " · paused"
	}
	note := "Owners may be hidden by permissions; lsof may omit inaccessible sockets. UDP ports are bound, not TCP listeners."
	if n.view == 3 {
		note = "Rates use byte-counter deltas between polls; loopback and virtual interfaces are included and may overlap."
	}
	if n.snapshot.Note != "" {
		note += " · " + clean(n.snapshot.Note)
	}
	if n.failure[n.view] != "" {
		note = "Refresh failed; previous sample retained: " + clean(n.failure[n.view])
	}
	stateColour := n.w.palette().muted
	if n.busy {
		stateColour = n.w.palette().accent
	}
	if n.failure[n.view] != "" {
		stateColour = n.w.palette().error
	}
	source := n.snapshot.Source
	if n.view == 3 {
		source = n.snapshot.CounterSource
	}
	n.status.SetText(fmt.Sprintf(" [%s::b]%d rows[-::-] · [%s]%s[-] · poll %ds · %s\n [%s]%s[-]", n.w.palette().accent, count, stateColour, state, n.w.settings.RefreshSeconds, tview.Escape(clean(source)), n.w.palette().muted, tview.Escape(strings.ReplaceAll(note, "\n", " · "))))
}
func (n *networkPage) report() string {
	host, _ := os.Hostname()
	at := n.snapshot.At
	if n.view == 2 {
		at = n.snapshot.InterfaceAt
	} else if n.view == 3 {
		at = n.snapshot.CounterAt
	}
	sampled := "not available"
	if !at.IsZero() {
		sampled = at.Format(time.RFC3339)
	}
	var out strings.Builder
	source := n.snapshot.Source
	if n.view == 3 {
		source = n.snapshot.CounterSource
	}
	fmt.Fprintf(&out, "SYSTEMDOC NETWORK SNAPSHOT\nHost: %s\nSource: %s\nSampled: %s\nView: %s\nFilter: %s\n\n", host, source, sampled, []string{"Ports (TCP listeners / unconnected UDP)", "Connections (all TCP/UDP)", "Interfaces", "Interface speed (rates between polls)"}[n.view], n.filters[n.view])
	if n.failure[n.view] != "" {
		fmt.Fprintf(&out, "Refresh error; data may be stale: %s\n\n", n.failure[n.view])
	}
	if n.view == 2 {
		for _, i := range n.interfaces {
			fmt.Fprintf(&out, "%s  %s  flags=%s  MTU=%d  hardware=%s\n", i.Name, i.Addresses, i.Flags, i.MTU, i.Hardware)
		}
	} else if n.view == 3 {
		for _, rate := range n.visibleRates {
			receive, transmit := "collecting baseline", "collecting baseline"
			if rate.Ready {
				receive, transmit = networkBytes(rate.Receive)+"/s", networkBytes(rate.Transmit)+"/s"
			}
			fmt.Fprintf(&out, "%s  download=%s  upload=%s  received=%s  sent=%s\n", rate.Name, receive, transmit, networkBytes(float64(rate.ReceiveTotal)), networkBytes(float64(rate.SendTotal)))
		}
	} else {
		for _, s := range n.visible {
			fmt.Fprintf(&out, "%s %s  %s -> %s  process=%s pid=%d user=%s uid=%s fd=%s cgroup=%s\n", s.Protocol, s.State, s.Local, available(s.Remote), available(s.Process), s.PID, available(s.User), available(s.UID), available(s.FD), available(s.Cgroup))
		}
	}
	out.WriteString("\nHost network view; container/VM/remote Docker endpoints are not queried. Owners and sockets may be hidden by permissions. Multiple owners can share a socket. PID reuse and separately sampled process details are possible. Speed is host-interface throughput between polls, not negotiated link capacity; virtual interfaces may overlap.\n")
	out.WriteString(n.snapshot.Note)
	return clean(out.String())
}
func (n *networkPage) processDetails(s networkSocket) {
	if s.PID <= 0 {
		n.w.message("Process owner unavailable", "The socket is visible, but no PID was reported. Native permissions may hide ownership; closed sockets can also have no owner.\n\nCgroup: "+available(s.Cgroup))
		return
	}
	ctx, cancel := context.WithCancel(n.ctx)
	view := textView().SetScrollable(true).SetWrap(true).SetText("Reading process details…")
	view.SetBorder(true).SetTitle(" Process · Escape returns ")
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			cancel()
			n.w.pages.RemovePage("network-process")
			n.w.app.SetFocus(n.table)
			return nil
		}
		return e
	})
	n.w.pages.AddPage("network-process", centered(view, 120, 24), true, true)
	go func() {
		raw, diagnostic, err := networkCommand(ctx, "ps", "-ww", "-p", strconv.Itoa(s.PID), "-o", "pid=,ppid=,user=,etime=,command=")
		if ctx.Err() != nil {
			return
		}
		content := fmt.Sprintf("Socket owner at observation: %s · PID %d\n%s %s -> %s\n\nPID  PPID  USER  ELAPSED  COMMAND\n%s\n\nProcess data is fetched separately; an exited/reused PID may no longer be this socket's owner.\nCgroup: %s", s.Process, s.PID, s.Protocol, s.Local, available(s.Remote), raw, available(s.Cgroup))
		if err != nil {
			content += "\nProcess lookup failed: " + err.Error() + "\n" + diagnostic
		}
		n.w.queue(func() {
			if ctx.Err() == nil {
				view.SetText(clean(content))
			}
		})
	}()
}
