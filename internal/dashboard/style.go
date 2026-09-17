package dashboard

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/rivo/tview"
)

// blend is the one place colours are interpolated: gradients, surface tints,
// selection rows, frame lighting and panel hues all pass through it. It mixes
// in CIE Lab so a step of the gradient is a step in perceived colour, not in
// channel arithmetic, and clamps back into the sRGB gamut for the terminal.
func blend(a, b tcell.Color, amount float64) tcell.Color {
	return blendTableFor(a, b).at(blendStep(amount))
}

// blendStep quantises an amount to 256 steps: finer than a 24-bit terminal
// can show across any gradient the interface draws, and it bounds the table.
func blendStep(amount float64) int {
	return int(max(0, min(1, amount))*255 + 0.5)
}

type blendKey struct{ from, to int32 }

// blendTable memoises one colour pair. Lab mixing costs about a microsecond
// and paintSurfaces asks for it once per cell per frame, so each step is
// computed once and then read without a lock: a slot holds the tcell.Color
// itself, which always carries ColorValid, so zero means "not yet computed".
// Two goroutines racing on one slot store the same deterministic value.
type blendTable struct {
	from, to tcell.Color
	cells    [256]atomic.Uint64
}

func (t *blendTable) at(step int) tcell.Color {
	if v := t.cells[step].Load(); v != 0 {
		return tcell.Color(v)
	}
	mixed := colorfulOf(t.from).BlendLab(colorfulOf(t.to), float64(step)/255).Clamped()
	r, g, b := mixed.RGB255()
	colour := tcell.NewRGBColor(int32(r), int32(g), int32(b))
	t.cells[step].Store(uint64(colour))
	return colour
}

// The table registry is copy-on-write: readers load one pointer and index an
// immutable map, writers (a new colour pair, rare after the first frame)
// serialise on the mutex and publish a fresh copy.
var blendTables struct {
	sync.Mutex
	current atomic.Pointer[map[blendKey]*blendTable]
}

func blendTableFor(a, b tcell.Color) *blendTable {
	key := blendKey{a.Hex(), b.Hex()}
	if tables := blendTables.current.Load(); tables != nil {
		if table, ok := (*tables)[key]; ok {
			return table
		}
	}
	blendTables.Lock()
	defer blendTables.Unlock()
	previous := blendTables.current.Load()
	if previous != nil {
		if table, ok := (*previous)[key]; ok {
			return table
		}
	}
	next := make(map[blendKey]*blendTable, 1)
	if previous != nil {
		next = make(map[blendKey]*blendTable, len(*previous)+1)
		for k, v := range *previous {
			next[k] = v
		}
	}
	table := &blendTable{from: a, to: b}
	next[key] = table
	blendTables.current.Store(&next)
	return table
}

func colorfulOf(c tcell.Color) colorful.Color {
	r, g, b := c.RGB()
	return colorful.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}
}

// writeColourTag appends "[#rrggbb]" without going through fmt.
func writeColourTag(out *strings.Builder, colour tcell.Color) {
	const digits = "0123456789abcdef"
	hex := colour.Hex()
	var tag [9]byte
	tag[0], tag[1], tag[8] = '[', '#', ']'
	for i := 7; i >= 2; i-- {
		tag[i] = digits[hex&0xf]
		hex >>= 4
	}
	out.Write(tag[:])
}

// gradientText is for trusted, single-cell decorative glyphs only.
func gradientText(value, from, to string) string {
	runes := []rune(value)
	table := blendTableFor(tcell.GetColor(from), tcell.GetColor(to))
	span := float64(max(1, len(runes)-1))
	var out strings.Builder
	out.Grow(len(runes)*13 + 3)
	for i, r := range runes {
		writeColourTag(&out, table.at(blendStep(float64(i)/span)))
		out.WriteRune(r)
	}
	out.WriteString("[-]")
	return out.String()
}

// Finish surfaces after text rendering so glyph backgrounds share the same
// light as empty cells. This wrapper is inside the main page: overlays draw last.
type luminousDashboard struct {
	*tview.Flex
	w *workspace
}

func (w *workspace) railPanel(box *tview.Box) bool {
	if box == w.selectionCard.Box {
		return true
	}
	for _, card := range w.cards {
		if box == card.Box {
			return true
		}
	}
	return false
}

func (d *luminousDashboard) Draw(screen tcell.Screen) {
	d.Flex.Draw(screen)
	p := d.w.palette()
	rails := map[*tview.Box]string{d.w.selectionCard.Box: p.accent}
	for i, card := range d.w.cards {
		// Active and attention are states, so they wear state colours. The two
		// host cards are identities and take decorative hues; their figures
		// carry severity in the headline instead.
		rails[card.Box] = []string{p.success, p.error, p.accent, p.glow}[i]
	}
	paintSurfaces(screen, d.Flex, p, d.w.header, d.w.table, rails, d.w.limitedColours)
}

