package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestPollIntervalUpdatesVisibleControl(t *testing.T) {
	w := testWorkspace()
	if err := w.setPollInterval(17); err != nil {
		t.Fatal(err)
	}
	if w.settings.RefreshSeconds != 17 || !strings.Contains(w.pollButton.GetLabel(), "17s") {
		t.Fatalf("poll interval was not reflected in the UI: %q", w.pollButton.GetLabel())
	}
	if err := w.setPollInterval(1); err == nil || w.settings.RefreshSeconds != 17 {
		t.Fatal("invalid poll interval changed the active setting")
	}
}

func TestPollDueUsesCurrentInterval(t *testing.T) {
	now := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	last := now.Add(-6 * time.Second)
	if !pollDue(last, now, 5) {
		t.Fatal("elapsed current interval should be due")
	}
	if pollDue(last, now, 30) {
		t.Fatal("a longer current interval should not be due")
	}
	if !pollDue(time.Time{}, now, 300) {
		t.Fatal("a page without an earlier poll should refresh immediately")
	}
}

func TestSplashPresentsWorkspaceAndPollingContext(t *testing.T) {
	w := testWorkspace()
	w.settings.RefreshSeconds = 17
	w.splash()
	text := w.splashView.GetText(true)
	for _, want := range []string{"S Y S T E M D O C", "SERVICES", "CONTAINERS", "NETWORK", "PROCESSES", "STORAGE", "polling every 17s", "PRESS ANY KEY"} {
		if !strings.Contains(text, want) {
			t.Fatalf("splash is missing %q:\n%s", want, text)
		}
	}
	if !w.pages.HasPage("splash") {
		t.Fatal("splash was not added to the workspace")
	}
}
