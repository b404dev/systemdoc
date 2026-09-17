package dashboard

import (
	"strconv"
	"strings"

	"github.com/rivo/tview"
)

// sysdig's default line format is
//
//	<evtnum> <HH:MM:SS.nnnnnnnnn> <cpu> <comm> (<pid>) <dir> <event> <args>
//
// The stream panel collapses consecutive repeats of everything after the two
// leading columns and paints the parsed fields. Both passes are single scans
// over the retained buffer without regular expressions, because they run on
// every 400 ms tick against up to 2 MiB of text.

// sysdigPrefix returns the byte offset where the event body starts, after the
// event number and timestamp, or -1 when the line is not a sysdig event.
func sysdigPrefix(line string) int {
	num := strings.IndexByte(line, ' ')
	if num <= 0 {
		return -1
	}
	for i := 0; i < num; i++ {
		if line[i] < '0' || line[i] > '9' {
			return -1
		}
	}
	rest := line[num+1:]
	stampEnd := strings.IndexByte(rest, ' ')
	if stampEnd <= 0 {
		return -1
	}
	if !isClockStamp(rest[:stampEnd]) {
		return -1
	}
	return num + 1 + stampEnd + 1
}

// isClockStamp accepts HH:MM:SS with an optional fractional part, which is
// how sysdig prints %evt.outputtime by default.
func isClockStamp(stamp string) bool {
	if len(stamp) < 8 || stamp[2] != ':' || stamp[5] != ':' {
		return false
	}
	for i := 0; i < 8; i++ {
		if i == 2 || i == 5 {
			continue
		}
		if stamp[i] < '0' || stamp[i] > '9' {
			return false
		}
	}
	if len(stamp) == 8 {
		return true
	}
	if stamp[8] != '.' {
		return false
	}
	for i := 9; i < len(stamp); i++ {
		if stamp[i] < '0' || stamp[i] > '9' {
			return false
		}
	}
	return true
}

// collapseSysdigStream folds consecutive lines that are identical after the
// event number and timestamp columns into the first line of the run with a
// " ×N" suffix. Lines that are not sysdig events pass through unchanged and
// end any run in progress.
func collapseSysdigStream(text string) string {
	if text == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(text))
	var runKey, runFirst string
	runCount := 0
	flush := func() {
		if runCount == 0 {
			return
		}
		out.WriteString(runFirst)
		if runCount > 1 {
			out.WriteString(" ×")
			out.WriteString(strconv.Itoa(runCount))
		}
		out.WriteByte('\n')
		runCount = 0
	}
	rest := text
	for len(rest) > 0 {
		line := rest
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			line, rest = rest[:nl], rest[nl+1:]
		} else {
			rest = ""
		}
		start := sysdigPrefix(line)
		if start >= 0 && runCount > 0 && line[start:] == runKey {
			runCount++
			continue
		}
		flush()
		if start >= 0 {
			runKey, runFirst, runCount = line[start:], line, 1
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	flush()
	collapsed := out.String()
	if text[len(text)-1] != '\n' {
		collapsed = collapsed[:len(collapsed)-1]
	}
	return collapsed
}

// sysdigErrorResult reports whether an event's arguments carry a failed
// result: a negative res= value or one of the errno names sysdig prints.
func sysdigErrorResult(args string) bool {
	if strings.Contains(args, "res=-") {
		return true
	}
	for _, errno := range [...]string{"EAGAIN", "ENOENT", "EACCES", "ECONNREFUSED", "ETIMEDOUT"} {
		if strings.Contains(args, errno) {
			return true
		}
	}
	return false
}

// repeatSuffix splits a trailing " ×N" marker written by collapseSysdigStream.
func repeatSuffix(line string) (body, suffix string) {
	at := strings.LastIndex(line, " ×")
	if at < 0 || at+len(" ×") >= len(line) {
		return line, ""
	}
	for i := at + len(" ×"); i < len(line); i++ {
		if line[i] < '0' || line[i] > '9' {
			return line, ""
		}
	}
	return line[:at], line[at:]
}

// sysdigRich paints collapsed sysdig text for a dynamic-colours TextView.
// Every external fragment passes through tview.Escape; lines that are not
// sysdig events take richOutput's default per-line treatment.
func sysdigRich(text string, p palette) string {
	paint := func(colour, text string) string { return "[" + colour + "]" + tview.Escape(text) + "[-]" }
	var out strings.Builder
	out.Grow(len(text) * 2)
	rest := text
	for {
		line := rest
		nl := strings.IndexByte(rest, '\n')
		if nl >= 0 {
			line, rest = rest[:nl], rest[nl+1:]
		}
		start := sysdigPrefix(line)
		if start < 0 {
			out.WriteString(richOutput(line, 0, p))
		} else {
			body, suffix := repeatSuffix(line)
			out.WriteString(paint(p.muted, body[:start]))
			writeSysdigBody(&out, body[start:], p, paint)
			if suffix != "" {
				out.WriteString("[" + p.warning + "::b]" + tview.Escape(suffix) + "[-::-]")
			}
		}
		if nl < 0 {
			break
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// writeSysdigBody paints "<cpu> <comm> (<pid>) <dir> <event> <args>".
func writeSysdigBody(out *strings.Builder, body string, p palette, paint func(colour, text string) string) {
	dir := -1
	for i := 0; i < len(body); i++ {
		if (body[i] == '>' || body[i] == '<') && (i == 0 || body[i-1] == ' ') && (i+1 == len(body) || body[i+1] == ' ') {
			dir = i
			break
		}
	}
	if dir < 0 {
		out.WriteString(paint(p.text, body))
		return
	}
	out.WriteString(paint(p.text, body[:dir]))
	if body[dir] == '>' {
		out.WriteString(paint(p.accent, ">"))
	} else {
		out.WriteString(paint(p.glow, "<"))
	}
	after := body[dir+1:]
	if after == "" {
		return
	}
	// after begins with the space before the event name.
	name := after[1:]
	args := ""
	if space := strings.IndexByte(name, ' '); space >= 0 {
		name, args = name[:space], name[space:]
	}
	out.WriteString(paint(p.text, " "))
	out.WriteString("[" + p.text + "::b]" + tview.Escape(name) + "[-::-]")
	if args == "" {
		return
	}
	colour := p.text
	if sysdigErrorResult(args) {
		colour = p.error
	}
	out.WriteString(paint(colour, args))
}

// sysdigSnapshot builds the stream panel's snapshot for a sysdig probe: the
// collapsed text is retained for export and the last 500 collapsed lines are
// painted with sysdigRich. It keeps the logSnapshot contract render() expects.
func sysdigSnapshot(text, ending string, style logStyle) logSnapshot {
	collapsed := collapseSysdigStream(text)
	return logSnapshot{text: collapsed, ending: ending, style: style, formatted: sysdigRich(recentLogLines(collapsed, 500)+ending, style.palette)}
}
