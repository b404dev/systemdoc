package promapi

import (
	"strconv"
	"strings"
	"time"
)

// PanelKind selects how the dashboard renders a panel.
type PanelKind int

// Panel kinds.
const (
	// KindArea is a filled single-series chart over a range query.
	KindArea PanelKind = iota
	// KindSpark is a multi-series line chart over a range query.
	KindSpark
	// KindBars is a horizontal bar list from an instant query.
	KindBars
	// KindStat is a single value or short value list from an instant query.
	KindStat
)

// String returns the kind name used in logs and tests.
func (k PanelKind) String() string {
	switch k {
	case KindArea:
		return "area"
	case KindSpark:
		return "spark"
	case KindBars:
		return "bars"
	case KindStat:
		return "stat"
	}
	return "unknown"
}

// Scope describes which selector a family needs before its panels make sense.
type Scope int

// Family scopes.
const (
	// ScopeNone means the family is server-wide and ignores selectors.
	ScopeNone Scope = iota
	// ScopeInstance means panels are filtered by the instance label.
	ScopeInstance
	// ScopePVENode means panels are filtered by instance and a PVE node name.
	ScopePVENode
)

// Units attached to panels. The dashboard formats values by unit.
const (
	UnitPercent        = "%"
	UnitBytes          = "B"
	UnitBytesPerSecond = "B/s"
	UnitSeconds        = "s"
	UnitLoad           = "load"
	UnitCelsius        = "°C"
	UnitCount          = "count"
	UnitPerSecond      = "/s"
	UnitNone           = ""
)

// Family groups panels that depend on the same exporter.
type Family struct {
	// ID is a short stable identifier such as "node".
	ID string
	// Title is shown as the section heading.
	Title string
	// Detect lists metric names; the family is offered when any is present.
	Detect []string
	// Scope says which selector fields the panels use.
	Scope Scope
	// Panels in display order.
	Panels []Panel
}

// Panel is one chart or statistic backed by a PromQL template.
type Panel struct {
	// ID is unique across the catalog, e.g. "node.cpu".
	ID string
	// Title is the panel heading.
	Title string
	// Unit is one of the Unit constants.
	Unit string
	// Query is the PromQL template; see Expand for placeholders.
	Query string
	// Fallback is an alternate template using older metric names. It is
	// used when Detect names a metric the server does not expose.
	Fallback string
	// Detect optionally names the metric Query depends on. When the server
	// lacks it the dashboard uses Fallback if set, or hides the panel.
	Detect string
	// Ceiling fixes the axis maximum; 0 means autoscale.
	Ceiling float64
	// CeilingQ is an instant query whose scalar result replaces Ceiling.
	CeilingQ string
	// Kind selects the renderer.
	Kind PanelKind
	// Legend names the label(s) identifying each series, joined with "/".
	// Empty means a single unnamed series.
	Legend string
	// TopN is the default $top value; 0 when the template has no $top.
	TopN int
	// Range is true when the panel uses query_range rather than query.
	Range bool
	// Optional panels are hidden silently when they return no data.
	Optional bool
	// Note is a short hint shown next to the title.
	Note string
}

// Selector carries the values substituted into panel templates.
type Selector struct {
	// Instance is the Prometheus instance label value, or empty for all.
	Instance string
	// Node is the Proxmox node name for ScopePVENode families.
	Node string
	// Window is the rate window such as "2m"; empty uses the default.
	Window string
	// Top is the topk limit; 0 uses the panel default.
	Top int
}

// DefaultRateWindow is used when a Selector carries no window.
const DefaultRateWindow = "2m"

// DefaultTop is used when neither the selector nor the panel sets a limit.
const DefaultTop = 8

var labelValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// EscapeLabelValue quotes a value for use inside a PromQL label matcher.
func EscapeLabelValue(v string) string {
	return labelValueEscaper.Replace(v)
}

