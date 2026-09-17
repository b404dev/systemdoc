package dashboard

import "strings"

// The splash carries the only large-format wordmark in the product. It is a
// block letterform rather than a font choice, so it renders identically in any
// monospace terminal without a patched font or a graphics protocol.
var wordmarkGlyphs = map[rune][5]string{
	'S': {"█████", "█    ", "█████", "    █", "█████"},
	'Y': {"█   █", " █ █ ", "  █  ", "  █  ", "  █  "},
	'T': {"█████", "  █  ", "  █  ", "  █  ", "  █  "},
	'E': {"█████", "█    ", "████ ", "█    ", "█████"},
	'M': {"█   █", "██ ██", "█ █ █", "█   █", "█   █"},
	'D': {"████ ", "█   █", "█   █", "█   █", "████ "},
	'O': {"█████", "█   █", "█   █", "█   █", "█████"},
	'C': {"█████", "█    ", "█    ", "█    ", "█████"},
}

const wordmarkHeight = 5

// renderWordmark lays the letters out side by side. An unknown rune becomes a
// blank column rather than a missing-glyph box, so the mark never breaks.
func renderWordmark(text string) []string {
	var builders [wordmarkHeight]strings.Builder
	for i, r := range text {
		glyph, known := wordmarkGlyphs[r]
		if !known {
			glyph = [5]string{"  ", "  ", "  ", "  ", "  "}
		}
		for row := 0; row < wordmarkHeight; row++ {
			if i > 0 {
				builders[row].WriteByte(' ')
			}
			builders[row].WriteString(glyph[row])
		}
	}
	rows := make([]string, wordmarkHeight)
	for row := range builders {
		rows[row] = builders[row].String()
	}
	return rows
}

// The gradient runs across each row so the mark reads as one object lit from
// the accent side, matching how the header and selected row are lit.
func wordmarkBlock(text string, from, to string) string {
	rows := renderWordmark(text)
	for i, row := range rows {
		rows[i] = gradientText(row, from, to)
	}
	return strings.Join(rows, "\n")
}
