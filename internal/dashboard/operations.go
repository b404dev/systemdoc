package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tailBuffer bounds command output even during long builds or noisy failures.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 1024 * 1024
	n := len(p)
	if n >= limit {
		b.data = append(b.data[:0], p[n-limit:]...)
	} else {
		excess := len(b.data) + n - limit
		if excess > 0 {
			b.data = b.data[excess:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *tailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

type operation struct {
	Target, Command, Output, Status string
	Started                         time.Time
}

func actionArgs(mode int, user bool, item workload, verb string) (string, []string, error) {
	if mode == 0 && usesLaunchd() {
		return launchActionArgs(user, item, verb)
	}
	if item.ID == "" || strings.HasPrefix(item.ID, "-") || strings.ContainsAny(item.ID, "*?[") {
		return "", nil, fmt.Errorf("select a valid workload")
	}
	if mode == 0 && isTemplate(item) {
		switch verb {
		case "start", "stop", "restart", "reload", "reset-failed":
			return "", nil, fmt.Errorf("select a named service instance; %s is a template", item.ID)
		}
	}
	if mode == 1 {
		switch verb {
		case "start", "stop", "restart", "pause", "unpause", "rm":
			return "docker", []string{verb, item.ID}, nil
		}
	} else {
		switch verb {
		case "start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask", "reset-failed":
			args := []string{verb, "--", item.ID}
			if user {
				args = append([]string{"--user"}, args...)
			}
			return "systemctl", args, nil
		}
	}
	return "", nil, fmt.Errorf("unsupported action %q", verb)
}

func (w *workspace) current() workload {
	for _, item := range w.visible {
		if item.ID == w.selected[w.mode] {
			return item
		}
	}
	return workload{}
}

func (w *workspace) confirmAction(verb string) {
	item := w.current()
	name, args, err := actionArgs(w.mode, w.user, item, verb)
	if err != nil {
		w.message("Cannot perform action", err.Error())
		return
	}
	detail := name + " " + strings.Join(args, " ")
	if w.mode == 0 && usesLaunchd() {
		detail += "\n\nStop sends SIGTERM; launchd may restart a KeepAlive/on-demand job. Enable/disable changes future eligibility and does not load/unload or immediately stop a job."
	}
	w.confirm(verb+" · "+item.Name, detail, func() { w.execute(item.Name, name, args) })
}

func (w *workspace) confirm(title, detail string, run func()) {
	focus := w.app.GetFocus()
	p := w.palette()
	surface := tcell.GetColor(p.surface)
	view := textView().SetDynamicColors(true).SetWrap(true).SetScrollable(true)
	view.SetBackgroundColor(surface)
	view.SetText("[" + p.warning + "::b]" + w.icon(iconLock) + " APPROVAL REQUIRED[-::-]\n" +
		"[" + p.text + "::b]" + tview.Escape(title) + "[-::-]\n\n" +
		"[" + p.muted + "]Exact command or change[-]\n" + richOutput(detail, 2, p) + "\n\n" +
		"[" + p.muted + "]Nothing runs until you approve.  y approve · Esc cancel · Tab actions[-]")
	close := func() { w.pages.RemovePage("confirm"); w.app.SetFocus(focus) }
	accept := func() { close(); run() }
	buttons := tview.NewForm().
		AddButton(w.iconLabel(iconApprove, "Approve & run"), accept).
		AddButton(w.iconLabel(iconCancel, "Cancel"), close).
		SetButtonsAlign(tview.AlignRight).
		SetButtonStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.text)).Background(tcell.GetColor(p.background))).
		SetButtonActivatedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(p.background)).Background(tcell.GetColor(p.accent)).Bold(true))
	buttons.SetBackgroundColor(surface).SetBorderPadding(0, 0, 1, 1)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(buttons, 3, 0, false)
	panel.SetBackgroundColor(surface).
		SetBorder(true).
		SetBorderColor(tcell.GetColor(p.accent)).
		SetBorderAttributes(tcell.AttrBold).
		SetTitleColor(tcell.GetColor(p.accent)).
		SetTitle(" " + w.iconLabel(iconEye, "SYSTEMDOC · ACTION APPROVAL "))
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		if e.Key() == tcell.KeyTab && w.app.GetFocus() == view {
			w.app.SetFocus(buttons)
			return nil
		}
		if e.Rune() == 'y' || e.Rune() == 'Y' {
			accept()
			return nil
		}
		return e
	})
	w.pages.AddPage("confirm", centeredDialog(panel, 88, 20), true, true)
	w.app.SetFocus(view)
}

func (w *workspace) execute(target, name string, args []string) {

	if w.operationCancel != nil {
		w.message("Operation running", "Wait for the active operation or cancel it from Operations.")
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	w.operationCancel = cancel
	op := &operation{Target: target, Command: name + " " + strings.Join(args, " "), Status: "running", Started: time.Now()}
	w.operations = append(w.operations, op)
	if len(w.operations) > 50 {
		w.operations = w.operations[len(w.operations)-50:]
	}
	w.footer.SetText(" Running: " + op.Command + " · H operations")
	go func() {
		var output tailBuffer
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = &output
		cmd.Stderr = &output
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		ticker := time.NewTicker(time.Second)
		var err error
		running := true
		for running {
			select {
			case err = <-done:
				running = false
			case <-ticker.C:
				snapshot := clean(output.String())
				w.queue(func() {
					op.Output = snapshot
					front, _ := w.pages.GetFrontPage()
					if front == "history" {
						w.operationHistory()
					}
				})
			}
		}
		ticker.Stop()
		w.queue(func() {
			cancelled := ctx.Err() != nil
			w.operationCancel = nil
			cancel()
			op.Output = clean(output.String())
			op.Status = "command completed · verify workload health"
			if err != nil {
				op.Status = "failed"
				op.Output += "\n" + err.Error()
			}
			if cancelled && err != nil {
				op.Status = "stopped locally · backend may continue"
			}
			w.footer.SetText(" " + op.Status + ": " + target + " · H operations")
			w.load()
			w.operationHistory()
		})
	}()
}

func (w *workspace) operationHistory() {
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	var text strings.Builder
	for i := len(w.operations) - 1; i >= 0; i-- {
		op := w.operations[i]
		fmt.Fprintf(&text, "%s · %s · %s\n%s\n%s\n\n", op.Started.Format("15:04:05"), op.Target, op.Status, op.Command, op.Output)
	}
	if len(w.operations) == 0 {
		text.WriteString("No operations in this session.")
	}
	view.SetText(richOutput(text.String(), 0, w.palette())).SetBorder(true).SetTitle(" Operations · Escape closes · Ctrl-X cancels active process ")
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.pages.RemovePage("history")
			w.app.SetFocus(w.table)
			return nil
		}
		if e.Key() == tcell.KeyCtrlX && w.operationCancel != nil {
			w.operationCancel()
			return nil
		}
		return e
	})
	w.pages.AddPage("history", view, true, true)
}

func (w *workspace) native(name string, args ...string) error {

	var err error
	w.app.Suspend(func() {
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)
		cmd := exec.Command(name, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
	})
	if err != nil {
		w.message("Native command failed", err.Error())
	}
	w.load()
	return err
}
