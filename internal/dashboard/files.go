package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeNewFile publishes a complete file atomically, and never replaces an existing path.
func writeNewFile(target, content string, mode os.FileMode) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, ".systemdoc-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(f.Name(), target); err != nil {
		return fmt.Errorf("cannot publish new file (existing paths are never overwritten): %w", err)
	}
	return nil
}
