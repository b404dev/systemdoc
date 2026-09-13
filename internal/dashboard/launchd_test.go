package dashboard

import (
	"context"
	"github.com/rivo/tview"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Existing systemd fixtures must run identically on Linux and macOS runners.
	// The launchd routing test runs in its own process, so workers never race a
	// mutable platform switch. Actual launchd adapters have platform-neutral tests.
	servicePlatform = "linux"
	if os.Getenv("SYSTEMDOC_TEST_PLATFORM") == "darwin" {
		servicePlatform = "darwin"
	}
	os.Exit(m.Run())
}

const launchFixture = `system = {
 services = {
  123 0 homebrew.mxcl.postgresql
  0 - com.example.idle
  - 78 com.example.failed
  0 0x0 com.example.completed
 }
 endpoints = {
  ignored = { anything }
 }
}`

func TestLaunchServicesParseStatesAndRejectBrokenOutput(t *testing.T) {
	rows, err := parseLaunchServices(launchFixture)
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	byID := map[string]workload{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["homebrew.mxcl.postgresql"].PID != 123 || byID["homebrew.mxcl.postgresql"].State != "active" || byID["com.example.idle"].State != "inactive" || byID["com.example.failed"].State != "failed" || byID["com.example.completed"].State != "inactive" {
		t.Fatal(rows)
	}
	for _, raw := range []string{"", "system = {\n services = {\n 123 0 job\n", "services = {\n bad row\n}", "services = {\n 0 0 foo\n 0 0 foo\n}"} {
		if _, err := parseLaunchServices(raw); err == nil {
			t.Fatal("accepted invalid inventory", raw)
		}
	}
	rows, err = parseLaunchServices("services = {\n 123 0 application.example[123]\n 0 - custom service label\n}")
	if err != nil || len(rows) != 2 || rows[1].ID != "custom service label" {
		t.Fatal("unusual label disabled inventory", rows, err)
	}
	rows, err = parseLaunchServices("system = {\n services = {\n }\n}")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
func fakeLaunchTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("LAUNCH_CALLS", calls)
	t.Setenv("LAUNCH_FIXTURE", launchFixture)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$LAUNCH_CALLS"
case "$1" in
 print)
  case "$2" in
   system|gui/*) case "$2" in */homebrew.*) ;; *) printf '%s\n' "$LAUNCH_FIXTURE"; exit 0 ;; esac ;;
  esac
  printf '%s\n' "$2 = {" ' path = /Library/LaunchDaemons/custom name.plist' ' pid = 123' ' environment = {' '  pid = 999' '  path = /wrong.plist' ' }' '}' ;;
 print-disabled) printf '%s\n' 'disabled services = {' ' "homebrew.mxcl.postgresql" => true' '}' ;;
 *) exit 9 ;;
