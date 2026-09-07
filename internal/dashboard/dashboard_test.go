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

func TestResourceSortKeepsSelectionAndUnknownsLast(t *testing.T) {
	w := testWorkspace()
	w.items[0] = []workload{
		{ID: "unknown", Name: "unknown", State: "failed"},
		{ID: "large", Name: "large", CPU: "2.0%", Memory: "1.5 GiB"},
		{ID: "busy", Name: "busy", CPU: "90%", Memory: "500 MiB"},
	}
	w.selected[0] = "large"
	w.sortMode = 2
	w.renderTable()
	if w.visible[0].ID != "busy" || w.visible[2].ID != "unknown" || w.current().ID != "large" {
		t.Fatal("CPU sort lost selection or misplaced missing counters", w.visible)
	}
	w.sortMode = 3
	w.renderTable()
	if w.visible[0].ID != "large" {
		t.Fatal("memory units were not normalized")
	}
	w.quickFilter = 2
	w.renderTable()
	if len(w.visible) != 1 || w.current().ID != "unknown" {
		t.Fatal("attention filter failed")
	}
}

func TestFleetMetricsDistinguishMissingFromZero(t *testing.T) {
	got := fleetTotals([]workload{{State: "running", CPU: "0%", Memory: "0B / 8GiB"}, {State: "failed"}, {State: "active", CPU: "125%", Memory: "1 GiB"}})
	if got.cpuCount != 2 || got.memoryCount != 2 || got.cpu != 125 || got.memory != 1024 || got.active != 2 || got.attention != 1 {
		t.Fatal(got)
	}
}

func TestDashboardFocusResizeZoomAndMouseFilter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	w.app.ForceDraw()
	if w.body.GetItemCount() != 2 {
		t.Fatal("wide layout should show both panes")
	}
	x, y, _, _ := w.cards[1].GetRect()
	w.cards[1].MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(x+2, y+1, tcell.ButtonNone, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if len(w.visible) != 1 || w.current().ID != "worker.service" {
		t.Fatal("clicking attention card did not filter")
	}
	w.quickFilter = 0
	w.renderTable()
	screen.SetSize(80, 24)
	w.app.ForceDraw()
	if w.body.GetItemCount() != 1 || w.body.GetItem(0) != w.table {
		t.Fatal("compact layout should show the workload list")
	}
	w.input(tcell.NewEventKey(tcell.KeyRune, 'c', 0))
	w.app.ForceDraw()
	if w.body.GetItem(0) != w.inspector {
		t.Fatal("config shortcut should reveal inspector on compact terminals")
	}
	screen.SetSize(140, 40)
	w.app.ForceDraw()
	w.input(tcell.NewEventKey(tcell.KeyRune, 'z', 0))
	w.app.ForceDraw()
	if w.body.GetItemCount() != 1 || w.body.GetItem(0) != w.inspector {
		t.Fatal("zoom did not expand focused pane")
	}
	w.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	w.app.ForceDraw()
	if w.body.GetItemCount() != 2 {
		t.Fatal("Escape did not restore split layout")
	}
	w.toggleLogDrawer()
	w.input(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	w.input(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	if !w.logDrawer.HasFocus() {
		t.Fatal("Tab did not reach the log drawer")
	}
	w.input(tcell.NewEventKey(tcell.KeyRune, 'z', 0))
	w.app.ForceDraw()
	if w.body.GetItem(0) != w.logDrawer {
		t.Fatal("drawer did not expand")
	}
	w.toggleLogDrawer()
	w.app.ForceDraw()
	if w.zoom != 0 || !w.table.HasFocus() {
		t.Fatal("closing expanded drawer left focus hidden")
	}
}

func TestDrawerFollowsSelectionAndCancelsWhenClosed(t *testing.T) {
	w := testWorkspace()
	w.toggleLogDrawer()
	first := w.drawerGeneration
	w.selected[0] = "nginx.service"
	w.syncLogDrawer()
	if !strings.HasSuffix(w.drawerKey, "/nginx.service") {
		t.Fatal(w.drawerKey)
	}
	w.search.SetText("state:failed")
	if !strings.HasSuffix(w.drawerKey, "/worker.service") || w.drawerGeneration <= first {
		t.Fatal("drawer did not retarget")
	}
	w.toggleLogDrawer()
	if w.drawerCancel != nil || w.drawerKey != "" {
		t.Fatal("closed drawer kept its stream")
	}
}

func TestDockerInventoryUsesOneBulkStatsSnapshot(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
 ps) printf '%s\n' '{"ID":"abc","Names":"web","State":"running","Status":"Up","Image":"web:local"}' ;;
 stats) printf '%s\n' '{"ID":"abc","CPUPerc":"23.5%","MemUsage":"1GiB / 8GiB"}' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	items, err := inventory(context.Background(), 1, false)
	if err != nil || len(items) != 1 || items[0].CPU != "23.5%" || items[0].Memory != "1024.0 MiB" {
		t.Fatal(items, err)
	}
}

