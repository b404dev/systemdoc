package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (w *workspace) refreshLoop() {
	ticker := time.NewTicker(time.Second)
	animation := time.NewTicker(120 * time.Millisecond)
	ticks := 0
	defer ticker.Stop()
	defer animation.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-animation.C:
			// Idle frames do not queue redraws. Only the small glyph changes
			// during a current-backend inventory job, including enrichment.
			if w.loadingAnimation.Load() {
				w.queue(func() { w.updateLoadingIndicator() })
			}
		case <-ticker.C:
			w.queue(func() {
				w.updateDashboard()
				front, _ := w.pages.GetFrontPage()
				ticks++
				if ticks < w.settings.RefreshSeconds {
					return
				}
				ticks = 0
				if front != "main" || w.loading || w.paused {
					return
				}
				w.startInventory(w.mode)
			})
		}
	}
}

func matchesFilter(item workload, query string) bool {
	for _, term := range strings.Fields(query) {
		key, value, field := strings.Cut(term, ":")
		if field {
			var actual string
			switch key {
			case "state":
				if strings.ToLower(item.State) != value {
					return false
				}
				continue
			case "enabled":
				if item.Enablement == "" || item.Enablement == "default" {
					return false
				}
				enabled := strings.HasPrefix(item.Enablement, "enabled")
				if (value == "true" && !enabled) || (value == "false" && enabled) || (value != "true" && value != "false") {
					return false
				}
				continue
			case "boot", "override":
				if item.Enablement != value {
					return false
				}
				continue
			case "project":
				actual = item.Project
			case "name":
				actual = item.Name + " " + item.Aliases
			default:
				return false
			}
			if !strings.Contains(strings.ToLower(actual), value) {
				return false
			}
		} else if !strings.Contains(strings.ToLower(item.Name+" "+item.Aliases+" "+item.State+" "+item.Project+" "+item.Description), term) {
			return false
		}
	}
	return true
}

