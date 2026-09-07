package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type parsedCommand struct {
	mode         int
	user         bool
	verb, target string
	tab          int
}

func parseCommand(text string, currentUser bool) (parsedCommand, error) {
	result := parsedCommand{user: currentUser, tab: -1}
	parts := strings.Fields(text)
	if len(parts) < 3 || strings.ContainsAny(text, ";|&`$<>\n\r") {
		return result, fmt.Errorf("use a supported systemctl, journalctl, or docker command without shell syntax")
	}
	name := parts[0]
	parts = parts[1:]
	if name != "docker" && len(parts) > 0 && parts[0] == "--user" {
		result.user = true
		parts = parts[1:]
	}
	switch name {
	case "systemctl":
		if len(parts) != 2 {
			return result, fmt.Errorf("use systemctl [--user] ACTION UNIT")
		}
		result.verb, result.target = parts[0], parts[1]
		switch result.verb {
		case "status":
			result.tab = 0
		case "cat":
			result.tab = 2
		case "show":
			result.tab = 3
		case "list-dependencies":
			result.tab = 4
		}
	case "journalctl":
		if len(parts) < 2 || parts[0] != "-u" {
			return result, fmt.Errorf("use journalctl [--user] -u UNIT [-f]")
		}
		if len(parts) > 3 || len(parts) == 3 && parts[2] != "-f" {
			return result, fmt.Errorf("unsupported journalctl option")
		}
		result.target = parts[1]
		result.tab = 1
	case "docker":
		result.mode = 1
		if len(parts) == 3 && parts[0] == "logs" && (parts[1] == "-f" || parts[1] == "--follow") {
			parts = []string{parts[0], parts[2]}
		}
		if len(parts) != 2 {
			return result, fmt.Errorf("use docker ACTION CONTAINER or docker logs --follow CONTAINER")
		}
		result.verb, result.target = parts[0], parts[1]
		switch result.verb {
		case "logs":
			result.tab = 1
		case "inspect":
			result.tab = 2
		case "stats":
			result.tab = 3
		}
	default:
		return result, fmt.Errorf("unsupported command %q", name)
	}
	if result.target == "" || strings.HasPrefix(result.target, "-") {
		return result, fmt.Errorf("a workload name is required")
	}
	if result.tab < 0 {
		_, _, err := actionArgs(result.mode, result.user, workload{ID: result.target}, result.verb)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (w *workspace) commandDialog() {
	field := tview.NewInputField().SetLabel(": ").SetFieldWidth(0)
	field.SetBorder(true).SetTitle(" Commands · Tab completes · Up/Down history · Escape cancels ")
	field.SetAutocompleteFunc(func(text string) []string {
		var candidates []string
		for _, prefix := range []string{"systemctl status ", "systemctl restart ", "systemctl --user status ", "journalctl -u ", "docker logs ", "docker restart "} {
			if strings.HasPrefix(prefix, text) {
				candidates = append(candidates, prefix)
			}
			for _, items := range w.items {
				for _, item := range items {
					candidate := prefix + item.Name
					if strings.HasPrefix(candidate, text) {
						candidates = append(candidates, candidate)
					}
				}
			}
		}
		if len(candidates) > 30 {
			candidates = candidates[:30]
		}
		return candidates
	})
	historyIndex := len(w.commandHistory)
	field.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyUp && historyIndex > 0 {
			historyIndex--
			field.SetText(w.commandHistory[historyIndex])
			return nil
		}
		if e.Key() == tcell.KeyDown {
			if historyIndex < len(w.commandHistory)-1 {
				historyIndex++
				field.SetText(w.commandHistory[historyIndex])
			} else {
				historyIndex = len(w.commandHistory)
				field.SetText("")
			}
			return nil
		}
		return e
	})
	field.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter && key != tcell.KeyEscape {
			return
		}
		w.pages.RemovePage("command")
		w.app.SetFocus(w.table)
		if key == tcell.KeyEscape {
			return
		}
		text := field.GetText()
		command, err := parseCommand(text, w.user)
		if err != nil {
			w.message("Unsupported command", err.Error())
			return
		}
		w.commandHistory = append(w.commandHistory, text)
		if len(w.commandHistory) > 100 {
			w.commandHistory = w.commandHistory[1:]
		}
		if command.tab >= 0 {
			w.savedQuickFilters[w.mode] = w.quickFilter
			w.mode = command.mode
			if w.mode == 0 && w.user != command.user {
				if job := w.inventoryJobs[0]; job != nil {
					job.cancel()
					w.inventoryJobs[0] = nil
				}
				w.user = command.user
				w.items[0] = nil
				w.selected[0] = ""
				w.lastRefresh[0] = time.Time{}
				w.fleetHistory[0] = nil
			}
			// An explicit target should not be hidden by a previous mode's filter.
			w.quickFilter = 0
			w.favoriteOnly = false
			w.tab = command.tab
			found := false
			for _, item := range w.items[w.mode] {
				if matchesUnitName(item, command.target) {
					w.selected[w.mode] = item.ID
					found = true
					break
				}
			}
			if !found {
				w.items[w.mode] = append(w.items[w.mode], workload{ID: command.target, Name: command.target, State: "requested"})
				w.selected[w.mode] = command.target
			}
			w.filters[w.mode] = ""
			w.search.SetText("")
			w.chrome()
			w.renderTable()
			return
		}
		name, args, _ := actionArgs(command.mode, command.user, workload{ID: command.target}, command.verb)
		w.confirm("Run command", name+" "+strings.Join(args, " "), func() { w.execute(command.target, name, args) })
	})
	w.pages.AddPage("command", field, true, true)
}
