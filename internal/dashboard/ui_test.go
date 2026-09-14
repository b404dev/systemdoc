package dashboard

import (
	"context"
	"github.com/gdamore/tcell/v2"
	"strings"
	"testing"
	"time"
)

func TestApplicationDrawsFirstFrameAndQuits(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	screen.SetSize(120, 30)
	frames := make(chan string, 1)
	w.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		var rendered strings.Builder
		width, height := screen.Size()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, _, _, _ := screen.GetContent(x, y)
				rendered.WriteRune(r)
			}
		}
		select {
		case frames <- rendered.String():
		default:
		}
	})
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	select {
	case frame := <-frames:
		if !strings.Contains(frame, "SYSTEMDOC") || !strings.Contains(frame, "nginx.service") {
			t.Error("first application frame is missing essential content")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("application blocked before drawing its first frame")
	}
	if err := screen.PostEvent(tcell.NewEventKey(tcell.KeyRune, 'q', 0)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("application did not respond to quit")
	}
}

func testWorkspace() *workspace {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := newWorkspace(ctx, false)
	// The simulation screen reports 256 colours; keep the 24-bit paint path
	// under test and benchmark, since that is what real terminals run.
	w.colourDepthKnown = true
	w.items[0] = []workload{{ID: "nginx.service", Name: "nginx.service", State: "active"}, {ID: "worker.service", Name: "worker.service", State: "failed"}}
	w.renderTable()
	return w
}
func TestFilteringAndSelection(t *testing.T) {
	w := testWorkspace()
	w.search.SetText("state:failed")
	if len(w.visible) != 1 || w.selected[0] != "worker.service" {
		t.Fatal("failed filter did not select worker")
	}
	w.search.SetText("")
	if w.selected[0] != "worker.service" {
		t.Fatal("selection identity was lost")
	}
}
func TestTextEntryDoesNotTriggerModeSwitch(t *testing.T) {
	w := testWorkspace()
	w.app.SetFocus(w.search)
	e := tcell.NewEventKey(tcell.KeyRune, '2', 0)
	if w.input(e) != e || w.mode != 0 {
		t.Fatal("global shortcut intercepted input")
	}
}
func TestLayoutDrawsAtSupportedSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		w := testWorkspace()
		screen := tcell.NewSimulationScreen("UTF-8")
		if err := screen.Init(); err != nil {
			t.Fatal(err)
		}
		screen.SetSize(size[0], size[1])
		w.root.SetRect(0, 0, size[0], size[1])
		w.root.Draw(screen)
		var rendered strings.Builder
		for y := 0; y < size[1]; y++ {
			for x := 0; x < size[0]; x++ {
				r, _, _, _ := screen.GetContent(x, y)
				rendered.WriteRune(r)
			}
		}
		if !strings.Contains(rendered.String(), "SYSTEMDOC") || !strings.Contains(rendered.String(), "nginx.service") {
			t.Fatal("essential content missing", size)
		}
		screen.Fini()
	}
}
