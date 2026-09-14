package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSysdigTargetsAndArguments(t *testing.T) {
	process := hostProcess{PID: 7796, Command: "/usr/bin/containerd --config x"}
	single := sysdigProcessTarget(process, false)
	if single.filter != "proc.pid=7796" || single.stem != "pid-7796" || !strings.Contains(single.label, "containerd") {
		t.Fatalf("%+v", single)
	}
	tree := sysdigProcessTarget(process, true)
	if tree.filter != "(proc.pid=7796 or proc.apid=7796)" || !strings.HasSuffix(tree.label, "and children") {
		t.Fatalf("%+v", tree)
	}
	container := sysdigContainerTarget(workload{ID: "0123456789abcdef0123456789abcdef", Name: "website-web-1"})
	if container.filter != "container.id=0123456789ab" || container.label != "container website-web-1" {
		t.Fatalf("%+v", container)
	}
	pod := sysdigPodTarget(workload{ID: podID("shop", "web-abc"), Name: "shop/web-abc"})
	if pod.filter != "(k8s.pod.name=web-abc and k8s.ns.name=shop)" || pod.stem != "pod-shop-web-abc" {
		t.Fatalf("%+v", pod)
	}
	byName := map[string]sysdigProbe{}
	for _, probe := range sysdigProbes {
		byName[probe.name] = probe
		if probe.live == (probe.seconds > 0) {
			t.Fatalf("probe %q must be either live or timed", probe.name)
		}
	}
	engine := []string{"--modern-bpf"}
	failed := sysdigArgs(byName["Failed syscalls · 15 s"], single, engine, "")
	if strings.Join(failed, " ") != "--modern-bpf -M 15 -t h -n 2000 proc.pid=7796 and evt.failed=true" {
		t.Fatal(failed)
	}
	live := sysdigArgs(byName["Live syscalls"], tree, nil, "")
	if strings.Join(live, " ") != "--unbuffered -t h (proc.pid=7796 or proc.apid=7796)" {
		t.Fatal(live)
	}
	top := sysdigArgs(byName["Top syscalls by time · 15 s"], container, engine, "")
	if strings.Join(top, " ") != "--modern-bpf -M 15 -c topscalls_time container.id=0123456789ab" {
		t.Fatal(top)
	}
	at := time.Date(2026, 9, 14, 15, 4, 5, 0, time.UTC)
	path := sysdigCapturePath(pod, at)
	capture := sysdigArgs(byName["Capture to file · 30 s"], pod, engine, path)
	if path != "systemdoc-sysdig-pod-shop-web-abc-20260914-150405.scap.gz" || strings.Join(capture, " ") != "--modern-bpf -M 30 -z -w "+path+" (k8s.pod.name=web-abc and k8s.ns.name=shop)" {
		t.Fatal(path, capture)
	}
	if args := sysdigArgs(byName["Slow file I/O over 1 ms · 15 s"], single, nil, ""); strings.Join(args, " ") != "-M 15 -c fileslower 1 proc.pid=7796" {
		t.Fatal(args)
	}
}

func TestSysdigEngineFollowsTheKernel(t *testing.T) {
	t.Setenv("SYSTEMDOC_SYSDIG_ENGINE", "")
	for release, want := range map[string]string{"7.1.9-arch1-2": "--modern-bpf", "5.8.0": "--modern-bpf", "5.4.0-150-generic": "", "4.18.0-553.el8": "", "garbage": ""} {
		if got := strings.Join(sysdigEngine(release), " "); got != want {
			t.Fatalf("%s → %q, want %q", release, got, want)
		}
	}
	t.Setenv("SYSTEMDOC_SYSDIG_ENGINE", "kmod")
	if len(sysdigEngine("7.1.9")) != 0 {
		t.Fatal("kmod override ignored")
	}
	t.Setenv("SYSTEMDOC_SYSDIG_ENGINE", "bpf")
	if strings.Join(sysdigEngine("4.18.0"), " ") != "-B" {
		t.Fatal("bpf override ignored")
	}
	for osRelease, want := range map[string]string{"ID=arch\n": "pacman", "NAME=\"Ubuntu\"\nID=ubuntu\n": "apt", "ID=\"fedora\"\n": "dnf", "ID=alpine\n": "apk", "ID=nixos\n": "github.com/draios/sysdig"} {
		if hint := sysdigInstallHint(osRelease); !strings.Contains(hint, want) {
			t.Fatalf("%q → %q", osRelease, hint)
		}
	}
}

