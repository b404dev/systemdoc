package promapi

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var identRE = regexp.MustCompile(`[A-Za-z_:][A-Za-z0-9_:]*`)

// familyPrefixes lists metric prefixes (or exact names) that count as a
// family reference when a panel does not name a Detect metric directly.
var familyPrefixes = map[string][]string{
	"node":       {"node_"},
	"cadvisor":   {"container_"},
	"kubelet":    {"container_"},
	"pve":        {"pve_"},
	"prometheus": {"prometheus_", "scrape_", "up"},
}

func checkExpanded(t *testing.T, id, label, q string) {
	t.Helper()
	if strings.Count(q, "(") != strings.Count(q, ")") {
		t.Errorf("%s %s: unbalanced parentheses: %s", id, label, q)
	}
	if strings.Count(q, "{") != strings.Count(q, "}") {
		t.Errorf("%s %s: unbalanced braces: %s", id, label, q)
	}
	for _, bad := range []string{"{,", ",,", ",}", "{}", "$"} {
		if strings.Contains(q, bad) {
			t.Errorf("%s %s: contains %q: %s", id, label, bad, q)
		}
	}
}

func allTemplates(p Panel) map[string]string {
	out := map[string]string{"query": p.Query}
	if p.Fallback != "" {
		out["fallback"] = p.Fallback
	}
	if p.CeilingQ != "" {
		out["ceiling"] = p.CeilingQ
	}
	return out
}

func TestCatalogExpandsCleanly(t *testing.T) {
	selectors := map[string]Selector{
		"empty":    {},
		"instance": {Instance: "host:9100", Node: "pve1", Window: "5m", Top: 3},
		"quoted":   {Instance: `we"ird\host`, Node: `n"1`},
	}
	for _, f := range Catalog {
		for _, p := range f.Panels {
			if p.Query == "" {
				t.Errorf("%s: empty query", p.ID)
			}
			for label, tpl := range allTemplates(p) {
				for name, sel := range selectors {
					checkExpanded(t, p.ID, label+"/"+name, Expand(tpl, sel))
				}
			}
			if p.TopN > 0 != strings.Contains(p.Query, "$top") {
				t.Errorf("%s: TopN=%d but query $top presence is %v", p.ID, p.TopN, strings.Contains(p.Query, "$top"))
			}
			if p.Range != (p.Kind == KindArea || p.Kind == KindSpark) {
				t.Errorf("%s: Range=%v does not match kind %s", p.ID, p.Range, p.Kind)
			}
			if f.Scope == ScopePVENode && strings.Contains(p.Query, "$node") && !strings.Contains(p.Query, "$sel") {
				t.Errorf("%s: uses $node without $sel", p.ID)
			}
		}
	}
}