esac
`
	for name, content := range map[string]string{"launchctl": script, "ps": "#!/bin/sh\nprintf '123 2.5 2048\\n999 90 9000\\n'", "plutil": "#!/bin/sh\nprintf '%s\\n' \"$@\"", "log": "#!/bin/sh\nprintf '%s\\n' \"$@\""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return calls
}
func TestLaunchAdaptersRespectScopeMetricsAndConfigPath(t *testing.T) {
	calls := fakeLaunchTools(t)
	rows, err := listLaunchServices(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	rows = enrichLaunchInventory(context.Background(), true, rows)
	item := rows[len(rows)-1]
	if item.ID != "homebrew.mxcl.postgresql" || item.CPU != "2.5%" || item.Memory != "2.0 MiB" || item.Enablement != "disabled" {
		t.Fatal(item)
	}
	text, err := inspectLaunchService(context.Background(), true, item, 2)
	if err != nil || !strings.Contains(text, "/Library/LaunchDaemons/custom name.plist") || strings.Contains(text, "wrong.plist") {
		t.Fatal(text, err)
	}
	args, err := launchLogArgs(context.Background(), true, item, true)
	if err != nil || strings.Join(args, " ") != "stream --style compact --predicate processIdentifier == 123" {
		t.Fatal(args, err)
	}
	text, err = inspectLaunchService(context.Background(), true, item, 3)
	if err != nil || !strings.Contains(text, "2.0 MiB") {
		t.Fatal(text, err)
	}
	raw, _ := os.ReadFile(calls)
	if !strings.Contains(string(raw), "print "+launchDomain(true)+"\n") {
		t.Fatal("GUI scope missing", string(raw))
	}
	for _, item := range rows {
		if item.PID == 0 && (item.CPU != "" || item.Memory != "") {
			t.Fatal("invented resources for idle job")
		}
	}
}
func TestLaunchActionsAndCommandTargets(t *testing.T) {
	for verb, args := range map[string][]string{"start": {"kickstart", "system/com.example.job"}, "restart": {"kickstart", "-k", "system/com.example.job"}, "stop": {"kill", "SIGTERM", "system/com.example.job"}, "disable": {"disable", "system/com.example.job"}} {
		name, got, err := launchActionArgs(false, workload{ID: "com.example.job"}, verb)
		if err != nil || name != "launchctl" || !reflect.DeepEqual(got, args) {
			t.Fatal(name, got, err)
		}
	}
	for _, id := range []string{"", "--all", "system/other", "foo\nbar", "foo*"} {
		if _, _, err := launchActionArgs(false, workload{ID: id}, "restart"); err == nil {
			t.Fatal("unsafe target accepted", id)
		}
	}
	if _, _, err := launchActionArgs(false, workload{ID: "job"}, "mask"); err == nil {
		t.Fatal("systemd verb accepted")
	}
	got, err := parseLaunchCommand([]string{"kickstart", "-k", launchDomain(true) + "/job"})
	if err != nil || !got.user || got.target != "job" || got.verb != "restart" {
		t.Fatal(got, err)
	}
	for _, parts := range [][]string{{"print", "gui/999999999/job"}, {"bootstrap", "system", "file"}, {"kickstart", "-x", "system/job"}, {"kill", "SIGKILL", "system/job"}} {
		if _, err := parseLaunchCommand(parts); err == nil {
			t.Fatal("unsupported command accepted", parts)
		}
	}
}
func TestLaunchdPlatformSubprocess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestLaunchdPlatformRouting$")
	cmd.Env = append(os.Environ(), "SYSTEMDOC_TEST_PLATFORM=darwin")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Darwin routing: %v\n%s", err, out)
	}
}
func TestLaunchdPlatformRouting(t *testing.T) {
	if !usesLaunchd() {
		t.Skip("runs in isolated Darwin-routing process")
	}
	fakeLaunchTools(t)
	rows, err := inventory(context.Background(), 0, true)
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	_, args, err := actionArgs(0, true, rows[3], "restart")
	if err != nil || args[0] != "kickstart" {
		t.Fatal(args, err)
	}
	if _, err := parseCommand("systemctl restart foo", false); err == nil {
		t.Fatal("systemctl accepted on Mac")
	}
	if _, err := parseCommand("launchctl print system/job", false); err != nil {
		t.Fatal(err)
	}
	w := testWorkspace()
	w.mode = 0
	w.tab = 4
	if w.inspectorTabName() != "Runtime" {
		t.Fatal("Linux tab label leaked")
	}
	w.actions()
	_, page := w.pages.GetFrontPage()
	list := page.(*overlay).content.(*tview.Flex).GetItem(1).(*tview.List)
	for i := 0; i < list.GetItemCount(); i++ {
		name, _ := list.GetItemText(i)
		if name == "Systemd timers" || name == "New service draft" || name == "Edit unit override" || name == "Follow journal" {
			t.Fatal("Linux action leaked", name)
		}
	}
	// Timer/draft shortcuts must remain read-only explanations on macOS.
	w.timers()
	w.newServiceFrom("")
	w.pages.RemovePage("dialog")
	w.items[0] = rows
	w.selected[0] = rows[3].ID
	w.quickFilter = 0
	w.tab = 1
	w.drawerOpen = true
	w.renderTable()
	generation := w.generation
	drawerGeneration := w.drawerGeneration
	changed := append([]workload(nil), rows...)
	changed[3].PID++
	w.publishInventory(0, changed, false)
	if w.generation <= generation || w.drawerGeneration <= drawerGeneration {
		t.Fatal("PID change did not reconnect log streams")
	}
	if err := w.applySavedView(savedView{Name: "Linux", ServiceManager: "systemd"}); err == nil {
		t.Fatal("cross-manager saved view opened")
	}
	report := collectSnapshot(context.Background(), snapshotRequest{Item: rows[3], User: true})
	if !strings.Contains(report, "Backend: launchd") || !strings.Contains(report, "Scope: "+launchDomain(true)) {
		t.Fatal(report)
	}
}

func TestLaunchDisabledUnknownAndPIDChanges(t *testing.T) {
	for _, raw := range []string{"", "permission denied", "disabled services = {\n", "disabled services = {\n \"job\" => unexpected\n}"} {
		if _, err := parseLaunchDisabled(raw); err == nil {
			t.Fatal("unknown override became default", raw)
		}
	}
	got, err := parseLaunchDisabled("disabled services = {\n \"job\" => false\n}")
	if err != nil || got["job"] != "enabled" {
		t.Fatal(got, err)
	}
	rows := carryAccounting([]workload{{ID: "job", State: "active", PID: 1, CPU: "99%", Memory: "10 MiB"}}, []workload{{ID: "job", State: "active", PID: 2}})
	if rows[0].CPU != "" || rows[0].Memory != "" {
		t.Fatal("new PID inherited old metrics", rows)
	}
	if matchesFilter(workload{Enablement: "default"}, "enabled:false") {
		t.Fatal("default override treated as explicitly disabled")
	}
}

// Opt-in because a developer's ordinary tests must use fixtures, not read host jobs.
func TestLiveLaunchdReadOnly(t *testing.T) {
	if os.Getenv("SYSTEMDOC_LIVE_LAUNCHD") != "1" {
		t.Skip("set SYSTEMDOC_LIVE_LAUNCHD=1 on a Mac")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("requires a Mac")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows, err := listLaunchServices(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("system launchd inventory unexpectedly empty")
	}
	rows = enrichLaunchInventory(ctx, false, rows)
	running := false
	for _, row := range rows {
		if row.PID > 0 {
			running = true
			break
		}
	}
	if !running {
		t.Fatal("no running system job observed")
	}
}
