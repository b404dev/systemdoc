package dashboard

import (
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

var ansiSequence = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-_])`)

// timestamp recognises ISO, syslog ("Sep 14 16:17:12") and bare clock stamps,
// so a journal tail inside a status block is dimmed like a Logs tab line.
var timestamp = regexp.MustCompile(`^(?:\d{4}-\d{2}-\d{2}[T ]\S+|[A-Z][a-z]{2} [ \d]\d \d{2}:\d{2}:\d{2}|\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
var errorWord = regexp.MustCompile(`(?i)\b(error|fatal|panic|failed|failure|unhealthy|refused)\b`)
var warningWord = regexp.MustCompile(`(?i)\b(warn|warning|restarting|retry|degraded)\b`)
var successWord = regexp.MustCompile(`(?i)\b(healthy|running|connected|started|active|success|completed)\b`)

var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ellipsize fits plain (unescaped, tag-free) text into width cells, ending in
// an ellipsis when it had to cut. Rails and overlays use it so a description
// is never chopped mid-word by the edge of its panel.
func ellipsize(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(text) <= width {
		return text
	}
	return runewidth.Truncate(text, width, "…")
}

func displayWidth(text string) int { return runewidth.StringWidth(text) }

// palette is asked for a dozen times a frame and from background workers, so
// the resolved theme is memoised against the inputs that shape it. The cache
// is one atomic pointer: any goroutine can read or replace it safely.
type paletteKey struct {
	theme              int
	accent, background string
}

type resolvedPalette struct {
	key     paletteKey
	palette palette
}

var paletteCache atomic.Pointer[resolvedPalette]

func (w *workspace) palette() palette {
	key := paletteKey{theme: w.theme, accent: w.settings.Accent, background: w.settings.Background}
	if cached := paletteCache.Load(); cached != nil && cached.key == key {
		return cached.palette
	}
	p := themes[key.theme]
	if hexColour.MatchString(key.accent) {
		p.accent = key.accent
	}
	if hexColour.MatchString(key.background) {
		p.background = key.background
	}
	paletteCache.Store(&resolvedPalette{key: key, palette: p})
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
