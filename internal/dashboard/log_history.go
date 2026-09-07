package dashboard

import (
	"github.com/gdamore/tcell/v2"
)

// History is an explicit frozen view, so laying out the full retained buffer
// happens once rather than on every live update.
func (w *workspace) logHistory() {
	snapshot := w.inspectorLog
	query := w.logQuery
	if w.logDrawer.HasFocus() {
		snapshot = w.drawerLog
		query = ""
	}
	focus := w.app.GetFocus()
	p := w.palette()
	w.logHistoryGeneration++
	generation := w.logHistoryGeneration
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(w.settings.WrapLogs)
	view.SetBorder(true).SetTitle(" Retained log history · frozen snapshot · Escape returns ")
	view.SetText("Preparing retained history…")
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
			w.logHistoryGeneration++
			w.pages.RemovePage("log-history")
			w.app.SetFocus(focus)
			return nil
		}
		if event.Rune() == 'j' {
			return tcell.NewEventKey(tcell.KeyDown, 0, 0)
		}
		if event.Rune() == 'k' {
			return tcell.NewEventKey(tcell.KeyUp, 0, 0)
		}
		return event
	})
	w.pages.AddPage("log-history", view, true, true)
	go func() {
		text := richOutput(filterLogLines(snapshot.text, query)+snapshot.ending, 1, p)
		w.queue(func() {
			if generation == w.logHistoryGeneration {
				view.SetText(text).ScrollToEnd()
			}
		})
	}()
}
