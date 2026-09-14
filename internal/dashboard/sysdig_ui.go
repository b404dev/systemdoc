package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// sysdigPalette lists the probes for one target. Every probe is reviewed with
// its exact command before anything runs; sysdig needs root, so the review
// says so and the terminal asks for the password once.
func (w *workspace) sysdigPalette(target sysdigTarget, page string) {
	w.summary.SetText(" Checking how sysdig can run…")
	go func() {
		runner, err := detectSysdigRunner(w.ctx)
		w.queue(func() {
			if err != nil {
				w.message("sysdig unavailable", err.Error())
				return
			}
			var choices []choice
			auth := "needs sudo"
			if !runner.needsSudo() {
				auth = "via Docker · no sudo"
			}
			for _, probe := range sysdigProbes {
				probe := probe
				description := probe.description
				if probe.live {
					description += " · " + auth
				} else {
					description += fmt.Sprintf(" · collects for %d s · %s", probe.seconds, auth)
				}
				choices = append(choices, choice{probe.name, description, func() { w.reviewSysdig(runner, probe, target) }})
			}
			w.choose(page, w.iconLabel(iconProcesses, "sysdig · "+target.label), choices)
		})
	}()
}

func (w *workspace) reviewSysdig(runner sysdigRunner, probe sysdigProbe, target sysdigTarget) {
	engine := sysdigEngine(kernelRelease())
	capturePath := sysdigCapturePath(target, time.Now())
	args := sysdigArgs(probe, target, engine, capturePath)
	detail := runner.display(args)
	switch {
	case probe.live:
		detail += "\n\nStreams into a panel here until Escape stops it. Space pauses the display, e exports the retained buffer. Tracing adds overhead to the traced workload while it runs."
	case strings.Contains(probe.name, "Capture"):
		detail += fmt.Sprintf("\n\nRecords %d seconds of events to %s in the current directory. sudo creates the file as root; read it with sudo sysdig -r. Captures contain data buffers and can hold secrets.", probe.seconds, capturePath)
	default:
		detail += fmt.Sprintf("\n\nCollects for %d seconds, then shows the summary here. Tracing adds overhead to the traced workload while it runs.", probe.seconds)
	}
	if len(engine) > 0 {
		detail += "\n\nDriver: CO-RE BPF probe (no kernel module). Set SYSTEMDOC_SYSDIG_ENGINE=kmod to use the scap module instead."
	} else {
		detail += "\n\nDriver: scap kernel module (scap-dkms). Set SYSTEMDOC_SYSDIG_ENGINE=modern-bpf to try the BPF probe."
	}
	detail += "\n\nRunner: " + runner.note + ". SYSTEMDOC_SYSDIG=native|container forces the choice."
	if runner.kind == "container" {
		detail = strings.Replace(detail, "sudo creates the file as root", "the container creates the file as root", 1)
	}
	start := func() {
		if probe.live {
			w.sysdigStream(runner, probe, target, args)
			return
		}
		w.runSysdigCollection(runner, probe, target, args, capturePath)
	}
	w.confirm("sysdig · "+probe.name+" · "+target.label, detail, func() {
		if runner.needsSudo() {
			w.ensureSudo(start)
			return
		}
		w.ensureSysdigImage(runner, start)
	})
}

// ensureSysdigImage pulls the container image once, under a cancellable
// overlay, so the first probe does not look like a hung capture.
func (w *workspace) ensureSysdigImage(runner sysdigRunner, then func()) {
	go func() {
		present := runner.imagePresent(w.ctx)
		w.queue(func() {
			if present {
				then()
				return
			}
			ctx, cancel := context.WithCancel(w.ctx)
			loading := textView().SetText("Pulling " + runner.image + " once…\n\nEscape cancels.")
			loading.SetBorder(true).SetTitle(" sysdig · container image ")
			loading.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
				if e.Key() == tcell.KeyEscape {
					cancel()
					w.pages.RemovePage("sysdig-pull")
					w.app.SetFocus(w.table)
					return nil
				}
				return e
			})
			w.pages.AddPage("sysdig-pull", centered(loading, 100, 6), true, true)
			go func() {
				output, err := runBounded(ctx, 15*time.Minute, 0, "docker", "pull", runner.image)
				if ctx.Err() != nil {
					return
				}
				w.queue(func() {
					cancel()
					w.pages.RemovePage("sysdig-pull")
					if err != nil {
						w.message("Image pull failed", firstLine(strings.TrimSpace(output+"\n"+err.Error())))
						return
					}
					then()
				})
			}()
		})
	}()
}

