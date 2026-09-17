package dashboard

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Each host suite carries its own signature hue so Network, Processes and
// Storage are distinguishable at a glance rather than reading as one table in
// three costumes. The hues are drawn from the theme's decorative accent-to-glow
// range: error, warning and success keep their single meaning everywhere and
// are never spent on identity.
func panelHue(p palette, tab int) string {
	switch tab {
	case 3:
		return hexColourOf(blend(tcell.GetColor(p.accent), tcell.GetColor(p.glow), 0.55))
	case 4:
		return p.glow
	default:
		return p.accent
	}
}

func hexColourOf(colour tcell.Color) string { return fmt.Sprintf("#%06x", colour.Hex()) }

// pressureHue reports severity and nothing else, so a full disk looks the same
// urgent colour in every theme and on every panel.
func pressureHue(p palette, percent int) string {
	switch {
	case percent >= 85:
		return p.error
	case percent >= 70:
		return p.warning
	default:
		return p.success
	}
}

// Card sparklines are drawn with a light rule so they stay in the background.
// A gauge is the subject of its line, so it uses the solid weight of the same
// three glyph vocabularies; the setting still changes presentation only.
func gaugeMeter(value, maximum float64, width int, mode string) string {
	filledGlyph, emptyGlyph := "\u2588", "\u2591"
	switch graphMode(mode) {
	case "braille":
		filledGlyph, emptyGlyph = "\u283f", "\u2801"
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

// gaugeLine is the selection band's workhorse: a label, a proportional meter, a
// figure, and whatever context belongs beside it. The meter honours the shared
// block/braille/ASCII setting, so it is presentation, never measurement.
func gaugeLine(label string, value, maximum float64, width int, colour, p palette, mode, figure, trailing string) string {
	meter := gaugeMeter(value, maximum, width, mode)
	line := fmt.Sprintf("[%s]%-7s[-] [%s::b]%s[-::-]  [%s::b]%s[-::-]", p.muted, label, colour.accent, meter, colour.accent, figure)
	if trailing != "" {
		line += fmt.Sprintf("   [%s]%s[-]", p.muted, trailing)
	}
	return line
}

// A gauge needs only one colour, but the surrounding text needs the palette;
// passing a palette whose accent is the gauge colour keeps the call sites short.
func hueOf(p palette, colour string) palette {
	p.accent = colour
	return p
}

// cellGauge puts a compact meter next to a figure inside a table cell. Table
// cells are escaped, so the bar carries its meaning through the cell's own
// colour rather than through markup. Only the filled part is drawn: a track
// on every row of a 500-process table was visual noise, and the figure sits
// in a fixed-width slot so the column still aligns.
func cellGauge(figure string, value, maximum float64, width int, mode string) string {
	filledGlyph := "\u2588"
	switch graphMode(mode) {
	case "braille":
		filledGlyph = "\u283f"
	case "ascii":
		filledGlyph = "#"
	}
	filled := 0
	if maximum > 0 {
		filled = int(value / maximum * float64(width))
	}
	filled = max(0, min(width, filled))
	return fmt.Sprintf("%-6s %s%s", figure, strings.Repeat(filledGlyph, filled), strings.Repeat(" ", width-filled))
}

// exposureOf describes how far a listening socket can be reached from. It reads
// only the bind address: a bind is not a firewall rule, so the wording stays
// about the binding itself and never promises reachability.
func exposureOf(local, state string) (string, int) {
	address := local
	if i := strings.LastIndex(local, ":"); i >= 0 {
		address = local[:i]
	}
	address = strings.Trim(address, "[]")
	switch {
	case state != "LISTEN" && state != "UNCONN":
		return "established flow", 0
	case address == "127.0.0.1" || address == "::1" || strings.HasPrefix(address, "127."):
		return "loopback only · not bound off this host", 1
	case address == "0.0.0.0" || address == "::" || address == "*":
		return "all interfaces · bound host-wide", 3
	default:
		return "specific address · " + address, 2
	}
}

// The exposure chip is the Network panel's signature mark: a filled band whose
// colour rises with how widely the socket is bound.
func exposureChip(p palette, local, state string) string {
	text, level := exposureOf(local, state)
	colour := []string{p.muted, p.success, p.accent, p.warning}[level]
	label := []string{"FLOW", "LOOPBACK", "ADDRESS", "HOST-WIDE"}[level]
	return fmt.Sprintf("[%s]EXPOSURE[-]  [%s::b] %s [-::-]  [%s]%s[-]", p.muted, colour, label, p.muted, tview.Escape(text))
}
