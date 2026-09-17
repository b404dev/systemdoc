package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// constellationGroupCap bounds each unit-kind group so a huge target such as
// multi-user.target still fits a scrollable view; the full list stays one
// keypress away in the Dependencies tab.
const constellationGroupCap = 40

// constellationRawCap keeps the Docker/pod Connections text bounded as before.
const constellationRawCap = 23

// dependencyGroups orders the relationship map by unit kind. The heading text
// starts with "── " so richOutput paints it in the accent colour on tab 4.
var dependencyGroups = []struct {
	name     string
	suffixes []string
}{
	{"SERVICES", []string{".service"}},
	{"TARGETS", []string{".target"}},
	{"SOCKETS · PATHS · TIMERS", []string{".socket", ".path", ".timer"}},
	{"MOUNTS", []string{".mount", ".automount", ".swap"}},
	{"SLICES · SCOPES", []string{".slice", ".scope"}},
	{"DEVICES", []string{".device"}},
	{"OTHER", nil},
}

// dependencyKind maps a unit name to its dependencyGroups index by suffix.
func dependencyKind(name string) int {
	for i, group := range dependencyGroups {
		for _, suffix := range group.suffixes {
			if strings.HasSuffix(name, suffix) {
				return i
			}
		}
	}
	return len(dependencyGroups) - 1
}

// dependencyRank orders units inside a group: attention first, then active,
// then everything else including units the inventory does not know about.
func dependencyRank(item workload, known bool) int {
	switch {
	case known && needsAttention(item):
		return 0
	case known && isActive(item):
		return 1
	default:
		return 2
	}
}

type dependencyEntry struct {
	name  string
	item  workload
	known bool
}

// constellationText renders the relationship map as plain text. The caller
// paints it with richOutput(content, 4, palette); the text carries no markup.
// states maps unit ID to the current inventory entry so each dependency is
// marked by its own state; it must be built on the UI goroutine by the caller.
func constellationText(item workload, mode int, raw string, nerd bool, states map[string]workload) string {
	var out strings.Builder
	title := item.Name
	if title == "" {
		title = item.ID
	}
	fmt.Fprintf(&out, "SYSTEM CONSTELLATION\n\n%s %s\n", iconFor(nerd, iconConstellation), title)
	fmt.Fprintf(&out, "├─ state      %s %s\n", stateSymbol(item, nerd), available(item.State))
	if item.PID > 0 {
		fmt.Fprintf(&out, "├─ process    PID %d\n", item.PID)
	}
	if item.CPU != "" || item.Memory != "" {
		fmt.Fprintf(&out, "├─ resources  CPU %s · MEM %s\n", available(item.CPU), available(item.Memory))
	}
	if mode == 1 && isPod(item) {
		fmt.Fprintf(&out, "├─ namespace  %s\n", available(item.Project))
		fmt.Fprintf(&out, "├─ owner      %s\n", available(item.Owner))
		fmt.Fprintf(&out, "├─ images     %s\n", available(item.Description))
	} else if mode == 1 {
		fmt.Fprintf(&out, "├─ project    %s\n", available(item.Project))
		fmt.Fprintf(&out, "├─ image      %s\n", available(item.Description))
	}
	out.WriteString("└─ relationships\n")
	lines := relationshipLines(item, raw)
	shown := 0
	if mode == 1 {
		shown = writeRawRelationships(&out, lines)
	} else {
		shown = writeUnitRelationships(&out, lines, nerd, states)
	}
	if shown == 0 {
		out.WriteString("   └─ no relationships reported\n")
	}
	out.WriteString("\nRoutes: 3 Network · 4 Processes · d Dependencies/Connections\nThis map is collected on demand and does not add background polling.")
	return out.String()
}

