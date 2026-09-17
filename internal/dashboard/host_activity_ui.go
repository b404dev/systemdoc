package dashboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type activityView struct {
	h                 *hostPage
	process           hostProcess
	view              *tview.TextView
	ctx               context.Context
	cancel            context.CancelFunc
	previous, current *activitySample
	first             *activitySample
	history           []float64
	journal           string
	// paused is read by the sampling goroutine and toggled on the UI goroutine.
	paused atomic.Bool
	note   string
	ticks  int
	focus  tview.Primitive
}

// openProcessActivity replaces the static record with a live view for the
// selected process. It samples one PID every two seconds regardless of the
// slower whole-table interval, and stops when the process exits or the PID
// is reused by another process.
func (h *hostPage) openProcessActivity() {
	process, err := h.selectedProcess()
	if err != nil && process.PID == 0 {
		h.inspectRecord()
		return
	}
	if usesLaunchd() {
		h.inspectRecord()
		return
	}
	if _, statErr := os.Stat("/proc/" + strconv.Itoa(process.PID)); statErr != nil {
		h.w.message("Process unavailable", fmt.Sprintf("PID %d is not present in /proc: %v", process.PID, statErr))
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	a := &activityView{h: h, process: process, ctx: ctx, cancel: cancel, focus: h.w.app.GetFocus()}
	p := h.w.palette()
	a.view = textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	a.view.SetBackgroundColor(tcell.GetColor(p.surface)).SetBorder(true).SetBorderColor(tcell.GetColor(panelHue(p, 3))).SetBorderAttributes(tcell.AttrBold)
	a.view.SetTitle(" " + h.w.iconLabel(iconProcesses, "PROCESS ACTIVITY · "+strconv.Itoa(process.PID)+" · Esc returns ")).SetTitleColor(tcell.GetColor(panelHue(p, 3)))
	a.view.SetInputCapture(a.input)
	a.journal = "reading journal…"
	a.render()
	h.w.pages.AddPage("process-activity", centered(a.view, 132, 42), true, true)
	h.w.app.SetFocus(a.view)
	go a.loop()
}

func (a *activityView) close() {
	a.cancel()
	a.h.w.pages.RemovePage("process-activity")
	a.h.w.app.SetFocus(a.focus)
}

func (a *activityView) children() []hostProcess {
	var result []hostProcess
	for _, candidate := range a.h.processes {
		if candidate.PPID == a.process.PID {
			result = append(result, candidate)
		}
	}
	return result
}

func (a *activityView) render() {
	row, col := a.view.GetScrollOffset()
	a.view.SetText(renderProcessActivity(a.h.w.palette(), a.h.w.settings.GraphMode, a.process, a.previous, a.current, a.history, a.journal, a.children(), a.paused.Load(), a.note, a.h.w.hostUsage))
	a.view.ScrollTo(row, col)
}

// loop samples off the UI thread and publishes results through the queue.
// The journal is a subprocess, so it is refreshed every third tick.
func (a *activityView) loop() {
	a.sample(true)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.paused.Load() {
				continue
			}
			a.ticks++
			a.sample(a.ticks%3 == 0)
		}
	}
}

func (a *activityView) sample(withJournal bool) {
	current, err := sampleProcessActivity("/proc", a.process.PID)
	journal := ""
	if withJournal && err == nil {
		journal = processJournal(a.ctx, a.process.PID, 14)
	}
	if a.ctx.Err() != nil {
		return
	}
	a.h.w.queue(func() {
		if a.ctx.Err() != nil {
			return
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				a.note = "The process has exited. The last sample is kept for reference."
			} else {
				a.note = "Sampling failed: " + firstLine(err.Error())
			}
			a.cancel()
			a.render()
			return
		}
		if a.first == nil {
			a.first = &current
		} else if a.first.StartTime != current.StartTime {
			a.note = "PID " + strconv.Itoa(a.process.PID) + " now belongs to a different process. Sampling stopped."
			a.cancel()
			a.render()
			return
		}
		a.previous, a.current = a.current, &current
		if rates := computeActivityRates(a.previous, a.current); rates.Valid {
			a.history = append(a.history, rates.CPU)
			if len(a.history) > 40 {
				a.history = a.history[len(a.history)-40:]
			}
		}
		if journal != "" {
			a.journal = journal
		}
		a.render()
	})
}

func (a *activityView) input(e *tcell.EventKey) *tcell.EventKey {
	switch {
	case e.Key() == tcell.KeyEscape:
		a.close()
	case e.Rune() == ' ':
		a.paused.Store(!a.paused.Load())
		a.render()
	case e.Rune() == 'r':
		go a.sample(true)
	case e.Rune() == 'G':
		a.h.w.cycleGraphMode()
		a.render()
	case e.Rune() == 'f':
		a.h.w.followJournalPanel(a.process.PID, fmt.Sprintf("PID %d · %s", a.process.PID, shortCommand(a.process.Command)))
	case e.Rune() == 'T':
		a.h.sysdigForProcess()
	case e.Rune() == 'K' || e.Key() == tcell.KeyF9:
		a.close()
		a.h.processActions()
	case e.Rune() == 'n':
		a.close()
		a.h.ports()
	case e.Rune() == 's':
		a.close()
		a.h.service()
	case e.Rune() == 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, 0)
	case e.Rune() == 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, 0)
	default:
		return e
	}
	return nil
}