func (w *workspace) savePreferences() {
	if w.configError != nil {
		w.message("Settings not saved", "Fix the existing invalid configuration first: "+w.configError.Error())
		return
	}
	if err := writeSettings(w.settings); err != nil {
		w.message("Settings not saved", err.Error())
	}
}
func (w *workspace) favoriteKey(id string) string { return fmt.Sprintf("%d/%t/%s", w.mode, w.user, id) }
func (w *workspace) isFavorite(id string) bool {
	key := w.favoriteKey(id)
	for _, v := range w.settings.Favorites {
		if v == key {
			return true
		}
	}
	return false
}
func (w *workspace) toggleFavorite() {
	item := w.current()
	if item.ID == "" {
		return
	}
	key := w.favoriteKey(item.ID)
	found := false
	for i, v := range w.settings.Favorites {
		if v == key {
			w.settings.Favorites = append(w.settings.Favorites[:i], w.settings.Favorites[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		w.settings.Favorites = append(w.settings.Favorites, key)
	}
	w.savePreferences()
	w.renderTable()
}

func (w *workspace) actions() {
	var choices []choice
	add := func(name, description string, run func()) { choices = append(choices, choice{name, description, run}) }
	add("AI troubleshooting", "Review text before sending to Codex or Claude", w.aiDialog)
	add("Ports and networking", "Find port owners, connections and host interfaces", w.networkPage)
	add("Process explorer", "CPU, memory, parent trees and process ports", func() { w.hostPage(3) })
	add("Disk and storage", "Mount usage, inode pressure and deleted open files", func() { w.hostPage(4) })
	add("Saved views", "Save and restore filters, sorting and layout", w.savedViews)
	if !usesLaunchd() {
		add("Systemd timers", "Browse next and last runs in the current system/user scope", w.timers)
	}
	add("Troubleshooting snapshot", "Collect, review and export a workload report", w.snapshotDialog)
	add("Refresh", "Update observed workloads", w.load)
	add("Workload activity", "Observed state transitions in this session", w.activityView)
	add("Incident storyline", "Chronological state changes across the current suite", w.storylineView)
	add("System Constellation", "Map the selected workload to processes and dependencies", w.constellation)
	add("Export inspector", "Save reviewed output to a new private file", w.exportView)
	add("Run on remote host", "Launch the full suite over SSH and return here on exit", w.remoteDialog)
	add("Preferences", "Refresh rate, layouts, wrapping, and startup", w.preferences)
	add("Theme studio", "Preview and save appearance", w.themeDialog)
	add("Compose projects", "Discover or register Compose source files", w.projectDialog)
	add("Operations", "Session history and cancellation", w.operationHistory)
	add("New Compose project", "Edit and validate source before deployment", w.newCompose)
	if !usesLaunchd() {
		add("New service draft", "Create a service file in an editor, then verify", w.newService)
	}
	item := w.current()
	verbs := serviceVerbs()
	if w.mode == 1 {
		verbs = []string{"start", "stop", "restart", "pause", "unpause", "rm"}
		if isPod(item) {
			verbs = podVerbs(item)
		}
	}
	for _, verb := range verbs {
		verb := verb
		description := "Review exact target and command"
		if isPod(item) && verb == "restart" {
			description = "Rollout restart of " + item.Owner + " · review exact command"
		}
		if isPod(item) && verb == "delete" {
			description = "Delete the pod; its controller, if any, replaces it"
		}
		add(verb, description, func() { w.confirmAction(verb) })
	}
	if w.mode == 0 {
		if !w.user {
			add("System service authorization", "Run a lifecycle command with terminal sudo authentication", w.authorizedActions)
		}
		if !usesLaunchd() {
			add("Edit unit override", "Open systemctl edit in your editor; existing authorization applies", func() {
				args := []string{"edit", item.ID}
				if w.user {
					args = append([]string{"--user"}, args...)
				}
				if item.ID != "" {
					w.native("systemctl", args...)
				}
			})
			add("Follow journal", "Open journalctl -f; Ctrl-C returns", func() {
				args := []string{"-u", item.ID, "-f"}
				if w.user {
					args = append([]string{"--user"}, args...)
				}
				if item.ID != "" {
					w.native("journalctl", args...)
				}
			})
		}
	} else if isPod(item) {
		namespace, name := podRef(item)
		argv := kubeArgv()
		add("Pod shell", "Open /bin/sh in the pod's default container with kubectl exec", func() {
			w.native(argv[0], append(append([]string{}, argv[1:]...), "exec", "-it", "--namespace", namespace, name, "--", "/bin/sh")...)
		})
		add("Follow logs", "Open kubectl logs -f for every container; Ctrl-C returns", func() {
			w.native(argv[0], append(append([]string{}, argv[1:]...), kubeLogArgs(item, true, "100")...)...)
		})
	} else {
		add("Container shell", "Open /bin/sh in selected container", func() {
			if item.ID != "" {
				w.native("docker", "exec", "-it", item.ID, "/bin/sh")
			}
		})
		add("Follow logs", "Open docker logs -f; Ctrl-C returns", func() {
			if item.ID != "" {
				w.native("docker", "logs", "--follow", "--tail", "100", item.ID)
			}
		})
	}
	w.choose("actions", "Actions · "+w.current().Name, choices)
}

func (w *workspace) projectDialog() {

	registered := append([]project(nil), w.settings.Projects...)
	w.summary.SetText(" Discovering Compose projects…")
	go func() {
		projects, err := discoverProjects(w.ctx, registered)
		w.queue(func() {
			w.projects = projects
			w.showProjects()
			if err != nil {
				w.footer.SetText(" Compose discovery: " + clean(err.Error()))
			}
		})
	}()
}
func (w *workspace) showProjects() {
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" Compose projects · Escape closes ")
	list.AddItem("+ Register project", "Keep a project available even after compose down", '+', func() { w.registerProject() })
	for _, p := range w.projects {
		p := p
		list.AddItem(tview.Escape(p.Name), tview.Escape(p.State+" · "+p.Directory), 0, func() { w.projectActions(p) })
	}
	list.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.pages.RemovePage("projects")
			w.app.SetFocus(w.table)
			return nil
		}
		return e
	})
	w.pages.AddPage("projects", list, true, true)
}
func (w *workspace) registerProject() {
	form := tview.NewForm().AddInputField("Project name", "", 35, nil, nil).AddInputField("Compose file (absolute)", "", 65, nil, nil)
	form.AddInputField("Override file (optional)", "", 65, nil, nil).
		AddInputField("Environment file (optional)", "", 65, nil, nil).
		AddInputField("Profiles (comma separated)", "", 45, nil, nil).
		AddInputField("Working directory (optional)", "", 65, nil, nil)
	form.SetBorder(true).SetTitle(" Register Compose project ")
	form.AddButton("Save", func() {
		name := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		file := form.GetFormItem(1).(*tview.InputField).GetText()
		if name == "" || !filepath.IsAbs(file) {
			w.message("Invalid project", "Enter a project name and absolute Compose file path.")
			return
		}
		if _, err := os.Stat(file); err != nil {
			w.message("Cannot read source", err.Error())
			return
		}
		p := project{Name: name, Directory: filepath.Dir(file), Files: []string{file}, Endpoint: dockerEndpoint()}
		if override := form.GetFormItem(2).(*tview.InputField).GetText(); override != "" {
			p.Files = append(p.Files, override)
		}
		if env := form.GetFormItem(3).(*tview.InputField).GetText(); env != "" {
			p.EnvFiles = []string{env}
		}
		if profiles := form.GetFormItem(4).(*tview.InputField).GetText(); profiles != "" {
			for _, v := range strings.Split(profiles, ",") {
				if value := strings.TrimSpace(v); value != "" {
					p.Profiles = append(p.Profiles, value)
				}
			}
		}
		if dir := form.GetFormItem(5).(*tview.InputField).GetText(); dir != "" {
			if !filepath.IsAbs(dir) {
				w.message("Invalid directory", "Use an absolute working directory.")
				return
			}
			p.Directory = dir
		}
		if _, err := projectArgs(p, "validate"); err != nil {
			w.message("Invalid project", err.Error())
			return
		}
		for _, existing := range w.settings.Projects {
			if existing.Name == name && existing.Endpoint == dockerEndpoint() {
				w.message("Duplicate project", "This project name is already registered.")
				return
			}
		}
		w.settings.Projects = append(w.settings.Projects, p)
		w.savePreferences()
		w.pages.RemovePage("register")
		w.projectDialog()
	}).AddButton("Cancel", func() { w.pages.RemovePage("register") })
	w.pages.AddPage("register", form, true, true)
}
func (w *workspace) projectActions(p project) {
	list := tview.NewList()
	list.SetBorder(true).SetTitle(" Compose · " + tview.Escape(p.Name) + " · Escape closes ")
	for _, verb := range []string{"ps", "logs", "config", "validate", "up", "start", "stop", "restart", "pull", "build", "down"} {
		verb := verb
		list.AddItem(verb, "Uses explicit project name, source files, and directory", 0, func() {
			args, err := projectArgs(p, verb)
			if err != nil {
				w.message("Source unavailable", err.Error())
				return
			}
			detail := "docker " + strings.Join(args, " ")
			if verb == "down" {
				detail += "\n\nRemoves project containers and their writable layers. Named volumes are retained."
			}
			w.confirm("Compose "+verb+" · "+p.Name, detail, func() { w.execute(p.Name, "docker", args) })
		})
	}
	list.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			w.pages.RemovePage("project-actions")
			return nil
		}
		return e
	})
	w.pages.AddPage("project-actions", list, true, true)
}
