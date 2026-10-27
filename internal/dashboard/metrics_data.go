package dashboard

import (
	"context"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"systemdoc/internal/promapi"
)

const (
	metricsPollFloor   = 10 // seconds; every poll re-reads a window per panel
	metricsWindow      = 30 * time.Minute
	metricsDetectEvery = 5 * time.Minute
	metricsWorkers     = 4
	metricsQueryLimit  = 5 * time.Second
)

type metricsState int

const (
	metricsUnconfigured metricsState = iota
	metricsProbing
	metricsUnreachable
	metricsNoFamilies
	metricsReady
)

// metricsPanel is one chart on the page: a catalog panel or a pinned metric
// plus whatever the last poll returned for it.
type metricsPanel struct {
	spec    promapi.Panel
	family  string
	pinned  bool
	pin     pinnedMetric
	ceiling float64
	series  []metricsSeries
	step    time.Duration
	newest  time.Time
	failure string
	empty   bool // last poll returned no series
}

// metricsSeries is one line of a panel. values holds one column per step
// with -1 for a missing sample; latest is -1 when unknown.
type metricsSeries struct {
	label  string
	values []float64
	latest float64
}

// metricsDetection is what one detection pass learned about the server.
type metricsDetection struct {
	families  []promapi.Family
	names     map[string]bool
	instances map[string][]string // family ID → instance label values
	nodes     []string            // Proxmox node names
	targets   []promapi.Target
	interval  time.Duration
	runtime   promapi.RuntimeInfo
}

// metricsResult is what one poll produced for one panel.
type metricsResult struct {
	id      string
	series  []metricsSeries
	ceiling float64
	step    time.Duration
	newest  time.Time
	err     error
}

// detectMetrics asks the server which exporter families it has series for.
// Every call is bounded to the names the catalog cares about.
func detectMetrics(ctx context.Context, client *promapi.Client) (metricsDetection, error) {
	since := time.Now().Add(-15 * time.Minute)
	d := metricsDetection{names: map[string]bool{}, instances: map[string][]string{}}
	names, err := client.MetricNames(ctx, promapi.DetectNames(), since)
	if err != nil {
		return d, err
	}
	for _, name := range names {
		d.names[name] = true
	}
	kubelet := false
	if d.names["container_cpu_usage_seconds_total"] {
		samples, err := client.Query(ctx, `count(container_cpu_usage_seconds_total{pod!=""})`, time.Time{})
		kubelet = err == nil && len(samples) > 0 && samples[0].Value > 0
	}
	d.families = promapi.DetectFamilies(names, kubelet)
	for _, family := range d.families {
		if family.Scope == promapi.ScopeInstance && len(family.Detect) > 0 {
			d.instances[family.ID], _ = client.LabelValues(ctx, "instance", family.Detect[:1], since)
		}
		if family.Scope == promapi.ScopePVENode {
			samples, _ := client.Query(ctx, "pve_node_info", time.Time{})
			for _, sample := range samples {
				d.nodes = append(d.nodes, sample.Labels["name"])
			}
			sort.Strings(d.nodes)
		}
	}
	d.targets, _ = client.Targets(ctx)
	for _, target := range d.targets {
		if target.ScrapeInterval > 0 && (d.interval == 0 || target.ScrapeInterval < d.interval) {
			d.interval = target.ScrapeInterval
		}
	}
	d.runtime, _ = client.RuntimeInfo(ctx)
	return d, nil
}

// chooseInstance picks which scraped host to show when a Prometheus knows
// several: a saved choice, then this machine, then the only one, then the first.
func chooseInstance(candidates []string, saved, hostname string, localAddrs []string) string {
	if len(candidates) == 0 {
		return ""
	}
	for _, candidate := range candidates {
		if candidate == saved {
			return saved
		}
	}
	short, _, _ := strings.Cut(hostname, ".")
	for _, candidate := range candidates {
		host := candidate
		if h, _, err := net.SplitHostPort(candidate); err == nil {
			host = h
		}
		switch {
		case host == "localhost", host == "127.0.0.1", host == "::1":
			return candidate
		case strings.EqualFold(host, hostname), short != "" && strings.EqualFold(host, short):
			return candidate
		}
		for _, addr := range localAddrs {
			if host == addr {
				return candidate
			}
		}
	}
	return candidates[0]
}

func localAddresses() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ip, ok := addr.(*net.IPNet); ok {
			out = append(out, ip.IP.String())
		}
	}
	return out
}

