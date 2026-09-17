package dashboard

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (w *workspace) preferences() {
	form := tview.NewForm()
	layouts := []string{"auto", "stacked", "side-by-side"}
	selected := 0
	for i, v := range layouts {
		if v == w.settings.Layout {
			selected = i
		}
	}
	form.AddInputField("Refresh seconds (2–300)", strconv.Itoa(w.settings.RefreshSeconds), 6, tview.InputFieldInteger, nil).
		AddDropDown("Layout", layouts, selected, nil).
		AddInputField("Inventory pane width % (30–70)", strconv.Itoa(w.settings.PaneRatio), 6, tview.InputFieldInteger, nil).
		AddCheckbox("Wrap inspector text", w.settings.WrapLogs, nil).
		AddCheckbox("Startup splash when loading", w.settings.Splash, nil).
		AddInputField("Accent override (#RRGGBB)", w.settings.Accent, 12, nil, nil).
		AddInputField("Background override (#RRGGBB)", w.settings.Background, 12, nil, nil).
		AddCheckbox("Nerd Font interface (recommended)", w.settings.NerdIcons, nil).
		AddDropDown("Signal graphics", graphModes, graphModeIndex(w.settings.GraphMode), nil)
	form.SetBorder(true).SetTitle(" Preferences · saved under XDG config/systemdoc ")
	form.AddButton("Save", func() {
		seconds, err := strconv.Atoi(form.GetFormItem(0).(*tview.InputField).GetText())
		if err != nil || seconds < 2 || seconds > 300 {
			w.message("Invalid refresh interval", "Choose 2–300 seconds.")
			return
		}
		_, layout := form.GetFormItem(1).(*tview.DropDown).GetCurrentOption()
		paneRatio, err := strconv.Atoi(form.GetFormItem(2).(*tview.InputField).GetText())
		if err != nil || paneRatio < 30 || paneRatio > 70 {
			w.message("Invalid pane width", "Choose 30–70 percent.")
			return
		}
		accent := form.GetFormItem(5).(*tview.InputField).GetText()
		background := form.GetFormItem(6).(*tview.InputField).GetText()
		for _, value := range []string{accent, background} {
			if value != "" && !hexColour.MatchString(value) {
				w.message("Invalid colour", "Use #RRGGBB or leave blank for the theme default.")
				return
			}
		}
		w.settings.Accent = accent
		w.settings.Background = background
		if err := w.setPollInterval(seconds); err != nil {
			w.message("Invalid refresh interval", err.Error())
			return
		}
		w.settings.Layout = layout
		w.settings.PaneRatio = paneRatio
		w.settings.WrapLogs = form.GetFormItem(3).(*tview.Checkbox).IsChecked()
		w.settings.Splash = form.GetFormItem(4).(*tview.Checkbox).IsChecked()
		w.settings.NerdIcons = form.GetFormItem(7).(*tview.Checkbox).IsChecked()
		_, w.settings.GraphMode = form.GetFormItem(8).(*tview.DropDown).GetCurrentOption()
		w.detail.SetWrap(w.settings.WrapLogs)
		w.lastWidth = 0
		w.applyTheme()
		w.pages.RemovePage("preferences")
		w.savePreferences()
		w.app.SetFocus(w.table)
	}).AddButton("Cancel", func() { w.pages.RemovePage("preferences"); w.app.SetFocus(w.table) })
	w.pages.AddPage("preferences", centered(form, 88, 29), true, true)
}

func graphModeIndex(value string) int {
	value = graphMode(value)
	for i, mode := range graphModes {
		if mode == value {
			return i
		}
	}
	return 0
}

func pollDue(last, now time.Time, seconds int) bool {
	return last.IsZero() || now.Sub(last) >= time.Duration(max(2, seconds))*time.Second
}

func (w *workspace) setPollInterval(seconds int) error {
	if seconds < 2 || seconds > 300 {
		return fmt.Errorf("choose 2–300 seconds")
	}
	w.settings.RefreshSeconds = seconds
	w.updatePollButton()
	return nil
}

func (w *workspace) updatePollButton() {
	if w.pollButton != nil {
		w.pollButton.SetLabel(fmt.Sprintf("%s Poll %ds", w.icon(iconRefresh), w.settings.RefreshSeconds))
	}
	if w.pages == nil {
		return
	}
	if page, ok := w.pages.GetPage("network").(*networkPage); ok {
		page.updateStatus()
	}
	for _, name := range []string{"processes", "storage"} {
		if page, ok := w.pages.GetPage(name).(*hostPage); ok {
			page.updateStatus()
		}
	}
	if w.splashView != nil {
		w.splashView.SetText(w.splashText())
	}
}