// sysdigStream shows a live probe inside the workspace.
func (w *workspace) sysdigStream(runner sysdigRunner, probe sysdigProbe, target sysdigTarget, args []string) {
	w.streamPanel("sysdig · "+probe.name+" · "+target.label, runner.display(args), "sysdig", func(ctx context.Context) *exec.Cmd { return sysdigCommand(ctx, runner, args) })
}

// streamPanel follows a long-lived command inside the workspace: the last 500
// lines follow as they arrive, scrolling up or Space pauses, g resumes, e
// exports the retained buffer and Escape stops the command.
func (w *workspace) streamPanel(title, commandLine, name string, build func(context.Context) *exec.Cmd) {
	focus := w.app.GetFocus()
	p := w.palette()
	ctx, cancel := context.WithCancel(w.ctx)
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderColor(tcell.GetColor(panelHue(p, 3))).SetBorderAttributes(tcell.AttrBold)
	view.SetTitleColor(tcell.GetColor(panelHue(p, 3)))
	view.SetText(fmt.Sprintf("[%s]%s\n\nStarting…[-]", p.muted, tview.Escape(clean(commandLine))))
	var snapshot logSnapshot
	paused, lines := false, 0
	setTitle := func() {
		state := "LIVE"
		if paused {
			state = "PAUSED · g follows"
		}
		if snapshot.ending != "" {
			state = "ENDED"
		}
		view.SetTitle(fmt.Sprintf(" %s · %s · %d lines · Space pause · e export · Esc stops ", tview.Escape(clean(title)), state, lines))
	}
	render := func() {
		if paused {
			setTitle()
			return
		}
		view.SetText(snapshot.display(logStyle{palette: w.palette()})).ScrollToEnd()
		setTitle()
	}
	close := func() {
		cancel()
		w.pages.RemovePage("stream-panel")
		w.app.SetFocus(focus)
	}
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		switch {
		case e.Key() == tcell.KeyEscape:
			close()
		case e.Rune() == ' ':
			paused = !paused
			render()
		case e.Rune() == 'g':
			paused = false
			render()
		case e.Rune() == 'e':
			w.reviewSnapshot(commandLine + "\n\n" + clean(snapshot.text) + snapshot.ending)
		case e.Key() == tcell.KeyUp || e.Key() == tcell.KeyPgUp || e.Rune() == 'k':
			paused = true
			setTitle()
			if e.Rune() == 'k' {
				return tcell.NewEventKey(tcell.KeyUp, 0, 0)
			}
			return e
		case e.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, 0)
		default:
			return e
		}
		return nil
	})
	view.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseScrollUp && view.InRect(event.Position()) {
			paused = true
			setTitle()
		}
		return action, event
	})
	setTitle()
	w.pages.AddPage("stream-panel", centered(view, 140, 42), true, true)
	w.app.SetFocus(view)
	style := &w.drawerLogStyle
	go streamCommand(ctx, build(ctx), name, func(text, ending string) {
		next := prepareLogSnapshot(text, ending, *style.Load())
		count := strings.Count(text, "\n")
		if ctx.Err() != nil {
			return
		}
		w.queue(func() {
			if ctx.Err() != nil {
				return
			}
			snapshot, lines = next, count
			render()
		})
	})
}

// followJournalPanel streams journalctl for one PID inside the workspace.
func (w *workspace) followJournalPanel(pid int, label string) {
	args := []string{"_PID=" + strconv.Itoa(pid), "--follow", "--lines", "100", "--no-pager", "--output=short-iso"}
	w.streamPanel("journal · "+label, "journalctl "+strings.Join(args, " "), "journalctl", func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "journalctl", args...)
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 2 * time.Second
		return cmd
	})
}

