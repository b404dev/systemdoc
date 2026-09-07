package dashboard

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"strings"
	"testing"
	"time"
)

func BenchmarkCachedModeSwitch(b *testing.B) {
	w := testWorkspace()
	w.items[0] = nil
	for i := 0; i < 1000; i++ {
		w.items[0] = append(w.items[0], workload{ID: fmt.Sprintf("unit-%04d.service", i), Name: fmt.Sprintf("unit-%04d.service", i), State: "active"})
	}
	w.items[1] = []workload{{ID: "container", Name: "container", State: "running"}}
	w.lastRefresh = [2]time.Time{time.Now(), time.Now()}
	w.settings.RefreshSeconds = 300
	w.renderTable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.switchMode(1 - w.mode)
	}
}

func BenchmarkLogFormatting(b *testing.B) {
	text := strings.Repeat("2026-09-05T12:00:00 INFO worker processed another task successfully\n", 16000)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		richOutput(text, 1, themes[0])
	}
}

func BenchmarkPreparedLogDraw(b *testing.B) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(140, 40)
	text := strings.Repeat("2026-09-05T12:00:00 INFO worker processed another task successfully\n", 16000)
	prepared := prepareLogSnapshot(text, "", logStyle{palette: themes[0]})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.detail.SetText(prepared.formatted).ScrollToEnd()
		w.app.ForceDraw()
	}
}
