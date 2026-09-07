package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionSharesPrivateSocketAndCleansUp(t *testing.T) {
	o := Options{Host: "server", User: "deploy", Binary: "systemdoc"}
	close, err := o.shareConnection()
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	directory := filepath.Dir(o.controlPath)
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("control directory must be private", info, err)
	}
	for _, tty := range []bool{false, true} {
		args := strings.Join(o.sshArgs(tty, "true"), "|")
		for _, expected := range []string{"ControlMaster=auto", "ControlPersist=60", "ControlPath=" + o.controlPath} {
			if !strings.Contains(args, expected) {
				t.Fatal("SSH call does not reuse authentication", args)
			}
		}
	}
	close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("control directory not cleaned", err)
	}
}

func TestKeyInstallationCopiesOnlySelectedPublicKey(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "test key")
	for _, path := range []string{key, key + ".pub"} {
		if err := os.WriteFile(path, []byte("test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "copy-args")
	t.Setenv("SSH_TEST_ARGS", log)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SSH_TEST_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh-copy-id"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	o := Options{Host: "server", User: "deploy", Port: 2222, Binary: "systemdoc"}
	if err := InstallKey(o, key); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "-i\n"+key+".pub\n") || !strings.Contains(got, "User=deploy") || !strings.Contains(got, "-p\n2222\n") {
		t.Fatal(got)
	}
	if strings.Contains(got, key+"\n") {
		t.Fatal("private key passed for copying")
	}
	if err := InstallKey(o, key+".pub"); err == nil {
		t.Fatal("public key accepted as private key path")
	}
}

func TestKeyInstallationRefusesOrphanedPublicKey(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key+".pub", []byte("existing public key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh-copy-id"), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := InstallKey(Options{Host: "server", Binary: "systemdoc"}, key); err == nil {
		t.Fatal("orphaned public key could be overwritten")
	}
	if data, _ := os.ReadFile(key + ".pub"); string(data) != "existing public key" {
		t.Fatal("existing key changed")
	}
}