func (w *workspace) pollingDialog() {
	focus := w.app.GetFocus()
	form := tview.NewForm()
	form.AddInputField("Polling interval in seconds (2–300)", strconv.Itoa(w.settings.RefreshSeconds), 6, tview.InputFieldInteger, nil)
	form.SetBorder(true).SetTitle(" Live polling · applies immediately ")
	close := func() {
		w.pages.RemovePage("polling")
		w.app.SetFocus(focus)
	}
	form.AddButton("Apply", func() {
		seconds, err := strconv.Atoi(form.GetFormItem(0).(*tview.InputField).GetText())
		if err != nil {
			w.message("Invalid polling interval", "Choose 2–300 seconds.")
			return
		}
		if err := w.setPollInterval(seconds); err != nil {
			w.message("Invalid polling interval", err.Error()+".")
			return
		}
		close()
		w.savePreferences()
	}).AddButton("Cancel", close)
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		return event
	})
	w.pages.AddPage("polling", centered(form, 62, 9), true, true)
}

func (w *workspace) splash() {
	view := textView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	p := w.palette()
	view.SetBackgroundColor(tcell.GetColor(p.surface))
	view.SetTextColor(tcell.GetColor(p.text))
	view.SetBorder(true).SetBorderColor(tcell.GetColor(p.accent)).SetTitleColor(tcell.GetColor(p.accent)).SetBorderAttributes(tcell.AttrBold)
	view.SetTitle(" "+w.iconLabel(iconEye, "SYSTEMDOC · SYSTEM OBSERVATORY ")).SetBorderPadding(1, 1, 1, 1)
	w.splashView = view
	view.SetText(w.splashText())
	view.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey {
		w.pages.RemovePage("splash")
		w.splashView = nil
		w.app.SetFocus(w.table)
		return nil
	})
	w.pages.AddPage("splash", centered(view, 82, 24), true, true)
}

// The splash is re-rendered on every spinner tick, but only the spinner
// changes: the wordmark and gradients are a few hundred blend lookups and tag
// writes that depend on the palette, icon set and polling interval alone. The
// body is rendered once per distinct set of inputs with a slot where the
// spinner goes, and each tick splices the current frame into it.
type splashKey struct {
	palette palette
	nerd    bool
	refresh int
	manager string
}

type splashBody struct {
	key  splashKey
	text string
}

var splashCache atomic.Pointer[splashBody]

const splashSpinnerSlot = "\x00"

func (w *workspace) splashText() string {
	frames := []rune("◐◓◑◒")
	spinner := frames[(time.Now().UnixMilli()/180)%int64(len(frames))]
	return strings.Replace(w.splashBodyText(), splashSpinnerSlot, string(spinner), 1)
}

func (w *workspace) splashBodyText() string {
	p := w.palette()
	key := splashKey{palette: p, nerd: w.settings.NerdIcons, refresh: w.settings.RefreshSeconds, manager: serviceManager()}
	if cached := splashCache.Load(); cached != nil && cached.key == key {
		return cached.text
	}
	eye := gradientText("───────────  "+w.icon(iconEye)+"  ───────────", p.accent, p.glow)
	text := fmt.Sprintf("%s\n%s\n\n%s\n[%s]SYSTEM OBSERVATORY[-]\n\n[%s]%s OBSERVE[-]  ·  [%s]%s INSPECT[-]  ·  [%s]%s ACT[-]\n\n[%s::b]01 %s SERVICES[-::-]   [%s::b]02 %s CONTAINERS[-::-]   [%s::b]03 %s NETWORK[-::-]\n[%s::b]04 %s PROCESSES[-::-]  [%s::b]05 %s STORAGE[-::-]\n\n[%s::b]%s[-::-] Connecting to %s and Docker  ·  polling every %ds\n[%s]PRESS ANY KEY TO ENTER[-]",
		wordmarkBlock("SYSTEMDOC", p.accent, p.glow), eye, gradientText("S Y S T E M D O C", p.glow, p.accent), p.muted,
		p.accent, w.icon(iconEye), p.glow, w.icon(iconSearch), p.warning, w.icon(iconActions),
		p.success, w.icon(iconServices), p.accent, w.icon(iconContainers), p.glow, w.icon(iconNetwork), p.warning, w.icon(iconProcesses), p.success, w.icon(iconStorage),
		p.accent, splashSpinnerSlot, tview.Escape(key.manager), key.refresh, p.muted)
	splashCache.Store(&splashBody{key: key, text: text})
	return text
}
