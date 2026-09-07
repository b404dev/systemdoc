package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type systemTimer struct {
	Unit      string  `json:"unit"`
	Activates string  `json:"activates"`
	Next      *uint64 `json:"next"`
	Last      *uint64 `json:"last"`
}

func timerTime(value *uint64) string {
	if value == nil || *value == 0 || *value == ^uint64(0) {
		return "—"
	}
	return time.UnixMicro(int64(*value)).Local().Format("2006-01-02 15:04:05 MST")
}
func timerDue(value *uint64, now time.Time) string {
	if value == nil || *value == 0 || *value == ^uint64(0) {
		return "unscheduled"
	}
	d := time.UnixMicro(int64(*value)).Sub(now)
	if d < 0 {
		return "due / awaiting update"
	}
	return "in " + d.Round(time.Second).String()
}
func listTimers(ctx context.Context, user bool) ([]systemTimer, error) {
	args := []string{"list-timers", "--all", "--output=json", "--no-pager"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	raw, err := command(ctx, "systemctl", args...)
	if err != nil {
		return nil, err
	}
	var rows []systemTimer
	if err = json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, fmt.Errorf("cannot read timer JSON (systemctl must support JSON list-timers): %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Next, rows[j].Next
		valid := func(v *uint64) bool { return v != nil && *v != 0 && *v != ^uint64(0) }
		if valid(a) != valid(b) {
			return valid(a)
		}
		if valid(a) && *a != *b {
			return *a < *b
		}
		return rows[i].Unit < rows[j].Unit
	})
	return rows, nil
}
func (w *workspace) timers() {
	user := w.user
	ctx, cancel := context.WithCancel(w.ctx)
	var jobCancel context.CancelFunc
	generation := 0
	rows := []systemTimer{}
	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	table.SetBorder(true)
	detail := textView().SetWrap(true)
	detail.SetBorder(true).SetTitle(" Schedule · local timezone ")
	help := textView().SetText(" Enter actions · r refresh · u system/user · Escape closes")
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(table, 0, 1, true).AddItem(detail, 7, 0, false).AddItem(help, 1, 0, false)
	close := func() { cancel(); w.pages.RemovePage("timers"); w.app.SetFocus(w.table) }
	updateDetail := func(row int) {
		if row < 1 || row > len(rows) {
			return
		}
		v := rows[row-1]
		detail.SetText(fmt.Sprintf("%s\nNext  %s · %s\nLast  %s\nTriggers  %s\nLast trigger time does not establish whether the job succeeded.", v.Unit, timerTime(v.Next), timerDue(v.Next, time.Now()), timerTime(v.Last), available(v.Activates)))
	}
	table.SetSelectionChangedFunc(func(row, col int) { updateDetail(row) })
	table.SetSelectedFunc(func(row, col int) {
		if row < 1 || row > len(rows) {
			return
		}
		v := rows[row-1]
		scope := user
		choices := []choice{
			{"Timer status", v.Unit, func() { w.timerRead(scope, v.Unit, 0) }},
			{"Timer configuration", v.Unit, func() { w.timerRead(scope, v.Unit, 2) }},
		}
		if v.Activates != "" {
			choices = append(choices,
				choice{"Triggered unit status", v.Activates, func() { w.timerRead(scope, v.Activates, 0) }},
				choice{"Triggered unit logs", "Latest 150 journal lines · " + v.Activates, func() { w.timerRead(scope, v.Activates, 1) }})
		}
		w.choose("timer-actions", v.Unit, choices)
	})
	refresh := func() {
		if jobCancel != nil {
			jobCancel()
		}
		var job context.Context
		job, jobCancel = context.WithCancel(ctx)
		generation++
		gen := generation
		scope := user
		label := "system"
		if user {
			label = "user"
		}
		table.SetTitle(" SYSTEMD TIMERS · " + label + " · loading ")
		rows = nil
		table.Clear()
		detail.SetText("Loading timer schedules…")
		go func() {
			result, err := listTimers(job, scope)
			if job.Err() != nil {
				return
			}
			w.queue(func() {
				if ctx.Err() != nil || gen != generation {
					return
				}
				table.SetTitle(" SYSTEMD TIMERS · " + label + " · " + time.Now().Format("15:04:05") + " snapshot ")
				if err != nil {
					detail.SetText(err.Error())
					return
				}
				rows = result
				for col, title := range []string{"TIMER", "NEXT RUN", "LAST RUN", "TRIGGERS"} {
					table.SetCell(0, col, tview.NewTableCell(title).SetSelectable(false).SetTextColor(tcell.GetColor(w.palette().accent)))
				}
				for i, v := range rows {
					for col, value := range []string{v.Unit, timerTime(v.Next), timerTime(v.Last), v.Activates} {
						table.SetCell(i+1, col, tview.NewTableCell(tview.Escape(clean(value))).SetExpansion(1).SetMaxWidth(48))
					}
				}
				if len(rows) == 0 {
					detail.SetText("No loaded timers in this scope. Press u to switch system/user scope. Disabled timer files that are not loaded are not listed.")
				} else {
					table.Select(1, 0)
					updateDetail(1)
				}
			})
		}()
	}
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		switch {
		case e.Key() == tcell.KeyEscape:
			close()
			return nil
		case e.Rune() == 'r':
			refresh()
			return nil
		case e.Rune() == 'u':
			user = !user
			refresh()
			return nil
		case e.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, 0)
		case e.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, 0)
		}
		return e
	})
	w.pages.AddPage("timers", centered(panel, 140, 34), true, true)
	refresh()
}
func (w *workspace) timerRead(user bool, id string, tab int) {
	if id == "" || strings.HasPrefix(id, "-") {
		w.message("Invalid timer target", id)
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	view := textView().SetScrollable(true).SetWrap(true).SetText("Loading…")
	view.SetBorder(true).SetTitle(" " + tview.Escape(clean(id)) + " · Escape returns ")
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			cancel()
			w.pages.RemovePage("timer-read")
			w.pages.SendToFront("timers")
			return nil
		}
		return e
	})
	w.pages.AddPage("timer-read", centered(view, 130, 32), true, true)
	go func() {
		raw, err := inspect(ctx, 0, user, workload{ID: id, Name: id}, tab)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			raw = err.Error()
		}
		w.queue(func() {
			if ctx.Err() == nil {
				view.SetText(clean(raw))
			}
		})
	}()
}
