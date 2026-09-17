package dashboard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTaskStatParsingKeepsCommandsWithSpacesAndParens(t *testing.T) {
	line := "4242 (Web Content (x)) S 1 4242 4242 0 -1 4194624 1653 2998 7 14 110 75 0 4 20 -5 21 0 157164 2579238912 20258 18446744073709551615 1 1 0 0 0 0 0 0 2143420159 0 0 0 -1 8 0 0 0 0 0 0 0 0 0 0 0 0 0"
	comm, fields, err := parseTaskStat(line)
	if err != nil || comm != "Web Content (x)" || fields[0] != "S" {
		t.Fatal(comm, fields, err)
	}
	if statUint(fields, 10) != 1653 || statUint(fields, 12) != 7 || statUint(fields, 14) != 110 || statUint(fields, 15) != 75 || statInt(fields, 19) != -5 || statInt(fields, 20) != 21 || statUint(fields, 22) != 157164 {
		t.Fatalf("field mapping wrong: %v", fields)
	}
	if _, _, err := parseTaskStat("garbage"); err == nil {
		t.Fatal("accepted garbage")
	}
}

// fakeStat builds a 52-field /proc/PID/stat line with the documented field
// numbers: 14 utime, 15 stime, 20 threads, 22 starttime, 39 processor.
func fakeStat(pid, comm, state, utime, stime, cpu string) string {
	fields := make([]string, 53)
	for i := range fields {
		fields[i] = "0"
	}
	fields[3], fields[4], fields[5], fields[6], fields[8] = state, "1", "1", "1", "-1"
	fields[10], fields[12] = "1000", "3"
	fields[14], fields[15] = utime, stime
	fields[18], fields[20], fields[22] = "20", "3", "5000"
	fields[39] = cpu
	return pid + " (" + comm + ") " + strings.Join(fields[3:], " ")
}

