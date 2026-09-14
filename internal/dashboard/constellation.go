package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func constellationText(item workload, mode int, raw string, nerd bool) string {
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
	lines := strings.Split(clean(raw), "\n")
	shown := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == item.ID || line == item.Name || strings.HasPrefix(line, "SYSTEM CONSTELLATION") {
			continue
		}
		line = strings.TrimLeft(line, "●○◆◇├└│─ ")
		if line == "" {
			continue
		}
		branch := "├─"
		if shown >= 23 {
			fmt.Fprintf(&out, "   └─ … %d more lines available in the normal Dependencies/Connections view\n", len(lines)-shown)
			break
		}
		fmt.Fprintf(&out, "   %s %s\n", branch, line)
		shown++
	}
	if shown == 0 {
		out.WriteString("   └─ no relationships reported\n")
	}
	out.WriteString("\nRoutes: 3 Network · 4 Processes · d Dependencies/Connections\nThis map is collected on demand and does not add background polling.")
	return out.String()
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
	mode, user := w.mode, w.user
	go func() {
		// Reuse the existing relationship collector. It is invoked only for this lens.
		raw, err := inspect(ctx, mode, user, item, 4)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			raw = "relationship lookup failed: " + err.Error()
		}
		content := constellationText(item, mode, raw, w.settings.NerdIcons)
		w.queue(func() {
			if ctx.Err() == nil {
				view.SetText(richOutput(content, 4, w.palette()))
				view.SetTitle(" SYSTEM CONSTELLATION · sampled " + time.Now().Format("15:04:05") + " · Esc returns ")
			}
		})
	}()
}
