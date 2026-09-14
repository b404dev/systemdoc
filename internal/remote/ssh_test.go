package remote

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestShellQuoting(t *testing.T) {
	for _, value := range []string{"/opt/My Tools/systemdoc", "a'b", "$(echo BAD)", "; echo BAD", "line\nbreak"} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(value)).Output()
		if err != nil || string(out) != value {
			t.Fatalf("argument changed: %q -> %q (%v)", value, out, err)
		}
	}
}
func TestHostValidation(t *testing.T) {
	for _, host := range []string{"-oProxyCommand=bad", "host;id", "user@host bad", "$(id)", ""} {
		if (Options{Host: host, Binary: "systemdoc"}).Validate() == nil {
			t.Fatal("accepted", host)
		}
	}
	for _, host := range []string{"production", "bill@192.168.1.2", "bill@server.example"} {
		if err := (Options{Host: host, Binary: "systemdoc"}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSSHArguments(t *testing.T) {
	o := Options{Host: "bill@server", Port: 2222, Identity: "/keys/my key"}
	args := o.sshArgs(true, "exec 'systemdoc'")
	if !reflect.DeepEqual(args[len(args)-3:], []string{"--", "bill@server", "exec 'systemdoc'"}) {
		t.Fatal(args)
	}
	if !strings.Contains(strings.Join(args, "|"), "-i|/keys/my key") {
		t.Fatal(args)
	}
	for _, arg := range args {
		if strings.Contains(arg, "StrictHostKeyChecking=no") {
			t.Fatal("disabled host verification")
		}
	}
}

func TestExplicitSSHUsername(t *testing.T) {
	for _, tty := range []bool{false, true} {
		o := Options{Host: "production", User: "deploy", Binary: "systemdoc"}
		if err := o.Validate(); err != nil {
			t.Fatal(err)
		}
		args := o.sshArgs(tty, "exec 'systemdoc'")
		if !strings.Contains(strings.Join(args, "|"), "-l|deploy|--|production|") {
			t.Fatal("login user missing from SSH setup or interactive session", args)
		}
	}
	for _, user := range []string{"-root", "root;id", "user name", "$(id)"} {
		if (Options{Host: "server", User: user, Binary: "systemdoc"}).Validate() == nil {
			t.Fatal("accepted invalid username", user)
		}
	}
	if (Options{Host: "alice@server", User: "bob", Binary: "systemdoc"}).Validate() == nil {
		t.Fatal("ambiguous login accepted")
	}
	if err := (Options{Host: "alice@server", User: "alice", Binary: "systemdoc"}).Validate(); err != nil {
		t.Fatal(err)
	}
	args := (Options{Host: "production", Binary: "systemdoc"}).sshArgs(true, "exec 'systemdoc'")
	for _, arg := range args {
		if arg == "-l" {
			t.Fatal("blank username should preserve SSH configuration")
		}
	}
}

func TestRemoteEnvironmentForwardsTruecolor(t *testing.T) {
	if got := remoteEnvironment("truecolor", "xterm-256color"); len(got) != 2 || got[1] != "COLORTERM=truecolor" {
		t.Fatalf("COLORTERM=truecolor not forwarded: %v", got)
	}
	if got := remoteEnvironment("", "xterm-ghostty"); len(got) != 2 {
		t.Fatalf("known 24-bit terminal not forwarded: %v", got)
	}
	if got := remoteEnvironment("", "xterm-256color"); got != nil {
		t.Fatalf("256-colour terminal must not claim truecolor: %v", got)
	}
}
