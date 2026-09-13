package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func shortWorkload(value string) string {
	value = strings.TrimSuffix(value, ".service")
	if len([]rune(value)) <= 18 {
		return value
	}
	return string([]rune(value)[:17]) + "…"
}

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

func (w *workspace) updateStoryline() {
	if w.storyline == nil {
		return
	}
	p := w.palette()
	parts := []string{}
	for i := len(w.activity) - 1; i >= 0 && len(parts) < 3; i-- {
		event := w.activity[i]
		if event.mode != w.mode || event.user != w.user {
			continue
		}
		colour := p.warning
		if strings.Contains(event.text, "failed") || strings.Contains(event.text, "unhealthy") || strings.Contains(event.text, "no longer") {
			colour = p.error
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-] [%s::b]%s[-::-] %s", p.muted, event.at.Format("15:04"), colour, tview.Escape(shortWorkload(event.id)), tview.Escape(clean(event.text))))
	}
	if len(parts) == 0 {
		w.storyline.SetText(fmt.Sprintf(" [%s::b]%s STORYLINE[-::-]  [%s]quiet · changes observed during this session will appear here · click or I opens history[-]", p.accent, w.icon(iconStoryline), p.muted))
		return
	}
	// Events were collected newest first: keep “now” on the right like a chart.
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	w.storyline.SetText(fmt.Sprintf(" [%s::b]%s STORYLINE[-::-]  %s  [%s]→ NOW[-]", p.accent, w.icon(iconStoryline), strings.Join(parts, fmt.Sprintf(" [%s]──[-] ", p.muted)), p.accent))
}

func (w *workspace) storylineView() {
	focus := w.app.GetFocus()
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	p := w.palette()
	var content strings.Builder
	fmt.Fprintf(&content, "[%s::b]SYSTEM STORYLINE[-::-] · %s\n[%s]Observed changes from existing inventory polls; newest events appear first.[-]\n\n", p.accent, []string{"Services", "Containers"}[w.mode], p.muted)
	count := 0
	for i := len(w.activity) - 1; i >= 0; i-- {
		event := w.activity[i]
		if event.mode != w.mode || event.user != w.user {
			continue
		}
		colour := p.warning
		if strings.Contains(event.text, "failed") || strings.Contains(event.text, "unhealthy") || strings.Contains(event.text, "no longer") {
			colour = p.error
		}
		fmt.Fprintf(&content, "[%s]%s[-]  [%s::b]◆ %-24s[-::-]  %s\n", p.muted, event.at.Format("15:04:05"), colour, tview.Escape(shortWorkload(event.id)), tview.Escape(clean(event.text)))
		count++
	}
	if count == 0 {
		fmt.Fprintf(&content, "[%s]No state changes have been observed in this session.[-]\n", p.muted)
	}
	fmt.Fprintf(&content, "\n[%s]Polling can miss brief transitions. This is a bounded session history, not an audit log.[-]", p.muted)
	view.SetText(content.String()).SetBorder(true).SetTitle(" INCIDENT STORYLINE · Esc returns ")
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			w.pages.RemovePage("storyline")
			w.app.SetFocus(focus)
			return nil
		}
		return event
	})
	w.pages.AddPage("storyline", centered(view, 124, 32), true, true)
}