// relationshipLines strips tree decoration and the unit's own line from the
// collected text, keeping the collector's order.
func relationshipLines(item workload, raw string) []string {
	var lines []string
	for _, line := range strings.Split(clean(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == item.ID || line == item.Name || strings.HasPrefix(line, "SYSTEM CONSTELLATION") {
			continue
		}
		line = strings.TrimLeft(line, "●○◆◇├└│─ ")
		if line == "" || line == item.ID {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// writeRawRelationships keeps the Connections view text as a flat, bounded list.
func writeRawRelationships(out *strings.Builder, lines []string) int {
	shown := 0
	for _, line := range lines {
		if shown >= constellationRawCap {
			fmt.Fprintf(out, "   └─ … %d more lines available in the normal Dependencies/Connections view\n", len(lines)-shown)
			break
		}
		fmt.Fprintf(out, "   ├─ %s\n", line)
		shown++
	}
	return shown
}

// writeUnitRelationships groups systemd dependencies by unit kind, marks each
// by its own inventory state and lists attention units first in every group.
func writeUnitRelationships(out *strings.Builder, lines []string, nerd bool, states map[string]workload) int {
	groups := make([][]dependencyEntry, len(dependencyGroups))
	seen := make(map[string]bool, len(lines))
	var notes []string
	for _, line := range lines {
		if strings.ContainsAny(line, " \t") {
			// Collector messages such as "relationship lookup failed: …" are
			// not unit names; keep them visible above the map.
			notes = append(notes, line)
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		entry := dependencyEntry{name: line}
		entry.item, entry.known = states[line]
		kind := dependencyKind(line)
		groups[kind] = append(groups[kind], entry)
	}
	shown := 0
	for _, note := range notes {
		fmt.Fprintf(out, "   ├─ %s\n", note)
		shown++
	}
	for kind, entries := range groups {
		if len(entries) == 0 {
			continue
		}
		sort.SliceStable(entries, func(i, j int) bool {
			return dependencyRank(entries[i].item, entries[i].known) < dependencyRank(entries[j].item, entries[j].known)
		})
		fmt.Fprintf(out, "── %s · %d\n", dependencyGroups[kind].name, len(entries))
		limit := len(entries)
		if limit > constellationGroupCap {
			limit = constellationGroupCap
		}
		for i, entry := range entries[:limit] {
			branch := "├─"
			if i == len(entries)-1 {
				branch = "└─"
			}
			symbol, state := iconFor(nerd, iconIdle), "not in inventory"
			if entry.known {
				symbol, state = stateSymbol(entry.item, nerd), available(entry.item.State)
			}
			fmt.Fprintf(out, "   %s %s %s  %s\n", branch, symbol, entry.name, state)
			shown++
		}
		if limit < len(entries) {
			fmt.Fprintf(out, "   └─ … %d more in Dependencies\n", len(entries)-limit)
		}
	}
	return shown
}

// inventoryStates indexes the current inventory by unit ID for the map. It
// reads w.items and therefore runs on the UI goroutine only.
func (w *workspace) inventoryStates() map[string]workload {
	items := w.items[w.mode]
	states := make(map[string]workload, len(items))
	for _, item := range items {
		states[item.ID] = item
	}
	return states
}

func (w *workspace) constellation() {
	item := w.current()
	if item.ID == "" {
		w.message("Constellation unavailable", "Select a workload first.")
		return
	}
	focus := w.app.GetFocus()
	ctx, cancel := context.WithCancel(w.ctx)
	view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
	view.SetBorder(true).SetTitle(" " + w.iconLabel(iconConstellation, "SYSTEM CONSTELLATION · x · Esc returns "))
	p := w.palette()
	view.SetText(fmt.Sprintf("[%s::b]%s %s[-::-]\n\n[%s]Mapping relationships on demand…[-]", p.accent, w.icon(iconConstellation), tview.Escape(clean(item.Name)), p.muted))
	close := func() {
		cancel()
		w.pages.RemovePage("constellation")
		w.app.SetFocus(focus)
	}
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'x' {
			close()
			return nil
		}
		return event
	})
	w.pages.AddPage("constellation", centered(view, 124, 34), true, true)
	mode, user, nerd := w.mode, w.user, w.settings.NerdIcons
	// Snapshot the inventory here: the goroutine below must never read w.items.
	states := w.inventoryStates()
	go func() {
		// Reuse the existing relationship collector. It is invoked only for this lens.
		raw, err := inspect(ctx, mode, user, item, 4)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			raw = "relationship lookup failed: " + err.Error()
		}
		content := constellationText(item, mode, raw, nerd, states)
		w.queue(func() {
			if ctx.Err() == nil {
				view.SetText(richOutput(content, 4, w.palette()))
				view.SetTitle(" SYSTEM CONSTELLATION · sampled " + time.Now().Format("15:04:05") + " · Esc returns ")
			}
		})
	}()
}
