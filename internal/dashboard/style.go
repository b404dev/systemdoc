package dashboard

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func blend(a, b tcell.Color, amount float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	return tcell.NewRGBColor(int32(float64(ar)+float64(br-ar)*amount), int32(float64(ag)+float64(bg-ag)*amount), int32(float64(ab)+float64(bb-ab)*amount))
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
	var visit func(tview.Primitive)
	visit = func(primitive tview.Primitive) {
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
		if box.GetDrawFunc() == nil && primitive != d.w.header {
			return
		}
		colour := p.accent
		for i, card := range d.w.cards {
			if primitive == card {
				colour = []string{p.success, p.error, p.accent, p.warning}[i]
			}
		}
		x, y, width, height := box.GetRect()
		sw, sh := screen.Size()
		strength := 0.025
		if d.w.railPanel(box) {
			strength = 0.22
		} else if primitive.HasFocus() {
			strength = 0.09
		}
		for row := max(0, y); row < min(sh, y+height); row++ {
			for col := max(0, x); col < min(sw, x+width); col++ {
				if d.w.railPanel(box) && (col == x || col == x+width-1 || row == y+height-1) {
					continue
				}
				r, combining, style, _ := screen.GetContent(col, row)
				_, bg, _ := style.Decompose()
				if primitive == d.w.table && bg == tcell.GetColor(p.accent) {
					position := float64(col-x-1) / float64(max(1, width-3))
					selection := blend(tcell.GetColor(p.accent), tcell.GetColor(p.glow), 0.55*max(0, min(1, position)))
					if col == x+1 {
						r, combining = '▸', nil
					}
					screen.SetContent(col, row, r, combining, style.Background(selection))
					continue
				}
				// Preserve matches and explicit log backgrounds.
				if bg != tcell.GetColor(p.surface) && bg != tcell.GetColor(p.background) {
					continue
				}
				amount := strength * (1 - float64(col-x)/float64(max(1, width-1))) * (1 - 0.7*float64(row-y)/float64(max(1, height-1)))
				if primitive == d.w.header {
					position := float64(col-x) / float64(max(1, width-1))
					hue := blend(tcell.GetColor(p.accent), tcell.GetColor(p.glow), position)
					style = style.Background(blend(tcell.GetColor(p.background), hue, 0.22))
					if height >= 3 && row == y+height-1 {
						r, combining = '━', nil
						style = style.Foreground(hue).Background(tcell.GetColor(p.background))
					}
					screen.SetContent(col, row, r, combining, style)
					continue
				}
				screen.SetContent(col, row, r, combining, style.Background(blend(bg, tcell.GetColor(colour), amount)))
			}
		}
	}
	visit(d.Flex)
}

// Light lives inside each panel's bounds, so adjacent panes and overlays never
// get painted over. Native tview still owns titles, content, and hit testing.
func (w *workspace) illuminate(box *tview.Box, focused func() bool, colour func(palette) string) {
	box.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		p := w.palette()
		base, accent := tcell.GetColor(p.surface), tcell.GetColor(colour(p))
		rail := w.railPanel(box)
		sw, sh := screen.Size()
		for row := max(0, y); row < min(sh, y+height); row++ {
			vertical := 1 - float64(row-y)/float64(max(1, height-1))
			for col := max(0, x); col < min(sw, x+width); col++ {
				if row != y && row != y+height-1 && col != x && col != x+width-1 {
					continue
				}
				horizontal := 1 - float64(col-x)/float64(max(1, width-1))
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
