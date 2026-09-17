package dashboard

import (
	"fmt"
	"strings"
	"testing"
)

func TestSignalGraphicsModesAndBounds(t *testing.T) {
	values := []float64{0, 1, 2, 4, -1}
	blocks := signalChart(values, "blocks")
	braille := signalChart(values, "braille")
	ascii := signalChart(values, "ascii")
	if blocks == braille || braille == ascii || blocks == ascii {
		t.Fatal("graphics modes did not produce distinct charts")
	}
	if !strings.Contains(blocks, "█") || !strings.Contains(braille, "⣿") || !strings.Contains(ascii, "@") {
		t.Fatal("charts do not show their peak glyph", blocks, braille, ascii)
	}
	if graphMode("unknown") != "blocks" || signalDelta([]float64{2, 5}) != "↑ +3.0" || signalDelta([]float64{5, 2}) != "↓ -3.0" {
		t.Fatal("signal normalization or direction is incorrect")
	}
	if signalDirection([]float64{2, 5}) != "↑ rising" || signalDirection([]float64{5, 2}) != "↓ falling" {
		t.Fatal("signal direction label is incorrect")
	}
	area := signalArea([]float64{0, 4, 8, 12, 16}, 16, "blocks")
	if strings.Count(area, "\n") != 1 || !strings.Contains(area, "█") {
		t.Fatal("two-row signal area was not rendered", area)
	}
	history := []float64{}
	for i := 0; i < 80; i++ {
		history = appendSignal(history, float64(i))
	}
	if len(history) != 60 || history[0] != 20 {
		t.Fatal("signal history is not bounded", len(history), history[0])
	}
}

func TestConstellationBuildsOperationalMap(t *testing.T) {
	item := workload{ID: "nginx.service", Name: "nginx.service", State: "active", PID: 42, CPU: "12%", Memory: "48 MiB"}
	states := map[string]workload{
		"nginx.service":      item,
		"postgresql.service": {ID: "postgresql.service", State: "failed"},
		"redis.service":      {ID: "redis.service", State: "active"},
		"network.target":     {ID: "network.target", State: "active"},
	}
	raw := "nginx.service\n● redis.service\n○ php-fpm.service\n● postgresql.service\n● network.target\n○ nginx.socket\n● system.slice\n○ dev-sda1.device\n○ tmp.mount\n○ mystery"
	got := constellationText(item, 0, raw, false, states)
	for _, want := range []string{"SYSTEM CONSTELLATION", "MAP nginx.service", "PID 42", "relationships", "network.target", "3 Network", "does not add background polling"} {
		if !strings.Contains(got, want) {
			t.Fatalf("constellation missing %q:\n%s", want, got)
		}
	}
	// Grouping by unit kind, with accent-heading counts.
	for _, want := range []string{"── SERVICES · 3\n", "── TARGETS · 1\n", "── SOCKETS · PATHS · TIMERS · 1\n", "── MOUNTS · 1\n", "── SLICES · SCOPES · 1\n", "── DEVICES · 1\n", "── OTHER · 1\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("constellation missing group heading %q:\n%s", want, got)
		}
	}
	// Attention first, then active, then the rest; each marked by its own state.
	failed, active, unknown := strings.Index(got, "! postgresql.service  failed"), strings.Index(got, "● redis.service  active"), strings.Index(got, "○ php-fpm.service  not in inventory")
	if failed < 0 || active < 0 || unknown < 0 || !(failed < active && active < unknown) {
		t.Fatalf("constellation ordering is wrong (failed=%d active=%d unknown=%d):\n%s", failed, active, unknown, got)
	}
	if strings.Contains(got, "more lines available") || strings.Count(got, "nginx.service") != 1 {
		t.Fatalf("systemd map should list every unit once and never fall back to the flat cap:\n%s", got)
	}
}

func TestConstellationCapsEachGroupAndKeepsRawModeBounded(t *testing.T) {
	var raw strings.Builder
	for i := 0; i < 55; i++ {
		fmt.Fprintf(&raw, "unit-%02d.service\n", i)
	}
	raw.WriteString("basic.target\n")
	states := map[string]workload{"unit-54.service": {ID: "unit-54.service", State: "failed"}}
	got := constellationText(workload{ID: "app.target", Name: "app.target"}, 0, raw.String(), false, states)
	if !strings.Contains(got, "── SERVICES · 55\n") || !strings.Contains(got, "   └─ … 15 more in Dependencies\n") {
		t.Fatalf("per-group cap missing:\n%s", got)
	}
	if strings.Count(got, ".service  ") != 40 || !strings.Contains(got, "! unit-54.service  failed") {
		t.Fatalf("expected 40 services with the failed one surfaced first:\n%s", got)
	}
	if !strings.Contains(got, "── TARGETS · 1\n   └─ ○ basic.target  not in inventory\n") {
		t.Fatalf("target group missing after the capped services:\n%s", got)
	}
	docker := constellationText(workload{ID: "abc", Name: "web"}, 1, raw.String(), false, nil)
	if strings.Count(docker, "   ├─ unit-") != 23 || !strings.Contains(docker, "… 33 more lines available in the normal Dependencies/Connections view") || strings.Contains(docker, "── ") {
		t.Fatalf("docker mode should keep the flat 23-line cap:\n%s", docker)
	}
	empty := constellationText(workload{ID: "x.service"}, 0, "relationship lookup failed: boom", false, nil)
	if !strings.Contains(empty, "   ├─ relationship lookup failed: boom\n") {
		t.Fatalf("collector messages must stay visible:\n%s", empty)
	}
}

func TestWorkloadHistoryIsBoundedAndFeedsFocusLens(t *testing.T) {
	w := testWorkspace()
	for i := 0; i < 70; i++ {
		w.sampleWorkloads(0, []workload{{ID: "nginx.service", Name: "nginx.service", State: "active", CPU: "12%", Memory: "48 MiB"}})
	}
	history := w.workloadHistory["0/false/nginx.service"]
	if len(history) != 60 {
		t.Fatal("workload history is not bounded", len(history))
	}
	w.items[0] = []workload{{ID: "nginx.service", Name: "nginx.service", State: "active", CPU: "12%", Memory: "48 MiB"}}
	w.selected[0] = "nginx.service"
	w.renderTable()
	if !strings.Contains(w.selectionCard.GetText(true), "CPU") || !strings.Contains(w.selectionCard.GetText(true), "MEM") {
		t.Fatal("focus lens did not render workload trends")
	}
}