// panelsFor builds the page's panel list from detected families and pins.
// A panel whose metric is missing uses its fallback query or is left out.
func panelsFor(families []promapi.Family, names map[string]bool, pins []pinnedMetric) []*metricsPanel {
	var panels []*metricsPanel
	for _, family := range families {
		for _, spec := range family.Panels {
			if spec.Detect != "" && !names[spec.Detect] {
				if spec.Fallback == "" {
					continue
				}
				spec.Query = spec.Fallback
			}
			panels = append(panels, &metricsPanel{spec: spec, family: family.ID, ceiling: spec.Ceiling})
		}
	}
	for _, pin := range pins {
		if pin.validate() != nil {
			continue
		}
		spec := promapi.Panel{ID: "pin." + pin.Name + pin.Selector, Title: pin.Title, Unit: pin.Unit, Query: pin.expr(), Ceiling: pin.Ceiling, Kind: promapi.KindArea, Range: true}
		panels = append(panels, &metricsPanel{spec: spec, family: "pinned", pinned: true, pin: pin, ceiling: pin.Ceiling})
	}
	return panels
}

// queryPanels runs every panel's query with a small worker pool. columns is
// how many samples a chart can show; the step follows from it.
func queryPanels(ctx context.Context, client *promapi.Client, panels []*metricsPanel, selectors map[string]promapi.Selector, columns int, interval time.Duration) []metricsResult {
	results := make([]metricsResult, len(panels))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < metricsWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = queryPanel(ctx, client, panels[i], selectors[panels[i].family], columns, interval)
			}
		}()
	}
	for i := range panels {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func queryPanel(ctx context.Context, client *promapi.Client, panel *metricsPanel, sel promapi.Selector, columns int, interval time.Duration) metricsResult {
	ctx, cancel := context.WithTimeout(ctx, metricsQueryLimit)
	defer cancel()
	spec := panel.spec
	result := metricsResult{id: spec.ID, ceiling: spec.Ceiling, newest: time.Now().Truncate(time.Second)}
	expr := spec.Expr(sel)
	if spec.CeilingQ != "" {
		if samples, err := client.Query(ctx, spec.CeilingExpr(sel), time.Time{}); err == nil && len(samples) > 0 && samples[0].Known {
			result.ceiling = samples[0].Value
		}
	}
	if !spec.Range {
		samples, err := client.Query(ctx, expr, time.Time{})
		result.err = err
		for _, sample := range samples {
			latest := -1.0
			if sample.Known {
				latest = sample.Value
			}
			result.series = append(result.series, metricsSeries{label: seriesLabel(sample.Labels, spec.Legend), latest: latest})
		}
		result.series = sortSeries(result.series, spec.TopN)
		return result
	}
	result.step = promapi.StepFor(metricsWindow, columns, interval)
	start := result.newest.Add(-result.step * time.Duration(columns-1))
	series, err := client.QueryRange(ctx, expr, start, result.newest, result.step)
	result.err = err
	for _, s := range series {
		values := bucketPoints(s.Points, start, result.step, columns)
		result.series = append(result.series, metricsSeries{label: seriesLabel(s.Labels, spec.Legend), values: values, latest: lastKnown(values)})
	}
	result.series = sortSeries(result.series, spec.TopN)
	return result
}

// bucketPoints places samples on a fixed grid so every chart column is one
// step and a missing step stays a gap.
func bucketPoints(points []promapi.Point, start time.Time, step time.Duration, columns int) []float64 {
	values := make([]float64, columns)
	for i := range values {
		values[i] = -1
	}
	for _, point := range points {
		i := int(point.At.Sub(start) / step)
		if point.Known && i >= 0 && i < columns {
			values[i] = point.Value
		}
	}
	return values
}

func lastKnown(values []float64) float64 {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i] >= 0 {
			return values[i]
		}
	}
	return -1
}