func TestFollowLogsReturnsOutputAndEndState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte("#!/bin/sh\nprintf 'ERROR test log\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var output, final string
	followLogs(ctx, 0, false, workload{ID: "test.service"}, func(text, end string) { output, final = text, end })
	if !strings.Contains(output, "ERROR test log") || !strings.Contains(final, "ended") {
		t.Fatal(output, final)
	}
}

func TestPausedLogViewsCanResumeAfterStreamEnds(t *testing.T) {
	w := testWorkspace()
	w.tab = 1
	w.detail.SetText("held position")
	w.inspectorLog = logSnapshot{text: "last message", ending: "\nstream ended"}
	w.logPaused = true
	w.renderInspectorLogs()
	if w.detail.GetText(true) != "held position" {
		t.Fatal("paused inspector moved")
	}
	w.app.SetFocus(w.detail)
	w.input(tcell.NewEventKey(tcell.KeyRune, 'g', 0))
	if !strings.Contains(w.detail.GetText(true), "last message\nstream ended") {
		t.Fatal("final inspector snapshot was lost")
	}
	w.drawerOpen = true
	w.drawerPaused = true
	w.drawerLog = logSnapshot{text: "drawer message", ending: "\nstream ended"}
	w.logDrawer.SetText("held position")
	w.renderDrawerLogs()
	if w.logDrawer.GetText(true) != "held position" {
		t.Fatal("paused drawer moved")
	}
	w.app.SetFocus(w.logDrawer)
	w.input(tcell.NewEventKey(tcell.KeyRune, 'g', 0))
	if !strings.Contains(w.logDrawer.GetText(true), "drawer message\nstream ended") {
		t.Fatal("final drawer snapshot was lost")
	}
}

func TestExpandedListRevealsConfigurationColumns(t *testing.T) {
	w := testWorkspace()
	w.app.SetFocus(w.table)
	w.toggleZoom()
	if w.table.GetColumnCount() != 7 || w.table.GetCell(0, 4).Text != "BOOT" {
		t.Fatal("expanded service columns missing")
	}
	w.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if w.table.GetColumnCount() != 4 {
		t.Fatal("restored list is not compact")
	}
	w.mode = 1
	w.toggleZoom()
	if w.table.GetCell(0, 4).Text != "PROJECT" {
		t.Fatal("expanded Docker columns missing")
	}
}

func TestOverlayFitsTerminalAndKeepsDashboardVisible(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	w.chooseSort()
	_, primitive := w.pages.GetFrontPage()
	panel, ok := primitive.(*overlay)
	if !ok {
		t.Fatal("sort chooser should use a bounded overlay")
	}
	for _, size := range [][2]int{{140, 40}, {80, 24}} {
		screen.SetSize(size[0], size[1])
		w.app.ForceDraw()
		x, y, width, height := panel.content.GetRect()
		if x < 0 || y < 0 || x+width > size[0] || y+height > size[1] {
			t.Fatal("overlay overflowed terminal", size)
		}
		if size[0] == 140 && (x == 0 || y == 0) {
			t.Fatal("large terminal lost its surrounding workspace")
		}
	}
}

func TestRemoteDialogAsksForUsernameBeforeConnecting(t *testing.T) {
	w := testWorkspace()
	w.remoteDialog()
	name, primitive := w.pages.GetFrontPage()
	if name != "remote" {
		t.Fatal("missing connection form")
	}
	panel := primitive.(*overlay).content.(*tview.Flex)
	form := panel.GetItem(0).(*tview.Form)
	username := form.GetFormItem(1).(*tview.InputField)
	if username.GetLabel() != "SSH username" {
		t.Fatal("connection form does not ask for a username")
	}
	username.SetText("bad user")
	form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	name, _ = w.pages.GetFrontPage()
	if name != "remote" || !strings.Contains(panel.GetItem(1).(*tview.TextView).GetText(true), "username") {
		t.Fatal("invalid connection should leave the form open with a useful error")
	}
}

func TestFirstSSHConnectionOffersKeySetupAndCanCancel(t *testing.T) {
	w := testWorkspace()
	w.remoteDialog()
	_, primitive := w.pages.GetFrontPage()
	form := primitive.(*overlay).content.(*tview.Flex).GetItem(0).(*tview.Form)
	form.GetFormItem(0).(*tview.InputField).SetText("test-host")
	form.GetFormItem(1).(*tview.InputField).SetText("deploy")
	form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	name, primitive := w.pages.GetFrontPage()
	if name != "ssh-key-offer" {
		t.Fatal("first SSH connection did not offer key setup")
	}
	primitive.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	name, _ = w.pages.GetFrontPage()
	if name != "remote" {
		t.Fatal("cancel should return to the connection form")
	}
	if len(w.settings.SSHConnections) != 0 {
		t.Fatal("cancelled setup changed saved connection preferences")
	}
}
