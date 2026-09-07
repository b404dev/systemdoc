package dashboard

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (w *workspace) streamLogs(ctx context.Context, mode int, user bool, item workload, generation int) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(120 * time.Millisecond):
	}
	followLogs(ctx, mode, user, item, func(text, final string) {
		snapshot := prepareLogSnapshot(text, final, *w.inspectorLogStyle.Load())
		if ctx.Err() != nil {
			return
		}
		w.queue(func() {
			if generation != w.generation || ctx.Err() != nil {
				return
			}
			w.inspectorLog = snapshot
			w.renderInspectorLogs()
		})
	})
}

// followLogs batches bounded CLI output; callers choose how to display it.
func followLogs(ctx context.Context, mode int, user bool, item workload, update func(string, string)) {
	name, args := "journalctl", []string{"--unit", item.ID, "--follow", "--lines", "150", "--no-pager", "--output=short-iso"}
	if mode == 1 {
		name = "docker"
		args = []string{"logs", "--follow", "--tail", "150", "--timestamps", item.ID}
	} else if user {
		args = append([]string{"--user"}, args...)
	}
	var output tailBuffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &output
	cmd.Stderr = &output
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	previous := ""
	published := false
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-done:
			message := "\n[Log stream ended]"
			if err != nil {
				message = "\nLog stream failed: " + err.Error()
			}
			update(output.String(), message)
			return
		case <-ticker.C:
			text := output.String()
			if !published || text != previous {
				published = true
				previous = text
				update(text, "")
			}
		}
	}
}

func (w *workspace) pauseLogsOnScroll(view *tview.TextView, drawer bool) {
	view.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseScrollUp && view.InRect(event.Position()) {
			w.app.SetFocus(view)
			if drawer {
				w.drawerPaused = true
				view.SetTitle(" LOGS · paused · g follows · L closes ")
			} else if w.tab == 1 {
				w.logPaused = true
				view.SetTitle(" Logs · paused · g resumes ")
			}
		}
		return action, event
	})
}

// Snapshots remain bounded by the stream buffer. Keeping the latest snapshot
// while paused lets follow resume even after the backend stream has ended.
type logStyle struct {
	palette palette
	query   string
}

type logSnapshot struct {
	text, ending string
	formatted    string
	style        logStyle
}

func prepareLogSnapshot(text, ending string, style logStyle) logSnapshot {
	return logSnapshot{text: text, ending: ending, style: style, formatted: richOutput(recentLogLines(filterLogLines(text, style.query), 500)+ending, 1, style.palette)}
}

func (w *workspace) updateLogStyles() {
	w.inspectorLogStyle.Store(&logStyle{palette: w.palette(), query: w.logQuery})
	w.drawerLogStyle.Store(&logStyle{palette: w.palette()})
}

func (snapshot logSnapshot) display(style logStyle) string {
	if snapshot.formatted != "" && snapshot.style == style {
		return snapshot.formatted
	}
	// Only explicit theme/filter changes or small, unformatted test snapshots
	// take this path. Streaming updates arrive preformatted by their worker.
	return richOutput(recentLogLines(filterLogLines(snapshot.text, style.query), 500)+snapshot.ending, 1, style.palette)
}

func (w *workspace) renderInspectorLogs() {
	if w.logPaused {
		return
	}
	w.detail.SetText(w.inspectorLog.display(logStyle{palette: w.palette(), query: w.logQuery})).ScrollToEnd()
	title := " Logs · LIVE · last 500 lines · h history · Space pause "
	if w.inspectorLog.ending != "" {
		title = " Logs · stream ended · l reconnects "
	}
	w.detail.SetTitle(title)
}

func (w *workspace) renderDrawerLogs() {
	if w.drawerPaused || !w.drawerOpen {
		return
	}
	w.logDrawer.SetText(w.drawerLog.display(logStyle{palette: w.palette()})).ScrollToEnd()
	state := "LIVE · h history"
	if w.drawerLog.ending != "" {
		state = "ENDED · L close/reopen"
	}
	w.logDrawer.SetTitle(" " + state + " · " + tview.Escape(clean(w.current().Name)) + " · L closes ")
}

// Bound live layout work while keeping the complete retained buffer available
// to history and export. Count backwards without splitting the whole buffer.
func recentLogLines(text string, limit int) string {
	end := len(text)
	if end > 0 && text[end-1] == '\n' {
		end--
	}
	for i := 0; i < limit; i++ {
		previous := strings.LastIndexByte(text[:end], '\n')
		if previous < 0 {
			return text
		}
		end = previous
	}
	return text[end+1:]
}
