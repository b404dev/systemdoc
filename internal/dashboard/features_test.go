package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSavedViewRestoresWorkspaceAndRoundTrips(t *testing.T) {
	w := testWorkspace()
	w.settings.Layout = "stacked"
	w.settings.PaneRatio = 61
	w.quickFilter = 2
	w.sortMode = 3
	w.tab = 2
	w.drawerOpen = true
	w.filters[0] = "worker"
	saved := w.captureView("Failures")
	data, err := json.Marshal(settings{Views: []savedView{saved}})
	if err != nil {
		t.Fatal(err)
	}
	var restored settings
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	w.quickFilter = 0
	w.sortMode = 0
	w.filters[0] = ""
	w.settings.Layout = "auto"
	w.drawerOpen = false
	w.tab = 0
	if err = w.applySavedView(restored.Views[0]); err != nil {
		t.Fatal(err)
	}
	if w.current().ID != "worker.service" || w.quickFilter != 2 || w.sortMode != 3 || w.settings.Layout != "stacked" || w.settings.PaneRatio != 61 || !w.drawerOpen || w.tab != 2 || w.search.GetText() != "worker" {
		t.Fatalf("view not restored: %+v", w.captureView("Failures"))
	}
}

func TestControlDeckDocumentsFiveSuites(t *testing.T) {
	w := testWorkspace()
	w.controlDeck()
	_, page := w.pages.GetFrontPage()
	panel := page.(*overlay).content.(*tview.Flex)
	list := panel.GetItem(1).(*tview.List)
	if list.GetItemCount() != 5 {
		t.Fatalf("control deck has %d suites", list.GetItemCount())
	}
	for i, want := range []string{"SERVICES", "CONTAINERS", "NETWORK", "PROCESSES", "STORAGE"} {
		name, description := list.GetItemText(i)
		if !strings.Contains(name, want) || strings.TrimSpace(description) == "" {
			t.Fatalf("suite %d is not self-documenting: %q / %q", i, name, description)
		}
	}
}

func TestPaneResizeChangesGeometryAndIsBounded(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	w.app.ForceDraw()
	_, _, before, _ := w.table.GetRect()
	w.resizePanes(10)
	w.app.ForceDraw()
	_, _, after, _ := w.table.GetRect()
	if after <= before || w.settings.PaneRatio != 56 {
		t.Fatal("inventory pane did not grow", before, after, w.settings.PaneRatio)
	}
	w.resizePanes(100)
	if w.settings.PaneRatio != 70 {
		t.Fatal("pane ratio exceeded upper bound", w.settings.PaneRatio)
	}
	w.resizePanes(-100)
	if w.settings.PaneRatio != 30 {
		t.Fatal("pane ratio exceeded lower bound", w.settings.PaneRatio)
	}
}
func TestSavedViewRejectsInvalidAndForeignEndpoint(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "local")
	w := testWorkspace()
	for _, v := range []savedView{{Name: "bad", Mode: 3}, {Name: "bad", Sort: 99}, {Name: "bad", Tab: -1}, {Name: "bad", QuickFilter: 99}, {Name: "remote", Mode: 1, Endpoint: "context:production"}} {
		if err := w.applySavedView(v); err == nil {
			t.Fatalf("accepted %+v", v)
		}
		if w.mode != 0 {
			t.Fatal("invalid view mutated mode")
		}
	}
}
func TestSavedViewsPersistWithoutLosingPreferences(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := settings{Theme: "Crypt", RefreshSeconds: 10, Views: []savedView{{Name: "Failures", Filter: "state:failed", Layout: "auto"}}}
	if err := writeSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := readSettings()
	if err != nil || len(got.Views) != 1 || got.Views[0].Filter != "state:failed" || got.Theme != "Crypt" {
		t.Fatal(got, err)
	}
}
func TestLogSearchRangesRegexAndMergedContext(t *testing.T) {
	raw := "2026-09-06T10:00:00Z before\n2026-09-06T10:01:00Z ERROR one\ntrace context\n2026-09-06T10:02:00Z failure two\n2026-09-06T10:03:00Z after\n2026-09-06T10:04:00Z ERROR outside"
	since, _ := parseLogTime("2026-09-06T10:01:00Z")
	until, _ := parseLogTime("2026-09-06T10:02:00Z")
	out, hits, err := searchRetainedLogs(raw, logSearchOptions{Query: "error|failure", Regex: true, Since: since, Until: until, Context: 1})
	if err != nil || len(hits) != 2 || hits[0] != 1 || hits[1] != 3 || strings.Count(out, "trace context") != 1 || strings.Contains(out, "outside") {
		t.Fatal(out, hits, err)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatal("context missing", out)
	}
	for _, o := range []logSearchOptions{{Query: "[", Regex: true}, {Context: 21}, {Since: until, Until: since}} {
		if _, _, err = searchRetainedLogs(raw, o); err == nil {
			t.Fatal("invalid search accepted", o)
		}
	}
	out, hits, err = searchRetainedLogs("[red] ERROR\nother\nerror", logSearchOptions{Query: "ERROR"})
	if err != nil || len(hits) != 2 || !strings.Contains(out, "[red]") {
		t.Fatal(out, hits, err)
	}
	_, hits, err = searchRetainedLogs("", logSearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatal(hits, err)
	}
}
func TestLogSearchJournalTimeAndUnknownTimestamps(t *testing.T) {
	at, err := parseLogTime("2026-09-06T11:00:00+0100")
	if err != nil {
		t.Fatal(err)
	}
	_, hits, err := searchRetainedLogs("untimed error\n2026-09-06T11:00:00+0100 error", logSearchOptions{Query: "error", Since: at, Until: at})
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
}
func TestTimersUseScopeAndHandleUnscheduled(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" != "--user" ] || [ "$2" != "list-timers" ]; then exit 2; fi
printf '%s' '[{"unit":"off.timer","activates":"off.service","next":null,"last":null},{"unit":"later.timer","next":2000000000000000},{"unit":"soon.timer","next":1000000000000000},{"unit":"never.timer","next":18446744073709551615}]'
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	rows, err := listTimers(context.Background(), true)
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	if rows[0].Unit != "soon.timer" || rows[1].Unit != "later.timer" || timerTime(rows[2].Next) != "—" || timerDue(nil, time.Now()) != "unscheduled" {
		t.Fatal(rows)
	}
	if _, err = listTimers(context.Background(), false); err == nil {
		t.Fatal("backend error hidden")
	}
}
func TestSnapshotCollectsExactTargetAndOptionalSections(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("SNAPSHOT_CALLS", calls)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SNAPSHOT_CALLS"
case "$*" in
 *status*) printf 'service status\n'; exit 3 ;;
 *show*) printf 'CPUUsageNSec=123\nMemoryCurrent=456\n' ;;
 *cat*) printf 'CONFIG_SECRET\n' ;;
 *--unit*) printf 'RECENT_LOG\n' ;;
 *) exit 2 ;;
