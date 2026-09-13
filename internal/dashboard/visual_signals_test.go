package dashboard

import (
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
	got := constellationText(item, 0, "nginx.service\nnetwork.target\npostgresql.service", false)
	for _, want := range []string{"SYSTEM CONSTELLATION", "MAP nginx.service", "PID 42", "relationships", "network.target", "3 Network", "does not add background polling"} {
		if !strings.Contains(got, want) {
			t.Fatalf("constellation missing %q:\n%s", want, got)
		}
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
