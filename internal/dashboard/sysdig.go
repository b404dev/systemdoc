package dashboard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// sysdig looks at what a binary actually does at runtime: every syscall, file,
// connection and error, attributed to processes, containers and pods. It needs
// root and a capture driver, so every probe here is reviewed first, names the
// exact command, and is bounded in time and output.

type sysdigTarget struct {
	label, filter, stem string
}

func sysdigProcessTarget(process hostProcess, children bool) sysdigTarget {
	pid := strconv.Itoa(process.PID)
	target := sysdigTarget{label: "PID " + pid + " · " + shortCommand(process.Command), filter: "proc.pid=" + pid, stem: "pid-" + pid}
	if children {
		target.label += " and children"
		target.filter = "(proc.pid=" + pid + " or proc.apid=" + pid + ")"
		target.stem += "-tree"
	}
	return target
}

// sysdig identifies containers by the 12-character short ID the runtime prints.
func sysdigContainerTarget(item workload) sysdigTarget {
	id := item.ID
	if len(id) > 12 {
		id = id[:12]
	}
	return sysdigTarget{label: "container " + item.Name, filter: "container.id=" + id, stem: "container-" + id}
}

// Pod attribution relies on the runtime labelling containers with their pod;
// host-native runtimes do, a cluster nested inside another container may not.
func sysdigPodTarget(item workload) sysdigTarget {
	namespace, name := podRef(item)
	return sysdigTarget{label: "pod " + item.Name, filter: "(k8s.pod.name=" + name + " and k8s.ns.name=" + namespace + ")", stem: "pod-" + namespace + "-" + name}
}

type sysdigProbe struct {
	name, description string
	live              bool
	seconds           int
	args              []string
	failedOnly        bool
}

// Each entry is one question about the target. Live probes stream into a
// panel until Escape; timed probes collect for a fixed window and show a
// summary the reader can export.
var sysdigProbes = []sysdigProbe{
	{name: "Live syscalls", description: "Every system call as it happens · Escape stops", live: true, args: []string{"-t", "h"}},
	{name: "Failed syscalls · 15 s", description: "Only calls that returned an error, with the errno", seconds: 15, args: []string{"-t", "h", "-n", "2000"}, failedOnly: true},
	{name: "Top syscalls by count · 15 s", description: "Which calls dominate the workload", seconds: 15, args: []string{"-c", "topscalls"}},
	{name: "Top syscalls by time · 15 s", description: "Where the process spends its time in the kernel", seconds: 15, args: []string{"-c", "topscalls_time"}},
	{name: "Slow syscalls over 1 ms · 15 s", description: "Individual calls that took longer than a millisecond", seconds: 15, args: []string{"-c", "scallslower", "1"}},
	{name: "Top files by bytes · 15 s", description: "Which files it reads and writes most", seconds: 15, args: []string{"-c", "topfiles_bytes"}},
	{name: "Slow file I/O over 1 ms · 15 s", description: "File operations that stalled", seconds: 15, args: []string{"-c", "fileslower", "1"}},
	{name: "File errors · 15 s", description: "Files whose operations failed, ranked", seconds: 15, args: []string{"-c", "topfiles_errors"}},
	{name: "Top connections · 15 s", description: "Network peers ranked by bytes", seconds: 15, args: []string{"-c", "topconns"}},
	{name: "Descriptor I/O · live", description: "Data read from and written to files and sockets · Escape stops", live: true, args: []string{"-c", "echo_fds"}},
	{name: "stdout · live", description: "What the process prints to standard output · Escape stops", live: true, args: []string{"-c", "stdout"}},
	{name: "stderr · live", description: "What the process prints to standard error · Escape stops", live: true, args: []string{"-c", "stderr"}},
	{name: "Capture to file · 30 s", description: "Record everything for offline sysdig -r analysis", seconds: 30, args: []string{"-z", "-w"}},
}

// sysdigEngine chooses the capture driver. The CO-RE BPF probe needs no kernel
// module and works from kernel 5.8; older kernels use the scap module.
func sysdigEngine(kernel string) []string {
	if value := os.Getenv("SYSTEMDOC_SYSDIG_ENGINE"); value != "" {
		switch value {
		case "kmod":
			return nil
		case "bpf":
			return []string{"-B"}
		default:
			return []string{"--modern-bpf"}
		}
	}
	major, minor := kernelVersion(kernel)
	if major > 5 || (major == 5 && minor >= 8) {
		return []string{"--modern-bpf"}
	}
	return nil
}

