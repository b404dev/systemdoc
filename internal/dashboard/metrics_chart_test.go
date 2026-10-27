package dashboard

import (
	"strings"
	"testing"
	"time"

	"systemdoc/internal/promapi"
)

func TestRenderTrendMatchesSignalAreaAtTwoRows(t *testing.T) {
	values := []float64{0, 3, 12.5, -1, 50, 99, 100, 0.001, 42, 7}
	for _, mode := range graphModes {
		for _, fixed := range []float64{0, 100} {
			got := strings.Join(renderTrend(values, trendStyle{width: 40, rows: 2, fixedMax: fixed, mode: mode}), "\n")
			want := signalArea(values, fixed, mode)
			if got != strings.TrimRight(want, " ") {
				t.Errorf("%s fixed=%v\n got %q\nwant %q", mode, fixed, got, want)
			}
		}
	}
}

func TestRenderTrendRulesAtMoreRows(t *testing.T) {
	lines := renderTrend([]float64{0.001, 0, -1, 100}, trendStyle{width: 10, rows: 6, fixedMax: 100, mode: "blocks"})
	if len(lines) != 6 {
		t.Fatalf("rows: %d", len(lines))
	}
	bottom := []rune(lines[5])
	if bottom[0] != '▁' {
		t.Errorf("a non-zero reading must show at least one level, got %q", string(bottom[0]))
	}
	if bottom[1] != ' ' {
		t.Errorf("zero draws nothing, got %q", string(bottom[1]))
	}
	for _, line := range lines {
		if []rune(line)[2] != '·' {
			t.Errorf("gap must show in every row: %q", line)
		}
	}
	if []rune(lines[0])[3] != '█' {
		t.Errorf("full value must fill the top row: %q", lines[0])
	}
	ascii := renderTrend([]float64{50, -1, 100}, trendStyle{width: 10, rows: 3, fixedMax: 100, mode: "ascii"})
	for _, line := range ascii {
		if strings.Trim(line, "#.·") != "" {
			t.Errorf("ascii uses only # . and gaps: %q", line)
		}
	}
}

func TestRenderTrendTrimsAndBars(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	lines := renderTrend(values, trendStyle{width: 20, rows: 2, mode: "blocks"})
	if len([]rune(lines[1])) != 20 || []rune(lines[1])[19] != '█' {
		t.Errorf("newest 20 samples expected, got %q", lines[1])
	}
	bars := renderTrend(values, trendStyle{width: 20, rows: 2, mode: "blocks", bars: true})
	if runes := []rune(bars[1]); len(runes) != 19 || runes[1] != ' ' || runes[0] == ' ' {
		t.Errorf("bars alternate glyph and blank over half the width, got %q", bars[1])
	}
}

func TestFormatMetricAndCaption(t *testing.T) {
	cases := map[string]string{
		formatMetric(-1, promapi.UnitPercent):                                               "—",
		formatMetric(42.26, promapi.UnitPercent):                                            "42.3%",
		formatMetric(1536, promapi.UnitBytes):                                               "1.5 KiB",
		formatMetric(2048, promapi.UnitBytesPerSecond):                                      "2.0 KiB/s",
		formatMetric(0.0123, promapi.UnitSeconds):                                           "12 ms",
		formatMetric(90, promapi.UnitSeconds):                                               "1.5 m",
		formatMetric(172800, promapi.UnitSeconds):                                           "2.0 d",
		formatMetric(48213, promapi.UnitCount):                                              "48.2k",
		formatMetric(1.5, promapi.UnitPerSecond):                                            "1.50/s",
		formatMetric(0.73, promapi.UnitLoad):                                                "0.73",
		formatMetric(63, promapi.UnitCelsius):                                               "63°C",
		formatMetric(3.14159, promapi.UnitNone):                                             "3.142",
		trendCaption(time.Time{}, 0, 0, 40):                                                 "collecting samples",
		trendCaption(time.Date(2024, 5, 1, 15, 17, 0, 0, time.UTC), 15*time.Second, 61, 40): "15:02 ─ 15:17 · 15s/col",
		trendCaption(time.Date(2024, 5, 1, 15, 17, 0, 0, time.UTC), 15*time.Second, 61, 20): "15:02 ─ 15:17",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func BenchmarkRenderTrend(b *testing.B) {
	values := make([]float64, 152)
	for i := range values {
		values[i] = float64(i % 100)
	}
	for i := 0; i < b.N; i++ {
		renderTrend(values, trendStyle{width: 152, rows: 6, fixedMax: 100, mode: "braille"})
	}
}

func TestRenderTrendLeadingGapsAreBlankAndAxisNamesThePeak(t *testing.T) {
	values := []float64{-1, -1, 5, -1, 20}
	rows := renderTrend(values, trendStyle{width: 10, rows: 2, mode: "blocks"})
	bottom := []rune(rows[1])
	if bottom[0] != ' ' || bottom[1] != ' ' || bottom[3] != '·' {
		t.Fatalf("leading gaps blank, interior gaps dotted: %q", rows[1])
	}
	if knownSpan(values) != 3 {
		t.Fatalf("known span %d", knownSpan(values))
	}
	axis := axisChart(values, promapi.UnitPercent, trendStyle{width: 20, rows: 3, mode: "blocks", fixedMax: 100})
	if len(axis) != 3 || !strings.HasPrefix(axis[0], "25.0% ┤") || !strings.HasPrefix(axis[2], "    0 ┤") || !strings.HasPrefix(axis[1], "      ┤") {
		t.Fatalf("axis rows: %q", axis)
	}
	if width := displayWidth(axis[2]); width > 20 {
		t.Fatalf("axis chart wider than asked: %d", width)
	}
	if strings.HasSuffix(axis[0], " ") || strings.HasSuffix(axis[0], "█") {
		t.Fatalf("the peak reaches into the top row with headroom above it: %q", axis[0])
	}
	capped := axisChart([]float64{90, 95}, promapi.UnitPercent, trendStyle{width: 20, rows: 2, mode: "blocks", fixedMax: 100})
	if !strings.HasPrefix(capped[0], "100.0% ┤") {
		t.Fatalf("headroom must stop at the ceiling: %q", capped[0])
	}
}
