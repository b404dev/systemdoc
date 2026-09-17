package dashboard

import (
	"strings"
	"testing"
)

func TestLogFilterArgsAndLabel(t *testing.T) {
	if args := (logFilter{}).journalArgs(); len(args) != 0 {
		t.Fatalf("no filter adds no arguments: %v", args)
	}
	both := logFilter{errorsOnly: true, sinceBoot: true}
	if got := strings.Join(both.journalArgs(), " "); got != "--priority 0..3 --boot" {
		t.Fatalf("journal arguments = %q", got)
	}
	if got := both.label(); got != "errors and worse · this boot" {
		t.Fatalf("label = %q", got)
	}
	w := testWorkspace()
	w.tab = 1
	w.toggleLogFilter(true)
	if !w.logFilter.errorsOnly || !strings.Contains(w.inspectorTabName(), "errors and worse") {
		t.Fatalf("tab name must show the active filter: %q", w.inspectorTabName())
	}
	w.toggleLogFilter(true)
	if w.logFilter.errorsOnly || w.inspectorTabName() != "Logs" {
		t.Fatalf("toggling again restores the plain tab: %q", w.inspectorTabName())
	}
}