func kernelVersion(release string) (int, int) {
	parts := strings.SplitN(release, ".", 3)
	if len(parts) < 2 {
		return 0, 0
	}
	major, _ := strconv.Atoi(strings.TrimLeft(parts[0], "v"))
	minor, _ := strconv.Atoi(strings.TrimFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' }))
	return major, minor
}

// sysdigArgs assembles the reviewed argv (after the sysdig binary). The filter
// always comes last, and the capture probe gets its private output path.
func sysdigArgs(probe sysdigProbe, target sysdigTarget, engine []string, capturePath string) []string {
	args := append([]string{}, engine...)
	if probe.live {
		// Lines must reach the panel as they happen, not in 4 KiB stdio chunks.
		args = append(args, "--unbuffered")
	}
	if probe.seconds > 0 {
		args = append(args, "-M", strconv.Itoa(probe.seconds))
	}
	args = append(args, probe.args...)
	if len(probe.args) > 0 && probe.args[len(probe.args)-1] == "-w" {
		args = append(args, capturePath)
	}
	filter := target.filter
	if probe.failedOnly {
		filter += " and evt.failed=true"
	}
	return append(args, filter)
}

func sysdigCapturePath(target sysdigTarget, at time.Time) string {
	return "systemdoc-sysdig-" + target.stem + "-" + at.Format("20060102-150405") + ".scap.gz"
}

// sysdigInstallHint reads the distribution so the message names the package
// manager the reader actually has.
func sysdigInstallHint(osRelease string) string {
	id := ""
	for _, line := range strings.Split(osRelease, "\n") {
		if value, ok := strings.CutPrefix(line, "ID="); ok {
			id = strings.Trim(value, "\"")
		}
	}
	switch id {
	case "arch", "manjaro", "endeavouros", "cachyos", "omarchy":
		return "sudo pacman -S sysdig"
	case "debian", "ubuntu", "linuxmint", "pop":
		return "sudo apt install sysdig"
	case "fedora", "rhel", "centos", "rocky", "almalinux":
		return "sudo dnf install sysdig"
	case "opensuse", "opensuse-tumbleweed", "opensuse-leap", "sles":
		return "sudo zypper install sysdig"
	case "alpine":
		return "sudo apk add sysdig"
	}
	return "install the sysdig package from https://github.com/draios/sysdig"
}

// sysdigRunner is how sysdig is executed: the distribution binary through
// sudo, or the official container image when the binary is missing or its
// libraries do not load (a frozen package set can ship a sysdig linked
// against a different abseil or protobuf than the system has).
type sysdigRunner struct {
	kind  string // native or container
	argv  []string
	note  string
	image string
}

const sysdigImage = "docker.io/sysdig/sysdig:0.41.4"

func (r sysdigRunner) needsSudo() bool { return r.kind == "native" }

// command line as shown in the review dialog.
func (r sysdigRunner) display(args []string) string {
	shown := append([]string{}, r.argv...)
	if r.kind == "native" {
		shown = []string{"sudo", "sysdig"}
	}
	return strings.Join(append(shown, args...), " ")
}

// The container needs the host's process table, devices and account names to
// report the same PIDs and users the dashboard shows, and the Docker socket
// for container names. The image entrypoint is bypassed: it runs the kernel
// module loader, which the BPF probe does not need and which can outlast a
// probe window. Captures land in the working directory via /capture.
func containerSysdigArgv(name, workdir string) []string {
	argv := []string{"docker", "run", "--rm", "-i", "--privileged", "--pid=host", "--net=host", "--name", name,
		"-v", "/var/run/docker.sock:/host/var/run/docker.sock",
		"-v", "/dev:/host/dev", "-v", "/proc:/host/proc:ro", "-v", "/etc:/host/etc:ro",
		"-v", workdir + ":/capture", "-w", "/capture"}
	// Timestamps should match the host's clock display, not UTC.
	if _, err := os.Stat("/etc/localtime"); err == nil {
		argv = append(argv, "-v", "/etc/localtime:/etc/localtime:ro")
	}
	return append(argv, "--entrypoint", "sysdig", sysdigImage)
}

// probeNativeSysdig reports whether the installed binary actually loads; a
// symbol lookup error surfaces here instead of inside every probe.
func probeNativeSysdig(ctx context.Context) error {
	if _, err := exec.LookPath("sysdig"); err != nil {
		return err
	}
	output, err := command(ctx, "sysdig", "--version")
	if err != nil {
		return fmt.Errorf("%s", firstLine(strings.TrimSpace(output+"\n"+err.Error())))
	}
	return nil
}

func dockerUsable(ctx context.Context) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	_, err := command(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	return err == nil
}

// detectSysdigRunner prefers the native binary when it loads, falls back to
// the container image when Docker is usable, and otherwise explains both.
// SYSTEMDOC_SYSDIG=native|container forces one.
func detectSysdigRunner(ctx context.Context) (sysdigRunner, error) {
	if usesLaunchd() {
		return sysdigRunner{}, fmt.Errorf("sysdig captures Linux kernel events; it is not available on macOS")
	}
	workdir, _ := os.Getwd()
	name := fmt.Sprintf("systemdoc-sysdig-%d-%d", os.Getpid(), time.Now().Unix())
	container := sysdigRunner{kind: "container", argv: containerSysdigArgv(name, workdir), image: sysdigImage, note: "runs the official sysdig image with Docker · no sudo · privileged container with the host's /proc, /dev and /etc, BPF probe only"}
	native := sysdigRunner{kind: "native", argv: []string{"sudo", "-n", "--", "sysdig"}, note: "runs the installed sysdig through sudo"}
	mode := os.Getenv("SYSTEMDOC_SYSDIG")
	nativeErr := probeNativeSysdig(ctx)
	switch mode {
	case "native":
		if nativeErr != nil {
			return sysdigRunner{}, fmt.Errorf("SYSTEMDOC_SYSDIG=native but sysdig does not run · %v", nativeErr)
		}
		return native, nil
	case "container":
		if !dockerUsable(ctx) {
			return sysdigRunner{}, fmt.Errorf("SYSTEMDOC_SYSDIG=container but Docker is not usable")
		}
		return container, nil
	}
	if nativeErr == nil {
		return native, nil
	}
	if dockerUsable(ctx) {
		if _, lookErr := exec.LookPath("sysdig"); lookErr == nil {
			container.note += " · the installed sysdig does not load: " + nativeErr.Error()
		}
		return container, nil
	}
	hint := "install sysdig"
	if raw, err := os.ReadFile("/etc/os-release"); err == nil {
		hint = sysdigInstallHint(string(raw))
	}
	if _, lookErr := exec.LookPath("sysdig"); lookErr == nil {
		return sysdigRunner{}, fmt.Errorf("the installed sysdig does not run (%v) and Docker is not available for the container image", nativeErr)
	}
	return sysdigRunner{}, fmt.Errorf("sysdig is not installed · %s · or make Docker available and Systemdoc will use %s", hint, sysdigImage)
}

// imagePresent reports whether the container image is already pulled, so
// the first run can show a pull step instead of a silent wait.
func (r sysdigRunner) imagePresent(ctx context.Context) bool {
	if r.kind != "container" {
		return true
	}
	_, err := command(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", r.image)
	return err == nil
}

// sysdigCommand runs a probe through the runner. Cancelling sends SIGINT:
// sudo relays it to sysdig, and the docker CLI proxies it into the container,
// so the capture stops cleanly and prints its summary exactly as Ctrl-C
// would; a stuck child is killed after a grace period. For the container the
// named container is interrupted first, synchronously, so a killed CLI cannot
// leave a privileged sysdig running: Wait does not return until Cancel has,
// and quitting the workspace waits for the stream before exiting.
func sysdigCommand(ctx context.Context, runner sysdigRunner, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, runner.argv[0], append(append([]string{}, runner.argv[1:]...), args...)...)
	cmd.Cancel = func() error {
		if runner.kind == "container" {
			for i, arg := range runner.argv {
				if arg == "--name" && i+1 < len(runner.argv) {
					killCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
					exec.CommandContext(killCtx, "docker", "kill", "--signal", "INT", runner.argv[i+1]).Run()
					done()
				}
			}
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 3 * time.Second
	return cmd
}

// collectSysdig runs a timed probe to completion. The window plus a grace
// period bounds the command; output is capped at 4 MiB.
func collectSysdig(ctx context.Context, runner sysdigRunner, probe sysdigProbe, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(probe.seconds+20)*time.Second)
	defer cancel()
	output := tailBuffer{limit: 4 * 1024 * 1024}
	cmd := sysdigCommand(ctx, runner, args)
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	text := output.String()
	if err != nil && strings.Contains(text, "password is required") {
		return text, fmt.Errorf("sudo authorization expired · run the probe again to authenticate")
	}
	if err != nil {
		return text, fmt.Errorf("sysdig: %w", err)
	}
	return lastFrame(text), nil
}

// Top-style chisels redraw the terminal every second; a collected result
// keeps only the final table rather than every intermediate frame.
func lastFrame(text string) string {
	frames := strings.Split(text, "\x1b[2J")
	for i := len(frames) - 1; i >= 0; i-- {
		if strings.TrimSpace(clean(frames[i])) != "" {
			return frames[i]
		}
	}
	return text
}

// streamSysdig runs a live probe and publishes the bounded buffer every 400 ms
// until the context is cancelled or sysdig exits.
func streamSysdig(ctx context.Context, runner sysdigRunner, args []string, update func(text, ending string)) {
	streamCommand(ctx, sysdigCommand(ctx, runner, args), "sysdig", update)
}

// streamCommand runs any long-lived command and publishes its bounded output
// every 400 ms until the context is cancelled or the command exits. The final
// update carries the ending so a panel can say whether the stream stopped or
// failed. Cancellation sends the command's configured interrupt.
func streamCommand(ctx context.Context, cmd *exec.Cmd, name string, update func(text, ending string)) {
	output := tailBuffer{limit: 2 * 1024 * 1024}
	cmd.Stdout, cmd.Stderr = &output, &output
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	previous := ""
	for {
		select {
		case err := <-done:
			text := output.String()
			ending := "\n[" + name + " stopped]"
			if ctx.Err() == nil && err != nil {
				ending = "\n" + name + " failed: " + firstLine(err.Error())
				if strings.Contains(text, "password is required") {
					ending = "\nsudo authorization expired · reopen the probe to authenticate"
				}
			} else if ctx.Err() == nil {
				ending = "\n[" + name + " ended]"
			}
			update(text, ending)
			return
		case <-ticker.C:
			text := output.String()
			if text != previous {
				previous = text
				update(text, "")
			}
		}
	}
}
