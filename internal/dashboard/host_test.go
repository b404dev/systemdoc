package dashboard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const processFixture = `    1 0 root 0.1 900 Ss 02:30:00 /sbin/init
42 1 alice 125.5 204800 R+ 12:09 /usr/bin/node  server.js --name hello world
43 42 alice 2.0 10240 S 00:30 /Applications/Some App/helper --flag
44 999 bob 0.0 0 Z 1-03:00:00 [worker] <defunct>
45 42 alice - - S 00:10 inaccessible command
`

const diskFixture = `Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/root 1000000 850000 100000 90% /
/dev/mapper/my disk 2000000 1000000 1000000 50% /media/Work Drive
tmpfs 1000 0 1000 0% /run
`

const inodeFixture = `Filesystem Inodes IUsed IFree IUse% Mounted on
/dev/root 100 90 10 90% /
/dev/mapper/my disk 0 0 0 - /media/Work Drive
tmpfs 500 1 499 1% /run
`

const macDiskFixture = `Filesystem 1024-blocks Used Available Capacity iused ifree %iused Mounted on
/dev/disk3s1s1 1000000000 8000000 200000000 4% 400000 2000000000 0% /
/dev/disk3s5 1000000000 700000000 200000000 78% 700000 2000000000 0% /System/Volumes/Data
/dev/disk4s1 100000 90000 -500 101% 900 100 90% /Volumes/Work Drive
`