// runSysdigCollection collects a timed probe in the background under a
// cancellable overlay so the dashboard stays live; sudo was validated first.
func (w *workspace) runSysdigCollection(runner sysdigRunner, probe sysdigProbe, target sysdigTarget, args []string, capturePath string) {
	ctx, cancel := context.WithCancel(w.ctx)
	loading := textView().SetText(fmt.Sprintf("sysdig is collecting for %d seconds…\n%s\n\nEscape cancels and discards the capture.", probe.seconds, target.label))
	loading.SetBorder(true).SetTitle(" sysdig · " + probe.name + " ")
	loading.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			cancel()
			w.pages.RemovePage("sysdig-loading")
			w.app.SetFocus(w.table)
			return nil
		}
		return e
	})
	w.pages.AddPage("sysdig-loading", centered(loading, 100, 7), true, true)
	started := time.Now()
	go func() {
		output, err := collectSysdig(ctx, runner, probe, args)
		if ctx.Err() != nil {
			return
		}
		w.queue(func() {
			if ctx.Err() != nil {
				return
			}
			cancel()
			w.pages.RemovePage("sysdig-loading")
			header := fmt.Sprintf("%s\n%s · collected %d s from %s\n\n", runner.display(args), target.label, probe.seconds, started.Format("15:04:05"))
			body := strings.TrimSpace(clean(output))
			if err != nil {
				body = "Collection failed: " + err.Error() + "\n\n" + body
			} else if body == "" {
				body = "No events matched during the window."
			}
			if strings.Contains(probe.name, "Capture") && err == nil {
				body = "Capture written to " + capturePath + " (owned by root).\nRead it with: sudo sysdig -r " + capturePath + " [filter]\nSummaries: sudo sysdig -r " + capturePath + " -c topscalls\n\n" + body
			}
			w.traceResult("sysdig · "+probe.name+" · "+target.label, header+body)
		})
	}()
}

// traceResult shows collected output with export to a reviewed private file.
func (w *workspace) traceResult(title, body string) {
	focus := w.app.GetFocus()
	p := w.palette()
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	view.SetText(richOutput(body, 0, p))
	view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderColor(tcell.GetColor(panelHue(p, 3))).SetBorderAttributes(tcell.AttrBold)
	view.SetTitle(" " + tview.Escape(clean(title)) + " · e export · Esc returns ").SetTitleColor(tcell.GetColor(panelHue(p, 3)))
	close := func() { w.pages.RemovePage("trace-result"); w.app.SetFocus(focus) }
	view.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		switch {
		case e.Key() == tcell.KeyEscape:
			close()
		case e.Rune() == 'e':
			w.reviewSnapshot(body)
		case e.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, 0)
		case e.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, 0)
		default:
			return e
		}
		return nil
	})
	w.pages.AddPage("trace-result", centered(view, 132, 42), true, true)
	w.app.SetFocus(view)
}

// sysdigForProcess opens the palette for the selected process, with the
// children variant when the process has any in the current snapshot.
func (h *hostPage) sysdigForProcess() {
	process, err := h.selectedProcess()
	if err != nil && process.PID == 0 {
		h.w.message("Select a process", err.Error())
		return
	}
	children := false
	for _, candidate := range h.processes {
		if candidate.PPID == process.PID {
			children = true
			break
		}
	}
	if !children {
		h.w.sysdigPalette(sysdigProcessTarget(process, false), "sysdig")
		return
	}
	h.w.choose("sysdig-scope", "sysdig scope · PID "+fmt.Sprint(process.PID), []choice{
		{"This process only", "Filter proc.pid", func() { h.w.sysdigPalette(sysdigProcessTarget(process, false), "sysdig") }},
		{"Process and its children", "Filter proc.pid or proc.apid · workers and helpers included", func() { h.w.sysdigPalette(sysdigProcessTarget(process, true), "sysdig") }},
	})
}

// sysdigForWorkload opens the palette for the selected container or pod.
func (w *workspace) sysdigForWorkload(item workload) {
	if item.ID == "" {
		w.message("Select a workload", "Choose a container or pod first.")
		return
	}
	if isPod(item) {
		w.sysdigPalette(sysdigPodTarget(item), "sysdig")
		return
	}
	w.sysdigPalette(sysdigContainerTarget(item), "sysdig")
}