func writeFakeProcess(t *testing.T, root string, pid string, utime, stime, vol string, tasks map[string]string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "task"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	stat := fakeStat(pid, "fake daemon", "S", utime, stime, "2")
	files := map[string]string{
		"stat":   stat,
		"status": "Name:\tfake daemon\nState:\tS (sleeping)\nVmRSS:\t   81804 kB\nVmSwap:\t    1024 kB\nThreads:\t3\nCpus_allowed_list:\t0-3\nvoluntary_ctxt_switches:\t" + vol + "\nnonvoluntary_ctxt_switches:\t4\n",
		"io":     "rchar: 5000\nwchar: 100\nsyscr: 9\nsyscw: 1\nread_bytes: 4096\nwrite_bytes: 8192\n",
		"cgroup": "0::/system.slice/fake.service\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for fd, target := range map[string]string{"0": "/dev/null", "3": "socket:[1234]", "4": "pipe:[99]", "5": "/var/log/fake.log (deleted)", "6": "anon_inode:[eventpoll]"} {
		os.Symlink(target, filepath.Join(dir, "fd", fd))
	}
	os.Symlink("/usr/bin/fake", filepath.Join(dir, "exe"))
	os.Symlink("/srv/fake", filepath.Join(dir, "cwd"))
	for tid, ticks := range tasks {
		if err := os.MkdirAll(filepath.Join(dir, "task", tid), 0o755); err != nil {
			t.Fatal(err)
		}
		state := "S"
		if tid == "4243" {
			state = "R"
		}
		cpu := "0"
		if tid == "4243" {
			cpu = "5"
		}
		line := fakeStat(tid, "worker-"+tid, state, ticks, "0", cpu)
		if err := os.WriteFile(filepath.Join(dir, "task", tid, "stat"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProcessActivitySamplesAFakeProcTree(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "4242", "100", "50", "1000", map[string]string{"4242": "100", "4243": "40", "4244": "10"})
	first, err := sampleProcessActivity(root, 4242)
	if err != nil {
		t.Fatal(err)
	}
	if first.Comm != "fake daemon" || first.State != "S (sleeping)" || first.Threads != 3 || first.RSSKiB != 81804 || first.SwapKiB != 1024 || first.VoluntarySwitches != 1000 || first.Preemptions != 4 {
		t.Fatalf("%+v", first)
	}
	if !first.IOOK || first.ReadBytes != 4096 || first.WriteBytes != 8192 || !first.FDOK || first.FDTotal != 5 || first.FDSockets != 1 || first.FDPipes != 1 || first.FDFiles != 2 || first.FDDeleted != 1 || first.FDOther != 1 {
		t.Fatalf("io/fd: %+v", first)
	}
	if first.Unit != "fake.service" || first.Executable != "/usr/bin/fake" || first.WorkingDir != "/srv/fake" || len(first.Tasks) != 3 {
		t.Fatalf("identity: %+v", first)
	}
	if first.LastCPU != 2 || first.CPUsAllowed != "0-3" {
		t.Fatalf("placement: last CPU %d allowed %q", first.LastCPU, first.CPUsAllowed)
	}
	for _, task := range first.Tasks {
		if want := map[int]int{4243: 5}[task.TID]; task.CPU != want {
			t.Fatalf("thread %d on CPU %d, want %d", task.TID, task.CPU, want)
		}
	}
	// Two seconds later the process burned 150 ticks (1.5 CPU-seconds) and one thread did most of it.
	writeFakeProcess(t, root, "4242", "200", "100", "1400", map[string]string{"4242": "110", "4243": "170", "4244": "10"})
	if err := os.WriteFile(filepath.Join(root, "4242", "io"), []byte("rchar: 9000\nwchar: 100\nread_bytes: 8192\nwrite_bytes: 16384\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := sampleProcessActivity(root, 4242)
	if err != nil {
		t.Fatal(err)
	}
	second.At = first.At.Add(2 * time.Second)
	rates := computeActivityRates(&first, &second)
	if !rates.Valid || rates.CPU != 75 || rates.UserCPU != 50 || rates.SystemCPU != 25 || rates.VoluntarySwitches != 200 || rates.Preemptions != 0 {
		t.Fatalf("%+v", rates)
	}
	if rates.ThreadCPU[4243] != 65 || rates.ThreadCPU[4242] != 5 || rates.ThreadCPU[4244] != 0 {
		t.Fatalf("thread rates: %+v", rates.ThreadCPU)
	}
	reused := second
	reused.StartTime = 9999
	if computeActivityRates(&first, &reused).Valid {
		t.Fatal("rates were computed across a PID reuse")
	}
	if _, err := sampleProcessActivity(root, 1); !os.IsNotExist(err) {
		t.Fatal("missing process must report not-exist", err)
	}
	p := themes[0]
	text := renderProcessActivity(p, "ascii", hostProcess{PID: 4242, User: "svc", Command: "/usr/bin/fake --serve"}, &first, &second, []float64{10, 75}, "2026-09-14T12:00:00+0100 host fake[4242]: listening", []hostProcess{{PID: 5000, Command: "/usr/bin/fake-child"}}, false, "", hostUtilisation{})
	for _, want := range []string{"fake daemon · /usr/bin/fake --serve", "sleeping · interruptible", "3 threads · nice 0", " 75.0%", "user 50.0% · system 25.0%", "79.9 MiB RSS · 1.0 MiB swapped", "voluntary 200/s · preempted 0.00/s", "read 2.0 KiB/s · write 4.0 KiB/s", "5 open", "1 sockets · 2 files · 1 pipes · 1 other", "1 deleted-but-open", "fake.service", "/usr/bin/fake", "/srv/fake", "5000 fake-child", "worker-4243", "65.0%", "2 of 3 threads used CPU", "listening", "_PID=4242"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(text, "4243") || strings.Index(text, "worker-4243") > strings.Index(text, "worker-4242") {
		t.Fatal("busiest thread is not listed first")
	}
	initial := renderProcessActivity(p, "blocks", hostProcess{PID: 4242, CPU: 3.5}, nil, &first, nil, "reading journal…", nil, false, "", hostUtilisation{})
	if !strings.Contains(initial, "rates need a second sample") || !strings.Contains(initial, "lifetime read 4.0 KiB") {
		t.Fatal(initial)
	}
	exited := renderProcessActivity(p, "blocks", hostProcess{PID: 4242}, &first, &second, nil, "", nil, true, "The process has exited.", hostUtilisation{})
	if !strings.Contains(exited, "The process has exited.") || !strings.Contains(exited, "PAUSED") {
		t.Fatal(exited)
	}
}

func TestProcessActivityReportsUnreadableCountersHonestly(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, "77", "1", "1", "1", map[string]string{"77": "1"})
	os.Remove(filepath.Join(root, "77", "io"))
	os.RemoveAll(filepath.Join(root, "77", "fd"))
	sample, err := sampleProcessActivity(root, 77)
	if err != nil {
		t.Fatal(err)
	}
	if sample.IOOK || sample.FDOK || !strings.Contains(sample.IONote, "I/O counters unavailable") || !strings.Contains(sample.FDNote, "open descriptors unavailable") {
		t.Fatalf("%+v", sample)
	}
	if note := permissionNote(os.ErrPermission, "I/O counters"); !strings.Contains(note, "another user") {
		t.Fatal(note)
	}
	text := renderProcessActivity(themes[0], "blocks", hostProcess{PID: 77}, nil, &sample, nil, "no journal entries carry this PID", nil, false, "", hostUtilisation{})
	if !strings.Contains(text, "I/O counters unavailable") || !strings.Contains(text, "open descriptors unavailable") || !strings.Contains(text, "no journal entries") {
		t.Fatal(text)
	}
}

func TestProcessActivitySamplesTheRunningProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc is Linux-only")
	}
	sample, err := sampleProcessActivity("/proc", os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Comm == "" || sample.Threads < 1 || len(sample.Tasks) < 1 || !sample.FDOK || !sample.IOOK || sample.RSSKiB <= 0 {
		t.Fatalf("own process should be fully readable: %+v", sample)
	}
	if stateWord("D") != "waiting on disk or device · uninterruptible" || stateWord("R (running)") != "running" {
		t.Fatal("state words changed")
	}
}