// Both primary pages use the same post-draw surface treatment. With flat set
// every panel takes one tint and the header one hue, because a terminal
// without 24-bit colour would otherwise snap each blend step to a different
// palette entry and draw the gradient as blocks.
//
// The light falling on a panel is separable: amount = strength × horizontal(col)
// × vertical(row). Each panel resolves its blend tables and the two factor
// vectors once, so a cell costs a read, a multiply and a table index. The
// factors are kept as float64 and multiplied in the same order as the original
// per-cell expression, so every quantised step is bit-identical to it.
func paintSurfaces(screen tcell.Screen, root *tview.Flex, p palette, header *tview.TextView, table *tview.Table, rails map[*tview.Box]string, flat bool) {
	sw, sh := screen.Size()
	accent, glow := tcell.GetColor(p.accent), tcell.GetColor(p.glow)
	surface, background := tcell.GetColor(p.surface), tcell.GetColor(p.background)
	accentGlow := blendTableFor(accent, glow)
	var visit func(tview.Primitive)
	visit = func(primitive tview.Primitive) {
		_, _, width, height := primitive.GetRect()
		if width <= 0 || height <= 0 {
			return
		}
		if flex, ok := primitive.(*tview.Flex); ok {
			for i := 0; i < flex.GetItemCount(); i++ {
				visit(flex.GetItem(i))
			}
			return
		}
		var box *tview.Box
		switch view := primitive.(type) {
		case *tview.TextView:
			box = view.Box
		case *tview.Table:
			box = view.Box
		default:
			return
		}
		if box.GetDrawFunc() == nil && primitive != header {
			return
		}
		colour, rail := rails[box]
		if !rail {
			colour = p.accent
		}
		x, y, width, height := box.GetRect()
		x0, x1 := max(0, x), min(sw, x+width)
		y0, y1 := max(0, y), min(sh, y+height)
		if x0 >= x1 || y0 >= y1 {
			return
		}
		strength := 0.025
		if rail {
			strength = 0.22
		} else if primitive.HasFocus() {
			strength = 0.09
		}
		tint := tcell.GetColor(colour)
		surfaceTint, backgroundTint := blendTableFor(surface, tint), blendTableFor(background, tint)
		isTable := primitive == table

		if primitive == header {
			// The header hue varies with the column only, so resolve each
			// column's hue and its background wash once for all header rows.
			var hueBuffer, washBuffer [paintSpan]tcell.Color
			hues, washes := colourSpan(&hueBuffer, x1-x0), colourSpan(&washBuffer, x1-x0)
			for col := x0; col < x1; col++ {
				position := float64(col-x) / float64(max(1, width-1))
				if flat {
					position = 0.5
				}
				hue := accentGlow.at(blendStep(position))
				hues[col-x0] = hue
				washes[col-x0] = blendTableFor(background, hue).at(blendStep(0.22))
			}
			rule := height >= 3
			for row := y0; row < y1; row++ {
				for col := x0; col < x1; col++ {
					if rail && (col == x || col == x+width-1 || row == y+height-1) {
						continue
					}
					r, combining, style, _ := screen.GetContent(col, row)
					_, bg, _ := style.Decompose()
					if bg != surface && bg != background {
						continue
					}
					style = style.Background(washes[col-x0])
					if rule && row == y+height-1 {
						r, combining = '━', nil
						style = style.Foreground(hues[col-x0]).Background(background)
					}
					screen.SetContent(col, row, r, combining, style)
				}
			}
			return
		}

		flatStep := blendStep(strength * 0.5)
		var horizontalBuffer, verticalBuffer [paintSpan]float64
		horizontal, vertical := factorSpan(&horizontalBuffer, x1-x0), factorSpan(&verticalBuffer, y1-y0)
		for col := x0; col < x1; col++ {
			horizontal[col-x0] = strength * (1 - float64(col-x)/float64(max(1, width-1)))
		}
		for row := y0; row < y1; row++ {
			vertical[row-y0] = 1 - 0.7*float64(row-y)/float64(max(1, height-1))
		}
		for row := y0; row < y1; row++ {
			lastRow := row == y+height-1
			for col := x0; col < x1; col++ {
				if rail && (col == x || col == x+width-1 || lastRow) {
					continue
				}
				r, combining, style, _ := screen.GetContent(col, row)
				_, bg, _ := style.Decompose()
				if isTable && bg == accent {
					position := float64(col-x-1) / float64(max(1, width-3))
					if flat {
						position = 0.5
					}
					selection := accentGlow.at(blendStep(0.55 * max(0, min(1, position))))
					if col == x+1 {
						r, combining = '▸', nil
					}
					screen.SetContent(col, row, r, combining, style.Background(selection))
					continue
				}
				// Preserve matches and explicit log backgrounds.
				var pair *blendTable
				switch bg {
				case surface:
					pair = surfaceTint
				case background:
					pair = backgroundTint
				default:
					continue
				}
				step := flatStep
				if !flat {
					step = blendStep(horizontal[col-x0] * vertical[row-y0])
				}
				// Writing an unchanged cell back is a no-op for the buffer but
				// not for the screen lock, so skip it.
				if next := style.Background(pair.at(step)); next != style {
					screen.SetContent(col, row, r, combining, next)
				}
			}
		}
	}
	visit(root)
}

