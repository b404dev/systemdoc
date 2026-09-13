package dashboard

import (
	"strings"
	"testing"
)

func TestPanelHueSeparatesTheThreeHostSuites(t *testing.T) {
	p := themes[0]
	network, processes, storage := panelHue(p, 2), panelHue(p, 3), panelHue(p, 4)
	if network == processes || processes == storage || network == storage {
		t.Fatalf("host suites share a hue: %s / %s / %s", network, processes, storage)
	}
	// Identity is decorative. Severity colours keep one meaning everywhere.
	for _, hue := range []string{network, processes, storage} {
		if hue == p.error || hue == p.warning || hue == p.success {
			t.Errorf("%s spends a severity colour on panel identity", hue)
		}
	}
	// Every theme must separate them, not just the default.
	for _, theme := range themes {
		if panelHue(theme, 2) == panelHue(theme, 4) {
			t.Errorf("%s does not separate network from storage", theme.name)
		}
	}
}

func TestPressureHueRisesWithUsage(t *testing.T) {
	p := themes[0]
	for _, c := range []struct {
		percent int
		want    string
	}{{0, p.success}, {69, p.success}, {70, p.warning}, {84, p.warning}, {85, p.error}, {101, p.error}} {
		if got := pressureHue(p, c.percent); got != c.want {
			t.Errorf("%d%% = %s, want %s", c.percent, got, c.want)
		}
	}
}

func TestGaugeMeterFillsProportionally(t *testing.T) {
	if got := gaugeMeter(50, 100, 10, "blocks"); got != strings.Repeat("█", 5)+strings.Repeat("░", 5) {
		t.Errorf("half gauge = %q", got)
	}
	if got := gaugeMeter(0, 100, 4, "blocks"); got != strings.Repeat("░", 4) {
		t.Errorf("empty gauge = %q", got)
	}
	// A value beyond the maximum clamps to full rather than overflowing.
	if got := gaugeMeter(400, 100, 4, "blocks"); got != strings.Repeat("█", 4) {
		t.Errorf("over-full gauge = %q", got)
	}
	if got := gaugeMeter(50, 0, 4, "blocks"); got != strings.Repeat("░", 4) {
		t.Errorf("unknown maximum must not fill: %q", got)
	}
	// Every glyph mode renders the same width, so columns never shift.
	for _, mode := range []string{"blocks", "braille", "ascii"} {
		if width := len([]rune(gaugeMeter(30, 100, 12, mode))); width != 12 {
			t.Errorf("%s width = %d, want 12", mode, width)
		}
	}
}

func TestExposureOfReadsTheBindAddress(t *testing.T) {
	for _, c := range []struct {
		local, state string
		level        int
	}{
		{"127.0.0.1:3000", "LISTEN", 1},
		{"[::1]:8080", "LISTEN", 1},
		{"0.0.0.0:8080", "LISTEN", 3},
		{"[::]:5432", "LISTEN", 3},
		{"192.168.1.10:443", "LISTEN", 2},
		{"0.0.0.0:5353", "UNCONN", 3},
		{"10.0.0.2:54321", "ESTAB", 0},
	} {
		if _, level := exposureOf(c.local, c.state); level != c.level {
			t.Errorf("%s/%s level = %d, want %d", c.local, c.state, level, c.level)
		}
	}
	// The wording describes the binding and never promises reachability.
	text, _ := exposureOf("0.0.0.0:80", "LISTEN")
	if strings.Contains(strings.ToLower(text), "reachable") {
		t.Errorf("exposure text claims reachability: %q", text)
	}
}

func TestSelectionBandIsNamedForItsPanel(t *testing.T) {
	w := testWorkspace()
	processes := &hostPage{w: w, tab: 3}
	storage := &hostPage{w: w, tab: 4}
	deleted := &hostPage{w: w, tab: 4, deletedView: true}
	titles := []string{processes.bandTitle(), storage.bandTitle(), deleted.bandTitle()}
	for i, title := range titles {
		if strings.Contains(title, "SELECTED") {
			t.Errorf("title %d still uses the generic label: %q", i, title)
		}
		for j, other := range titles {
			if i != j && title == other {
				t.Errorf("titles %d and %d are identical: %q", i, j, title)
			}
		}
	}
}
