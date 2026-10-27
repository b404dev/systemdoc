package dashboard

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"systemdoc/internal/promapi"
)

// trendStyle says how renderTrend draws one history.
type trendStyle struct {
	width, rows int
	fixedMax    float64 // <= 0 scales to the window's own maximum
	mode        string  // blocks, braille or ascii
	bars        bool    // one glyph column and one blank column per sample
}

// renderTrend draws the newest samples that fit the width as an area chart
// rows high. It follows signalArea's rules: eight levels per row, a reading
// above zero never draws as nothing, and a negative sample is a gap (·).
// Samples before the first known one are blank rather than gaps, so a window
// that predates the data starts clean. With two rows and bars off the output
// otherwise matches signalArea exactly.
func renderTrend(values []float64, s trendStyle) []string {
	rows := max(1, s.rows)
	perSample := 1
	if s.bars {
		perSample = 2
	}
	values = trimHistory(values, max(1, s.width/perSample))
	maximum := s.fixedMax
	if maximum <= 0 {
		for _, value := range values {
			maximum = max(maximum, value)
		}
	}
	maximum = max(maximum, 1)
	mode := graphMode(s.mode)
	glyphs := []rune(" ▁▂▃▄▅▆▇█")
	if mode == "braille" {
		glyphs = []rune(" ⡀⡄⡆⡇⣇⣧⣷⣿")
	}
	levels := rows * 8
	out := make([]strings.Builder, rows)
	seen := false // a known sample has been drawn
	for i, value := range values {
		level := -1
		if value >= 0 {
			seen = true
			level = max(0, min(levels, int(value/maximum*float64(levels))))
			if value > 0 && level == 0 {
				level = 1
			}
		}
		for row := 0; row < rows; row++ {
			// out[0] is the top row; row r from the bottom holds levels r*8..r*8+8.
			b := &out[rows-1-row]
			switch {
			case level < 0 && !seen:
				b.WriteRune(' ')
			case level < 0:
				b.WriteRune('·')
			case mode == "ascii":
				if level-row*8 > 0 {
					b.WriteRune('#')
				} else {
					b.WriteRune('.')
				}
			default:
				b.WriteRune(glyphs[max(0, min(8, level-row*8))])
			}
			if s.bars && i < len(values)-1 {
				b.WriteRune(' ')
			}
		}
	}
	lines := make([]string, rows)
	for i := range out {
		lines[i] = strings.TrimRight(out[i].String(), " ")
	}
	return lines
}

// formatMetric renders a value in its unit; a negative value is unknown.
func formatMetric(value float64, unit string) string {
	if value < 0 {
		return "—"
	}
	switch unit {
	case promapi.UnitPercent:
		return strconv.FormatFloat(value, 'f', 1, 64) + "%"
	case promapi.UnitBytes:
		return hostBytes(int64(value), false)
	case promapi.UnitBytesPerSecond:
		return hostBytes(int64(value), false) + "/s"
	case promapi.UnitSeconds:
		return formatSeconds(value)
	case promapi.UnitCelsius:
		return strconv.FormatFloat(value, 'f', 0, 64) + "°C"
	case promapi.UnitCount:
		return formatCount(value)
	case promapi.UnitPerSecond:
		return strconv.FormatFloat(value, 'f', 2, 64) + "/s"
	case promapi.UnitLoad:
		return strconv.FormatFloat(value, 'f', 2, 64)
	default:
		return strconv.FormatFloat(value, 'g', 4, 64)
	}
}

func formatSeconds(seconds float64) string {
	switch {
	case seconds < 1:
		return strconv.FormatFloat(seconds*1000, 'f', 0, 64) + " ms"
	case seconds < 60:
		return strconv.FormatFloat(seconds, 'f', 1, 64) + " s"
	case seconds < 3600:
		return strconv.FormatFloat(seconds/60, 'f', 1, 64) + " m"
	case seconds < 86400:
		return strconv.FormatFloat(seconds/3600, 'f', 1, 64) + " h"
	default:
		return strconv.FormatFloat(seconds/86400, 'f', 1, 64) + " d"
	}
}

func formatCount(value float64) string {
	switch {
	case value >= 1e9:
		return strconv.FormatFloat(value/1e9, 'f', 1, 64) + "G"
	case value >= 1e6:
		return strconv.FormatFloat(value/1e6, 'f', 1, 64) + "M"
	case value >= 1e3:
		return strconv.FormatFloat(value/1e3, 'f', 1, 64) + "k"
	default:
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
}

// axisChart draws a trend with a small axis on the left naming the top of
// the scale, so a flat line and a full line can be told apart at a glance.
// The scale is the window's peak plus a quarter of headroom, never above the
// panel's ceiling (s.fixedMax); colour, not scale, carries the ceiling.
func axisChart(values []float64, unit string, s trendStyle) []string {
	peak := 0.0
	for _, value := range values {
		peak = max(peak, value)
	}
	scale := peak * 1.25
	if s.fixedMax > 0 {
		scale = min(scale, s.fixedMax)
	}
	top := formatMetric(scale, unit)
	width := max(displayWidth(top), 1)
	s.width -= width + 2
	s.fixedMax = scale
	rows := renderTrend(values, s)
	for i := range rows {
		label := ""
		switch i {
		case 0:
			label = top
		case len(rows) - 1:
			label = "0"
		}
		rows[i] = fmt.Sprintf("%*s ┤%s", width, label, rows[i])
	}
	return rows
}

// knownSpan counts the columns from the first known sample to the end.
func knownSpan(values []float64) int {
	for i, value := range values {
		if value >= 0 {
			return len(values) - i
		}
	}
	return 0
}

// trendCaption names the window a chart covers: "15:02 ─ 15:17 · 15s/col".
func trendCaption(newest time.Time, step time.Duration, points, width int) string {
	if newest.IsZero() || points == 0 {
		return "collecting samples"
	}
	oldest := newest.Add(-step * time.Duration(points-1))
	span := oldest.Format("15:04") + " ─ " + newest.Format("15:04")
	if width < 28 {
		return span
	}
	return span + " · " + shortDuration(step) + "/col"
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
}
