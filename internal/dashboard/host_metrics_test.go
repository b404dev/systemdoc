package dashboard

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParseProcStatSplitsBusyFromIdle(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	sample := parseProcStat("cpu  100 20 30 700 50 5 5 10\ncpu0 10 2 3 70 5 0 0 1\nintr 12345\n")
	if !sample.ok {
		t.Fatal("expected the aggregate cpu line to parse")
	}
	if sample.total != 920 {
		t.Fatalf("total = %d, want 920", sample.total)
	}
	// idle 700 + iowait 50 are not busy; everything else is.
	if sample.busy != 170 {
		t.Fatalf("busy = %d, want 170", sample.busy)
	}
}

func TestParseProcStatRejectsUnusableInput(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":       "",
		"no cpu line": "intr 1\nctxt 2\n",
		"short":       "cpu 1 2\n",
		"non numeric": "cpu  1 2 3 four 5\n",
		"all zero":    "cpu  0 0 0 0 0\n",
	} {
		if parseProcStat(raw).ok {
			t.Errorf("%s: expected no usable sample", name)
		}
	}
}

func TestCPUPercentNeedsTwoAdvancingSamples(t *testing.T) {
	first := parseProcStat("cpu  100 0 0 900 0\n")
	second := parseProcStat("cpu  150 0 0 1850 0\n")
	// 50 busy ticks of 1000 elapsed.
	value, ok := cpuPercent(first, second)
	if !ok {
		t.Fatal("expected a reading from two advancing samples")
	}
	if value < 4.9 || value > 5.1 {
		t.Fatalf("percent = %v, want about 5", value)
	}
	if _, ok := cpuPercent(first, first); ok {
		t.Error("an identical pair must not report 0%%")
	}
	if _, ok := cpuPercent(cpuTimes{}, second); ok {
		t.Error("a missing baseline must not report a percentage")
	}
	// A counter reset must not produce a reading from negative deltas.
	if _, ok := cpuPercent(second, first); ok {
		t.Error("a reset counter must not report a percentage")
	}
}

func TestParseMeminfoPrefersMemAvailable(t *testing.T) {
	used, total, ok := parseMeminfo("MemTotal:       16000 kB\nMemFree:         1000 kB\nMemAvailable:    6000 kB\nBuffers:          500 kB\n")
	if !ok {
		t.Fatal("expected meminfo to parse")
	}
	if total != 16000*1024 {
		t.Fatalf("total = %d", total)
	}
	// Used is total minus available, not total minus free.
	if used != 10000*1024 {
		t.Fatalf("used = %d, want %d", used, 10000*1024)
	}
}

func TestParseMeminfoFallsBackWithoutMemAvailable(t *testing.T) {
	used, total, ok := parseMeminfo("MemTotal:       1000 kB\nMemFree:         100 kB\nBuffers:          50 kB\nCached:          150 kB\n")
	if !ok {
		t.Fatal("expected meminfo to parse")
	}
	if used != 700*1024 || total != 1000*1024 {
		t.Fatalf("used = %d total = %d", used, total)
	}
	if _, _, ok := parseMeminfo("MemFree: 100 kB\n"); ok {
		t.Error("meminfo without MemTotal must not report a reading")
	}
}

func TestParseVMStatCountsResidentPages(t *testing.T) {
	raw := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:                    100.\nPages active:                  200.\nPages inactive:                300.\nPages speculative:              50.\nPages wired down:              400.\nPages occupied by compressor:  100.\n"
	used, ok := parseVMStat(raw)
	if !ok {
		t.Fatal("expected vm_stat to parse")
	}
	// active 200 + wired 400 + compressed 100, at the reported page size.
	if want := int64(700 * 16384); used != want {
		t.Fatalf("used = %d, want %d", used, want)
	}
	if _, ok := parseVMStat("Mach Virtual Memory Statistics:\n"); ok {
		t.Error("vm_stat without page counts must not report a reading")
	}
}

func TestParseProcessCPUDividesBySpanOfCores(t *testing.T) {
	value, ok := parseProcessCPU(" 50.0\n30.0\n 20.0\n\n", 4)
	if !ok {
		t.Fatal("expected process CPU to parse")
	}
	if value < 24.9 || value > 25.1 {
		t.Fatalf("percent = %v, want about 25", value)
	}
	// A machine reporting more than its cores is clamped, never above 100.
	high, _ := parseProcessCPU("900\n", 2)
	if high != 100 {
		t.Fatalf("percent = %v, want 100", high)
	}
	if _, ok := parseProcessCPU("10\n", 0); ok {
		t.Error("an unknown core count must not report a percentage")
	}
	if _, ok := parseProcessCPU("\n\n", 4); ok {
		t.Error("no process rows must not report 0%%")
	}
}

