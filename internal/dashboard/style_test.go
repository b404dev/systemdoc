package dashboard

import (
	"context"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestNetworkSharesDashboardChromeAndSurfaces(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	for _, theme := range []int{0, 1, len(themes) - 1} {
		w.theme = theme
		w.applyTheme()
		for _, size := range [][2]int{{160, 44}, {120, 30}, {80, 24}, {40, 12}, {120, 30}} {
			screen.SetSize(size[0], size[1])
			w.app.ForceDraw()
			_, _, _, mainHeaderHeight := w.header.GetRect()
			if size[0] >= 100 && size[1] >= 28 && mainHeaderHeight != 3 {
				t.Fatalf("wide dashboard masthead height = %d, want 3", mainHeaderHeight)
			}
			backgrounds := make([]tcell.Color, 0, size[0])
			for x := 0; x < size[0]; x++ {
				_, _, style, _ := screen.GetContent(x, 0)
				_, bg, _ := style.Decompose()
				backgrounds = append(backgrounds, bg)
			}
			w.networkPage()
			_, page := w.pages.GetFrontPage()
			n := page.(*networkPage)
			n.ctx, n.cancel = context.WithCancel(context.Background())
			n.snapshot = networkSnapshot{Sockets: mustSS(t)}
			n.render()
			w.app.ForceDraw()
			_, _, _, networkHeaderHeight := n.header.GetRect()
			if !strings.Contains(n.header.GetText(true), "SYSTEMDOC") || n.header == w.header {
				t.Fatal("network must keep the application branding with independent widget geometry")
			}
			var navigation strings.Builder
			for x := 0; x < size[0]; x++ {
				r, _, _, _ := screen.GetContent(x, networkHeaderHeight)
				navigation.WriteRune(r)
			}
			if strings.Contains(navigation.String(), "Active only") || strings.Contains(navigation.String(), "Actions") {
				t.Fatal("underlying toolbar leaked through the network navigation")
			}
			for x := 0; x < size[0]; x++ {
				_, _, style, _ := screen.GetContent(x, 0)
				_, bg, _ := style.Decompose()
				if bg != backgrounds[x] {
					t.Fatalf("header fill differs at %d,0 (theme %d, size %v)", x, theme, size)
				}
			}
			if size[1] >= 28 {
				card := n.cards[0]
				x, y, width, _ := card.GetRect()
				_, _, left, _ := screen.GetContent(x+2, y+1)
				_, _, right, _ := screen.GetContent(x+width-3, y+1)
				_, leftBG, _ := left.Decompose()
				_, rightBG, _ := right.Decompose()
				if leftBG == rightBG {
					t.Fatal("network card gradient fill missing")
				}
				r, _, _, _ := screen.GetContent(x, y+1)
				if r != ' ' {
					t.Fatal("network card should use the dashboard's open sides")
				}
			}
			w.networkPage()
			if _, current := w.pages.GetFrontPage(); current != n {
				t.Fatal("reselecting Network replaced the live page")
			}
			n.modeButtons[0].InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
			if front, _ := w.pages.GetFrontPage(); front != "main" || n.ctx.Err() == nil {
				t.Fatal("Services button must close network and cancel its refresh loop")
			}
		}
	}
}

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

func TestBlendInterpolatesPerceptuallyAndStaysInGamut(t *testing.T) {
	a, b := tcell.GetColor("#64ddea"), tcell.GetColor("#a889e8")
	if got := blend(a, b, 0); got.Hex() != a.Hex() {
		t.Fatalf("blend at 0 = %06x, want the start colour", got.Hex())
	}
	if got := blend(a, b, 1); got.Hex() != b.Hex() {
		t.Fatalf("blend at 1 = %06x, want the end colour", got.Hex())
	}
	// Out-of-range amounts clamp to the endpoints rather than extrapolating.
	if blend(a, b, -3).Hex() != a.Hex() || blend(a, b, 7).Hex() != b.Hex() {
		t.Fatal("blend must clamp amount into [0, 1]")
	}
	// Every channel stays a legal 8-bit value after Lab mixing and clamping.
	for i := 0; i <= 20; i++ {
		r, g, bl := blend(a, b, float64(i)/20).RGB()
		for _, channel := range []int32{r, g, bl} {
			if channel < 0 || channel > 255 {
				t.Fatalf("step %d produced channel %d outside sRGB", i, channel)
			}
		}
	}
	// Lab mixing must not wander: the midpoint stays between the endpoints in
	// lightness, which is what makes a gradient read as one smooth ramp.
	mid := colorfulOf(blend(a, b, 0.5))
	la, _, _ := colorfulOf(a).Lab()
	lb, _, _ := colorfulOf(b).Lab()
	lm, _, _ := mid.Lab()
	if lm < min(la, lb)-0.01 || lm > max(la, lb)+0.01 {
		t.Fatalf("midpoint lightness %.3f is outside the endpoints %.3f..%.3f", lm, la, lb)
	}
}
