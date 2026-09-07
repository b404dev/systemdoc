package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAliasesUseCanonicalRuntimeStateAndTemplatesStayDistinct(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *list-units*) printf '%s\n' '[{"unit":"dbus-broker.service","load":"loaded","active":"inactive","sub":"dead","description":"D-Bus"}]' ;;
 *list-unit-files*) printf '%s\n' '[{"unit_file":"dbus-broker.service","state":"enabled"},{"unit_file":"dbus.service","state":"alias"},{"unit_file":"worker@.service","state":"static"},{"unit_file":"backup.service","state":"disabled"}]' ;;
 *show*) printf 'Id=dbus-broker.service\nNames=dbus-broker.service dbus.service\nLoadState=loaded\nActiveState=active\nSubState=running\nCPUUsageNSec=1000000000\nMemoryCurrent=1048576\n' ;;
 *) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, user := range []bool{false, true} {
		items, err := inventory(context.Background(), 0, user)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Fatal("alias was duplicated as a separate service", items)
		}
		found := false
		for _, item := range items {
			switch item.ID {
			case "dbus-broker.service":
				found = true
				if item.State != "active" || item.Detail != "running" || item.Enablement != "enabled" {
					t.Fatal("runtime properties or boot state lost", item)
				}
				if !matchesFilter(item, "name:dbus.service") || !matchesUnitName(item, "dbus.service") {
					t.Fatal("canonical service cannot be found by alias")
				}
			case "worker@.service":
				if item.State != "template" || !item.UnitFileOnly {
					t.Fatal("template has fabricated runtime state")
				}
			case "backup.service":
				if item.State != "unknown" || !item.UnitFileOnly {
					t.Fatal("missing runtime state was guessed")
				}
			default:
				t.Fatal("unexpected row", item)
			}
		}
		if !found || fleetTotals(items).active != 1 {
			t.Fatal("alias should not inflate active count")
		}
	}
}

func TestStatusFourIsExplainedAndInactiveStatusIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *missing.service*) printf 'Unit missing.service could not be found.\n'; exit 4 ;;
 *) printf 'Active: inactive (dead)\n'; exit 3 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	output, err := inspect(context.Background(), 0, true, workload{ID: "missing.service"}, 0)
	if err != nil || !strings.Contains(output, "Unit not found in the user systemd manager") {
		t.Fatal(output, err)
	}
	output, err = inspect(context.Background(), 0, false, workload{ID: "inactive.service"}, 0)
	if err != nil || output != "Active: inactive (dead)\n" {
		t.Fatal("normal inactive status became a command failure", output, err)
	}
}

func TestTemplateInspectionDoesNotExecuteRuntimeCommands(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	item := workload{ID: "worker@.service", State: "template", UnitFileOnly: true}
	for _, tab := range []int{0, 1, 3, 4} {
		output, err := inspect(context.Background(), 0, false, item, tab)
		if err != nil || !strings.Contains(output, "named instances") {
			t.Fatal("template caused a runtime query", output, err)
		}
	}
	if _, _, err := actionArgs(0, false, item, "start"); err == nil {
		t.Fatal("template start should require an instance")
	}
	if _, _, err := actionArgs(0, false, item, "enable"); err != nil {
		t.Fatal("template unit-file operations should remain available")
	}
}
