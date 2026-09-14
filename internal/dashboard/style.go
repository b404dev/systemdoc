package dashboard

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/rivo/tview"
)

// blend is the one place colours are interpolated: gradients, surface tints,
// selection rows, frame lighting and panel hues all pass through it. It mixes
// in CIE Lab so a step of the gradient is a step in perceived colour, not in
// channel arithmetic, and clamps back into the sRGB gamut for the terminal.
func blend(a, b tcell.Color, amount float64) tcell.Color {
	// Lab mixing costs about a microsecond and paintSurfaces asks for it once
	// per cell per frame, so the answer is memoised per colour pair. Amount is
	// quantised to 256 steps: finer than a 24-bit terminal can show across any
	// gradient the interface draws, and it bounds the table.
	step := int(max(0, min(1, amount))*255 + 0.5)
	key := blendKey{a.Hex(), b.Hex()}
	blendCache.RLock()
	table, ok := blendCache.tables[key]
	blendCache.RUnlock()
	if !ok {
		table = &blendTable{}
		blendCache.Lock()
		if existing, found := blendCache.tables[key]; found {
			table = existing
		} else {
			blendCache.tables[key] = table
		}
		blendCache.Unlock()
	}
	if colour, ready := table.entry(step); ready {
		return colour
	}
	from, to := colorfulOf(a), colorfulOf(b)
	mixed := from.BlendLab(to, float64(step)/255).Clamped()
	r, g, bl := mixed.RGB255()
	colour := tcell.NewRGBColor(int32(r), int32(g), int32(bl))
	table.store(step, colour)
	return colour
}

type blendKey struct{ from, to int32 }

// blendTable fills lazily: a gradient only pays for the steps it uses.
type blendTable struct {
	mu     sync.RWMutex
	ready  [256]bool
	colour [256]tcell.Color
}

func (t *blendTable) entry(step int) (tcell.Color, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.colour[step], t.ready[step]
}

func (t *blendTable) store(step int, colour tcell.Color) {
	t.mu.Lock()
	t.colour[step], t.ready[step] = colour, true
	t.mu.Unlock()
}

var blendCache = struct {
	sync.RWMutex
	tables map[blendKey]*blendTable
}{tables: map[blendKey]*blendTable{}}

func colorfulOf(c tcell.Color) colorful.Color {
	r, g, b := c.RGB()
	return colorful.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}
}

// gradientText is for trusted, single-cell decorative glyphs only.
func gradientText(value, from, to string) string {
	runes := []rune(value)
	var out strings.Builder
	for i, r := range runes {
		colour := blend(tcell.GetColor(from), tcell.GetColor(to), float64(i)/float64(max(1, len(runes)-1)))
		fmt.Fprintf(&out, "[#%06x]%c", colour.Hex(), r)
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
func paintSurfaces(screen tcell.Screen, root *tview.Flex, p palette, header *tview.TextView, table *tview.Table, rails map[*tview.Box]string, flat bool) {
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
		sw, sh := screen.Size()
		strength := 0.025
		if rail {
			strength = 0.22
		} else if primitive.HasFocus() {
			strength = 0.09
		}
		// tcell.GetColor parses a "#rrggbb" string each call. Resolve the
		// palette once per panel, not once per cell: this loop runs for every
		// cell on the screen every frame.
		accent, glow := tcell.GetColor(p.accent), tcell.GetColor(p.glow)
		surface, background := tcell.GetColor(p.surface), tcell.GetColor(p.background)
		tint := tcell.GetColor(colour)
		for row := max(0, y); row < min(sh, y+height); row++ {
			for col := max(0, x); col < min(sw, x+width); col++ {
				if rail && (col == x || col == x+width-1 || row == y+height-1) {
					continue
				}
				r, combining, style, _ := screen.GetContent(col, row)
				_, bg, _ := style.Decompose()
				if primitive == table && bg == accent {
					position := float64(col-x-1) / float64(max(1, width-3))
					if flat {
						position = 0.5
					}
					selection := blend(accent, glow, 0.55*max(0, min(1, position)))
					if col == x+1 {
						r, combining = '▸', nil
					}
					screen.SetContent(col, row, r, combining, style.Background(selection))
					continue
				}
				// Preserve matches and explicit log backgrounds.
				if bg != surface && bg != background {
					continue
				}
				amount := strength * (1 - float64(col-x)/float64(max(1, width-1))) * (1 - 0.7*float64(row-y)/float64(max(1, height-1)))
				if flat {
					amount = strength * 0.5
				}
				if primitive == header {
					position := float64(col-x) / float64(max(1, width-1))
					if flat {
						position = 0.5
					}
					hue := blend(accent, glow, position)
					style = style.Background(blend(background, hue, 0.22))
					if height >= 3 && row == y+height-1 {
						r, combining = '━', nil
						style = style.Foreground(hue).Background(background)
					}
					screen.SetContent(col, row, r, combining, style)
					continue
				}
				screen.SetContent(col, row, r, combining, style.Background(blend(bg, tint, amount)))
			}
		}
	}
	visit(root)
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
		sw, sh := screen.Size()
		for row := max(0, y); row < min(sh, y+height); row++ {
			vertical := 1 - float64(row-y)/float64(max(1, height-1))
			for col := max(0, x); col < min(sw, x+width); col++ {
				if row != y && row != y+height-1 && col != x && col != x+width-1 {
					continue
				}
				horizontal := 1 - float64(col-x)/float64(max(1, width-1))
				if w.limitedColours {
					horizontal, vertical = 0.5, 0.5
				}
				r, combining, style, _ := screen.GetContent(col, row)
				style = style.Background(base)
				if rail && (row == y+height-1 || col == x || col == x+width-1) {
					screen.SetContent(col, row, ' ', nil, tcell.StyleDefault.Background(tcell.GetColor(p.background)))
					continue
				}
				if row == y || row == y+height-1 || col == x || col == x+width-1 {
					// Keep title lettering bright; only fade the frame itself.
					if strings.ContainsRune("╭╮╰╯─│", r) {
						intensity := 0.12 + 0.22*horizontal*vertical
						if focused != nil && focused() {
							intensity = 0.30 + 0.70*horizontal*vertical
						}
						if rail && row == y {
							r = '━'
							intensity = 0.25 + 0.75*horizontal
						}
						style = style.Foreground(blend(base, accent, intensity))
					}
				}
				screen.SetContent(col, row, r, combining, style)
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
