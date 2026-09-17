package dashboard

import (
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// legacyPaintSurfaces is the per-cell formulation paintSurfaces was optimised
// from, kept verbatim as the reference: every cell computed its own gradient
// factors and went through blend(). The optimised version must produce the
// identical frame, so this test paints the same widget tree both ways and
// compares every cell.
func legacyPaintSurfaces(screen tcell.Screen, root *tview.Flex, p palette, header *tview.TextView, table *tview.Table, rails map[*tview.Box]string, flat bool) {
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

func TestPaintSurfacesMatchesLegacyPerCellFormula(t *testing.T) {
	for _, flat := range []bool{false, true} {
		for _, size := range [][2]int{{160, 44}, {120, 30}} {
			for _, page := range []string{"main", "storage"} {
				w := testWorkspace()
				w.limitedColours = flat
				screen := tcell.NewSimulationScreen("UTF-8")
				w.app.SetScreen(screen)
				screen.SetSize(size[0], size[1])
				p := w.palette()
				var root *tview.Flex
				var header *tview.TextView
				var table *tview.Table
				rails := map[*tview.Box]string{}
				if page == "storage" {
					w.hostPage(4)
					_, front := w.pages.GetFrontPage()
					h := front.(*hostPage)
					h.mounts, _ = parseMounts(macDiskFixture, "darwin", false)
					h.render()
					w.app.ForceDraw()
					root, header, table = h.Flex, h.header, h.table
					for i, card := range h.cards {
						rails[card.Box] = []string{p.accent, p.warning, p.success}[i]
					}
				} else {
					w.app.ForceDraw()
					root, header, table = w.root, w.header, w.table
					rails[w.selectionCard.Box] = p.accent
					for i, card := range w.cards {
						rails[card.Box] = []string{p.success, p.error, p.accent, p.glow}[i]
					}
				}
				reference := tcell.NewSimulationScreen("UTF-8")
				if err := reference.Init(); err != nil {
					t.Fatal(err)
				}
				reference.SetSize(size[0], size[1])
				root.Draw(reference)
				legacyPaintSurfaces(reference, root, p, header, table, rails, flat)
				mismatches := 0
				for y := 0; y < size[1]; y++ {
					for x := 0; x < size[0]; x++ {
						gotRune, gotComb, gotStyle, _ := screen.GetContent(x, y)
						wantRune, wantComb, wantStyle, _ := reference.GetContent(x, y)
						if gotRune != wantRune || gotStyle != wantStyle || !reflect.DeepEqual(gotComb, wantComb) {
							if mismatches < 5 {
								_, gotBG, _ := gotStyle.Decompose()
								_, wantBG, _ := wantStyle.Decompose()
								t.Errorf("%s %dx%d flat=%v cell %d,%d: got %q bg %06x, want %q bg %06x", page, size[0], size[1], flat, x, y, gotRune, gotBG.Hex(), wantRune, wantBG.Hex())
							}
							mismatches++
						}
					}
				}
				if mismatches > 0 {
					t.Errorf("%s %dx%d flat=%v: %d cells differ from the legacy formula", page, size[0], size[1], flat, mismatches)
				}
				reference.Fini()
				screen.Fini()
			}
		}
	}
}
