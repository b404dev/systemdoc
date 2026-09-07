package dashboard

import (
	"regexp"
	"strings"

	"github.com/rivo/tview"
)

var ansiSequence = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-_])`)
var timestamp = regexp.MustCompile(`^(?:\d{4}-\d{2}-\d{2}[T ]\S+|\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
var errorWord = regexp.MustCompile(`(?i)\b(error|fatal|panic|failed|failure|unhealthy|refused)\b`)
var warningWord = regexp.MustCompile(`(?i)\b(warn|warning|restarting|retry|degraded)\b`)
var successWord = regexp.MustCompile(`(?i)\b(healthy|running|connected|started|active|success|completed)\b`)

var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (w *workspace) palette() palette {
	p := themes[w.theme]
	if hexColour.MatchString(w.settings.Accent) {
		p.accent = w.settings.Accent
	}
	if hexColour.MatchString(w.settings.Background) {
		p.background = w.settings.Background
	}
	return p
}

// Rich output is built exclusively from escaped text, never trusted input markup.
func richOutput(raw string, tab int, p palette) string {
	lines := strings.Split(clean(raw), "\n")
	paint := func(colour, text string) string { return "[" + colour + "]" + tview.Escape(text) + "[-]" }
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case (tab == 0 || tab == 4) && strings.HasPrefix(line, "── "):
			lines[i] = "[" + p.accent + "::b]" + tview.Escape(line) + "[-::-]"
		case tab == 2 && strings.HasPrefix(trim, "#"):
			lines[i] = paint(p.muted, line)
		case tab == 2 && strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]"):
			lines[i] = paint(p.accent, line)
		case tab == 2 && strings.Contains(line, "="):
			key, value, _ := strings.Cut(line, "=")
			lines[i] = paint(p.accent, key) + paint(p.muted, " = ") + paint(p.text, value)
		default:
			colour := p.text
			switch {
			case errorWord.MatchString(line):
				colour = p.error
			case warningWord.MatchString(line):
				colour = p.warning
			case successWord.MatchString(line):
				colour = p.success
			}
			if stamp := timestamp.FindString(line); stamp != "" {
				lines[i] = paint(p.muted, stamp) + paint(colour, line[len(stamp):])
			} else {
				lines[i] = paint(colour, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}
