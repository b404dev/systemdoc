package dashboard

import (
	"fmt"
	"strings"
	"testing"
)

// syntheticProcesses is a process table the size of a busy host, with the
// parent links, states and CPU spread the rows and cards react to.
func syntheticProcesses(count int) []hostProcess {
	processes := make([]hostProcess, 0, count)
	states := []string{"S", "R", "S", "I", "Z"}
	for i := 0; i < count; i++ {
		processes = append(processes, hostProcess{
			PID: 100 + i, PPID: 1 + (i % 37), User: []string{"root", "www-data", "bill"}[i%3],
			State: states[i%len(states)], Elapsed: fmt.Sprintf("%02d:%02d:%02d", i%24, i%60, (i*7)%60),
			Command: fmt.Sprintf("/usr/bin/worker-%03d --queue %d", i, i%9), CPU: float64((i*13)%1000) / 10, RSS: int64(1024 * (i%512 + 1)),
		})
	}
	return processes
}

func benchHostPage(b *testing.B) *hostPage {
	w := testWorkspace()
	w.hostPage(3)
	_, page := w.pages.GetFrontPage()
	h := page.(*hostPage)
	h.processes = syntheticProcesses(500)
	h.width = 160
	b.ReportAllocs()
	return h
}

// BenchmarkProcessRows is the row model rebuilt on every filter keystroke,
// sort change and width change of the processes page.
func BenchmarkProcessRows(b *testing.B) {
	h := benchHostPage(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.processRows()
	}
}

// The selection band must still render every fact the eager version carried.
func TestLazyRichRowsRenderOnSelection(t *testing.T) {
	w := testWorkspace()
	w.hostPage(3)
	_, page := w.pages.GetFrontPage()
	h := page.(*hostPage)
	h.processes = syntheticProcesses(12)
	h.width = 160
	h.render()
	if len(h.rows) != 12 {
		t.Fatalf("rows = %d, want 12", len(h.rows))
	}
	h.selectRow(1)
	text := h.detail.GetText(true)
	row := h.rows[0]
	for _, want := range []string{"CPU", "MEMORY", "PID", row.cells[3], "parent"} {
		if !strings.Contains(text, want) {
			t.Errorf("selected process band lacks %q: %q", want, text)
		}
	}
	if row.rich == nil || row.detail == "" {
		t.Fatal("process rows must keep a lazy band and an eager detail for export")
	}
	if !strings.Contains(h.report(), "PID "+row.cells[3]) {
		t.Fatal("export lost the per-row detail")
	}
}
