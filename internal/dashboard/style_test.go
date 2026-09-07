package dashboard

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestIlluminationPreservesSelectionAndOverlays(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	for theme := range themes {
		w.theme = theme
		w.applyTheme()
		for _, size := range [][2]int{{160, 44}, {120, 30}, {80, 24}, {30, 12}} {
			screen.SetSize(size[0], size[1])
			w.app.ForceDraw()
			p := w.palette()
			_, _, left, _ := screen.GetContent(0, 0)
			_, _, right, _ := screen.GetContent(size[0]-1, 0)
			_, leftBG, _ := left.Decompose()
			_, rightBG, _ := right.Decompose()
			if leftBG == rightBG {
				t.Fatal("header gradient missing", theme, size)
			}
			x, y, _, _ := w.table.GetInnerRect()
			row, _ := w.table.GetSelection()
			_, _, selected, _ := screen.GetContent(x, y+row)
			_, selectedBG, _ := selected.Decompose()
			if selectedBG != tcell.GetColor(p.accent) {
				t.Fatal("illumination changed selection", theme, size, selectedBG)
			}
		}
	}
	overlay := tview.NewBox().SetBackgroundColor(tcell.ColorRed)
	w.pages.AddPage("test-overlay", overlay, true, true)
	w.app.ForceDraw()
	_, _, style, _ := screen.GetContent(0, 0)
	_, bg, _ := style.Decompose()
	if bg != tcell.ColorRed {
		t.Fatal("dashboard painted over overlay")
	}
}