func TestProcessesParsingFilteringAndTree(t *testing.T) {
	rows, err := parseProcesses(processFixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[1].CPU != 125.5 || rows[1].RSS != 204800 || rows[1].Command != "/usr/bin/node  server.js --name hello world" || rows[4].RSS != -1 {
		t.Fatalf("bad process parse: %+v", rows)
	}
	for query, want := range map[string]bool{"pid:42 user:alice": true, "pid:4": false, "ppid:1 command:server.js": true, "user:bob": false, "hello world": true} {
		if matchesProcess(rows[1], query) != want {
			t.Fatal(query)
		}
	}
	tree := processTree(rows)
	if len(tree) != 5 || tree[0].Process.PID != 1 || tree[1].Process.PID != 42 || tree[2].Process.PID != 43 || tree[2].Depth != 2 {
		t.Fatalf("bad tree: %+v", tree)
	}
	cycle := processTree([]hostProcess{{PID: 2, PPID: 3}, {PID: 3, PPID: 2}, {PID: 4, PPID: 4}})
	if len(cycle) != 3 {
		t.Fatal("cycle lost rows")
	}
	for _, raw := range []string{"bad row", "1 0 root NaN 100 R 00:00 cmd", "1 0 root 0.0 nope R 00:00 cmd", "1 0 root 0 1 R 00:00 x\n1 0 root 0 1 R 00:00 y"} {
		if _, err := parseProcesses(raw); err == nil {
			t.Fatal("accepted malformed ps", raw)
		}
	}
}

func TestProcessCollectorOmitsOnlyItsOwnPSChild(t *testing.T) {
	rows := []hostProcess{
		{PID: 10, PPID: 7, Command: "/usr/bin/ps -axww"},
		{PID: 11, PPID: 8, Command: "/usr/bin/ps aux"},
		{PID: 12, PPID: 7, Command: "/usr/bin/worker ps"},
	}
	got := omitProcessCollector(rows, 7)
	if len(got) != 2 || got[0].PID != 11 || got[1].PID != 12 {
		t.Fatalf("collector filter removed the wrong processes: %+v", got)
	}
}

func TestProcessExplorerUsesMinimumPollInterval(t *testing.T) {
	w := testWorkspace()
	w.settings.RefreshSeconds = 2
	processes := &hostPage{w: w, tab: 3}
	storage := &hostPage{w: w, tab: 4}
	if processes.pollSeconds() != 15 || storage.pollSeconds() != 2 {
		t.Fatal("process and storage polling cadence is incorrect")
	}
	w.settings.RefreshSeconds = 30
	if processes.pollSeconds() != 30 {
		t.Fatal("process explorer ignored a slower configured interval")
	}
}

func TestProcessSignalTargetsProtectCriticalIdentities(t *testing.T) {
	for _, process := range []hostProcess{{PID: 0, Command: "zero"}, {PID: 1, Command: "init"}, {PID: os.Getpid(), Command: "systemdoc"}, {PID: 42}} {
		if err := processTargetError(process); err == nil {
			t.Fatalf("unsafe process target accepted: %+v", process)
		}
	}
	if err := processTargetError(hostProcess{PID: 42, Command: "/usr/bin/worker --queue jobs"}); err != nil {
		t.Fatalf("valid process target rejected: %v", err)
	}
	if len(processSignals) != 6 || processSignals[0].signal.String() != "terminated" || processSignals[len(processSignals)-1].signal.String() != "killed" {
		t.Fatalf("unexpected process signal palette: %+v", processSignals)
	}
}

func TestVerifiedProcessSignalTerminatesExactPID(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()

	raw, diagnostic, err := networkCommand(context.Background(), "ps", "-ww", "-p", strconv.Itoa(cmd.Process.Pid), "-o", "args=")
	if err != nil {
		t.Fatalf("read child identity: %v %s", err, diagnostic)
	}
	process := hostProcess{PID: cmd.Process.Pid, Command: strings.TrimSpace(clean(raw))}
	changed := process
	changed.Command += " --different-identity"
	if err := verifyAndSignalProcess(context.Background(), changed, syscall.SIGTERM); err == nil || !strings.Contains(err.Error(), "changed identity") {
		t.Fatalf("reused PID protection failed: %v", err)
	}
	if err := verifyAndSignalProcess(context.Background(), process, syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to exact child: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("sleep exited normally instead of receiving SIGTERM")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGTERM did not terminate the child process")
	}
}

func TestFilesystemParsingAcrossPlatforms(t *testing.T) {
	rows, err := parseMounts(diskFixture, "linux", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1].Path != "/media/Work Drive" || rows[1].Source != "/dev/mapper/my disk" || rows[0].Free != 100000 {
		t.Fatalf("bad Linux mounts %+v", rows)
	}
	inodes, err := parseMounts(inodeFixture, "linux", true)
	if err != nil {
		t.Fatal(err)
	}
	if inodes[0].InodePercent != 90 || inodes[1].InodePercent != -1 {
		t.Fatal(inodes)
	}
	mac, err := parseMounts(macDiskFixture, "darwin", false)
	if err != nil {
		t.Fatal(err)
	}
	if mac[2].Path != "/Volumes/Work Drive" || mac[2].Free != -500 || mac[2].InodePercent != 90 || mac[0].Inodes != 2000400000 {
		t.Fatalf("bad mac mounts %+v", mac)
	}
	for _, raw := range []string{"failure", "Filesystem\nno counts here", "Filesystem\n/dev/a 9999999999999999999999 0 1 0% /"} {
		if _, err := parseMounts(raw, "linux", false); err == nil {
			t.Fatal("accepted invalid df")
		}
	}
}

func TestDeletedFilesRetainOwnersAndPaths(t *testing.T) {
	raw := "p42\x00cnode\x00u1000\x00\nf3\x00tREG\x00D0x1\x00i100\x00s4096\x00n/tmp/a file [red] (deleted)\x00\nf4\x00tREG\x00D0x1\x00i100\x00s4096\x00n/tmp/a file [red] (deleted)\x00\np43\x00cworker\x00u1001\x00\nf5\x00tREG\x00D0x1\x00i101\x00n/tmp/unknown\x00\n"
	files, err := parseDeletedFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].PID != 42 || files[0].Process != "node" || files[0].Size != 4096 || files[2].PID != 43 || files[2].Size != -1 || !strings.Contains(files[0].Path, "[red]") {
		t.Fatal(files)
	}
	w := testWorkspace()
	w.hostPage(4)
	_, page := w.pages.GetFrontPage()
	h := page.(*hostPage)
	defer h.close()
	h.deletedView = true
	h.deleted = files
	h.render()
	if !strings.Contains(h.cards[1].GetText(true), "4.0 KiB") || !strings.Contains(h.cards[0].GetText(true), "2 observed") {
		t.Fatal("duplicate handles counted twice")
	}
	if _, err := parseDeletedFiles("pbad\x00"); err == nil {
		t.Fatal("accepted bad PID")
	}
	if _, err := parseDeletedFiles("f3\x00tREG\x00"); err == nil {
		t.Fatal("accepted ownerless file")
	}
}

