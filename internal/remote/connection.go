package remote

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Use a private control socket for this invocation only. Upload, execution and
// cleanup reuse one authenticated connection, without changing ~/.ssh/config.
func (o *Options) shareConnection() (func(), error) {
	directory, err := os.MkdirTemp("/tmp", "systemdoc-ssh-")
	if err != nil {
		return nil, fmt.Errorf("SSH control directory: %w", err)
	}
	o.controlPath = filepath.Join(directory, "control")
	return func() {
		if _, err := os.Lstat(o.controlPath); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := o.sshArgs(false, "")
			args = append([]string{"-O", "exit"}, args[:len(args)-1]...)
			if err := exec.CommandContext(ctx, "ssh", args...).Run(); err != nil {
				fmt.Fprintln(os.Stderr, "SSH control connection cleanup failed; idle timeout will close it:", err)
			}
		}
		os.Remove(o.controlPath)
		os.Remove(directory)
	}, nil
}