// seriesLabel names a series by the legend labels, joined with "/".
func seriesLabel(labels promapi.Labels, legend string) string {
	if legend == "" {
		return ""
	}
	var parts []string
	for _, key := range strings.Split(legend, "/") {
		value := labels[key]
		if value == "" {
			value = "?"
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, "/")
}

// sortSeries puts the busiest series first and keeps at most top of them.
func sortSeries(series []metricsSeries, top int) []metricsSeries {
	sort.SliceStable(series, func(i, j int) bool { return series[i].latest > series[j].latest })
	if top <= 0 {
		top = promapi.DefaultTop
	}
	if len(series) > top {
		series = series[:top]
	}
	return series
}

// refresh polls the server off the UI thread and applies the outcome back on it.
func (m *metricsPage) refresh() {
	if m.busy || m.ctx.Err() != nil {
		return
	}
	m.lastPoll = time.Now()
	endpoint := m.w.prometheusEndpoint()
	m.endpoint = endpoint
	if endpoint.URL == "" {
		m.state = metricsUnconfigured
		m.render()
		return
	}
	if m.state == metricsUnconfigured {
		m.state = metricsProbing
	}
	m.busy = true
	m.updateStatus()
	generation := m.generation
	detect := time.Since(m.detectedAt) > metricsDetectEvery
	panels := m.visiblePanels()
	selectors := m.selectors()
	columns := m.chartWidth()
	pins := append([]pinnedMetric(nil), m.w.settings.PrometheusMetrics...)
	saved := m.w.settings.PrometheusInstances
	interval := m.interval
	count := m.columns * m.visibleRows
	ctx := m.ctx
	go func() {
		client, build, problem := currentPrometheus(ctx, endpoint)
		var detection *metricsDetection
		var results []metricsResult
		var detectErr error
		if client != nil && detect {
			d, err := detectMetrics(ctx, client)
			detection, detectErr = &d, err
			if err == nil {
				interval = d.interval
				selectors = selectorsFor(d.families, d.instances, d.nodes, saved, endpoint.URL, d.interval)
				panels = panelsFor(d.families, d.names, pins)
				if len(panels) > count {
					panels = panels[:count]
				}
			}
		}
		if client != nil && detectErr == nil {
			results = queryPanels(ctx, client, panels, selectors, columns, interval)
		}
		m.w.queue(func() {
			if ctx.Err() != nil || generation != m.generation {
				return
			}
			m.busy = false
			m.build = build
			m.apply(client != nil, problem, detection, detectErr, results)
		})
	}()
}

// apply folds a poll's outcome into the page state and redraws.
func (m *metricsPage) apply(reachable bool, problem string, d *metricsDetection, detectErr error, results []metricsResult) {
	switch {
	case !reachable:
		m.state = metricsUnreachable
		m.failure = problem
	case detectErr != nil:
		m.state = metricsUnreachable
		m.failure = detectErr.Error()
		dropPrometheus(detectErr)
	default:
		m.failure = ""
		if d != nil {
			m.families, m.names, m.instances, m.nodes = d.families, d.names, d.instances, d.nodes
			m.targets, m.interval, m.runtime, m.detectedAt = d.targets, d.interval, d.runtime, time.Now()
			m.panels = panelsFor(d.families, d.names, m.w.settings.PrometheusMetrics)
			m.selected = min(m.selected, max(0, len(m.panels)-1))
			m.hidden = 0
		}
		m.state = metricsReady
		if len(m.panels) == 0 {
			m.state = metricsNoFamilies
		}
		m.sampledAt = time.Now()
	}
	byID := map[string]*metricsPanel{}
	for _, panel := range m.panels {
		byID[panel.spec.ID] = panel
	}
	failed := 0
	for _, result := range results {
		panel := byID[result.id]
		if panel == nil {
			continue
		}
		if result.err != nil {
			failed++
			panel.failure = firstLine(result.err.Error())
			continue
		}
		panel.failure = ""
		panel.series, panel.ceiling, panel.step, panel.newest = result.series, result.ceiling, result.step, result.newest
		panel.empty = len(result.series) == 0
	}
	if len(results) > 0 && failed == len(results) {
		m.pollFailures++
		if m.pollFailures >= 6 {
			m.state = metricsUnreachable
			m.failure = "every panel query failed; see the panel notes"
			retryPrometheusProbe()
		}
	} else {
		m.pollFailures = 0
	}
	m.hideEmpty()
	if m.state == metricsReady && !m.autoRouted {
		m.autoRouted = true
		if m.view == metricsSetupView {
			m.view = metricsPanelsView
		}
	}
	if m.state != metricsReady && m.state != metricsNoFamilies && !m.autoRouted {
		m.view = metricsSetupView
	}
	m.render()
}

// hideEmpty drops panels the server answered with no series, so a host
// without a collector does not show empty cards. They return on the next
// detection pass if data appears.
func (m *metricsPage) hideEmpty() {
	kept := m.panels[:0]
	for _, panel := range m.panels {
		if panel.empty && panel.failure == "" {
			m.hidden++
			continue
		}
		kept = append(kept, panel)
	}
	m.panels = kept
	m.selected = min(m.selected, max(0, len(m.panels)-1))
}

// selectors returns the per-family selectors for the next poll.
func (m *metricsPage) selectors() map[string]promapi.Selector {
	return selectorsFor(m.families, m.instances, m.nodes, m.w.settings.PrometheusInstances, m.endpoint.URL, m.interval)
}

// selectorsFor decides, per family, which host and rate window the queries
// use. saved holds the user's host choices keyed by "url|family".
func selectorsFor(families []promapi.Family, instances map[string][]string, nodes []string, saved map[string]string, url string, interval time.Duration) map[string]promapi.Selector {
	hostname, _ := os.Hostname()
	local := localAddresses()
	window := promapi.RateWindow(interval)
	selectors := map[string]promapi.Selector{"pinned": {Window: window}}
	for _, family := range families {
		sel := promapi.Selector{Window: window}
		switch family.Scope {
		case promapi.ScopeInstance:
			sel.Instance = chooseInstance(instances[family.ID], saved[url+"|"+family.ID], hostname, local)
		case promapi.ScopePVENode:
			sel.Node = chooseInstance(nodes, saved[url+"|"+family.ID], hostname, local)
		}
		selectors[family.ID] = sel
	}
	return selectors
}
