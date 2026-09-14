package dashboard

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type logSearchOptions struct {
	Query        string
	Regex        bool
	Since, Until time.Time
	Context      int
}

func parseLogTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05.999999-0700", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("use RFC3339 or local YYYY-MM-DD HH:MM:SS")
}

// logStampSource skips the `[pod/namespace/name/container] ` prefix that
// kubectl logs --prefix puts before each timestamp, so time bounds apply to
// pod lines as well as journal and Docker lines.
func logStampSource(line string) string {
	if strings.HasPrefix(line, "[pod/") {
		if end := strings.Index(line, "] "); end > 0 {
			return line[end+2:]
		}
	}
	return line
}

func (o logSearchOptions) matcher() (func(string) bool, error) {
	if o.Context < 0 || o.Context > 20 {
		return nil, fmt.Errorf("context must be 0–20 lines")
	}
	if !o.Since.IsZero() && !o.Until.IsZero() && o.Until.Before(o.Since) {
		return nil, fmt.Errorf("until must be at or after since")
	}
	if o.Regex {
		r, err := regexp.Compile("(?i)" + o.Query)
		if err != nil {
			return nil, err
		}
		return r.MatchString, nil
	}
	return func(line string) bool { return strings.Contains(strings.ToLower(line), strings.ToLower(o.Query)) }, nil
}

// Hits are output row offsets, with context merged so overlapping matches never duplicate lines.
func searchRetainedLogs(raw string, o logSearchOptions) (string, []int, error) {
	match, err := o.matcher()
	if err != nil {
		return "", nil, err
	}
	lines := strings.Split(strings.TrimSuffix(clean(raw), "\n"), "\n")
	if raw == "" {
		return "", nil, nil
	}
	hits := make([]bool, len(lines))
	include := make([]bool, len(lines))
	for i, line := range lines {
		if !match(line) {
			continue
		}
		if !o.Since.IsZero() || !o.Until.IsZero() {
			stamp := timestamp.FindString(logStampSource(line))
			at, err := parseLogTime(stamp)
			if err != nil || at.IsZero() || !o.Since.IsZero() && at.Before(o.Since) || !o.Until.IsZero() && at.After(o.Until) {
				continue
			}
		}
		hits[i] = true
		for j := max(0, i-o.Context); j <= min(len(lines)-1, i+o.Context); j++ {
			include[j] = true
		}
	}
	var output []string
	var offsets []int
	previous := -1
	for i, line := range lines {
		if !include[i] {
			continue
		}
		if previous >= 0 && i > previous+1 {
			output = append(output, "      …")
		}
		mark := " "
		if hits[i] {
			mark = "›"
			offsets = append(offsets, len(output))
		}
		output = append(output, fmt.Sprintf("%s %5d  %s", mark, i+1, line))
		previous = i
	}
	return strings.Join(output, "\n"), offsets, nil
}
func (w *workspace) logSearch() {
	drawer := w.logDrawer.HasFocus()
	focus := w.app.GetFocus()
	snapshot := w.inspectorLog
	if drawer {
		snapshot = w.drawerLog
	}
	form := tview.NewForm().AddInputField("Find (case insensitive)", w.logQuery, 55, nil, nil).
		AddCheckbox("Regular expression", false, nil).
		AddInputField("Since (optional)", "", 35, nil, nil).
		AddInputField("Until (optional)", "", 35, nil, nil).
		AddInputField("Context lines (0–20)", "2", 5, tview.InputFieldInteger, nil)
	form.SetBorder(true).SetTitle(" Search retained logs · RFC3339 or local YYYY-MM-DD HH:MM:SS ")
	close := func() { w.pages.RemovePage("log-search"); w.app.SetFocus(focus) }
	form.AddButton("Search history", func() {
		since, err := parseLogTime(form.GetFormItem(2).(*tview.InputField).GetText())
		if err != nil {
			w.message("Invalid since", err.Error())
			return
		}
		until, err := parseLogTime(form.GetFormItem(3).(*tview.InputField).GetText())
		if err != nil {
			w.message("Invalid until", err.Error())
			return
		}
		count, err := strconv.Atoi(form.GetFormItem(4).(*tview.InputField).GetText())
		if err != nil {
			w.message("Invalid context", "Choose 0–20 lines.")
			return
		}
		o := logSearchOptions{form.GetFormItem(0).(*tview.InputField).GetText(), form.GetFormItem(1).(*tview.Checkbox).IsChecked(), since, until, count}
		if _, err := o.matcher(); err != nil {
			w.message("Invalid search", err.Error())
			return
		}
		close()
		w.showLogResults(snapshot, o, focus)
	}).AddButton("Live literal filter", func() {
		if drawer {
			w.message("Live filter", "Use Search history for the drawer. The live literal filter applies to the log inspector.")
			return
		}
		if form.GetFormItem(1).(*tview.Checkbox).IsChecked() || form.GetFormItem(2).(*tview.InputField).GetText() != "" || form.GetFormItem(3).(*tview.InputField).GetText() != "" {
			w.message("Use Search history", "Regex and time ranges apply to retained history. Clear those fields to apply a live literal filter.")
			return
		}
		w.logQuery = form.GetFormItem(0).(*tview.InputField).GetText()
		w.updateLogStyles()
		w.logPaused = false
		w.renderInspectorLogs()
		close()
	}).AddButton("Cancel", close)
	form.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		return e
	})
	w.pages.AddPage("log-search", centered(form, 100, 19), true, true)
}
func (w *workspace) showLogResults(snapshot logSnapshot, o logSearchOptions, focus tview.Primitive) {
	view := textView().SetScrollable(true).SetWrap(false).SetText("Searching retained snapshot…")
	view.SetBorder(true).SetTitle(" LOG SEARCH · frozen retained buffer · Escape returns ")
	status := textView().SetText(" n next · N previous · context may extend outside the time range")
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(status, 2, 0, false)
	var hits []int
	index := -1
	closed := false
	navigate := func(step int) {
		if len(hits) == 0 {
			return
		}
		index = (index + step + len(hits)) % len(hits)
		view.ScrollTo(hits[index], 0)
		status.SetText(fmt.Sprintf(" Match %d/%d · n next · N previous · Escape returns\n › match · numbered context · retained logs only; no older logs fetched", index+1, len(hits)))
	}
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			closed = true
			w.pages.RemovePage("log-results")
			w.app.SetFocus(focus)
			return nil
		}
		if e.Rune() == 'n' {
			navigate(1)
			return nil
		}
		if e.Rune() == 'N' {
			navigate(-1)
			return nil
		}
		return e
	})
	w.pages.AddPage("log-results", centered(panel, 140, 36), true, true)
	go func() {
		raw, offsets, err := searchRetainedLogs(snapshot.text, o)
		w.queue(func() {
			if closed {
				return
			}
			if err != nil {
				view.SetText(err.Error())
				return
			}
			hits = offsets
			if len(hits) == 0 {
				view.SetText("No matching retained log lines.\n\nTime ranges require a timestamp at the start of a matching line. This search does not fetch older logs.")
				return
			}
			view.SetText(raw)
			navigate(1)
		})
	}()
}
