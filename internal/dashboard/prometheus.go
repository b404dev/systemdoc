package dashboard

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"systemdoc/internal/promapi"
)

// maxPinnedMetrics bounds the user's own panels so settings stay small.
const maxPinnedMetrics = 64

// pinnedMetric is a user-chosen metric shown as its own panel in the Metrics
// suite. Name and Selector are kept apart so the picker can validate each.
type pinnedMetric struct {
	Title    string  `json:"title"`
	Name     string  `json:"name"`
	Selector string  `json:"selector,omitempty"` // {job="node"} style
	Unit     string  `json:"unit,omitempty"`
	Rate     bool    `json:"rate,omitempty"`
	Ceiling  float64 `json:"ceiling,omitempty"`
}

var pinnedSelector = regexp.MustCompile(`^\{[^{}\n]*\}$`)

func (m pinnedMetric) validate() error {
	if !promapi.ValidMetricName(m.Name) {
		return fmt.Errorf("metric name %q is not valid", m.Name)
	}
	if m.Selector != "" && !pinnedSelector.MatchString(m.Selector) {
		return fmt.Errorf("selector must look like {job=\"node\"}")
	}
	if title := strings.TrimSpace(m.Title); title == "" || utf8.RuneCountInString(title) > 40 {
		return fmt.Errorf("title must be 1–40 characters")
	}
	if m.Ceiling < 0 {
		return fmt.Errorf("ceiling cannot be negative")
	}
	return nil
}

// expr is the PromQL for the pin; $win is filled in by promapi.Expand.
func (m pinnedMetric) expr() string {
	expr := m.Name + m.Selector
	if m.Rate {
		return "rate(" + expr + "[$win])"
	}
	return expr
}

// promEndpoint is a resolved Prometheus URL and where it came from, so the
// Metrics suite can say "settings" or "detected: prometheus.service".
type promEndpoint struct {
	URL    string
	Source string
}

// prometheusEndpoint runs on the UI loop. An "Open in Metrics" choice wins,
// then the flag or environment, then settings, then the inventory, and
// finally the conventional local port, which the probe will report as
// unreachable when nothing listens there.
func (w *workspace) prometheusEndpoint() promEndpoint {
	if w.promSelected.URL != "" {
		return w.promSelected
	}
	if url := strings.TrimSpace(os.Getenv("SYSTEMDOC_PROMETHEUS")); url != "" {
		return promEndpoint{URL: url, Source: "flag or environment"}
	}
	if w.settings.PrometheusURL != "" {
		return promEndpoint{URL: w.settings.PrometheusURL, Source: "settings"}
	}
	if endpoint, ok := discoverPrometheus(w.items[0], w.items[1]); ok {
		return endpoint
	}
	return promEndpoint{URL: "http://localhost:9090", Source: "default"}
}

// discoverPrometheus looks for a running Prometheus in the inventory the
// suites already collected; it never runs a command.
func discoverPrometheus(services, containers []workload) (promEndpoint, bool) {
	for _, item := range services {
		if endpoint, ok := prometheusWorkload(0, item); ok {
			return endpoint, true
		}
	}
	for _, item := range containers {
		if endpoint, ok := prometheusWorkload(1, item); ok {
			return endpoint, true
		}
	}
	return promEndpoint{}, false
}

// prometheusWorkload reports whether one row is a running Prometheus server
// and the URL it answers on.
func prometheusWorkload(mode int, item workload) (promEndpoint, bool) {
	if !isActive(item) {
		return promEndpoint{}, false
	}
	detected := promEndpoint{URL: "http://localhost:9090", Source: "detected: " + item.Name}
	if mode == 0 {
		switch {
		case item.ID == "prometheus.service", strings.HasSuffix(item.ID, ".prometheus.service"):
			return detected, true
		case usesLaunchd() && (item.ID == "sh.brew.prometheus" || item.ID == "homebrew.mxcl.prometheus") && item.PID > 0:
			return detected, true
		}
		return promEndpoint{}, false
	}
	if isPod(item) || imageBase(item.Description) != "prometheus" {
		return promEndpoint{}, false
	}
	if item.Ports == "" {
		return detected, true // host networking
	}
	host, port, ok := publishedPort(item.Ports, "9090/tcp")
	if !ok {
		return promEndpoint{}, false
	}
	detected.URL = "http://" + host + ":" + port
	return detected, true
}

// metricsFamilyFor names the exporter family a workload belongs to, so the
// Actions menu can jump to its panels.
func metricsFamilyFor(item workload) (string, bool) {
	text := strings.ToLower(item.Name + " " + item.ID + " " + item.Description)
	switch {
	case strings.Contains(text, "node-exporter"), strings.Contains(text, "node_exporter"):
		return "node", true
	case strings.Contains(text, "cadvisor"):
		return "cadvisor", true
	case strings.Contains(text, "pve-exporter"), strings.Contains(text, "pve_exporter"):
		return "pve", true
	case strings.Contains(text, "prometheus"):
		return "targets", true
	}
	return "", false
}

