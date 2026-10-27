package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"systemdoc/internal/promapi"
)

// sampleDetection is what a server with node_exporter looks like.
func sampleDetection() metricsDetection {
	node, _ := promapi.FamilyByID("node")
	names := map[string]bool{}
	for _, name := range promapi.DetectNames() {
		names[name] = strings.HasPrefix(name, "node_")
	}
	return metricsDetection{
		families:  []promapi.Family{node},
		names:     names,
		instances: map[string][]string{"node": {"localhost:9100"}},
		targets:   []promapi.Target{{Job: "node", Instance: "localhost:9100", Health: "up", ScrapeInterval: 15 * time.Second}, {Job: "pve", Instance: "pve:9221", Health: "down", LastError: "connection refused [red]x"}},
		interval:  15 * time.Second,
	}
}

func sampleResults(panels []*metricsPanel, columns int) []metricsResult {
	var results []metricsResult
	for _, panel := range panels {
		values := make([]float64, columns)
		for i := range values {
			values[i] = float64(i % 100)
		}
		values[columns/2] = -1
		results = append(results, metricsResult{id: panel.spec.ID, ceiling: panel.spec.Ceiling, step: 15 * time.Second, newest: time.Now(), series: []metricsSeries{{label: "[red]cpu0", values: values, latest: 42}}})
	}
	return results
}

func metricsFixture(t *testing.T, width, height int) (*metricsPage, tcell.SimulationScreen) {
	t.Helper()
	t.Setenv("SYSTEMDOC_PROMETHEUS", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // saving preferences must never touch the real settings file
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(width, height)
	w.settings.PrometheusURL = "http://example.invalid:9090"
	m := w.metricsPage()
	m.endpoint = w.prometheusEndpoint()
	w.app.ForceDraw()
	d := sampleDetection()
	m.apply(true, "", &d, nil, nil)
	w.app.ForceDraw()
	m.apply(true, "", nil, nil, sampleResults(m.visiblePanels(), m.chartWidth()))
	w.app.ForceDraw()
	return m, screen
}

func screenText(screen tcell.SimulationScreen) string {
	width, height := screen.Size()
	var out strings.Builder
	for y := 0; y < height; y++ {
		out.WriteString(screenRow(screen, y, width))
		out.WriteString("\n")
	}
	return out.String()
}

func TestMetricsPageRendersAtThreeSizes(t *testing.T) {
	for _, size := range [][3]int{{160, 44, 3}, {120, 30, 2}, {80, 24, 1}} {
		m, screen := metricsFixture(t, size[0], size[1])
		if m.columns != size[2] || len(m.cards) == 0 {
			t.Fatalf("%dx%d: columns %d cards %d", size[0], size[1], m.columns, len(m.cards))
		}
		text := screenText(screen)
		if !strings.Contains(text, "NODE · CPU busy") || !strings.Contains(text, "42.0%") {
			t.Fatalf("%dx%d is missing panel content:\n%s", size[0], size[1], text)
		}
		if strings.Contains(text, "awaiting") {
			t.Fatalf("%dx%d still shows the waiting state", size[0], size[1])
		}
		m.close()
	}
}

func TestMetricsLabelValuesAreEscapedAndGapsDrawn(t *testing.T) {
	m, screen := metricsFixture(t, 120, 30)
	defer m.close()
	text := screenText(screen)
	if !strings.Contains(text, "[red]cpu0") {
		t.Fatalf("label markup was interpreted instead of shown:\n%s", text)
	}
	if !strings.Contains(text, "·") {
		t.Fatal("a missing sample must draw as a gap")
	}
	p := m.w.palette()
	width, height := screen.Size()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, _, style, _ := screen.GetContent(x, y)
			fg, _, _ := style.Decompose()
			if r == 'c' && fg == tcell.ColorRed && p.error != "#ff0000" {
				t.Fatal("label text rendered in red, so markup leaked through")
			}
		}
	}
}

func TestMetricsGraphModesChangeGlyphs(t *testing.T) {
	m, screen := metricsFixture(t, 120, 30)
	defer m.close()
	seen := map[string]string{}
	for range graphModes {
		seen[m.w.settings.GraphMode] = screenText(screen)
		m.input(tcell.NewEventKey(tcell.KeyRune, 'G', 0))
		m.w.app.ForceDraw()
	}
	if len(seen) != 3 || seen["blocks"] == seen["ascii"] || !strings.Contains(seen["ascii"], "#") {
		t.Fatalf("graph modes did not change the chart: %v", len(seen))
	}
	m.input(tcell.NewEventKey(tcell.KeyRune, 'b', 0))
	m.w.app.ForceDraw()
	if !m.bars {
		t.Fatal("b must toggle bar glyphs")
	}
}

