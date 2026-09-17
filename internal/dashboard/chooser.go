package dashboard

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type choice struct {
	name, description string
	run               func()
}

func (w *workspace) choose(page, title string, choices []choice) {
	focus := w.app.GetFocus()
	search := tview.NewInputField().SetLabel(" Search ")
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(tview.Escape(title) + " · Escape closes ")
	// The overlay takes as much width as the terminal allows up to 118 cells;
	// a description that still does not fit ends in an ellipsis instead of
	// being cut mid-word at the frame.
	width := 118
	if w.lastWidth > 0 {
		width = min(width, max(40, w.lastWidth-4))
	}
	inner := width - 4
	populate := func(query string) {
		list.Clear()
		for _, item := range choices {
			if matchesChoice(item.name+" "+item.description, query) {
				item := item
				list.AddItem(tview.Escape(ellipsize(item.name, inner)), tview.Escape(ellipsize(item.description, inner)), 0, func() { w.pages.RemovePage(page); w.app.SetFocus(focus); item.run() })
			}
		}
	}
	search.SetChangedFunc(populate)
	search.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			w.pages.RemovePage(page)
			w.app.SetFocus(focus)
		} else {
			w.app.SetFocus(list)
		}
	})
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(search, 1, 0, true).AddItem(list, 0, 1, false)
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.pages.RemovePage(page)
			w.app.SetFocus(focus)
			return nil
		}
		if e.Key() == tcell.KeyDown && w.app.GetFocus() == search {
			w.app.SetFocus(list)
			return nil
		}
		if e.Rune() == '/' && w.app.GetFocus() == list {
			w.app.SetFocus(search)
			return nil
		}
		return e
	})
	populate("")
	w.pages.AddPage(page, centered(panel, width, 26), true, true)
	w.app.SetFocus(search)
}

func matchesChoice(text, query string) bool {
	text = strings.ToLower(text)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}