// imageBase returns the last path element of an image reference without its
// tag or digest: quay.io/prometheus/prometheus:v2.51 → prometheus.
func imageBase(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	if slash := strings.LastIndex(image, "/"); slash >= 0 {
		image = image[slash+1:]
	}
	if colon := strings.Index(image, ":"); colon >= 0 {
		image = image[:colon]
	}
	return image
}

// publishedPort finds the host side of a docker ps publish list for one
// container port. Wildcard binds map to localhost.
func publishedPort(ports, containerPort string) (host, port string, ok bool) {
	for _, mapping := range strings.Split(ports, ",") {
		mapping = strings.TrimSpace(mapping)
		hostSide, target, found := strings.Cut(mapping, "->")
		if !found || target != containerPort {
			continue
		}
		colon := strings.LastIndex(hostSide, ":")
		if colon < 0 {
			continue
		}
		host, port = strings.Trim(hostSide[:colon], "[]"), hostSide[colon+1:]
		if host == "0.0.0.0" || host == "::" || host == "" {
			host = "localhost"
		}
		return host, port, true
	}
	return "", "", false
}

// Probe state is shared by page goroutines and the UI thread, like kube.
var prom struct {
	mu       sync.Mutex
	client   *promapi.Client
	url      string
	build    promapi.BuildInfo
	err      string
	probed   time.Time
	failures int
}

// currentPrometheus returns a client for endpoint, probing the server's build
// info when nothing is cached. Failed probes back off like the kubectl probe
// (30 s doubling to 10 min); a changed URL is probed at once.
func currentPrometheus(ctx context.Context, endpoint promEndpoint) (*promapi.Client, promapi.BuildInfo, string) {
	prom.mu.Lock()
	if prom.url != endpoint.URL {
		prom.client, prom.url, prom.build, prom.err, prom.probed, prom.failures = nil, endpoint.URL, promapi.BuildInfo{}, "", time.Time{}, 0
	}
	if prom.client != nil {
		client, build := prom.client, prom.build
		prom.mu.Unlock()
		return client, build, ""
	}
	wait := min(time.Duration(30<<min(prom.failures, 5))*time.Second, 10*time.Minute)
	if !prom.probed.IsZero() && time.Since(prom.probed) < wait {
		err := prom.err
		prom.mu.Unlock()
		return nil, promapi.BuildInfo{}, err
	}
	prom.mu.Unlock()

	token := os.Getenv("SYSTEMDOC_PROMETHEUS_TOKEN")
	client, err := promapi.New(endpoint.URL, token)
	var build promapi.BuildInfo
	if err == nil {
		build, err = client.BuildInfo(ctx)
	}
	if ctx.Err() != nil {
		return nil, promapi.BuildInfo{}, ""
	}

	prom.mu.Lock()
	defer prom.mu.Unlock()
	prom.probed = time.Now()
	if err != nil {
		prom.failures++
		prom.err = firstLine(err.Error())
		return nil, promapi.BuildInfo{}, prom.err
	}
	prom.client, prom.build, prom.err, prom.failures = client, build, "", 0
	return client, build, ""
}

// retryPrometheusProbe lets an explicit refresh probe again immediately.
func retryPrometheusProbe() {
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if prom.client == nil {
		prom.probed = time.Time{}
	}
}

// dropPrometheus forgets a server that stopped answering so it is re-probed
// with back-off. Query errors from a bad expression must not call this.
func dropPrometheus(err error) {
	prom.mu.Lock()
	defer prom.mu.Unlock()
	prom.client = nil
	prom.probed = time.Now()
	prom.err = firstLine(err.Error())
}

// resetPrometheus forgets everything, for a changed URL in Preferences.
func resetPrometheus() {
	prom.mu.Lock()
	defer prom.mu.Unlock()
	prom.client, prom.url, prom.build, prom.err, prom.probed, prom.failures = nil, "", promapi.BuildInfo{}, "", time.Time{}, 0
}

// prometheusStatus is the cached probe outcome for status lines.
func prometheusStatus() (build promapi.BuildInfo, err string, ok bool) {
	prom.mu.Lock()
	defer prom.mu.Unlock()
	return prom.build, prom.err, prom.client != nil
}

// openMetrics jumps to the Metrics suite for a workload's Prometheus server
// or exporter family.
func (w *workspace) openMetrics(endpoint promEndpoint, family string) {
	if endpoint.URL != "" {
		w.promSelected = endpoint
		retryPrometheusProbe()
	}
	if page := w.metricsPage(); family != "" {
		page.focusFamily(family)
	}
}
