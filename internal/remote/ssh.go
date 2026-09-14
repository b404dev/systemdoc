// Package remote launches Systemdoc under the remote host's own terminal and permissions.
package remote

import (
	"context"
	"debug/elf"
	"debug/macho"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Host         string
	User         string
	Port         int
	Identity     string
	Binary       string
	Upload       bool
	UploadBinary string
	Args         []string
	controlPath  string
}

var hostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9_.-]+@)?[A-Za-z0-9][A-Za-z0-9_.:-]*$`)
var userPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*\$?$`)
var temporaryDirectory = regexp.MustCompile(`^/tmp/systemdoc\.[A-Za-z0-9]+$`)

// Validate checks connection options before opening or suspending a terminal.
func (o Options) Validate() error {
	if o.User != "" && !userPattern.MatchString(o.User) {
		return fmt.Errorf("SSH username contains unsupported characters")
	}
	if user, _, embedded := strings.Cut(o.Host, "@"); embedded {
		if !userPattern.MatchString(user) {
			return fmt.Errorf("SSH username contains unsupported characters")
		}
		if o.User != "" && user != o.User {
			return fmt.Errorf("username differs from user@host; use one matching SSH username")
		}
	}
	if o.UploadBinary != "" && !o.Upload {
		return fmt.Errorf("--upload-binary requires --upload")
	}
	if !hostPattern.MatchString(o.Host) {
		return fmt.Errorf("use an SSH config alias or user@hostname (no URI or shell syntax)")
	}
	if o.Port < 0 || o.Port > 65535 {
		return fmt.Errorf("SSH port must be between 1 and 65535, or zero for SSH config default")
	}
	if o.Binary == "" {
		return fmt.Errorf("remote binary cannot be empty")
	}
	return nil
}

// OpenSSH sends a command string to a remote shell: quote each argument explicitly.
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func (o Options) sshArgs(tty bool, command string) []string {
	args := []string{"-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
	if o.controlPath != "" {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPersist=60", "-o", "ControlPath="+o.controlPath)
	}
	if tty {
		args = append(args, "-t")
	}
	if o.User != "" {
		args = append(args, "-l", o.User)
	}
	if o.Port != 0 {
		args = append(args, "-p", strconv.Itoa(o.Port))
	}
	if o.Identity != "" {
		args = append(args, "-i", o.Identity)
	}
	return append(args, "--", o.Host, command)
}

// uploadTimeout bounds the ~10 MB binary upload; SYSTEMDOC_UPLOAD_TIMEOUT
// (a Go duration such as 30m) extends it for very slow links.
var uploadTimeout = func() time.Duration {
	if value, err := time.ParseDuration(os.Getenv("SYSTEMDOC_UPLOAD_TIMEOUT")); err == nil && value > 0 {
		return value
	}
	return 15 * time.Minute
}()

func (o Options) capture(command string, input io.Reader) (string, error) {
	return o.captureWithin(2*time.Minute, command, input)
}

// captureWithin runs one remote command with its own deadline; the binary
// upload needs far longer than the checks on a slow link.
func (o Options) captureWithin(timeout time.Duration, command string, input io.Reader) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", o.sshArgs(false, command)...)
	cmd.Stdin = input
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("remote setup: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func Run(o Options) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("OpenSSH client is required: %w", err)
	}
	closeConnection, err := o.shareConnection()
	if err != nil {
		return err
	}
	defer closeConnection()
	if o.Upload {
		binary := o.UploadBinary
		if binary == "" {
			var err error
			binary, err = os.Executable()
			if err != nil {
				return err
			}
		}
		platform, err := o.capture("uname -sm", nil)
		if err != nil {
			return err
		}
		if err = checkBinary(binary, platform); err != nil {
			return err
		}
		directory, err := o.capture("mktemp -d /tmp/systemdoc.XXXXXXXXXX", nil)
		if err != nil {
			return err
		}
		if !temporaryDirectory.MatchString(directory) {
			return fmt.Errorf("remote mktemp returned an unexpected directory")
		}
		o.Binary = directory + "/systemdoc"
		defer func() {
			// Remove only the two artifacts this invocation created, never recursive trees.
			_, err := o.capture("rm -f -- "+shellQuote(o.Binary)+" && rmdir -- "+shellQuote(directory), nil)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Remote cleanup failed; remove temporary directory manually:", directory)
			}
		}()
		file, err := os.Open(binary)
		if err != nil {
			return err
		}
		_, err = o.captureWithin(uploadTimeout, "umask 077; cat > "+shellQuote(o.Binary)+" && chmod 700 "+shellQuote(o.Binary), file)
		file.Close()
		if err != nil {
			return err
		}
	}
	words := append(remoteEnvironment(os.Getenv("COLORTERM"), os.Getenv("TERM")), shellQuote(o.Binary))
	for _, arg := range o.Args {
		words = append(words, shellQuote(arg))
	}
	cmd := exec.Command("ssh", o.sshArgs(true, "exec "+strings.Join(words, " "))...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SSH session ended: %w (install Systemdoc remotely, specify --remote-bin, or use --upload)", err)
	}
	return nil
}

// remoteEnvironment carries the local terminal's colour capability to the
// remote command. sshd forwards TERM but not COLORTERM, so without this a
// 24-bit terminal is treated as 256 colours on the far side and every
// gradient is quantised into visible blocks.
func remoteEnvironment(colorterm, term string) []string {
	truecolor := false
	switch colorterm {
	case "truecolor", "24bit", "24-bit":
		truecolor = true
	}
	for _, marker := range []string{"ghostty", "kitty", "truecolor", "direct", "alacritty", "wezterm", "foot"} {
		if strings.Contains(term, marker) {
			truecolor = true
		}
	}
	if !truecolor {
		return nil
	}
	return []string{"env", "COLORTERM=truecolor"}
}

func checkBinary(path, platform string) error {
	if strings.HasPrefix(platform, "Darwin ") {
		if platform != "Darwin arm64" {
			return fmt.Errorf("macOS upload supports Apple Silicon (arm64) only")
		}
		file, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("upload to macOS requires a matching Mach-O executable: %w", err)
		}
		defer file.Close()
		expected := map[macho.Cpu]string{macho.CpuArm64: "Darwin arm64"}
		if file.Type != macho.TypeExec || expected[file.Cpu] == "" || expected[file.Cpu] != platform {
			return fmt.Errorf("Mach-O executable architecture does not match remote %q", platform)
		}
		return nil
	}

	file, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("upload requires a Linux ELF binary: %w", err)
	}
	defer file.Close()
	expected := map[elf.Machine]string{elf.EM_X86_64: "Linux x86_64", elf.EM_AARCH64: "Linux aarch64"}
	if expected[file.Machine] == "" || expected[file.Machine] != platform {
		return fmt.Errorf("binary architecture %s does not match remote %q; supply a matching build with --upload-binary", file.Machine, platform)
	}
	for _, program := range file.Progs {
		if program.Type == elf.PT_INTERP {
			return fmt.Errorf("automatic upload requires a static build; rebuild with CGO_ENABLED=0")
		}
	}
	return nil
}