func TestSysdigCollectionRunsThroughCachedSudo(t *testing.T) {
	dir := t.TempDir()
	sudo := `#!/bin/sh
[ "$1" = -n ] && [ "$2" = -- ] || { echo "unexpected sudo flags: $*" >&2; exit 90; }
shift 2
exec "$@"
`
	sysdig := `#!/bin/sh
echo "sysdig args: $*"
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(sudo), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sysdig"), []byte(sysdig), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	probe := sysdigProbe{name: "Top syscalls by count · 15 s", seconds: 15, args: []string{"-c", "topscalls"}}
	native := sysdigRunner{kind: "native", argv: []string{"sudo", "-n", "--", "sysdig"}}
	output, err := collectSysdig(context.Background(), native, probe, sysdigArgs(probe, sysdigTarget{filter: "proc.pid=1"}, []string{"--modern-bpf"}, ""))
	if err != nil || strings.TrimSpace(output) != "sysdig args: --modern-bpf -M 15 -c topscalls proc.pid=1" {
		t.Fatal(output, err)
	}
	expired := "#!/bin/sh\necho 'sudo: a password is required' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(expired), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := collectSysdig(context.Background(), native, probe, nil); err == nil || !strings.Contains(err.Error(), "authorization expired") {
		t.Fatal("expired sudo timestamp must be explained", err)
	}
}

func TestSysdigRunnerDetection(t *testing.T) {
	dir := t.TempDir()
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("SYSTEMDOC_SYSDIG", "")
	ctx := context.Background()
	// Nothing installed: explain both routes.
	if _, err := detectSysdigRunner(ctx); err == nil || !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), sysdigImage) {
		t.Fatal(err)
	}
	// A binary that loads wins.
	write("sysdig", "#!/bin/sh\necho sysdig version 0.41.4\n")
	runner, err := detectSysdigRunner(ctx)
	if err != nil || runner.kind != "native" || !runner.needsSudo() || runner.display([]string{"-M", "5", "proc.pid=1"}) != "sudo sysdig -M 5 proc.pid=1" {
		t.Fatalf("%+v %v", runner, err)
	}
	// A binary whose libraries do not load falls back to the container when Docker works.
	write("sysdig", "#!/bin/sh\necho 'sysdig: symbol lookup error: /usr/lib/libscap_engine_gvisor.so.0: undefined symbol: _ZN4absl12lts_20260526' >&2\nexit 127\n")
	if _, err := detectSysdigRunner(ctx); err == nil || !strings.Contains(err.Error(), "symbol lookup error") || !strings.Contains(err.Error(), "Docker is not available") {
		t.Fatal("broken binary without Docker must show the loader error", err)
	}
	write("docker", "#!/bin/sh\ncase \"$1\" in info) echo 29.7.2 ;; image) echo sha256:abc ;; *) echo \"docker $*\" ;; esac\n")
	runner, err = detectSysdigRunner(ctx)
	if err != nil || runner.kind != "container" || runner.needsSudo() || !strings.Contains(runner.note, "symbol lookup error") {
		t.Fatalf("%+v %v", runner, err)
	}
	argv := strings.Join(runner.argv, " ")
	for _, want := range []string{"docker run --rm -i --privileged --pid=host --net=host --name systemdoc-sysdig-", "-v /proc:/host/proc:ro", "-v /dev:/host/dev", "-v /etc:/host/etc:ro", ":/capture -w /capture", "--entrypoint sysdig " + sysdigImage} {
		if !strings.Contains(argv, want) {
			t.Fatalf("container argv missing %q: %s", want, argv)
		}
	}
	if !runner.imagePresent(ctx) {
		t.Fatal("image inspect success must count as present")
	}
	// The container runner passes probe arguments straight through.
	probe := sysdigProbe{name: "Top syscalls by count · 15 s", seconds: 15, args: []string{"-c", "topscalls"}}
	output, err := collectSysdig(ctx, runner, probe, sysdigArgs(probe, sysdigTarget{filter: "proc.pid=1"}, []string{"--modern-bpf"}, ""))
	if err != nil || !strings.HasSuffix(strings.TrimSpace(output), "--entrypoint sysdig "+sysdigImage+" --modern-bpf -M 15 -c topscalls proc.pid=1") {
		t.Fatal(output, err)
	}
	// Explicit choices are honoured and explained.
	t.Setenv("SYSTEMDOC_SYSDIG", "native")
	if _, err := detectSysdigRunner(ctx); err == nil || !strings.Contains(err.Error(), "SYSTEMDOC_SYSDIG=native") {
		t.Fatal(err)
	}
	t.Setenv("SYSTEMDOC_SYSDIG", "container")
	if runner, err := detectSysdigRunner(ctx); err != nil || runner.kind != "container" {
		t.Fatal(runner, err)
	}
}

func TestSysdigStreamStopsCleanlyOnCancel(t *testing.T) {
	dir := t.TempDir()
	// Real sudo relays SIGINT to its child; the fake execs the child so the
	// signal lands directly, and the fake sysdig prints a summary on SIGINT
	// the way the real one does.
	sudo := `#!/bin/sh
[ "$1" = -n ] && [ "$2" = -- ] || exit 90
shift 2
exec "$@"
`
	sysdig := `#!/bin/sh
trap 'echo "-- summary: capture stopped"; exit 0' INT
echo "args: $*"
i=0
while [ $i -lt 200 ]; do echo "event $i"; i=$((i+1)); /bin/sleep 0.1; done
`
	for name, script := range map[string]string{"sudo": sudo, "sysdig": sysdig} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	var last, ending string
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSysdig(ctx, sysdigRunner{kind: "native", argv: []string{"sudo", "-n", "--", "sysdig"}}, []string{"--unbuffered", "-t", "h", "proc.pid=1"}, func(text, end string) {
			last, ending = text, end
			if strings.Contains(text, "event 3") && ctx.Err() == nil {
				cancel()
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not finish after cancel")
	}
	if !strings.Contains(last, "args: --unbuffered -t h proc.pid=1") || !strings.Contains(last, "summary: capture stopped") || ending != "\n[sysdig stopped]" {
		t.Fatalf("SIGINT was not relayed for a clean stop:\n%s\nending %q", last, ending)
	}
	live := sysdigArgs(sysdigProbes[0], sysdigTarget{filter: "proc.pid=1"}, []string{"--modern-bpf"}, "")
	if strings.Join(live, " ") != "--modern-bpf --unbuffered -t h proc.pid=1" {
		t.Fatal(live)
	}
}

func TestSudoValidationFeedsThePasswordOnStdin(t *testing.T) {
	dir := t.TempDir()
	sudo := `#!/bin/sh
case "$*" in
 "-n -v") exit 1 ;;
 "-S -v -p ") read -r secret; [ "$secret" = "correct horse" ] && exit 0; echo "Sorry, try again." >&2; exit 1 ;;
 *) echo "unexpected: $*" >&2; exit 90 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(sudo), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if sudoCached(context.Background()) {
		t.Fatal("no cached credentials expected")
	}
	if err := sudoValidate(context.Background(), []byte("correct horse")); err != nil {
		t.Fatal(err)
	}
	if err := sudoValidate(context.Background(), []byte("wrong")); err == nil || !strings.Contains(err.Error(), "Sorry") {
		t.Fatal("wrong password must surface sudo's message", err)
	}
}

func TestCollectedChiselKeepsOnlyTheFinalFrame(t *testing.T) {
	text := "\x1b[2J\x1b[0;0H# Calls  Syscall\n10  read\n\x1b[2J\x1b[0;0H# Calls  Syscall\n20  read\n4   write\n\x1b[2J\x1b[0;0H\x1b[?25h\n"
	frame := lastFrame(text)
	if strings.Contains(frame, "10  read") || !strings.Contains(frame, "20  read") {
		t.Fatalf("intermediate frame kept or final table lost: %q", frame)
	}
	if lastFrame("plain output\n") != "plain output\n" {
		t.Fatal("plain output must pass through")
	}
}
