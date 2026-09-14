package dashboard

import (
	"fmt"
	"strings"
)

var graphModes = []string{"blocks", "braille", "ascii"}

func graphMode(value string) string {
	for _, mode := range graphModes {
		if value == mode {
			return value
		}
	}
	return graphModes[0]
}

func signalChart(values []float64, mode string) string {
	glyphs := []rune("▁▂▃▄▅▆▇█")
	switch graphMode(mode) {
	case "braille":
		glyphs = []rune("⡀⡄⡆⡇⣇⣧⣷⣿")
	case "ascii":
		glyphs = []rune("._-=+*#@")
	}
	maximum := 0.0
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	var out strings.Builder
	for _, value := range values {
		if value < 0 {
			out.WriteRune('·')
			continue
		}
		index := 0
		if maximum > 0 {
			index = int(value / maximum * float64(len(glyphs)-1))
		}
		out.WriteRune(glyphs[index])
	}
	return out.String()
}

func signalDelta(values []float64) string {
	if len(values) < 2 {
		return "baseline"
	}
	previous, current := values[len(values)-2], values[len(values)-1]
	if previous < 0 || current < 0 {
		return "no comparison"
	}
	delta := current - previous
	if delta == 0 {
		return "steady"
	}
	arrow := "↑"
	if delta < 0 {
		arrow = "↓"
	}
	return fmt.Sprintf("%s %+.1f", arrow, delta)
}

func signalDirection(values []float64) string {
	if len(values) < 2 || values[len(values)-2] < 0 || values[len(values)-1] < 0 {
		return "baseline"
	}
	switch {
	case values[len(values)-1] > values[len(values)-2]:
		return "↑ rising"
	case values[len(values)-1] < values[len(values)-2]:
		return "↓ falling"
	default:
		return "steady"
	}
}

func signalMeter(value, maximum float64, width int, mode string) string {
	filledGlyph, emptyGlyph := "━", "·"
	switch graphMode(mode) {
	case "braille":
		filledGlyph, emptyGlyph = "⣿", "⡀"
	case "ascii":
		filledGlyph, emptyGlyph = "#", "."
	}
	filled := 0
	if maximum > 0 {
		filled = int(value / maximum * float64(width))
	}
	filled = max(0, min(width, filled))
	return strings.Repeat(filledGlyph, filled) + strings.Repeat(emptyGlyph, width-filled)
}

func signalArea(values []float64, fixedMaximum float64, mode string) string {
	if len(values) == 0 {
		return "collecting samples\n·"
	}
	maximum := fixedMaximum
	if maximum <= 0 {
		for _, value := range values {
			maximum = max(maximum, value)
		}
	}
	maximum = max(maximum, 1)
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	if graphMode(mode) == "braille" {
		blocks = []rune(" ⡀⡄⡆⡇⣇⣧⣷⣿")
	}
	rows := [2]strings.Builder{}
	for row := 1; row >= 0; row-- {
		for _, value := range values {
			if value < 0 {
				rows[1-row].WriteRune('·')
				continue
			}
			level := int(value / maximum * 16)
			level = max(0, min(16, level))
			cell := max(0, min(8, level-row*8))
			if graphMode(mode) == "ascii" {
				if cell == 0 {
					rows[1-row].WriteRune('.')
				} else {
					rows[1-row].WriteRune('#')
				}
				continue
			}
			rows[1-row].WriteRune(blocks[cell])
		}
	}
	return strings.TrimRight(rows[0].String(), " ") + "\n" + rows[1].String()
}
