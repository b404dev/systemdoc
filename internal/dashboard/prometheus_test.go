package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestPrometheusWorkloadDiscovery(t *testing.T) {
	cases := []struct {
		name string
		mode int
		item workload
		url  string
	}{
		{"active unit", 0, workload{ID: "prometheus.service", Name: "prometheus", State: "active"}, "http://localhost:9090"},
		{"snap unit", 0, workload{ID: "snap.prometheus.prometheus.service", Name: "prometheus", State: "active"}, "http://localhost:9090"},
		{"inactive unit", 0, workload{ID: "prometheus.service", State: "inactive"}, ""},
		{"homebrew agent off macOS", 0, workload{ID: "sh.brew.prometheus", State: "active", PID: 12}, ""},
		{"exporter unit", 0, workload{ID: "prometheus-node-exporter.service", State: "active"}, ""},
		{"image with registry and tag", 1, workload{ID: "abc", Name: "prom", State: "running", Description: "quay.io/prometheus/prometheus:v2.51", Ports: "0.0.0.0:9091->9090/tcp, :::9091->9090/tcp"}, "http://localhost:9091"},
		{"bound address", 1, workload{ID: "abc", State: "running", Description: "prom/prometheus", Ports: "192.168.1.5:9090->9090/tcp"}, "http://192.168.1.5:9090"},
		{"host network", 1, workload{ID: "abc", State: "running", Description: "bitnami/prometheus@sha256:abcd"}, "http://localhost:9090"},
		{"other port only", 1, workload{ID: "abc", State: "running", Description: "prom/prometheus", Ports: "0.0.0.0:8080->8080/tcp"}, ""},
		{"node exporter image", 1, workload{ID: "abc", State: "running", Description: "quay.io/prometheus/node-exporter"}, ""},
		{"pve exporter image", 1, workload{ID: "abc", State: "running", Description: "prompve/prometheus-pve-exporter"}, ""},
		{"stopped container", 1, workload{ID: "abc", State: "exited", Description: "prom/prometheus"}, ""},
		{"pod never matches", 1, workload{ID: podPrefix + "monitoring/prometheus-0", State: "running", Description: "prom/prometheus"}, ""},
	}
	for _, c := range cases {
		endpoint, ok := prometheusWorkload(c.mode, c.item)
		if ok != (c.url != "") || endpoint.URL != c.url {
			t.Errorf("%s: want %q, got %q (%v)", c.name, c.url, endpoint.URL, ok)
		}
		if ok && !strings.HasPrefix(endpoint.Source, "detected: ") {
			t.Errorf("%s: source %q", c.name, endpoint.Source)
		}
	}
	endpoint, ok := discoverPrometheus([]workload{{ID: "sshd.service", State: "active"}}, []workload{{ID: "x", State: "running", Description: "prom/prometheus"}})
	if !ok || endpoint.URL != "http://localhost:9090" {
		t.Errorf("discovery across inventories: %+v %v", endpoint, ok)
	}
}

func TestMetricsFamilyFor(t *testing.T) {
	cases := map[string]workload{
		"node":     {ID: "prometheus-node-exporter.service"},
		"cadvisor": {ID: "abc", Description: "ghcr.io/google/cadvisor:v0.52.1"},
		"pve":      {ID: "abc", Name: "pve-exporter"},
		"targets":  {ID: "prometheus.service"},
		"":         {ID: "nginx.service", Description: "nginx"},
	}
	for want, item := range cases {
		family, ok := metricsFamilyFor(item)
		if family != want || ok != (want != "") {
			t.Errorf("%+v: want %q, got %q", item, want, family)
		}
	}
}

func TestPublishedPortAndImageBase(t *testing.T) {
	if host, port, ok := publishedPort("[::]:9090->9090/tcp", "9090/tcp"); !ok || host != "localhost" || port != "9090" {
		t.Errorf("ipv6 wildcard: %s %s %v", host, port, ok)
	}
	if _, _, ok := publishedPort("9090/tcp", "9090/tcp"); ok {
		t.Error("exposed-only port must not count as published")
	}
	for image, want := range map[string]string{"prometheus": "prometheus", "prom/prometheus:latest": "prometheus", "quay.io/prometheus/prometheus@sha256:ff": "prometheus", "localhost:5000/team/prometheus:v1": "prometheus"} {
		if got := imageBase(image); got != want {
			t.Errorf("%s: got %q", image, got)
		}
	}
}