// Expand substitutes selector placeholders into a PromQL template.
//
//	$sel  -> instance="<value>", (with trailing comma) or nothing
//	$win  -> rate window, default "2m"
//	$node -> escaped PVE node name
//	$top  -> topk limit
//
// Templates place $sel first inside braces. Empty matcher sets left behind
// by an empty selector are removed so the result stays valid PromQL.
func Expand(query string, sel Selector) string {
	selText := ""
	if sel.Instance != "" {
		selText = `instance="` + EscapeLabelValue(sel.Instance) + `",`
	}
	win := sel.Window
	if win == "" {
		win = DefaultRateWindow
	}
	top := sel.Top
	if top <= 0 {
		top = DefaultTop
	}
	r := strings.NewReplacer(
		"$sel", selText,
		"$win", win,
		"$node", EscapeLabelValue(sel.Node),
		"$top", strconv.Itoa(top),
	)
	out := r.Replace(query)
	out = strings.ReplaceAll(out, "{ ", "{")
	out = strings.ReplaceAll(out, ",}", "}")
	out = strings.ReplaceAll(out, "{}", "")
	return out
}

// Expr expands the panel query, using the panel's TopN when sel.Top is 0.
func (p Panel) Expr(sel Selector) string {
	return Expand(p.Query, p.withTop(sel))
}

// FallbackExpr expands the panel's Fallback template the same way as Expr.
// It returns "" when the panel has no fallback.
func (p Panel) FallbackExpr(sel Selector) string {
	if p.Fallback == "" {
		return ""
	}
	return Expand(p.Fallback, p.withTop(sel))
}

// CeilingExpr expands CeilingQ, or returns "" when the panel has none.
func (p Panel) CeilingExpr(sel Selector) string {
	if p.CeilingQ == "" {
		return ""
	}
	return Expand(p.CeilingQ, p.withTop(sel))
}

func (p Panel) withTop(sel Selector) Selector {
	if sel.Top == 0 && p.TopN > 0 {
		sel.Top = p.TopN
	}
	return sel
}

// Label matcher fragments shared by several templates.
const (
	guestSel     = `id=~"(qemu|lxc)/.+"`
	guestJoin    = ` * on (id) group_left (name, type) pve_guest_info{$sel node="$node"}`
	diskDevices  = `device!~"loop.*|ram.*|zram.*|sr.*|fd.*"`
	netDevices   = `device!~"lo|veth.*|docker.*|br-.*|virbr.*|cali.*|flannel.*|cni.*|tap.*|fwbr.*|fwln.*|fwpr.*|vmbr.*"`
	fsTypes      = `fstype!~"tmpfs|overlay|squashfs|ramfs|devtmpfs|nsfs|autofs|fuse.*"`
	containerNet = `interface!~"lo|veth.*"`
)

