package dashboard

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	for _, raw := range []string{"bad row", "1 0 root NaN 100 R 00:00 cmd", "1 0 root 0.0 nope R 00:00 cmd", "bad row\nworse row"} {
		if _, err := parseProcesses(raw); err == nil {
			t.Fatal("accepted malformed ps", raw)
		}
	}
	if rows, err := parseProcesses(""); err != nil || len(rows) != 0 {
		t.Fatal("empty ps output is not an error", rows, err)
	}
}

func TestProcessParserSkipsOnlyTheBadRows(t *testing.T) {
	rows, skipped, err := parseProcessesTolerant(processFixture + "garbage\n" + "1 0 root 0 1 R 00:00 duplicate pid\n")
	if err != nil || len(rows) != 5 || skipped != 2 || rows[0].PID != 1 || rows[4].PID != 45 {
		t.Fatalf("one bad row discarded the snapshot: %d rows, %d skipped, %v", len(rows), skipped, err)
	}
	for _, row := range rows {
		if row.RateCPU {
			t.Fatal("ps values must not be marked as interval rates")
		}
	}
	if _, skipped, err := parseProcessesTolerant("bad\nworse"); err == nil || skipped != 2 {
		t.Fatal("all-bad snapshot accepted", skipped, err)
	}
}

func TestProcessCPUSamplerTurnsTicksIntoIntervalRates(t *testing.T) {
	root := t.TempDir()
	write := func(pid, utime, stime, start string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		// Field 39, the CPU last run on, is 3 here.
		line := pid + " (fake daemon) S 1 1 1 0 -1 4194624 1000 0 3 0 " + utime + " " + stime + " 0 0 20 0 3 0 " + start + " 1 2048 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 0 3 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
		if err := os.WriteFile(filepath.Join(root, pid, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("100", "100", "50", "5000")
	write("200", "10", "10", "7000")
	sampler := newProcessCPUSampler(root)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := []hostProcess{{PID: 100, CPU: 7, LastCPU: -1}, {PID: 200, CPU: 1, LastCPU: -1}, {PID: 300, CPU: 2, LastCPU: -1}}
	sampler.apply(rows, t0)
	if rows[0].CPU != 7 || rows[0].RateCPU || rows[1].CPU != 1 || rows[2].CPU != 2 || rows[2].RateCPU {
		t.Fatalf("first sample must keep the ps estimate: %+v", rows)
	}
	// The CPU a process last ran on is read from the first sample already;
	// an unreadable PID keeps the unknown marker.
	if rows[0].LastCPU != 3 || rows[1].LastCPU != 3 || rows[2].LastCPU != -1 {
		t.Fatalf("last CPU: %+v", rows)
	}
	if len(sampler.samples) != 2 {
		t.Fatalf("unreadable PID 300 must not be remembered: %+v", sampler.samples)
	}
	// 150 ticks at 100 Hz over 3 s is half of one logical CPU.
	write("100", "200", "100", "5000")
	// Same PID, new start time: a different process, so no rate yet.
	write("200", "1000", "1000", "7001")
	rows = []hostProcess{{PID: 100, CPU: 7}, {PID: 200, CPU: 1}}
	sampler.apply(rows, t0.Add(3*time.Second))
	if !rows[0].RateCPU || math.Abs(rows[0].CPU-50) > 1e-9 {
		t.Fatalf("interval rate incorrect: %+v", rows[0])
	}
	if rows[1].RateCPU || rows[1].CPU != 1 {
		t.Fatalf("restarted PID inherited the old sample: %+v", rows[1])
	}
	// A repeated timestamp cannot produce a rate; the estimate is kept.
	rows = []hostProcess{{PID: 100, CPU: 7}}
	sampler.apply(rows, t0.Add(3*time.Second))
	if rows[0].RateCPU {
		t.Fatal("zero-length interval produced a rate")
	}
	if _, ok := sampler.samples[200]; ok {
		t.Fatal("PID absent from the poll was not pruned")
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
	rows, skipped, err := parseMountsTolerant(diskFixture+"df: /run/user/1000/gvfs: Permission denied\n", "linux", false)
	if err != nil || len(rows) != 3 || skipped != 1 {
		t.Fatalf("one odd df row discarded the table: %d rows, %d skipped, %v", len(rows), skipped, err)
	}
	mac, skipped, err = parseMountsTolerant(macDiskFixture+"map auto_home 0 0 0 100% /System/Volumes/Data/home\n", "darwin", false)
	if err != nil || len(mac) != 3 || skipped != 1 {
		t.Fatalf("mac row without inode columns discarded the table: %d rows, %d skipped, %v", len(mac), skipped, err)
	}
}

const mountInfoFixture = `24 29 0:22 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw
25 29 0:23 / /proc rw,nosuid,nodev,noexec,relatime shared:12 - proc proc rw
26 29 0:5 / /dev rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=16017752k,mode=755
27 26 0:24 / /dev/pts rw,nosuid,noexec,relatime shared:3 - devpts devpts rw,gid=5,mode=620
29 1 252:0 / / rw,relatime shared:1 - ext4 /dev/mapper/ubuntu--vg-ubuntu--lv rw
33 24 0:28 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:9 - cgroup2 cgroup2 rw,nsdelegate
36 25 0:31 / /proc/sys/fs/binfmt_misc rw,relatime shared:13 - autofs systemd-1 rw,fd=32
44 29 7:0 / /snap/core22/1748 ro,nodev,relatime shared:29 - squashfs /dev/loop0 ro,errors=continue
60 29 8:17 / /media/Work\040Drive rw,relatime shared:60 - ext4 /dev/sdb1\011x rw
61 29 8:33 / /media/Work\040Drive rw,relatime shared:61 - vfat /dev/sdc1 rw
62 29 0:50 / /run/user/1000/doc rw,nosuid,nodev,relatime shared:62 - fuse.portal portal rw,user_id=1000
63 29 0:51 / /run/user/1000/gvfs rw,nosuid,nodev,relatime shared:63 - fuse.gvfsd-fuse gvfsd-fuse rw,user_id=1000
64 29 0:52 / /mnt/nas rw,relatime shared:64 - nfs4 nas:/export rw,vers=4.2
645 109 0:4 mnt:[4026532389] /run/snapd/ns/docker.mnt rw - nsfs nsfs rw
this line is not a mount
`

func TestMountInfoFiltersPseudoAndKeepsShadowingMount(t *testing.T) {
	entries := parseMountInfo(mountInfoFixture)
	var paths []string
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	want := "/dev / /snap/core22/1748 /media/Work Drive /mnt/nas"
	if strings.Join(paths, " ") != want {
		t.Fatalf("mount inventory\n got %q\nwant %q", strings.Join(paths, " "), want)
	}
	if entries[3].Source != "/dev/sdc1" || entries[3].Type != "vfat" {
		t.Fatalf("shadowed mount point kept the earlier entry: %+v", entries[3])
	}
	if unescapeMountField(`/a\040b\011c\012d\134e\x`) != "/a b\tc\nd\\e\\x" {
		t.Fatal(unescapeMountField(`/a\040b\011c\012d\134e\x`))
	}
	if entries[0].Source != "udev" || entries[1].Source != "/dev/mapper/ubuntu--vg-ubuntu--lv" {
		t.Fatalf("sources mislocated: %+v", entries[:2])
	}
}

func TestMountUsageMatchesDFArithmetic(t *testing.T) {
	// df rounds Capacity up: 1 used, 2 available is 34%, not 33%.
	if dfPercent(1, 2) != 34 || dfPercent(0, 5) != 0 || dfPercent(5, 0) != 100 || dfPercent(0, 0) != -1 || dfPercent(850000, 100000) != 90 {
		t.Fatal(dfPercent(1, 2), dfPercent(0, 5), dfPercent(5, 0), dfPercent(0, 0), dfPercent(850000, 100000))
	}
	entry := mountEntry{Source: "/dev/root", Path: "/", Type: "ext4"}
	m := mountUsage{BlockSize: 4096, Blocks: 250000, Bfree: 37500, Bavail: 25000, Files: 100, Ffree: 10}.mount(entry)
	if m.Size != 1000000 || m.Used != 850000 || m.Free != 100000 || m.Percent != 90 || m.Inodes != 100 || m.InodeUsed != 90 || m.InodeFree != 10 || m.InodePercent != 90 || m.Source != "/dev/root" || m.Path != "/" {
		t.Fatalf("df parity broken: %+v", m)
	}
	m = mountUsage{BlockSize: 1024, Blocks: 3, Bfree: 2, Bavail: 2}.mount(entry)
	if m.Percent != 34 || m.Inodes != 0 || m.InodePercent != -1 {
		t.Fatalf("ceil or inode-less filesystem incorrect: %+v", m)
	}
}

func TestStatMountsReportsHungMountsWithoutBlockingTheView(t *testing.T) {
	entries := []mountEntry{{Path: "/", Source: "/dev/root", Type: "ext4"}, {Path: "/mnt/nas", Source: "nas:/x", Type: "nfs4"}, {Path: "/run/user/1000/gvfs", Source: "gvfsd-fuse", Type: "fuse"}, {Path: "/proc/sys/fs/binfmt_misc", Type: "binfmt_misc"}, {Path: "/boot", Source: "/dev/sda2", Type: "ext4"}}
	release := make(chan struct{})
	defer close(release)
	stat := func(path string) (mountUsage, error) {
		switch path {
		case "/mnt/nas":
			<-release // A stale NFS mount never answers within the poll.
			return mountUsage{}, nil
		case "/run/user/1000/gvfs":
			return mountUsage{}, syscall.EACCES
		case "/proc/sys/fs/binfmt_misc":
			return mountUsage{BlockSize: 4096}, nil
		}
		return mountUsage{BlockSize: 1024, Blocks: 100, Bfree: 50, Bavail: 40, Files: 10, Ffree: 5}, nil
	}
	start := time.Now()
	rows, note := statMounts(context.Background(), entries, 50*time.Millisecond, stat)
	if time.Since(start) > 2*time.Second {
		t.Fatal("hung mount stalled the poll")
	}
	if len(rows) != 2 || rows[0].Path != "/" || rows[1].Path != "/boot" || rows[0].Percent != 56 {
		t.Fatalf("reachable mounts lost: %+v", rows)
	}
	if !strings.Contains(note, "unreachable: /mnt/nas") || !strings.Contains(note, "unreadable: /run/user/1000/gvfs") || strings.Contains(note, "binfmt") {
		t.Fatalf("note: %q", note)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rows, _ := statMounts(ctx, entries[1:2], time.Second, stat); len(rows) != 0 {
		t.Fatal("cancelled context still waited for a hung mount")
	}
}

func TestLinuxMountsReadWithoutDF(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("mountinfo is Linux-only")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "df"), []byte("#!/bin/sh\necho 'df must not be called' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	rows, note, err := collectMounts(context.Background(), "linux")
	if err != nil {
		t.Fatalf("collectMounts: %v (%s)", err, note)
	}
	for _, m := range rows {
		if m.Path == "/" && m.Size > 0 && m.Percent >= 0 && m.Percent <= 100 {
			return
		}
	}
	t.Fatalf("root filesystem missing from %+v", rows)
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
	// A bad process record (and its files) and a file with an unreadable size
	// are skipped individually; the good records survive.
	tolerated := "pbad\x00cx\x00\nf9\x00tREG\x00D0x1\x00i1\x00s1\x00n/x\x00\n" + raw + "p44\x00cshell\x00u0\x00\nf7\x00tREG\x00D0x1\x00i102\x00sbig\x00n/tmp/b\x00\nf8\x00tREG\x00D0x1\x00i103\x00s10\x00n/tmp/c\x00\n"
	files, skipped, err := parseDeletedFilesTolerant(tolerated)
	if err != nil || len(files) != 4 || skipped != 2 || files[3].Path != "/tmp/c" || files[3].PID != 44 {
		t.Fatalf("tolerant lsof parse: %d files, %d skipped, %v", len(files), skipped, err)
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
