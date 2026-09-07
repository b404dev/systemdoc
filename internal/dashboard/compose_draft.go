package dashboard

import (
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (w *workspace) newCompose() {
	name := tview.NewInputField().SetLabel(" Project ").SetText("my-project")
	path := tview.NewInputField().SetLabel(" Absolute Compose path ")
	editor := tview.NewTextArea().SetText("services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n    restart: unless-stopped\n", true)
	editor.SetBorder(true).SetTitle(" New Compose source · Tab moves to controls ")
	buttons := tview.NewForm().AddButton("Validate and save", func() {
		target, projectName, content := path.GetText(), name.GetText(), editor.GetText()
		if !filepath.IsAbs(target) || projectName == "" {
			w.message("Missing fields", "Enter an absolute file path and project name.")
			return
		}
		for _, p := range w.settings.Projects {
			if p.Name == projectName && p.Endpoint == dockerEndpoint() {
				w.message("Duplicate project", "Choose a new project name.")
				return
			}
		}
		w.validateComposeDraft(projectName, target, content)
	}).AddButton("Cancel", func() { w.pages.RemovePage("compose-draft"); w.app.SetFocus(w.table) })
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(name, 1, 0, false).AddItem(path, 1, 0, false).AddItem(editor, 0, 1, true).AddItem(buttons, 3, 0, false)
	name.SetDoneFunc(func(tcell.Key) { w.app.SetFocus(path) })
	path.SetDoneFunc(func(tcell.Key) { w.app.SetFocus(editor) })
	panel.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyTab && w.app.GetFocus() == editor {
			w.app.SetFocus(buttons)
			return nil
		}
		return e
	})
	w.pages.AddPage("compose-draft", panel, true, true)
}

func (w *workspace) validateComposeDraft(name, target, content string) {
	dir := filepath.Dir(target)
	w.footer.SetText(" Validating Compose source…")
	go func() {
		file, err := os.CreateTemp(dir, ".systemdoc-*.yaml")
		if err != nil {
			w.queue(func() { w.message("Cannot stage Compose source", err.Error()) })
			return
		}
		defer os.Remove(file.Name())
		_, err = file.WriteString(content)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			_, err = command(w.ctx, "docker", "compose", "--project-name", name, "--project-directory", dir, "--file", file.Name(), "config", "--quiet")
		}
		w.queue(func() {
			if err != nil {
				w.message("Compose validation failed", err.Error())
				return
			}
			w.confirm("Save Compose project", target+"\n\n"+content+"\nCreates a new source file and registers it. Does not deploy containers.", func() {
				err := writeNewFile(target, content, 0600)
				if err != nil {
					w.message("Save failed", err.Error())
					return
				}
				w.settings.Projects = append(w.settings.Projects, project{Name: name, Directory: dir, Files: []string{target}, Endpoint: dockerEndpoint()})
				w.savePreferences()
				w.pages.RemovePage("compose-draft")
				w.projectDialog()
			})
		})
	}()
}