esac
`
	for _, name := range []string{"systemctl", "journalctl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	request := snapshotRequest{Item: workload{ID: "chosen.service", Name: "chosen"}, User: true, Host: "fixture", At: time.Now(), Activity: []activityEvent{{user: true, id: "chosen.service", text: "failed"}, {user: true, id: "other.service", text: "OTHER_EVENT"}}}
	report := collectSnapshot(context.Background(), request)
	if !strings.Contains(report, "service status") || !strings.Contains(report, "MemoryCurrent=456") || strings.Contains(report, "CONFIG_SECRET") || strings.Contains(report, "RECENT_LOG") || strings.Contains(report, "OTHER_EVENT") {
		t.Fatal(report)
	}
	request.Logs = true
	request.Config = true
	report = collectSnapshot(context.Background(), request)
	if !strings.Contains(report, "CONFIG_SECRET") || !strings.Contains(report, "RECENT_LOG") {
		t.Fatal(report)
	}
	commands, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
		if !strings.HasPrefix(line, "--user ") || !strings.Contains(line, "chosen.service") {
			t.Fatal("incorrect scope or target", line)
		}
	}
	target := filepath.Join(dir, "report.txt")
	if err = writeNewFile(target, report, 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err = writeNewFile(target, "replacement", 0600); err == nil {
		t.Fatal("overwrote snapshot")
	}
}
func TestNewFeatureDialogsDrawAndEscape(t *testing.T) {
	for _, open := range []func(*workspace){func(w *workspace) { w.controlDeck() }, func(w *workspace) { w.savedViews() }, func(w *workspace) { w.saveViewDialog() }, func(w *workspace) { w.timers() }, func(w *workspace) { w.logSearch() }, func(w *workspace) { w.snapshotDialog() }, func(w *workspace) { w.reviewSnapshot("example\n[red] literal text") }} {
		for _, size := range [][2]int{{80, 24}, {120, 30}, {40, 12}} {
			w := testWorkspace()
			open(w)
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			screen.SetSize(size[0], size[1])
			w.pages.SetRect(0, 0, size[0], size[1])
			w.pages.Draw(screen)
			_, page := w.pages.GetFrontPage()
			handler := page.InputHandler()
			if handler != nil {
				handler(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
			}
			if front, _ := w.pages.GetFrontPage(); front != "main" {
				t.Fatalf("Escape left %s open", front)
			}
			screen.Fini()
		}
	}
}

func TestNestedChooserAndMessageRestoreFocus(t *testing.T) {
	w := testWorkspace()
	list := tview.NewList().AddItem("timer", "", 0, nil)
	w.pages.AddPage("timers", list, true, true)
	w.choose("timer-actions", "Timer", nil)
	_, page := w.pages.GetFrontPage()
	page.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if !list.HasFocus() {
		t.Fatal("closing timer actions lost timer focus")
	}
	w.message("Example", "Example")
	_, page = w.pages.GetFrontPage()
	page.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if !list.HasFocus() {
		t.Fatal("closing a message lost underlying focus")
	}
}

func TestConfirmationTakesFocusAndKeyboardRunWorks(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	w.app.SetFocus(w.table)
	ran := false
	w.confirm("Terminate · PID 42", "review exact target", func() { ran = true })
	front, page := w.pages.GetFrontPage()
	if front != "confirm" {
		t.Fatalf("confirmation did not open: %s", front)
	}
	if _, ok := w.app.GetFocus().(*tview.TextView); !ok {
		t.Fatalf("confirmation did not take keyboard focus: %T", w.app.GetFocus())
	}
	dialog, ok := page.(*overlay)
	if !ok {
		t.Fatalf("confirmation is not a bounded overlay: %T", page)
	}
	w.app.ForceDraw()
	x, y, width, height := dialog.content.GetRect()
	if x <= 0 || y <= 0 || width >= 120 || height >= 30 {
		t.Fatalf("confirmation obscures the workspace: rect %d,%d %dx%d", x, y, width, height)
	}
	panel := dialog.content.(*tview.Flex)
	if title := panel.GetTitle(); !strings.Contains(title, "SYSTEMDOC") || !strings.Contains(title, "APPROVAL") {
		t.Fatalf("confirmation does not use branded action chrome: %q", title)
	}
	page.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'y', 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if !ran {
		t.Fatal("y did not run the reviewed action")
	}
	if front, _ := w.pages.GetFrontPage(); front != "main" || !w.table.HasFocus() {
		t.Fatal("completed confirmation did not restore table focus")
	}
}

func TestSnapshotEditorFocusCycle(t *testing.T) {
	w := testWorkspace()
	w.reviewSnapshot("example")
	_, page := w.pages.GetFrontPage()
	panel := page.(*overlay).content.(*tview.Flex)
	editor := panel.GetItem(0)
	file := panel.GetItem(1)
	buttons := panel.GetItem(2).(*tview.Form)
	press := func(key tcell.Key) {
		page.InputHandler()(tcell.NewEventKey(key, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	}
	press(tcell.KeyTab)
	if !file.HasFocus() {
		t.Fatal("cannot reach filename")
	}
	press(tcell.KeyTab)
	if !buttons.HasFocus() {
		t.Fatal("cannot reach save")
	}
	press(tcell.KeyBacktab)
	if !file.HasFocus() {
		t.Fatal("cannot return to filename")
	}
	press(tcell.KeyBacktab)
	if !editor.HasFocus() {
		t.Fatal("cannot return to report")
	}
	press(tcell.KeyBacktab)
	press(tcell.KeyTab)
	if !editor.HasFocus() {
		t.Fatal("cannot cycle buttons back to report")
	}
}
func TestLogResultNavigationInApplication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorkspace(ctx, false)
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	defer func() { cancel(); w.app.Stop(); <-done }()
	raw := "error first\n" + strings.Repeat("context\n", 30) + "error second"
	w.app.QueueUpdateDraw(func() {
		w.showLogResults(logSnapshot{text: raw}, logSearchOptions{Query: "error", Context: 20}, w.table)
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		ready := false
		w.app.QueueUpdateDraw(func() {
			_, page := w.pages.GetFrontPage()
			panel := page.(*overlay).content.(*tview.Flex)
			ready = strings.Contains(panel.GetItem(1).(*tview.TextView).GetText(true), "Match 1/2")
		})
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("search never completed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.app.QueueUpdateDraw(func() {
		_, page := w.pages.GetFrontPage()
		panel := page.(*overlay).content.(*tview.Flex)
		page.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'n', 0), func(p tview.Primitive) { w.app.SetFocus(p) })
		if !strings.Contains(panel.GetItem(1).(*tview.TextView).GetText(true), "Match 2/2") {
			t.Error("next match failed")
		}
		page.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'N', 0), func(p tview.Primitive) { w.app.SetFocus(p) })
		if !strings.Contains(panel.GetItem(1).(*tview.TextView).GetText(true), "Match 1/2") {
			t.Error("previous match failed")
		}
	})
}