func TestExpandSelector(t *testing.T) {
	got := Expand(`node_memory_MemTotal_bytes{$sel}`, Selector{})
	if got != "node_memory_MemTotal_bytes" {
		t.Errorf("empty selector: got %q", got)
	}
	got = Expand(`node_memory_MemTotal_bytes{$sel}`, Selector{Instance: "a:9100"})
	if got != `node_memory_MemTotal_bytes{instance="a:9100"}` {
		t.Errorf("instance selector: got %q", got)
	}
	got = Expand(`rate(x{$sel mode="idle"}[$win])`, Selector{Instance: `q"\`})
	if got != `rate(x{instance="q\"\\", mode="idle"}[2m])` {
		t.Errorf("escaping: got %q", got)
	}
	got = Expand(`topk($top, y{$sel id="node/$node"})`, Selector{Node: "pve1", Top: 2, Window: "5m"})
	if got != `topk(2, y{id="node/pve1"})` {
		t.Errorf("node/top: got %q", got)
	}
	p := Panel{Query: `topk($top, z)`, TopN: 12}
	if got := p.Expr(Selector{}); got != "topk(12, z)" {
		t.Errorf("Expr default TopN: got %q", got)
	}
	if got := p.Expr(Selector{Top: 2}); got != "topk(2, z)" {
		t.Errorf("Expr explicit Top: got %q", got)
	}
	if got := Expand(`topk($top, z)`, Selector{}); got != "topk(8, z)" {
		t.Errorf("Expand default top: got %q", got)
	}
	if got := (Panel{}).FallbackExpr(Selector{}); got != "" {
		t.Errorf("FallbackExpr without fallback: got %q", got)
	}
}

func TestCatalogContainerGuards(t *testing.T) {
	for _, f := range Catalog {
		for _, p := range f.Panels {
			for label, tpl := range allTemplates(p) {
				container := false
				for _, ident := range identRE.FindAllString(tpl, -1) {
					if strings.HasPrefix(ident, "container_") {
						container = true
					}
				}
				if !container {
					continue
				}
				if !strings.Contains(tpl, `name!=""`) && !strings.Contains(tpl, `container!=""`) && !strings.Contains(tpl, `pod!=""`) {
					t.Errorf("%s %s: container_ query without a name/container/pod guard: %s", p.ID, label, tpl)
				}
			}
		}
	}
}

func TestCatalogPanelsReferenceFamily(t *testing.T) {
	for _, f := range Catalog {
		prefixes := familyPrefixes[f.ID]
		if prefixes == nil {
			t.Fatalf("family %s has no prefix entry in the test table", f.ID)
		}
		for _, p := range f.Panels {
			if p.Optional {
				continue
			}
			text := p.Query + " " + p.CeilingQ
			found := false
			for _, ident := range identRE.FindAllString(text, -1) {
				for _, d := range f.Detect {
					if ident == d {
						found = true
					}
				}
				for _, pre := range prefixes {
					if ident == pre || strings.HasPrefix(ident, pre) && strings.HasSuffix(pre, "_") {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("%s: references neither a Detect metric nor a %s metric", p.ID, f.ID)
			}
		}
	}
}

func TestCatalogIDsUnique(t *testing.T) {
	seen := map[string]string{}
	famSeen := map[string]bool{}
	for _, f := range Catalog {
		if famSeen[f.ID] {
			t.Errorf("duplicate family %s", f.ID)
		}
		famSeen[f.ID] = true
		if len(f.Detect) == 0 {
			t.Errorf("family %s has no Detect metrics", f.ID)
		}
		for _, p := range f.Panels {
			if prev, ok := seen[p.ID]; ok {
				t.Errorf("panel %s in %s duplicates one in %s", p.ID, f.ID, prev)
			}
			seen[p.ID] = f.ID
			if p.Fallback != "" && p.Detect == "" {
				t.Errorf("panel %s has a Fallback but no Detect", p.ID)
			}
		}
	}
	if _, ok := FamilyByID("node"); !ok {
		t.Error("FamilyByID(node) missing")
	}
	if _, ok := FamilyByID("nope"); ok {
		t.Error("FamilyByID(nope) found")
	}
}

func TestDetectNames(t *testing.T) {
	names := DetectNames()
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate detect name %s", n)
		}
		seen[n] = true
	}
	for _, want := range []string{"node_cpu_seconds_total", "container_cpu_usage_seconds_total", "pve_up", "prometheus_tsdb_head_series", "pve_network_receive_bytes_total", "kube_pod_container_status_restarts_total"} {
		if !seen[want] {
			t.Errorf("DetectNames missing %s", want)
		}
	}
}

func TestDetectFamilies(t *testing.T) {
	ids := func(fs []Family) string {
		var out []string
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return strings.Join(out, ",")
	}
	all := []string{"pve_up", "prometheus_tsdb_head_series", "node_memory_MemTotal_bytes", "container_cpu_usage_seconds_total", "unrelated"}
	cases := []struct {
		name  string
		names []string
		kube  bool
		want  string
	}{
		{"none", nil, false, ""},
		{"unrelated", []string{"foo", "bar"}, false, ""},
		{"node only", []string{"node_cpu_seconds_total"}, false, "node"},
		{"node by second detect", []string{"node_memory_MemTotal_bytes"}, true, "node"},
		{"cadvisor", []string{"container_cpu_usage_seconds_total"}, false, "cadvisor"},
		{"kubelet hint", []string{"container_cpu_usage_seconds_total"}, true, "kubelet"},
		{"all", all, false, "node,cadvisor,pve,prometheus"},
		{"all kube", all, true, "node,kubelet,pve,prometheus"},
	}
	for _, c := range cases {
		if got := ids(DetectFamilies(c.names, c.kube)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestRateWindow(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "2m"},
		{-time.Second, "2m"},
		{5 * time.Second, "1m"},
		{15 * time.Second, "1m"},
		{20 * time.Second, "2m"},
		{30 * time.Second, "2m"},
		{45 * time.Second, "5m"},
		{60 * time.Second, "5m"},
		{10 * time.Minute, "5m"},
	}
	for _, c := range cases {
		if got := RateWindow(c.in); got != c.want {
			t.Errorf("RateWindow(%s) = %s want %s", c.in, got, c.want)
		}
	}
}

func TestStepFor(t *testing.T) {
	cases := []struct {
		window   time.Duration
		columns  int
		interval time.Duration
		want     time.Duration
	}{
		{time.Hour, 60, 15 * time.Second, time.Minute},
		{time.Hour, 120, 15 * time.Second, 30 * time.Second},
		{time.Hour, 1000, 15 * time.Second, 15 * time.Second},
		{time.Hour, 100, 15 * time.Second, 45 * time.Second},
		{time.Hour, 0, 15 * time.Second, time.Hour},
		{time.Hour, 60, 0, time.Minute},
		{0, 60, 30 * time.Second, 30 * time.Second},
		{61 * time.Second, 60, time.Second, 2 * time.Second},
		{7 * time.Second, 2, 2 * time.Second, 4 * time.Second},
	}
	for _, c := range cases {
		got := StepFor(c.window, c.columns, c.interval)
		if got != c.want {
			t.Errorf("StepFor(%s, %d, %s) = %s want %s", c.window, c.columns, c.interval, got, c.want)
		}
		if iv := c.interval; iv > 0 && got%iv != 0 {
			t.Errorf("StepFor(%s, %d, %s) = %s is not a multiple of the interval", c.window, c.columns, c.interval, got)
		}
	}
}