func TestMetricsEmptyStateShowsSetup(t *testing.T) {
	t.Setenv("SYSTEMDOC_PROMETHEUS", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(120, 30)
	m := w.metricsPage()
	defer m.close()
	m.install = promapi.InstallSteps(promapi.InstallContext{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get"})
	m.apply(false, "", nil, nil, nil) // an unreachable server routes to setup
	m.state = metricsUnconfigured     // the state refresh() reports without any URL
	m.render()
	w.app.ForceDraw()
	if m.view != metricsSetupView {
		t.Fatalf("view %d state %d", m.view, m.state)
	}
	text := screenText(screen)
	for _, want := range []string{"CONNECTION", "No Prometheus URL", "settings.json", "apt-get install", "NODE EXPORTER"} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup view lacks %q:\n%s", want, text)
		}
	}
	m.input(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	if front, _ := w.pages.GetFrontPage(); front != "confirm" {
		t.Fatalf("Enter must open the approval card, front page is %s", front)
	}
}

func TestMetricsTargetsViewAndNavigation(t *testing.T) {
	m, screen := metricsFixture(t, 120, 30)
	m.input(tcell.NewEventKey(tcell.KeyRune, 't', 0))
	m.w.app.ForceDraw()
	text := screenText(screen)
	if !strings.Contains(text, "UP") || !strings.Contains(text, "pve:9221") || !strings.Contains(text, "[red]x") || !strings.Contains(text, "url") {
		t.Fatalf("targets view:\n%s", text)
	}
	if row, _ := m.table.GetSelection(); row != 1 || m.w.app.GetFocus() != m.table {
		t.Fatalf("table focus/selection: row %d", row)
	}
	m.input(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	if m.w.app.GetFocus() != m.detail {
		t.Fatal("Tab must move to the detail pane")
	}
	m.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if m.w.app.GetFocus() != m.table {
		t.Fatal("Escape returns to the table")
	}
	m.input(tcell.NewEventKey(tcell.KeyRune, '4', 0))
	if front, _ := m.w.pages.GetFrontPage(); front != "processes" {
		t.Fatalf("4 must open Processes, got %s", front)
	}
	_, page := m.w.pages.GetFrontPage()
	page.(*hostPage).close()
	m.w.openHostTab(5)
	if front, _ := m.w.pages.GetFrontPage(); front != "metrics" {
		t.Fatalf("suite 6 must open Metrics, got %s", front)
	}
	_, page = m.w.pages.GetFrontPage()
	page.(*metricsPage).input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if front, _ := m.w.pages.GetFrontPage(); front != "main" || m.w.app.GetFocus() != m.w.table {
		t.Fatalf("Escape must return to the dashboard, got %s", front)
	}
}

func TestMetricsPanelSelectionScrolls(t *testing.T) {
	m, _ := metricsFixture(t, 80, 24)
	defer m.close()
	if m.visibleRows < 1 || len(m.panels) <= m.visibleRows {
		t.Skip("not enough panels to scroll")
	}
	m.input(tcell.NewEventKey(tcell.KeyEnd, 0, 0))
	if m.selected != len(m.panels)-1 || m.firstRow == 0 || m.selectedCard() == nil {
		t.Fatalf("End: selected %d firstRow %d", m.selected, m.firstRow)
	}
	m.input(tcell.NewEventKey(tcell.KeyHome, 0, 0))
	if m.selected != 0 || m.firstRow != 0 {
		t.Fatalf("Home: selected %d firstRow %d", m.selected, m.firstRow)
	}
}

// fakePrometheus answers enough of the API for one node_exporter host.
func fakePrometheus(t *testing.T) *httptest.Server {
	t.Helper()
	vector := func(w http.ResponseWriter, value string) {
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"instance":"localhost:9100"},"value":[%d,"%s"]}]}}`, time.Now().Unix(), value)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status/buildinfo":
			fmt.Fprint(w, `{"status":"success","data":{"version":"2.53.1"}}`)
		case "/api/v1/status/runtimeinfo":
			fmt.Fprint(w, `{"status":"success","data":{"startTime":"2024-05-01T08:00:00Z","reloadConfigSuccess":true}}`)
		case "/api/v1/label/__name__/values":
			fmt.Fprint(w, `{"status":"success","data":["node_cpu_seconds_total","node_memory_MemTotal_bytes"]}`)
		case "/api/v1/label/instance/values":
			fmt.Fprint(w, `{"status":"success","data":["localhost:9100"]}`)
		case "/api/v1/targets":
			fmt.Fprint(w, `{"status":"success","data":{"activeTargets":[{"labels":{"job":"node","instance":"localhost:9100"},"health":"up","scrapeInterval":"15s","lastScrape":"2024-05-01T10:00:00Z"}]}}`)
		case "/api/v1/query":
			vector(w, "8")
		case "/api/v1/query_range":
			now := time.Now().Unix()
			fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"cpu":"0"},"values":[[%d,"12.5"],[%d,"37.5"]]}]}}`, now-15, now)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestMetricsPageLiveAgainstFakeServer(t *testing.T) {
	defer resetPrometheusState()
	resetPrometheusState()
	server := fakePrometheus(t)
	defer server.Close()
	t.Setenv("SYSTEMDOC_PROMETHEUS", server.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorkspace(ctx, false)
	w.colourDepthKnown = true
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(120, 30)
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	defer func() { cancel(); w.app.Stop(); <-done }()
	var m *metricsPage
	w.app.QueueUpdateDraw(func() { m = w.metricsPage() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ready bool
		var text string
		w.app.QueueUpdateDraw(func() {
			ready = m != nil && m.state == metricsReady && len(m.panels) > 0 && len(m.panels[0].series) > 0
			text = screenText(screen)
		})
		if ready {
			if !strings.Contains(text, "37.5%") || !strings.Contains(text, "NODE · CPU busy") {
				t.Fatalf("live page:\n%s", text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("page never became ready; state %v failure %q", m.state, m.failure)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMetricsPinPersistsAndUnpins(t *testing.T) {
	m, screen := metricsFixture(t, 120, 30)
	defer m.close()
	pin := pinnedMetric{Title: "Open files", Name: "process_open_fds", Selector: `{job="prometheus"}`, Unit: promapi.UnitCount}
	if err := m.savePin(pin); err != nil {
		t.Fatal(err)
	}
	if err := m.savePin(pin); err == nil {
		t.Fatal("a duplicate title must be rejected")
	}
	last := m.panels[len(m.panels)-1]
	if !last.pinned || last.spec.Query != `process_open_fds{job="prometheus"}` || m.selected != len(m.panels)-1 {
		t.Fatalf("pinned panel: %+v selected %d", last.spec, m.selected)
	}
	m.w.app.ForceDraw()
	if text := screenText(screen); !strings.Contains(text, "PIN · Open files") {
		t.Fatalf("pinned card not shown:\n%s", text)
	}
	saved, _ := readSettings()
	if len(saved.PrometheusMetrics) != 1 || saved.PrometheusMetrics[0] != pin {
		t.Fatalf("settings: %+v", saved.PrometheusMetrics)
	}
	m.input(tcell.NewEventKey(tcell.KeyRune, 'd', 0))
	if front, _ := m.w.pages.GetFrontPage(); front != "confirm" {
		t.Fatalf("d must ask for approval, front page is %s", front)
	}
	m.w.pages.RemovePage("confirm")
	m.removePin(pin)
	saved, _ = readSettings()
	if len(saved.PrometheusMetrics) != 0 || m.panels[len(m.panels)-1].pinned {
		t.Fatal("unpin must remove the pin from settings and the page")
	}
}

func TestMetricsFullScreenFlicksBetweenPanels(t *testing.T) {
	m, screen := metricsFixture(t, 120, 30)
	defer m.close()
	m.input(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	if m.focus == nil {
		t.Fatal("Enter must open the full-screen view")
	}
	if front, _ := m.w.pages.GetFrontPage(); front != "metrics-focus" || m.w.app.GetFocus() != m.focus {
		t.Fatalf("front page %s", front)
	}
	m.w.app.ForceDraw()
	text := screenText(screen)
	if !strings.Contains(text, "1 of") || !strings.Contains(text, "NODE · CPU busy") || !strings.Contains(text, "┤") {
		t.Fatalf("full-screen view:\n%s", text)
	}
	if got := m.chartWidth(); got < 100 {
		t.Fatalf("full screen must poll at full width, got %d columns", got)
	}
	if len(m.visiblePanels()) != 1 {
		t.Fatal("only the shown panel is polled while full screen")
	}
	m.focusInput(tcell.NewEventKey(tcell.KeyRight, 0, 0))
	if m.selected != 1 {
		t.Fatalf("Right must flick to the next panel, selected %d", m.selected)
	}
	m.focusInput(tcell.NewEventKey(tcell.KeyRune, 'h', 0))
	if m.selected != 0 {
		t.Fatalf("h must flick back, selected %d", m.selected)
	}
	m.focusInput(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if m.focus != nil || m.w.app.GetFocus() != m.selectedCard() {
		t.Fatal("Escape must close the view and return to the card")
	}
}

func TestMetricsHidesPanelsWithoutData(t *testing.T) {
	m, _ := metricsFixture(t, 120, 30)
	defer m.close()
	before := len(m.panels)
	id := m.panels[0].spec.ID
	m.apply(true, "", nil, nil, []metricsResult{{id: id}})
	if len(m.panels) != before-1 || m.hidden != 1 || m.panels[0].spec.ID == id {
		t.Fatalf("empty panel must be hidden: %d → %d panels, hidden %d", before, len(m.panels), m.hidden)
	}
}
