package dashboard

import (
	"context"
	"github.com/rivo/tview"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceInventoryWithInstalledUnitsAndMetrics(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *list-unit-files*) printf '%s\n' '[{"unit_file":"web.service","state":"enabled"},{"unit_file":"backup.service","state":"disabled"}]' ;;
 *list-units*) printf '%s\n' '[{"unit":"web.service","active":"active","sub":"running","description":"Web"}]' ;;
 *show*) printf 'Id=web.service\nCPUUsageNSec=1000000000\nMemoryCurrent=10485760\n' ;;
 *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	items, err := inventory(context.Background(), 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "backup.service" || (items[0].State != "unknown" || !items[0].UnitFileOnly) {
		t.Fatal(items)
	}
	web := items[1]
	if web.Enablement != "enabled" || web.Memory != "10.0 MiB" || !web.HasCPU {
		t.Fatal(web)
	}
}

func TestCommandParser(t *testing.T) {
	for _, test := range []struct {
		text      string
		mode, tab int
		user      bool
	}{
		{"systemctl --user status worker.service", 0, 0, true},
		{"journalctl -u worker.service -f", 0, 1, false},
		{"docker logs --follow abc", 1, 1, false},
	} {
		got, err := parseCommand(test.text, false)
		if err != nil || got.mode != test.mode || got.tab != test.tab || got.user != test.user {
			t.Fatal(test.text, got, err)
		}
	}
	for _, text := range []string{"systemctl restart --all", "docker stop foo;id", "docker prune foo", "journalctl -u foo --vacuum-time=1s", "systemctl status foo extra"} {
		if _, err := parseCommand(text, false); err == nil {
			t.Fatal("accepted unsupported command", text)
		}
	}
}

func TestRatesUseCounterDeltas(t *testing.T) {
	now := time.Now()
	before := []workload{{ID: "web", HasCPU: true, CPUCounter: 100, SampleAt: now}}
	after := []workload{{ID: "web", HasCPU: true, CPUCounter: 500000100, SampleAt: now.Add(time.Second)}}
	computeResourceRates(before, after)
	if after[0].CPU != "50.0%" {
		t.Fatal(after)
	}
}

func TestComposeEndpointMismatch(t *testing.T) {
	t.Setenv("DOCKER_CONTEXT", "production")
	if _, err := projectArgs(project{Name: "site", Endpoint: "context:staging"}, "down"); err == nil {
		t.Fatal("cross-endpoint operation accepted")
	}
}

func TestRoundedBorders(t *testing.T) {
	w := testWorkspace()
	if w.table == nil {
		t.Fatal("missing table")
	}
	// Focus must not switch back to square, double-line corners.
	if string([]rune{tview.Borders.TopLeft, tview.Borders.TopRight, tview.Borders.BottomLeft, tview.Borders.BottomRight, tview.Borders.TopLeftFocus, tview.Borders.TopRightFocus, tview.Borders.BottomLeftFocus, tview.Borders.BottomRightFocus}) != "╭╮╰╯╭╮╰╯" {
		t.Fatal("rounded border contract changed")
	}
}