// paintSpan is the widest panel whose per-column and per-row factors fit in
// stack scratch; anything wider (an unusually large terminal) allocates.
const paintSpan = 512

func factorSpan(buffer *[paintSpan]float64, n int) []float64 {
	if n <= len(buffer) {
		return buffer[:n]
	}
	return make([]float64, n)
}

func colourSpan(buffer *[paintSpan]tcell.Color, n int) []tcell.Color {
	if n <= len(buffer) {
		return buffer[:n]
	}
	return make([]tcell.Color, n)
}

// Light lives inside each panel's bounds, so adjacent panes and overlays never
// get painted over. Native tview still owns titles, content, and hit testing.
func (w *workspace) illuminate(box *tview.Box, focused func() bool, colour func(palette) string) {
	w.illuminatePanel(box, focused, colour, w.railPanel(box))
}

func (w *workspace) illuminatePanel(box *tview.Box, focused func() bool, colour func(palette) string, rail bool) {
	box.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		p := w.palette()
		base, accent := tcell.GetColor(p.surface), tcell.GetColor(colour(p))
		frame := blendTableFor(base, accent)
		railStyle := tcell.StyleDefault.Background(tcell.GetColor(p.background))
		lit := focused != nil && focused()
		sw, sh := screen.Size()
		x0, x1 := max(0, x), min(sw, x+width)
		y0, y1 := max(0, y), min(sh, y+height)
		top, bottom, left, right := y, y+height-1, x, x+width-1
		paint := func(col, row int) {
			vertical := 1 - float64(row-y)/float64(max(1, height-1))
			horizontal := 1 - float64(col-x)/float64(max(1, width-1))
			if w.limitedColours {
				horizontal, vertical = 0.5, 0.5
			}
			r, combining, style, _ := screen.GetContent(col, row)
			style = style.Background(base)
			if rail && (row == bottom || col == left || col == right) {
				screen.SetContent(col, row, ' ', nil, railStyle)
				return
			}
			// Keep title lettering bright; only fade the frame itself.
			switch r {
			case '╭', '╮', '╰', '╯', '─', '│':
				intensity := 0.12 + 0.22*horizontal*vertical
				if lit {
					intensity = 0.30 + 0.70*horizontal*vertical
				}
				if rail && row == top {
					r = '━'
					intensity = 0.25 + 0.75*horizontal
				}
				style = style.Foreground(frame.at(blendStep(intensity)))
			}
			screen.SetContent(col, row, r, combining, style)
		}
		// Only the perimeter is lit; the interior belongs to the widget.
		if top >= y0 && top < y1 {
			for col := x0; col < x1; col++ {
				paint(col, top)
			}
		}
		if bottom != top && bottom >= y0 && bottom < y1 {
			for col := x0; col < x1; col++ {
				paint(col, bottom)
			}
		}
		for row := max(y0, top+1); row < min(y1, bottom); row++ {
			if left >= x0 && left < x1 {
				paint(left, row)
			}
			if right != left && right >= x0 && right < x1 {
				paint(right, row)
			}
		}
		if rail {
			return x + 2, y + 1, max(0, width-4), max(0, height-2)
		}
		return x + 1, y + 1, max(0, width-2), max(0, height-2)
	})
}

func init() {
	// A single border vocabulary keeps native tview widgets consistent.
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
	tview.Borders.TopLeftFocus = '╭'
	tview.Borders.TopRightFocus = '╮'
	tview.Borders.BottomLeftFocus = '╰'
	tview.Borders.BottomRightFocus = '╯'
	tview.Borders.HorizontalFocus = '─'
	tview.Borders.VerticalFocus = '│'
}
