package dashboard

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type snapshotRequest struct {
	Item           workload
	Mode           int
	User           bool
	Host, Endpoint string
	At             time.Time
	Logs, Config   bool
	Activity       []activityEvent
}

func collectSnapshot(ctx context.Context, request snapshotRequest) string {
	var out strings.Builder
	scope := "system"
	if request.User {
		scope = "user"
	}
	backend := "systemd"
	if request.Mode == 1 {
		backend = "Docker"
		scope = request.Endpoint
	}
	fmt.Fprintf(&out, "SYSTEMDOC TROUBLESHOOTING SNAPSHOT\nCaptured: %s\nHost: %s\nBackend: %s\nScope: %s\nWorkload: %s\nID: %s\n\nObservations are collected sequentially, not atomically. Review before sharing.\n\n", request.At.Format(time.RFC3339), request.Host, backend, scope, request.Item.Name, request.Item.ID)
	sections := []struct {
		name string
		tab  int
	}{{"OVERVIEW", 0}, {"RESOURCES", 3}}
	if request.Logs {
		sections = append(sections, struct {
			name string
			tab  int
		}{"RECENT LOGS (latest 150 requested; output capped at 1 MiB)", 1})
	}
	if request.Config {
		sections = append(sections, struct {
			name string
			tab  int
		}{"CONFIGURATION / INSPECT JSON (explicitly included)", 2})
	}
	for _, section := range sections {
		if ctx.Err() != nil {
			fmt.Fprintf(&out, "\nCollection cancelled: %v\n", ctx.Err())
			break
		}
		raw, err := inspect(ctx, request.Mode, request.User, request.Item, section.tab)
		fmt.Fprintf(&out, "── %s\nObserved: %s\n", section.name, time.Now().Format(time.RFC3339))
		if err != nil {
			fmt.Fprintf(&out, "Collection error: %v\n", err)
		} else {
			out.WriteString(raw)
		}
		out.WriteString("\n\n")
	}
	out.WriteString("── OBSERVED ACTIVITY (this session; polling may miss transitions)\n")
	count := 0
	for _, event := range request.Activity {
		if event.mode == request.Mode && event.user == request.User && event.id == request.Item.ID {
			fmt.Fprintf(&out, "%s  %s\n", event.at.Format(time.RFC3339), event.text)
			count++
		}
	}
	if count == 0 {
		out.WriteString("No matching events retained.\n")
	}
	if !request.Logs {
		out.WriteString("\nRecent log section omitted. Native status output may still contain journal excerpts.\n")
	}
	if !request.Config {
		out.WriteString("\nConfiguration / inspect JSON omitted. Overview and logs may still contain sensitive data.\n")
	}
	return clean(out.String())
}
func (w *workspace) snapshotDialog() {
	item := w.current()
	if item.ID == "" {
		w.message("Select a workload", "Select a service or container before collecting a snapshot.")
		return
	}
	host, _ := os.Hostname()
	request := snapshotRequest{Item: item, Mode: w.mode, User: w.user, Host: host, Endpoint: dockerEndpoint(), At: time.Now(), Activity: append([]activityEvent(nil), w.activity...)}
	form := tview.NewForm().AddCheckbox("Include recent logs (latest 150)", true, nil).AddCheckbox("Include configuration / inspect JSON", false, nil)
	form.SetBorder(true).SetTitle(" Troubleshooting snapshot · " + tview.Escape(clean(item.Name)) + " ")
	close := func() { w.pages.RemovePage("snapshot-options"); w.app.SetFocus(w.table) }
	form.AddButton("Collect and review", func() {
		request.Logs = form.GetFormItem(0).(*tview.Checkbox).IsChecked()
		request.Config = form.GetFormItem(1).(*tview.Checkbox).IsChecked()
		close()
		w.collectSnapshotView(request)
	}).AddButton("Cancel", close)
	form.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		return e
	})
	note := textView().SetWrap(true).SetText("Collects fresh status and resource readings for this exact workload, plus session activity. Review and edit the report before saving; status, labels and logs can contain sensitive data. Nothing is sent externally.")
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(note, 4, 0, false).AddItem(form, 0, 1, true)
	w.pages.AddPage("snapshot-options", centered(panel, 100, 15), true, true)
}
func (w *workspace) collectSnapshotView(request snapshotRequest) {
	ctx, cancel := context.WithCancel(w.ctx)
	loading := textView().SetText("Collecting status and resources…\nEscape cancels. Each backend command has an 8-second timeout.")
	loading.SetBorder(true).SetTitle(" Troubleshooting snapshot ")
	loading.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			cancel()
			w.pages.RemovePage("snapshot-loading")
			w.app.SetFocus(w.table)
			return nil
		}
		return e
	})
	w.pages.AddPage("snapshot-loading", centered(loading, 100, 6), true, true)
	go func() {
		report := collectSnapshot(ctx, request)
		if ctx.Err() != nil {
			return
		}
		w.queue(func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			w.pages.RemovePage("snapshot-loading")
			w.reviewSnapshot(report)
		})
	}()
}
func (w *workspace) reviewSnapshot(report string) {
	editor := tview.NewTextArea().SetText(report, false)
	editor.SetBorder(true).SetTitle(" REVIEW SNAPSHOT · edit to remove sensitive data · Tab to filename ")
	file := tview.NewInputField().SetLabel(" Save as ").SetText("systemdoc-snapshot-" + time.Now().Format("20060102-150405") + ".txt")
	close := func() { w.pages.RemovePage("snapshot-review"); w.app.SetFocus(w.table) }
	buttons := tview.NewForm().AddButton("Save reviewed report", func() {
		path := strings.TrimSpace(file.GetText())
		if path == "" {
			w.message("Filename required", "Enter a destination filename.")
			return
		}
		if err := writeNewFile(path, editor.GetText(), 0600); err != nil {
			w.message("Snapshot not saved", err.Error())
			return
		}
		close()
		w.message("Snapshot saved", path)
	}).AddButton("Cancel", close)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(editor, 0, 1, true).AddItem(file, 1, 0, false).AddItem(buttons, 3, 0, false)
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		if e.Key() == tcell.KeyTab {
			if editor.HasFocus() {
				w.app.SetFocus(file)
				return nil
			}
			if file.HasFocus() {
				w.app.SetFocus(buttons)
				return nil
			}
		}
		if buttons.HasFocus() {
			_, button := buttons.GetFocusedItemIndex()
			if e.Key() == tcell.KeyTab && button == 1 {
				w.app.SetFocus(editor)
				return nil
			}
			if e.Key() == tcell.KeyBacktab && button == 0 {
				w.app.SetFocus(file)
				return nil
			}
		}
		if e.Key() == tcell.KeyBacktab && editor.HasFocus() {
			w.app.SetFocus(buttons)
			buttons.SetFocus(1)
			return nil
		}
		if e.Key() == tcell.KeyBacktab && file.HasFocus() {
			w.app.SetFocus(editor)
			return nil
		}
		return e
	})
	w.pages.AddPage("snapshot-review", centered(panel, 140, 38), true, true)
}
