package dashboard

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestLoadingIndicatorKeepsFooterStableAndTracksCurrentJob(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	w.app.ForceDraw()
	x, y, width, height := w.loadingIndicator.GetRect()
	if width != 2 || height != 1 || y != 29 || x != 118 {
		t.Fatal("loading indicator is not confined to footer", x, y, width, height)
	}
	_, bodyY, _, bodyHeight := w.body.GetRect()
	_, storylineY, _, storylineHeight := w.storyline.GetRect()
	if bodyY+bodyHeight != storylineY || storylineHeight != 1 || storylineY+storylineHeight != y {
		t.Fatal("storyline and footer do not pack directly below workload panes")
	}
	w.inventoryJobs[1] = &inventoryJob{}
	w.updateDashboard()
	if w.loadingAnimation.Load() || w.loadingIndicator.GetText(true) != "" {
		t.Fatal("background backend animated the visible footer")
	}
	w.inventoryJobs[0] = &inventoryJob{}
	w.loading = false // Identity is published, but resource enrichment is still running.
	w.updateDashboard()
	if !w.loadingAnimation.Load() || w.loadingIndicator.GetText(true) == "" {
		t.Fatal("resource enrichment did not keep loading indicator active")
	}
	w.app.ForceDraw()
	x2, y2, width2, height2 := w.loadingIndicator.GetRect()
	if x2 != x || y2 != y || width2 != width || height2 != height {
		t.Fatal("refresh shifted footer geometry")
	}
	w.inventoryJobs[0] = nil
	w.updateDashboard()
	if w.loadingAnimation.Load() || w.loadingIndicator.GetText(true) != "" {
		t.Fatal("finished refresh kept animating")
	}
}