// Catalog lists every family in display order.
var Catalog = []Family{
	{
		ID:     "node",
		Title:  "Host (node_exporter)",
		Detect: []string{"node_cpu_seconds_total", "node_memory_MemTotal_bytes"},
		Scope:  ScopeInstance,
		Panels: []Panel{
			{ID: "node.cpu", Title: "CPU busy", Unit: UnitPercent, Ceiling: 100, Kind: KindArea, Range: true,
				Query: `100 * (1 - avg(rate(node_cpu_seconds_total{$sel mode="idle"}[$win])))`},
			{ID: "node.cpu.percpu", Title: "Per-CPU busy", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "cpu",
				Query: `100 * (1 - rate(node_cpu_seconds_total{$sel mode="idle"}[$win]))`},
			{ID: "node.cpu.iowait", Title: "I/O wait + steal", Unit: UnitPercent, Ceiling: 100, Kind: KindSpark, Legend: "mode", Range: true, Optional: true,
				Query: `100 * sum by (mode) (rate(node_cpu_seconds_total{$sel mode=~"iowait|steal"}[$win])) / scalar(count(node_cpu_seconds_total{$sel mode="idle"}) > 0)`,
				Note:  "share of all CPUs"},
			{ID: "node.mem.pct", Title: "Memory used", Unit: UnitPercent, Ceiling: 100, Kind: KindArea, Range: true,
				Detect:   "node_memory_MemAvailable_bytes",
				Query:    `100 * (1 - node_memory_MemAvailable_bytes{$sel} / (node_memory_MemTotal_bytes{$sel} > 0))`,
				Fallback: `100 * (node_memory_active_bytes{$sel} + node_memory_wired_bytes{$sel} + node_memory_compressed_bytes{$sel}) / (node_memory_total_bytes{$sel} > 0)`,
				Note:     "macOS: active + wired + compressed"},
			{ID: "node.swap", Title: "Swap used", Unit: UnitPercent, Ceiling: 100, Kind: KindArea, Range: true, Optional: true,
				Detect:   "node_memory_SwapTotal_bytes",
				Query:    `100 * (1 - node_memory_SwapFree_bytes{$sel} / (node_memory_SwapTotal_bytes{$sel} > 0))`,
				Fallback: `100 * node_memory_swap_used_bytes{$sel} / (node_memory_swap_total_bytes{$sel} > 0)`,
				Note:     "hidden without swap"},
			{ID: "node.load", Title: "Load 1/5/15", Unit: UnitLoad, Kind: KindSpark, Legend: "__name__", Range: true,
				Query:    `{$sel __name__=~"node_load(1|5|15)"}`,
				CeilingQ: `count(node_cpu_seconds_total{$sel mode="idle"})`,
				Note:     "ceiling = CPU count"},
			{ID: "node.fs", Title: "Filesystem used", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "mountpoint", TopN: 12,
				Query: `topk($top, 100 * (1 - node_filesystem_avail_bytes{$sel ` + fsTypes + `} / (node_filesystem_size_bytes{$sel ` + fsTypes + `} > 0)))`,
				Note:  "real filesystems only"},
			{ID: "node.disk.read", Title: "Disk read", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "device", TopN: 4, Range: true,
				Query: `topk($top, sum by (device) (rate(node_disk_read_bytes_total{$sel ` + diskDevices + `}[$win])))`},
			{ID: "node.disk.write", Title: "Disk write", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "device", TopN: 4, Range: true,
				Query: `topk($top, sum by (device) (rate(node_disk_written_bytes_total{$sel ` + diskDevices + `}[$win])))`},
			{ID: "node.disk.busy", Title: "Disk busy", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "device", TopN: 6,
				Query: `topk($top, 100 * rate(node_disk_io_time_seconds_total{$sel ` + diskDevices + `}[$win]))`},
			{ID: "node.net.rx", Title: "Network receive", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "device", TopN: 4, Range: true,
				Query: `topk($top, rate(node_network_receive_bytes_total{$sel ` + netDevices + `}[$win]))`,
				Note:  "physical interfaces; bridges and veths excluded"},
			{ID: "node.net.tx", Title: "Network transmit", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "device", TopN: 4, Range: true,
				Query: `topk($top, rate(node_network_transmit_bytes_total{$sel ` + netDevices + `}[$win]))`,
				Note:  "physical interfaces; bridges and veths excluded"},
			{ID: "node.uptime", Title: "Up since", Unit: UnitSeconds, Kind: KindStat,
				Query: `node_time_seconds{$sel} - node_boot_time_seconds{$sel}`},
			{ID: "node.psi", Title: "Pressure stall", Unit: UnitPercent, Ceiling: 100, Kind: KindSpark, Legend: "__name__", Range: true, Optional: true,
				Query: `100 * rate({$sel __name__=~"node_pressure_(cpu|memory|io)_waiting_seconds_total"}[$win])`,
				Note:  "needs kernel PSI"},
			{ID: "node.temp", Title: "Temperatures", Unit: UnitCelsius, Kind: KindBars, Legend: "label", TopN: 8, Optional: true,
				Query:    `topk($top, (node_hwmon_temp_celsius{$sel} * on (chip, sensor) group_left (label) node_hwmon_sensor_label{$sel}) or node_hwmon_temp_celsius{$sel})`,
				CeilingQ: `max(node_hwmon_temp_crit_celsius{$sel})`,
				Note:     "legend falls back to chip/sensor; ceiling = critical"},
		},
	},
	{
		ID:     "cadvisor",
		Title:  "Containers (cAdvisor)",
		Detect: []string{"container_cpu_usage_seconds_total"},
		Scope:  ScopeInstance,
		Panels: cadvisorPanels("cadvisor", `name!=""`, "name"),
	},
	{
		ID:     "kubelet",
		Title:  "Pods (kubelet cAdvisor)",
		Detect: []string{"container_cpu_usage_seconds_total"},
		Scope:  ScopeInstance,
		Panels: append(cadvisorPanels("kubelet", `container!="", container!="POD"`, "namespace/pod"),
			Panel{ID: "kubelet.restarts", Title: "Container restarts", Unit: UnitCount, Kind: KindBars, Legend: "namespace/pod", TopN: 8, Optional: true,
				Detect: "kube_pod_container_status_restarts_total",
				Query:  `topk($top, sum by (namespace, pod) (kube_pod_container_status_restarts_total))`,
				Note:   "needs kube-state-metrics"},
		),
	},
	{
		ID:     "pve",
		Title:  "Proxmox VE (pve-exporter)",
		Detect: []string{"pve_up"},
		Scope:  ScopePVENode,
		Panels: []Panel{
			{ID: "pve.node.cpu", Title: "Node CPU", Unit: UnitPercent, Ceiling: 100, Kind: KindArea, Range: true,
				Query: `100 * pve_cpu_usage_ratio{$sel id="node/$node"}`},
			{ID: "pve.node.mem", Title: "Node memory", Unit: UnitPercent, Ceiling: 100, Kind: KindArea, Range: true,
				Query: `100 * pve_memory_usage_bytes{$sel id="node/$node"} / (pve_memory_size_bytes{$sel id="node/$node"} > 0)`},
			{ID: "pve.node.up", Title: "Node status", Unit: UnitNone, Kind: KindStat,
				Query: `pve_up{$sel id="node/$node"}`, Note: "1 = online"},
			{ID: "pve.node.uptime", Title: "Node uptime", Unit: UnitSeconds, Kind: KindStat,
				Query: `pve_uptime_seconds{$sel id="node/$node"}`},
			{ID: "pve.node.version", Title: "PVE version", Unit: UnitNone, Kind: KindStat, Legend: "version", Optional: true,
				Query: `pve_version_info{$sel}`},
			{ID: "pve.guest.cpu", Title: "VM & CT CPU", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "name", TopN: 12,
				Query: `topk($top, 100 * pve_cpu_usage_ratio{$sel ` + guestSel + `}` + guestJoin + `)`},
			{ID: "pve.guest.mem", Title: "VM & CT memory", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "name", TopN: 12,
				Query: `topk($top, 100 * pve_memory_usage_bytes{$sel ` + guestSel + `} / (pve_memory_size_bytes{$sel ` + guestSel + `} > 0)` + guestJoin + `)`},
			{ID: "pve.guest.net.rx", Title: "Guest receive", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "name", TopN: 6, Range: true,
				Detect:   "pve_network_receive_bytes_total",
				Query:    `topk($top, rate(pve_network_receive_bytes_total{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Fallback: `topk($top, rate(pve_network_receive_bytes{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Note:     "older exporters use pve_network_receive_bytes"},
			{ID: "pve.guest.net.tx", Title: "Guest transmit", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "name", TopN: 6, Range: true,
				Detect:   "pve_network_transmit_bytes_total",
				Query:    `topk($top, rate(pve_network_transmit_bytes_total{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Fallback: `topk($top, rate(pve_network_transmit_bytes{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Note:     "older exporters use pve_network_transmit_bytes"},
			{ID: "pve.guest.disk.read", Title: "Guest disk read", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "name", TopN: 6, Range: true, Optional: true,
				Detect:   "pve_disk_read_bytes_total",
				Query:    `topk($top, rate(pve_disk_read_bytes_total{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Fallback: `topk($top, rate(pve_disk_read_bytes{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Note:     "older exporters use pve_disk_read_bytes"},
			{ID: "pve.guest.disk.write", Title: "Guest disk write", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: "name", TopN: 6, Range: true, Optional: true,
				Detect:   "pve_disk_written_bytes_total",
				Query:    `topk($top, rate(pve_disk_written_bytes_total{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Fallback: `topk($top, rate(pve_disk_write_bytes{$sel ` + guestSel + `}[$win])` + guestJoin + `)`,
				Note:     "older exporters use pve_disk_write_bytes"},
			{ID: "pve.guests.running", Title: "Guests running", Unit: UnitCount, Kind: KindStat,
				Query: `count(pve_up{$sel ` + guestSel + `} * on (id) group_left pve_guest_info{$sel node="$node"} == 1) or vector(0)`},
			{ID: "pve.guests.stopped", Title: "Guests stopped", Unit: UnitCount, Kind: KindStat,
				Query: `count(pve_up{$sel ` + guestSel + `} * on (id) group_left pve_guest_info{$sel node="$node"} == 0) or vector(0)`},
			{ID: "pve.storage", Title: "Storage used", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: "storage", TopN: 12,
				Query: `topk($top, 100 * pve_disk_usage_bytes{$sel id=~"storage/.+"} / (pve_disk_size_bytes{$sel id=~"storage/.+"} > 0) * on (id) group_left (storage, plugintype) pve_storage_info{$sel node="$node"})`},
			{ID: "pve.ha", Title: "HA state", Unit: UnitNone, Kind: KindStat, Legend: "id/state", Optional: true,
				Detect: "pve_ha_state",
				Query:  `pve_ha_state{$sel ` + guestSel + `, state!~"started|stopped"} == 1`,
				Note:   "guests in transitional HA states"},
		},
	},
	{
		ID:     "prometheus",
		Title:  "Prometheus server",
		Detect: []string{"prometheus_tsdb_head_series"},
		Scope:  ScopeNone,
		Panels: []Panel{
			{ID: "prom.series", Title: "Head series", Unit: UnitCount, Kind: KindArea, Range: true,
				Query: `max(prometheus_tsdb_head_series)`},
			{ID: "prom.samples", Title: "Samples appended", Unit: UnitPerSecond, Kind: KindArea, Range: true,
				Query: `sum(rate(prometheus_tsdb_head_samples_appended_total[$win]))`},
			{ID: "prom.targets", Title: "Targets up", Unit: UnitCount, Kind: KindStat,
				Query: `sum(up)`, Note: "of count(up)"},
			{ID: "prom.targets.total", Title: "Targets total", Unit: UnitCount, Kind: KindStat,
				Query: `count(up)`},
			{ID: "prom.scrape", Title: "Scrape duration", Unit: UnitSeconds, Kind: KindBars, Legend: "job/instance", TopN: 8,
				Query: `topk($top, scrape_duration_seconds)`},
		},
	},
}

// cadvisorPanels builds the shared container panels for the cadvisor and
// kubelet families. prefix is the panel ID prefix, guard the label matcher
// that excludes pause and cgroup-only series, and legend the labels that
// name a series ("name" or "namespace/pod"). Network metrics on kubelet are
// pod-level, so they use a pod guard instead.
func cadvisorPanels(prefix, guard, legend string) []Panel {
	by := strings.ReplaceAll(legend, "/", ", ")
	netGuard := guard
	if prefix == "kubelet" {
		netGuard = `pod!=""`
	}
	sel := `{$sel ` + guard + `}`
	netSel := func(metric string) string {
		return metric + `{$sel ` + netGuard + `, ` + containerNet + `}`
	}
	return []Panel{
		{ID: prefix + ".cpu", Title: "Container CPU", Unit: UnitPercent, Kind: KindSpark, Legend: legend, TopN: 8, Range: true,
			Query: `topk($top, 100 * sum by (` + by + `) (rate(container_cpu_usage_seconds_total` + sel + `[$win])))`,
			Note:  "percent of one CPU"},
		{ID: prefix + ".mem", Title: "Working set", Unit: UnitBytes, Kind: KindBars, Legend: legend, TopN: 8,
			Query: `topk($top, sum by (` + by + `) (container_memory_working_set_bytes` + sel + `))`},
		{ID: prefix + ".mem.pct", Title: "Memory vs limit", Unit: UnitPercent, Ceiling: 100, Kind: KindBars, Legend: legend, TopN: 8, Optional: true,
			Query: `topk($top, 100 * sum by (` + by + `) (container_memory_working_set_bytes` + sel + `) / (sum by (` + by + `) (container_spec_memory_limit_bytes` + sel + ` > 0 < 1e18) > 0))`,
			Note:  "only containers with a memory limit"},
		{ID: prefix + ".net.rx", Title: "Container receive", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: legend, TopN: 6, Range: true,
			Query: `topk($top, sum by (` + by + `) (rate(` + netSel("container_network_receive_bytes_total") + `[$win])))`},
		{ID: prefix + ".net.tx", Title: "Container transmit", Unit: UnitBytesPerSecond, Kind: KindSpark, Legend: legend, TopN: 6, Range: true,
			Query: `topk($top, sum by (` + by + `) (rate(` + netSel("container_network_transmit_bytes_total") + `[$win])))`},
		{ID: prefix + ".age", Title: "Running since", Unit: UnitSeconds, Kind: KindStat, Legend: legend,
			Query: `time() - max by (` + by + `) (container_start_time_seconds` + sel + `)`,
			Note:  "list"},
		{ID: prefix + ".count", Title: "Containers seen", Unit: UnitCount, Kind: KindStat,
			Query: `count(container_last_seen` + sel + ` > time() - 60)`,
			Note:  "seen in the last minute"},
	}
}

// FamilyByID returns the catalog family with the given ID.
func FamilyByID(id string) (Family, bool) {
	for _, f := range Catalog {
		if f.ID == id {
			return f, true
		}
	}
	return Family{}, false
}

// DetectFamilies returns the catalog families whose Detect metrics appear in
// names, in catalog order. container_cpu_usage_seconds_total is attributed to
// the kubelet family when kubeletHint is true and to cadvisor otherwise.
func DetectFamilies(names []string, kubeletHint bool) []Family {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	var out []Family
	for _, f := range Catalog {
		if f.ID == "cadvisor" && kubeletHint || f.ID == "kubelet" && !kubeletHint {
			continue
		}
		for _, d := range f.Detect {
			if present[d] {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// DetectNames returns every metric name the catalog probes for, without
// duplicates and in catalog order. It feeds the match[] parameter of the
// metric-name lookup.
func DetectNames() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, f := range Catalog {
		for _, d := range f.Detect {
			add(d)
		}
		for _, p := range f.Panels {
			add(p.Detect)
		}
	}
	return out
}

// RateWindow picks the rate() window for a scrape interval: four scrapes,
// clamped to 1m..5m and rounded up to "1m", "2m" or "5m". Unknown or
// non-positive intervals yield the default "2m".
func RateWindow(scrapeInterval time.Duration) string {
	if scrapeInterval <= 0 {
		return DefaultRateWindow
	}
	w := 4 * scrapeInterval
	switch {
	case w <= time.Minute:
		return "1m"
	case w <= 2*time.Minute:
		return "2m"
	default:
		return "5m"
	}
}

// StepFor returns the query_range step for drawing window across columns:
// at least one scrape interval, at least window/columns, and rounded up to
// a whole number of scrape intervals. A non-positive scrape interval is
// treated as 15s and columns below 1 as 1.
func StepFor(window time.Duration, columns int, scrapeInterval time.Duration) time.Duration {
	if scrapeInterval <= 0 {
		scrapeInterval = 15 * time.Second
	}
	if columns < 1 {
		columns = 1
	}
	if window < 0 {
		window = 0
	}
	raw := window / time.Duration(columns)
	if window%time.Duration(columns) != 0 {
		raw++
	}
	if raw < scrapeInterval {
		return scrapeInterval
	}
	if rem := raw % scrapeInterval; rem != 0 {
		raw += scrapeInterval - rem
	}
	return raw
}
