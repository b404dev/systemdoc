package dashboard

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func screenRow(screen tcell.SimulationScreen, y, width int) string {
	var b strings.Builder
	for x := 0; x < width; x++ {
		r, _, _, _ := screen.GetContent(x, y)
		b.WriteRune(r)
	}
	return b.String()
}

// Every page draws the same masthead: identical first line wording, the same
// height rule, and the same gradient rule beneath it, so moving between suites
// never shifts the top of the screen.
func TestMastheadIsUniformAcrossPages(t *testing.T) {
	for _, size := range [][2]int{{160, 44}, {80, 24}} {
		w := testWorkspace()
		screen := tcell.NewSimulationScreen("UTF-8")
		w.app.SetScreen(screen)
		screen.SetSize(size[0], size[1])
		rows := map[string][]string{}
		capture := func(name string) {
			w.app.ForceDraw()
			rows[name] = []string{screenRow(screen, 0, size[0]), screenRow(screen, 1, size[0]), screenRow(screen, 2, size[0])}
		}
		capture("main")
		w.networkPage()
		capture("network")
		w.pages.RemovePage("network")
		for _, tab := range []int{3, 4} {
			w.hostPage(tab)
			_, page := w.pages.GetFrontPage()
			capture(page.(*hostPage).name)
			page.(*hostPage).close()
		}
		screen.Fini()
		want := mastheadHeight(size[0], size[1])
		for name, got := range rows {
			if !strings.Contains(got[0], "// SYSTEM OBSERVATORY") {
				t.Errorf("%dx%d %s: first line lacks the shared wording: %q", size[0], size[1], name, strings.TrimSpace(got[0]))
			}
			ruleRow := strings.TrimSpace(got[2])
			isRule := ruleRow != "" && strings.Trim(ruleRow, "━") == ""
			if (want == 3) != isRule {
				t.Errorf("%dx%d %s: masthead rule present=%v, want %v", size[0], size[1], name, isRule, want == 3)
			}
			if want == 3 && !strings.Contains(got[1], "HOST") {
				t.Errorf("%dx%d %s: second line lacks the host readout: %q", size[0], size[1], name, strings.TrimSpace(got[1]))
			}
		}
	}
}