func TestHostHeadlinesKeepMissingReadingsVisible(t *testing.T) {
	usage := hostUtilisation{cpuPercent: 42.4, cpuOK: true, memPercent: 61.2, memUsed: 10 << 30, memTotal: 16 << 30, memOK: true}
	if got, want := hostCPUHeadline(usage, "38.4%"), "42% · 38.4% tracked"; got != want {
		t.Errorf("cpu headline = %q, want %q", got, want)
	}
	if got, want := hostMemoryHeadline(usage, "1.1 GiB"), "61% · 10.0/16.0 GiB"; got != want {
		t.Errorf("memory headline = %q, want %q", got, want)
	}
	// An unavailable host reading must not borrow the tracked workload sum.
	missing := hostUtilisation{}
	if got, want := hostCPUHeadline(missing, "38.4%"), "— · 38.4% tracked"; got != want {
		t.Errorf("cpu headline = %q, want %q", got, want)
	}
	if got, want := hostMemoryHeadline(missing, "1.1 GiB"), "— · 1.1 GiB tracked"; got != want {
		t.Errorf("memory headline = %q, want %q", got, want)
	}
}

func TestRecordHostUtilisationStoresGapsForMissingReadings(t *testing.T) {
	w := &workspace{}
	w.recordHostUtilisation(hostUtilisation{cpuPercent: 10, cpuOK: true, memPercent: 20, memOK: true})
	w.recordHostUtilisation(hostUtilisation{})
	if got := w.hostCPUHistory; len(got) != 2 || got[0] != 10 || got[1] != -1 {
		t.Fatalf("cpu history = %v, want [10 -1]", got)
	}
	if got := w.hostMemoryHistory; len(got) != 2 || got[0] != 20 || got[1] != -1 {
		t.Fatalf("memory history = %v, want [20 -1]", got)
	}
	for i := 0; i < 200; i++ {
		w.recordHostUtilisation(hostUtilisation{cpuPercent: 1, cpuOK: true})
	}
	if len(w.hostCPUHistory) != 64 {
		t.Fatalf("history length = %d, want the 64 sample bound", len(w.hostCPUHistory))
	}
}

func TestControlButtonWidthLeavesASeparatingGap(t *testing.T) {
	// Adjacent rail buttons must never merge into one another's labels.
	for _, label := range []string{"z  Expand", "a  Actions", "0 DECK Deck"} {
		if got := controlButtonWidth(label); got <= len([]rune(label)) {
			t.Errorf("width for %q = %d, want more than the label itself", label, got)
		}
	}
}

func TestHostPanelsExposeTheirOwnViews(t *testing.T) {
	w := testWorkspace()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	labels := map[int][]string{}
	for _, tab := range []int{3, 4} {
		w.hostPage(tab)
		_, page := w.pages.GetFrontPage()
		h := page.(*hostPage)
		labels[tab] = h.viewLabels()
		if h.currentView() != 0 {
			t.Fatalf("tab %d: expected the first view to be selected", tab)
		}
		// The rail must select a view directly, not merely toggle blindly.
		h.selectView(1)
		if h.currentView() != 1 {
			t.Fatalf("tab %d: selecting the second view did not take effect", tab)
		}
		h.selectView(1)
		if h.currentView() != 1 {
			t.Fatalf("tab %d: re-selecting the current view must not toggle it back", tab)
		}
		h.selectView(0)
		if h.currentView() != 0 {
			t.Fatalf("tab %d: selecting the first view did not take effect", tab)
		}
		h.close()
	}
	// Each suite names its own views; the two panels are not the same tool.
	if labels[3][0] == labels[4][0] || labels[3][1] == labels[4][1] {
		t.Errorf("processes and storage share view labels: %v / %v", labels[3], labels[4])
	}
}

func TestHostReadoutHidesOnNarrowTerminals(t *testing.T) {
	p := themes[0]
	usage := hostUtilisation{cpuPercent: 40, cpuOK: true, memPercent: 50, memOK: true}
	if got := hostReadoutFor(p, usage, 80); got != "" {
		t.Errorf("80 columns should omit the readout, got %q", got)
	}
	if got := hostReadoutFor(p, usage, 160); !strings.Contains(got, "CPU 40%") || !strings.Contains(got, "MEM 50%") {
		t.Errorf("160 columns should carry the readout, got %q", got)
	}
}