func TestPrometheusEndpointPrecedence(t *testing.T) {
	t.Setenv("SYSTEMDOC_PROMETHEUS", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := testWorkspace()
	if got := w.prometheusEndpoint(); got.Source != "default" || got.URL != "http://localhost:9090" {
		t.Errorf("default: %+v", got)
	}
	w.items[1] = []workload{{ID: "x", State: "running", Description: "prom/prometheus"}}
	if got := w.prometheusEndpoint(); got.URL != "http://localhost:9090" || !strings.HasPrefix(got.Source, "detected") {
		t.Errorf("discovery: %+v", got)
	}
	w.settings.PrometheusURL = "http://settings:9090"
	if got := w.prometheusEndpoint(); got.Source != "settings" {
		t.Errorf("settings: %+v", got)
	}
	t.Setenv("SYSTEMDOC_PROMETHEUS", "http://env:9090")
	if got := w.prometheusEndpoint(); got.URL != "http://env:9090" {
		t.Errorf("environment: %+v", got)
	}
	w.promSelected = promEndpoint{URL: "http://chosen:9090", Source: "detected: prom"}
	if got := w.prometheusEndpoint(); got.URL != "http://chosen:9090" {
		t.Errorf("selection: %+v", got)
	}
}

func TestPrometheusProbeBackoff(t *testing.T) {
	defer resetPrometheusState()
	resetPrometheusState()
	hits, status := 0, http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(status)
		w.Write([]byte(`{"status":"success","data":{"version":"2.53.1"}}`))
	}))
	defer server.Close()
	ctx := context.Background()
	endpoint := promEndpoint{URL: server.URL}
	if client, _, err := currentPrometheus(ctx, endpoint); client != nil || err == "" {
		t.Fatalf("failed probe: client %v err %q", client, err)
	}
	currentPrometheus(ctx, endpoint)
	if hits != 1 {
		t.Fatalf("back-off ignored: %d hits", hits)
	}
	retryPrometheusProbe()
	currentPrometheus(ctx, endpoint)
	if hits != 2 {
		t.Fatalf("explicit retry: %d hits", hits)
	}
	status = http.StatusOK
	currentPrometheus(ctx, promEndpoint{URL: server.URL + "/"}) // a changed URL is probed at once
	client, build, err := currentPrometheus(ctx, promEndpoint{URL: server.URL + "/"})
	if hits != 3 || client == nil || build.Version != "2.53.1" || err != "" {
		t.Fatalf("success cached: hits %d client %v build %+v err %q", hits, client, build, err)
	}
	if _, _, ok := prometheusStatus(); !ok {
		t.Error("status must report the cached client")
	}
}

func TestPrometheusTokenNeverPersisted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SYSTEMDOC_PROMETHEUS_TOKEN", "super-secret")
	if err := writeSettings(settings{SettingsVersion: 1, PrometheusURL: "http://localhost:9090", PrometheusMetrics: []pinnedMetric{{Title: "Load", Name: "node_load1"}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "systemdoc", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret") || !strings.Contains(string(data), `"prometheus_url": "http://localhost:9090"`) || !strings.Contains(string(data), `"node_load1"`) {
		t.Errorf("settings file: %s", data)
	}
}

func TestPinnedMetricValidate(t *testing.T) {
	good := pinnedMetric{Title: "Load", Name: "node_load1", Selector: `{job="node"}`, Rate: true}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	if got := good.expr(); got != `rate(node_load1{job="node"}[$win])` {
		t.Errorf("expr: %s", got)
	}
	for name, bad := range map[string]pinnedMetric{
		"name":     {Title: "x", Name: "1bad"},
		"selector": {Title: "x", Name: "ok", Selector: `job="node"`},
		"title":    {Name: "ok"},
		"ceiling":  {Title: "x", Name: "ok", Ceiling: -1},
	} {
		if bad.validate() == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestListDockerContainersKeepsPorts(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '{\"ID\":\"abc\",\"Names\":\"prom\",\"State\":\"running\",\"Status\":\"Up\",\"Image\":\"prom/prometheus\",\"Ports\":\"0.0.0.0:9090->9090/tcp\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	items, err := listDockerContainers(context.Background())
	if err != nil || len(items) != 1 || items[0].Ports != "0.0.0.0:9090->9090/tcp" {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestActionsOfferOpenInMetrics(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := testWorkspace()
	w.mode = 1
	w.visible = []workload{{ID: "abc", Name: "prom", State: "running", Description: "prom/prometheus"}}
	w.selected[1] = "abc"
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(120, 40)
	w.actions()
	for _, r := range "Open in Metrics" {
		w.app.GetFocus().InputHandler()(tcell.NewEventKey(tcell.KeyRune, r, 0), func(tview.Primitive) {})
	}
	w.app.ForceDraw()
	var text strings.Builder
	for y := 0; y < 40; y++ {
		text.WriteString(screenRow(screen, y, 120))
	}
	if !strings.Contains(text.String(), "Open in Metrics") || !strings.Contains(text.String(), "localhost:9090") {
		t.Fatalf("actions menu lacks the Prometheus entry:\n%s", text.String())
	}
}
