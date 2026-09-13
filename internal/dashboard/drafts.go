package dashboard

import (
	"bytes"
	"os"

	"path/filepath"
	"regexp"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var unitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*\.service$`)

func (w *workspace) newService() { w.newServiceFrom("") }

func (w *workspace) newServiceFrom(content string) {
	if usesLaunchd() {
		w.message("Systemd drafts unavailable", "macOS uses launchd property lists. Create and register a LaunchAgent/LaunchDaemon with the native tools, then inspect it here.")
		return
	}
	name := tview.NewInputField().SetLabel(" Unit name ").SetText("my-worker.service")
	editor := tview.NewTextArea().SetText("[Unit]\nDescription=My background worker\nAfter=network-online.target\n\n[Service]\nType=simple\nExecStart=/usr/bin/sleep infinity\nRestart=on-failure\nRestartSec=5s\n\n[Install]\nWantedBy=default.target\n", true)
	if content != "" {
		editor.SetText(content, true)
	}
	scope := tview.NewDropDown().SetLabel(" Scope ").SetOptions([]string{"User service", "System service (sudo)"}, nil)
	editor.SetBorder(true).SetTitle(" New service · edit draft ")
	buttons := tview.NewForm().AddButton("Validate and review", func() {
		unit := name.GetText()
		if !unitName.MatchString(unit) {
			w.message("Invalid name", "Use a simple name ending in .service; paths are not accepted.")
			return
		}
		index, _ := scope.GetCurrentOption()
		w.validateDraft(unit, editor.GetText(), index == 0)
	}).AddButton("Cancel", func() { w.pages.RemovePage("draft"); w.app.SetFocus(w.table) })
	layout := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(name, 1, 0, false).AddItem(scope, 1, 0, false).AddItem(editor, 0, 1, true).AddItem(buttons, 3, 0, false)
	// Explicit Tab routing keeps multiline editing separate from buttons.
	name.SetDoneFunc(func(_ tcell.Key) { w.app.SetFocus(scope) })
	layout.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyTab {
			if w.app.GetFocus() == editor {
				w.app.SetFocus(buttons)
			} else if w.app.GetFocus() == scope || w.app.GetFocus() == name {
				w.app.SetFocus(editor)
			} else {
				return e
			}
			return nil
		}
		return e
	})
	w.pages.AddPage("draft", layout, true, true)
}

func (w *workspace) validateDraft(name, content string, user bool) {
	w.footer.SetText(" Validating service draft…")
	go func() {
		dir, err := os.MkdirTemp("", "systemdoc-draft-")
		if err != nil {
			w.queue(func() { w.message("Draft error", err.Error()) })
			return
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, name)
		err = os.WriteFile(path, []byte(content), 0600)
		var output string
		if err == nil {
			args := []string{"verify", path}
			if user {
				args = append([]string{"--user"}, args...)
			}
			output, err = command(w.ctx, "systemd-analyze", args...)
		}
		w.queue(func() {
			if err != nil {
				w.message("Validation failed", err.Error())
				return
			}
			config, err := os.UserConfigDir()
			if err != nil {
				w.message("Cannot install", err.Error())
				return
			}
			target := filepath.Join(config, "systemd", "user", name)
			if !user {
				target = filepath.Join("/etc/systemd/system", name)
			}
			w.confirm("Install service", target+"\n\n"+content+"\n\n"+output+"\nInstalls a new file and reloads the selected manager. Does not enable or start it.", func() {
				if w.operationCancel != nil {
					w.message("Operation running", "Wait for the active operation before installing a unit.")
					return
				}
				if !user {
					w.installSystemDraft(name, target, content)
					return
				}
				if err := installNewUnit(target, content); err != nil {
					w.message("Install failed", err.Error())
					return
				}
				w.pages.RemovePage("draft")
				w.user = true
				w.mode = 0
				w.chrome()
				w.execute(name, "systemctl", []string{"--user", "daemon-reload"})
			})
		})
	}()
}

func installNewUnit(target, content string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return writeNewFile(target, content, 0644)
}

func (w *workspace) installSystemDraft(name, target, content string) {
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		w.message("Unit already exists", "Use Edit unit override for an existing system unit.")
		return
	}
	dir, err := os.MkdirTemp("", "systemdoc-install-")
	if err != nil {
		w.message("Cannot stage unit", err.Error())
		return
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, name)
	if err = os.WriteFile(source, []byte(content), 0644); err != nil {
		w.message("Cannot stage unit", err.Error())
		return
	}
	// cp refuses to replace any path that appeared after the review. The private staging directory prevents other users editing the source.
	if err = w.native("sudo", "--", "cp", "--no-clobber", "--preserve=mode", "--", source, target); err != nil {
		return
	}
	saved, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(saved, []byte(content)) {
		w.message("Installation conflict", "The destination was not installed as reviewed. Existing files were not overwritten.")
		return
	}
	w.pages.RemovePage("draft")
	w.user = false
	w.mode = 0
	w.chrome()
	if err = w.native("sudo", "--", "systemctl", "daemon-reload"); err != nil {
		w.message("Installed, reload failed", "The unit file exists; retry daemon-reload before starting it.")
	}
}
