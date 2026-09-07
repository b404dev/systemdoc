package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestBackgroundListDoesNotWaitForStatsAndSwitchReusesJob(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	started := filepath.Join(dir, "started")
	t.Setenv("STATS_RELEASE", release)
	t.Setenv("STATS_STARTED", started)
	script := `#!/bin/sh
case "$1" in
 ps) printf '%s\n' '{"ID":"abc","Names":"web","State":"running","Status":"Up"}' ;;
 stats)
  : > "$STATS_STARTED"
  while [ ! -f "$STATS_RELEASE" ]; do /usr/bin/sleep 0.01; done
  printf '%s\n' '{"ID":"abc","CPUPerc":"25%","MemUsage":"32MiB / 96MiB"}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorkspace(ctx, false)
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	defer func() { cancel(); w.app.Stop(); <-done }()
	w.app.QueueUpdateDraw(func() { w.startInventory(1) })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stats worker did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var ready, reused bool
	elapsed := time.Now()
	w.app.QueueUpdateDraw(func() {
		ready = len(w.items[1]) == 1 && w.items[1][0].CPU == ""
		job := w.inventoryJobs[1]
		w.startInventory(1)
		w.switchMode(1)
		reused = w.inventoryJobs[1] == job && len(w.visible) == 1 && w.current().ID == "abc"
	})
	if !ready {
		t.Fatal("container list was held behind resource sampling")
	}
	if !reused {
		t.Fatal("mode switch did not reuse the cached list and in-flight job")
	}
	if time.Since(elapsed) > time.Second {
		t.Fatal("mode switch waited for blocked stats")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		complete := false
		w.app.QueueUpdateDraw(func() { complete = w.inventoryJobs[1] == nil && w.items[1][0].CPU == "25%" })
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background resource result was not published")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestActiveOnlySurvivesModeSwitchAndIsSaved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := testWorkspace()
	w.items[1] = []workload{{ID: "container", Name: "container", State: "running"}}
	w.toggleActiveOnly()
	if len(w.visible) != 1 || w.current().ID != "nginx.service" {
		t.Fatal("active-only filter includes inactive services")
	}
	w.switchMode(1)
	if w.quickFilter != 0 {
		t.Fatal("Docker inherited the systemd filter")
	}
	w.switchMode(0)
	if w.quickFilter != 1 || len(w.visible) != 1 {
		t.Fatal("systemd active-only was lost on switching modes")
	}
	settings, err := readSettings()
	if err != nil || !settings.SystemdActiveOnly {
		t.Fatal("active-only was not persisted", err)
	}
	w.toggleActiveOnly()
	if len(w.visible) != 2 {
		t.Fatal("all services were not restored")
	}
}

func TestCachedDetailAppearsWithoutBackendQuery(t *testing.T) {
	w := testWorkspace()
	w.selected[0] = "nginx.service"
	w.cacheDetail(detailKey{mode: 0, id: "nginx.service", tab: 0}, "cached service overview")
	w.showDetail()
	if w.detail.GetText(true) != "cached service overview" || w.detailPending {
		t.Fatal("fresh cached inspector was not used")
	}
}

func TestEmptyFilterThenRestoreRefreshesInspector(t *testing.T) {
	w := testWorkspace()
	w.cacheDetail(detailKey{mode: 0, id: "nginx.service", tab: 0}, "nginx overview")
	w.cacheDetail(detailKey{mode: 0, id: "worker.service", tab: 0}, "worker overview")
	selected := w.current().ID
	w.search.SetText("no-such-workload")
	if w.current().ID != "" {
		t.Fatal("filter should be empty")
	}
	w.search.SetText("")
	if w.current().ID != selected || w.detail.GetText(true) == "" || strings.Contains(w.detail.GetText(true), "No matching") {
		t.Fatal("restoring filter left stale inspector")
	}
}

func TestLiveLogWindowRetainsOlderHistoryAndFiltersBeforeWindowing(t *testing.T) {
	text := "ERROR old-marker\n" + strings.Repeat("INFO recent heartbeat\n", 600)
	snapshot := prepareLogSnapshot(text, "", logStyle{palette: themes[0]})
	if strings.Contains(snapshot.formatted, "old-marker") {
		t.Fatal("live rendering was not bounded")
	}
	if !strings.Contains(snapshot.text, "old-marker") {
		t.Fatal("retained history was lost")
	}
	filtered := snapshot.display(logStyle{palette: themes[0], query: "old-marker"})
	if !strings.Contains(filtered, "old-marker") {
		t.Fatal("filter must search retained history, not just the live window")
	}
	for _, test := range []struct {
		raw, want string
		limit     int
	}{
		{"a\nb\nc\n", "b\nc\n", 2},
		{"a\nb\nc", "b\nc", 2},
		{"a\nb", "a\nb", 5},
		{"", "", 500},
	} {
		if got := recentLogLines(test.raw, test.limit); got != test.want {
			t.Fatalf("got %q want %q", got, test.want)
		}
	}
}

func TestInventoryRefreshKeepsReadingFocusAndScroll(t *testing.T) {
	for _, mode := range []int{0, 1} {
		for _, width := range []int{80, 120} {
			w := testWorkspace()
			w.mode = mode
			w.items[mode] = []workload{{ID: "reader", Name: "reader", State: "running"}}
			w.selected[mode] = "reader"
			content := strings.Repeat(strings.Repeat("x", 200)+"\n", 100)
			w.cacheDetail(detailKey{mode: mode, id: "reader", tab: 0}, content)
			w.renderTable()
			screen := tcell.NewSimulationScreen("UTF-8")
			w.app.SetScreen(screen)
			screen.SetSize(width, 40)
			w.toggleLogDrawer()
			for _, pane := range []tview.Primitive{w.detail, w.logDrawer, w.search} {
				w.app.SetFocus(pane)
				w.detail.SetWrap(false).SetText(content).ScrollTo(12, 5)
				w.logDrawer.SetText(content).ScrollTo(10, 3)
				w.app.ForceDraw()
				for _, complete := range []bool{false, true, false, true} {
					w.publishInventory(mode, append([]workload(nil), w.items[mode]...), complete)
					w.app.ForceDraw()
					if w.app.GetFocus() != pane {
						t.Fatalf("refresh moved focus (mode %d, width %d, pane %T, complete %t)", mode, width, pane, complete)
					}
					row, _ := w.detail.GetScrollOffset()
					drawerRow, drawerCol := w.logDrawer.GetScrollOffset()
					if row != 12 || drawerRow != 10 || drawerCol != 3 {
						t.Fatalf("refresh moved reading position: inspector=%d drawer=%d,%d", row, drawerRow, drawerCol)
					}
				}
			}
			first, editing := tview.NewInputField(), tview.NewInputField()
			overlay := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(first, 1, 0, true).AddItem(editing, 1, 0, false)
			w.pages.AddPage("editing", overlay, true, true)
			w.app.SetFocus(editing)
			w.publishInventory(mode, append([]workload(nil), w.items[mode]...), true)
			if w.app.GetFocus() != editing {
				t.Fatal("refresh reset the focused field in an overlay")
			}
			screen.Fini()
		}
	}
}

func TestSplashDismissalReturnsToDashboardOnce(t *testing.T) {
	w := testWorkspace()
	w.splash()
	w.dismissSplash()
	if w.pages.HasPage("splash") || !w.table.HasFocus() {
		t.Fatal("startup splash did not return focus to the dashboard")
	}
	w.app.SetFocus(w.detail)
	w.dismissSplash()
	if !w.detail.HasFocus() {
		t.Fatal("dismissing an absent splash changed focus")
	}
}

func TestSlowDetailRefreshKeepsLatestReadingPosition(t *testing.T) {
	dir := t.TempDir()
	release, started := filepath.Join(dir, "release"), filepath.Join(dir, "started")
	fixture := filepath.Join(dir, "output")
	content := strings.Repeat(strings.Repeat("x", 200)+"\n", 100)
	if err := os.WriteFile(fixture, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DETAIL_RELEASE", release)
	t.Setenv("DETAIL_STARTED", started)
	t.Setenv("DETAIL_OUTPUT", fixture)
	script := `#!/bin/sh
: > "$DETAIL_STARTED"
while [ ! -f "$DETAIL_RELEASE" ]; do /usr/bin/sleep 0.01; done
/bin/cat "$DETAIL_OUTPUT"
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorkspace(ctx, false)
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(120, 40)
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	defer func() { cancel(); w.app.Stop(); <-done }()
	w.app.QueueUpdateDraw(func() {
		w.items[0] = []workload{{ID: "reader.service", Name: "reader.service", State: "active"}}
		w.selected[0] = "reader.service"
		w.redrawRows()
		w.detail.SetText(content).SetWrap(false).ScrollTo(5, 4)
		w.app.SetFocus(w.detail)
		w.refreshDetail = true
		w.showDetail()
		w.refreshDetail = false
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detail command did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.app.QueueUpdateDraw(func() { w.detail.ScrollTo(20, 10) })
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		pending := true
		row, col := 0, 0
		focused := false
		w.app.QueueUpdateDraw(func() {
			pending = w.detailPending
			row, col = w.detail.GetScrollOffset()
			focused = w.detail.HasFocus()
		})
		if !pending {
			if row != 20 || col != 10 || !focused {
				t.Fatalf("late response rewound reading: row=%d col=%d focused=%t", row, col, focused)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detail command did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