func TestHostPagesResizeFilterSelectAndNavigate(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	for _, tab := range []int{3, 4} {
		w.hostPage(tab)
		_, page := w.pages.GetFrontPage()
		h := page.(*hostPage)
		h.processes, _ = parseProcesses(processFixture)
		h.mounts, _ = parseMounts(diskFixture, "linux", false)
		h.render()
		for _, size := range [][2]int{{160, 44}, {120, 30}, {80, 24}, {40, 12}, {120, 30}} {
			screen.SetSize(size[0], size[1])
			w.app.ForceDraw()
			if !strings.Contains(h.header.GetText(true), "SYSTEMDOC") || h.table.GetRowCount() < 2 {
				t.Fatal("missing host page contents")
			}
			_, _, left, _ := screen.GetContent(0, 0)
			_, _, right, _ := screen.GetContent(size[0]-1, 0)
			_, lb, _ := left.Decompose()
			_, rb, _ := right.Decompose()
			if lb == rb {
				t.Fatal("missing shared gradient")
			}
		}
		if tab == 3 {
			h.search.SetText("pid:42")
			if len(h.rows) != 1 || h.rows[0].pid != 42 {
				t.Fatal("process filter")
			}
			w.app.SetFocus(h.search)
			e := tcell.NewEventKey(tcell.KeyRune, '5', 0)
			if h.input(e) != e {
				t.Fatal("search intercepted navigation")
			}
			deleteKey := tcell.NewEventKey(tcell.KeyDelete, 0, 0)
			if h.input(deleteKey) != deleteKey {
				t.Fatal("process actions intercepted Delete while editing the search")
			}
			w.app.SetFocus(h.table)
			h.tree = true
			h.render()
			if h.selected != "42" {
				t.Fatal("selection changed")
			}
			h.processActions()
			if front, _ := w.pages.GetFrontPage(); front != "process-actions" {
				t.Fatal("process signal palette did not open")
			}
			w.pages.RemovePage("process-actions")
			w.app.SetFocus(h.table)
			h.inspect()
			_, detail := w.pages.GetFrontPage()
			detail.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
			if !h.table.HasFocus() {
				t.Fatal("details lost focus")
			}
			h.ports()
			_, network := w.pages.GetFrontPage()
			n := network.(*networkPage)
			if n.filters[1] != "pid:42" {
				t.Fatal("process ports not scoped")
			}
			n.close()
		} else {
			h.search.SetText("Work Drive")
			if len(h.rows) != 1 {
				t.Fatal("mount filter")
			}
			h.switchStorage()
			if h.search.GetText() != "" {
				t.Fatal("file filter inherited mount query")
			}
			h.switchStorage()
			if h.search.GetText() != "Work Drive" {
				t.Fatal("mount filter lost")
			}
			h.close()
		}
	}
}

func TestLiveHostCollectors(t *testing.T) {
	if os.Getenv("SYSTEMDOC_LIVE_HOST") != "1" {
		t.Skip("set SYSTEMDOC_LIVE_HOST=1 for read-only native commands")
	}
	ctx := context.Background()
	processes, err := collectProcesses(ctx)
	if err != nil || len(processes) == 0 {
		t.Fatalf("processes: %v", err)
	}
	found := false
	for _, p := range processes {
		if p.PID == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatal("test process absent")
	}
	mounts, note, err := collectMounts(ctx, servicePlatform)
	if err != nil || len(mounts) == 0 {
		t.Fatalf("mounts: %v (%s)", err, note)
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof unavailable; process and mount collectors passed")
	}
	file, err := os.CreateTemp(t.TempDir(), "systemdoc-deleted-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.WriteString(strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	files, note, err := collectDeletedFiles(ctx)
	if err != nil {
		t.Fatalf("deleted files: %v (%s)", err, note)
	}
	for _, f := range files {
		if f.PID == os.Getpid() && strings.Contains(f.Path, filepath.Base(file.Name())) && f.Size == 4096 {
			return
		}
	}
	t.Fatal("deleted file held by this process was not reported")
}

func TestHostRefreshPreservesRowsOnFailure(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	failed := filepath.Join(dir, "failed")
	if err := os.WriteFile(fixture, []byte(processFixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_FIXTURE", fixture)
	t.Setenv("HOST_FAILURE", failed)
	script := "#!/bin/sh\nif [ \"$LC_ALL\" != C ]; then exit 2; fi\nif [ -f \"$HOST_FAILURE\" ]; then echo 'ps permission denied' >&2; exit 1; fi\n/bin/cat \"$HOST_FIXTURE\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0700); err != nil {
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
	var h *hostPage
	w.app.QueueUpdateDraw(func() { w.hostPage(3); _, page := w.pages.GetFrontPage(); h = page.(*hostPage) })
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			busy := true
			w.app.QueueUpdateDraw(func() { busy = h.busy })
			if !busy {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("host refresh timed out")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait()
	w.app.QueueUpdateDraw(func() {
		if len(h.processes) != 5 {
			t.Error("missing initial processes")
		}
		h.search.SetText("pid:42")
	})
	if err := os.WriteFile(failed, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	w.app.QueueUpdateDraw(h.refresh)
	wait()
	w.app.QueueUpdateDraw(func() {
		if len(h.processes) != 5 || len(h.rows) != 1 || h.selected != "42" || !strings.Contains(h.failure[0], "permission denied") {
			t.Error("failed refresh discarded data or diagnostics")
		}
	})
}
