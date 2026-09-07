package dashboard

import (
	"math"
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func luminance(color tcell.Color) float64 {
	r, g, b := color.RGB()
	channel := func(v int32) float64 {
		n := float64(v) / 255
		if n <= 0.04045 {
			return n / 12.92
		}
		return math.Pow((n+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}
func contrast(a, b tcell.Color) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}
func TestDeepThemesKeepReadableTextAndSelection(t *testing.T) {
	names := map[string]bool{}
	for _, p := range themes {
		if names[p.name] {
			t.Fatal("duplicate theme", p.name)
		}
		names[p.name] = true
		bg, surface := tcell.GetColor(p.background), tcell.GetColor(p.surface)
		if luminance(bg) > 0.02 || luminance(surface) > 0.03 {
			t.Fatal("theme lost dark foundation", p.name)
		}
		for _, colour := range []string{p.text, p.muted, p.accent, p.error, p.warning, p.success} {
			if contrast(tcell.GetColor(colour), surface) < 4.5 {
				t.Fatal("low contrast on surface", p.name, colour)
			}
		}
		for step := 0; step <= 10; step++ {
			selected := blend(tcell.GetColor(p.accent), tcell.GetColor(p.glow), float64(step)/10*0.55)
			if contrast(bg, selected) < 4.5 {
				t.Fatal("unreadable gradient selection", p.name, step)
			}
		}
	}
}
func TestRetiredThemeFallsBackWithoutRewritingPreferences(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	original := settings{Theme: "retired-preset", RefreshSeconds: 5, Accent: "#aabbcc"}
	if err := writeSettings(original); err != nil {
		t.Fatal(err)
	}
	path, _ := settingsPath()
	before, _ := os.ReadFile(path)
	got, err := readSettings()
	if err != nil || got.Theme != "Deep Navy" || got.Accent != original.Accent {
		t.Fatal(got, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("reading migrated settings rewrote the file")
	}
}
