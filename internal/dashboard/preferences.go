package dashboard

import (
	"strconv"

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
		AddCheckbox("Wrap inspector text", w.settings.WrapLogs, nil).
		AddCheckbox("Startup splash when loading", w.settings.Splash, nil).
		AddInputField("Accent override (#RRGGBB)", w.settings.Accent, 12, nil, nil).
		AddInputField("Background override (#RRGGBB)", w.settings.Background, 12, nil, nil).
		AddCheckbox("Nerd Font icons (requires terminal font)", w.settings.NerdIcons, nil)
	form.SetBorder(true).SetTitle(" Preferences · saved under XDG config/systemdoc ")
	form.AddButton("Save", func() {
		seconds, err := strconv.Atoi(form.GetFormItem(0).(*tview.InputField).GetText())
		if err != nil || seconds < 2 || seconds > 300 {
			w.message("Invalid refresh interval", "Choose 2–300 seconds.")
			return
		}
		_, layout := form.GetFormItem(1).(*tview.DropDown).GetCurrentOption()
		accent := form.GetFormItem(4).(*tview.InputField).GetText()
		background := form.GetFormItem(5).(*tview.InputField).GetText()
		for _, value := range []string{accent, background} {
			if value != "" && !hexColour.MatchString(value) {
				w.message("Invalid colour", "Use #RRGGBB or leave blank for the theme default.")
				return
			}
		}
		w.settings.Accent = accent
		w.settings.Background = background
		w.settings.RefreshSeconds = seconds
		w.settings.Layout = layout
		w.settings.WrapLogs = form.GetFormItem(2).(*tview.Checkbox).IsChecked()
		w.settings.Splash = form.GetFormItem(3).(*tview.Checkbox).IsChecked()
		w.settings.NerdIcons = form.GetFormItem(6).(*tview.Checkbox).IsChecked()
		w.detail.SetWrap(w.settings.WrapLogs)
		w.lastWidth = 0
		w.applyTheme()
		w.pages.RemovePage("preferences")
		w.savePreferences()
		w.app.SetFocus(w.table)
	}).AddButton("Cancel", func() { w.pages.RemovePage("preferences"); w.app.SetFocus(w.table) })
	w.pages.AddPage("preferences", centered(form, 88, 25), true, true)
}

func (w *workspace) splash() {
	view := textView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	view.SetBackgroundColor(tcell.GetColor(w.palette().background))
	view.SetText("\n\n\n[" + w.palette().accent + "]S Y S T E M D O C[-]\n\nServices. Containers. One workspace.\n\nConnecting to your system…\n\nPress any key to enter")
	view.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey {
		w.pages.RemovePage("splash")
		w.app.SetFocus(w.table)
		return nil
	})
	w.pages.AddPage("splash", view, true, true)
}
