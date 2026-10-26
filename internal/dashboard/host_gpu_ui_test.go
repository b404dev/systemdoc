package dashboard

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"systemdoc/internal/hostgpu"
)

func TestDashboardGPUCardVisibleWithoutShortcut(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	w.updateDashboard()
	w.app.ForceDraw()
	card := w.cards[4]
	x, y, width, height := card.GetRect()
	if width < 20 || height != 5 {
		t.Fatalf("GPU card has no usable dashboard space: %dx%d", width, height)
	}
	if !strings.Contains(card.GetText(true), "Usage unavailable") {
		t.Fatal("missing driver should remain visible on the dashboard")
	}
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{{Name: "Example [red]GPU", Busy: 10, MemoryUsed: 512 * 1024 * 1024, MemoryTotal: 8 * 1024 * 1024 * 1024, Temperature: 63}}})
	gpu := w.hostUsage.gpus[0]
	gpu.Busy = 42
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{gpu}})
	w.updateDashboard()
	w.app.ForceDraw()
	var visible strings.Builder
	for row := y + 1; row < y+height-1; row++ {
		for col := x + 1; col < x+width-1; col++ {
			r, _, _, _ := screen.GetContent(col, row)
			visible.WriteRune(r)
		}
		visible.WriteByte('\n')
	}
	for _, want := range []string{"42%", "512M/8G", "63°C"} {
		if !strings.Contains(visible.String(), want) {
			t.Fatalf("GPU metric %q not visible in dashboard card:\n%s", want, visible.String())
		}
	}
	for _, mode := range []string{"blocks", "braille", "ascii"} {
		w.settings.GraphMode = mode
		w.updateDashboard()
		lines := strings.Split(card.GetText(true), "\n")
		want := signalArea([]float64{10, 42}, 100, mode)
		if len(lines) != 3 || strings.Join(lines[1:], "\n") != want {
			t.Fatalf("%s GPU history differs from CPU/memory renderer: %q, want %q", mode, lines, want)
		}
	}
	card.MouseHandler()(tview.MouseLeftClick, tcell.NewEventMouse(x+2, y+1, tcell.ButtonNone, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if w.pages.HasPage("host-gpus") {
		t.Fatal("GPU panel click should not open a detail view")
	}
	if strings.Contains(strings.ToLower(card.GetTitle()+card.GetText(true)), "click") {
		t.Fatal("GPU panel should have no click prompt")
	}
	_, bodyY, _, _ := w.body.GetRect()
	for count := 2; count <= 4; count++ {
		devices := make([]hostgpu.Device, count)
		for i := range devices {
			devices[i] = w.hostUsage.gpus[0]
		}
		w.recordHostUtilisation(hostUtilisation{gpus: devices})
		w.updateDashboard()
		w.app.ForceDraw()
		_, _, _, gpuHeight := card.GetRect()
		_, nextBodyY, _, _ := w.body.GetRect()
		if gpuHeight != 5 || nextBodyY != bodyY {
			t.Fatalf("%d GPUs changed dashboard geometry: card height %d, body y %d (was %d)", count, gpuHeight, nextBodyY, bodyY)
		}
		for index, line := range strings.Split(card.GetText(true), "\n") {
			if tview.TaggedStringWidth(line) > width-2 {
				t.Fatalf("GPU row overflows card: %q", line)
			}
			if index%3 == 0 && !strings.Contains(line, "42% 512M/8G 63°C") {
				t.Fatalf("GPU row missing compact readings: %q", line)
			}
		}
	}
}

func TestGPUHistoryKeepsDevicesSeparateAndMissingSamplesAsGaps(t *testing.T) {
	w := &workspace{}
	a := hostgpu.Device{ID: "nvidia:0", Name: "Same model", Busy: 10}
	b := hostgpu.Device{ID: "nvidia:1", Name: "Same model", Busy: 60}
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{a, b}})
	w.recordHostUtilisation(hostUtilisation{})
	a.Busy, b.Busy = 20, -1
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{b, a}})
	for key, want := range map[string][]float64{gpuHistoryKey(a, 0): {10, -1, 20}, gpuHistoryKey(b, 1): {60, -1, -1}} {
		got := w.hostGPUHistory[key]
		if len(got) != len(want) {
			t.Fatalf("history %q: %v", key, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("history %q: %v, want %v", key, got, want)
			}
		}
	}
	for i := 0; i < 100; i++ {
		w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{a}})
	}
	if len(w.hostGPUHistory[gpuHistoryKey(a, 0)]) != 64 || len(w.hostGPUHistory) != 1 {
		t.Fatal("GPU histories must stay bounded and drop removed devices")
	}
}

func TestProcessExplorerShowsGPUCardAndProcessReadings(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(120, 30)
	device := hostgpu.Device{ID: "drm:card0", Name: "GPU", Busy: 10, MemoryUsed: 1 << 30, MemoryTotal: 8 << 30, Temperature: -1}
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{device}})
	device.Busy = 60
	w.recordHostUtilisation(hostUtilisation{gpus: []hostgpu.Device{device}})
	w.hostPage(3)
	_, page := w.pages.GetFrontPage()
	h := page.(*hostPage)
	defer h.close()
	h.processes = []hostProcess{{PID: 42, Command: "renderer", CPU: 1, RSS: 100, LastCPU: -1}, {PID: 43, Command: "unknown", CPU: 0, LastCPU: -1}}
	h.gpuProcesses = map[int]hostgpu.Process{42: {Busy: 35, MemoryUsed: 512 << 20}}
	h.render()
	w.app.ForceDraw()
	if h.gpuCard == nil || h.summary.GetItemCount() != 4 {
		t.Fatal("Process Explorer must show the GPU card alongside CPU and memory")
	}
	_, _, _, height := h.gpuCard.GetRect()
	if height != 5 {
		t.Fatalf("GPU card changed process panel height: %d", height)
	}
	trend := signalArea([]float64{10, 60}, 100, w.settings.GraphMode)
	if !strings.HasSuffix(h.gpuCard.GetText(true), trend) {
		t.Fatalf("Process Explorer missing GPU trend: %q", h.gpuCard.GetText(true))
	}
	if h.table.GetCell(0, 4).Text != "GPU" || h.table.GetCell(0, 5).Text != "GPU MEM" {
		t.Fatal("missing GPU process columns")
	}
	for _, row := range h.rows {
		if row.pid == 42 && (row.cells[4] != "35%" || row.cells[5] != "512.0 MiB" || !strings.Contains(row.rich(), "GPU MEMORY") || !strings.Contains(row.detail, "GPU 35%")) {
			t.Fatalf("GPU process readings missing from table, selection or export: %+v", row)
		}
		if row.pid == 43 && (row.cells[4] != "—" || row.cells[5] != "—") {
			t.Fatal("missing GPU process readings must not become zero")
		}
	}
}
