package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

type activityEvent struct {
	mode     int
	user     bool
	id, text string
	at       time.Time
}

func (w *workspace) recordChanges(mode int, before, after []workload) {
	if len(before) == 0 {
		return
	}
	old := map[string]workload{}
	for _, item := range before {
		old[item.ID] = item
	}
	for _, item := range after {
		previous, exists := old[item.ID]
		if !exists || previous.State != item.State || previous.Detail != item.Detail {
			text := item.State + " / " + item.Detail
			if exists {
				text = previous.State + " → " + text
			} else {
				text = "discovered · " + text
			}
			w.activity = append(w.activity, activityEvent{mode, w.user, item.ID, text, time.Now()})
		}
		delete(old, item.ID)
	}
	for id := range old {
		w.activity = append(w.activity, activityEvent{mode, w.user, id, "no longer observed", time.Now()})
	}
	if len(w.activity) > 500 {
		w.activity = w.activity[len(w.activity)-500:]
	}
}

func (w *workspace) activityView() {
	item := w.current()
	var content strings.Builder
	fmt.Fprintf(&content, "%s · observed state changes this session\n\n", item.Name)
	for i := len(w.activity) - 1; i >= 0; i-- {
		event := w.activity[i]
		if event.mode == w.mode && event.user == w.user && event.id == item.ID {
			fmt.Fprintf(&content, "%s  %s\n", event.at.Format("15:04:05"), event.text)
		}
	}
	content.WriteString("\nPolling observations may miss short transitions. This is not a complete audit log.\nPress l to inspect current logs; Escape returns.")
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	view.SetText(richOutput(content.String(), 1, w.palette())).SetBorder(true).SetTitle(" Activity ")
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape || e.Rune() == 'l' {
			w.pages.RemovePage("activity")
			w.app.SetFocus(w.table)
			if e.Rune() == 'l' {
				w.tab = 1
				w.chrome()
				w.showDetail()
			}
			return nil
		}
		return e
	})
	w.pages.AddPage("activity", view, true, true)
}
