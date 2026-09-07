package remote

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultKey is app-specific, so creating it cannot replace the user's normal
// SSH identity. The UI shows this path before the user chooses key installation.
func DefaultKey() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "systemdoc_ed25519"), err
}

func terminalCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// InstallKey is called only after the user explicitly chooses key setup. Only
// the public key goes to the host; ssh-keygen owns the local passphrase prompt.
func InstallKey(o Options, path string) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || strings.HasSuffix(path, ".pub") {
		return fmt.Errorf("choose an absolute private-key path (without .pub)")
	}
	if _, err := exec.LookPath("ssh-copy-id"); err != nil {
		return fmt.Errorf("install the ssh-copy-id utility before setting up a key")
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		// A stale public key must not be overwritten by ssh-keygen either.
		if _, err := os.Lstat(path + ".pub"); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("public-key path already exists or cannot be checked: %s.pub", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Create a Systemdoc SSH key. Choose a passphrase in the ssh-keygen prompt.")
		if err := terminalCommand("ssh-keygen", "-t", "ed25519", "-f", path, "-C", "systemdoc"); err != nil {
			return fmt.Errorf("SSH key generation: %w", err)
		}
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(path + ".pub"); err != nil {
		return fmt.Errorf("matching public key is required at %s.pub: %w", path, err)
	}
	if err := terminalCommand("ssh-copy-id", o.copyKeyArgs(path)...); err != nil {
		return fmt.Errorf("SSH public-key installation: %w", err)
	}
	return nil
}

func (o Options) copyKeyArgs(path string) []string {
	args := []string{"-i", path + ".pub", "-o", "ConnectTimeout=10"}
	if o.User != "" {
		args = append(args, "-o", "User="+o.User)
	}
	if o.Port != 0 {
		args = append(args, "-p", strconv.Itoa(o.Port))
	}
	return append(args, o.Host)
}
