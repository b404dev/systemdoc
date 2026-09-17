package dashboard

import (
	"strings"
	"testing"
	"time"
)

func fakeCoresUsage() hostUtilisation {
	return hostUtilisation{
		cpuPercent: 40, cpuOK: true, load1: 2.5, load5: 2, load15: 1.5, loadOK: true,
		cpus:      cpuInventory{logical: 4, cores: 2, sockets: 1, model: "Fake CPU", ok: true, coreOf: []int{0, 0, 1, 1}, socketOf: []int{0, 0, 0, 0}},
		clocks:    cpuClocks{mhz: []float64{3400, 3400, 800, 800}, source: "cpufreq", ok: true},
		coreBusy:  []float64{95, 10, 0, -1},
		coreSplit: []coreLoad{{user: 80, system: 15, ok: true}, {user: 5, system: 5, iowait: 30, ok: true}, {steal: 12, ok: true}, {}},
	}
}

func TestRenderCPUCoresListsEveryCPUWithItsSplitAndProcess(t *testing.T) {
	p := themes[0]
	usage := fakeCoresUsage()
	history := [][]float64{{50, 95}, {10, 10}, {0, 0}, {-1, -1}}
	processes := []hostProcess{
		{PID: 42, Command: "/usr/bin/node server.js", CPU: 120, LastCPU: 0},
		{PID: 43, Command: "/usr/bin/sleepy", CPU: 0, LastCPU: 1},
		{PID: 44, Command: "[kworker/2:1-events]", CPU: 3, LastCPU: 0},
		{PID: 45, Command: "/bin/orphan", CPU: 9, LastCPU: -1},
	}
	text := renderCPUCores(p, "ascii", usage, []float64{30, 40}, history, processes, true, true, 146, time.Now())
	for _, want := range []string{
		"CPU CORES · Fake CPU", "4 CPUs · 2 cores × 2 threads", "3.4 GHz now", "cpufreq",
		"HOST", "40%", "load 2.50 2.00 1.50",
		"busiest first", "BUSIEST PROCESS",
		"95%", "80%", "15%", "3.4 GHz", "800 MHz",
		"node · PID 42 · 120.0%", "idle · 1 processes last here",
		"1 of 4 CPUs above 70%", "clocks 800 MHz–3.4 GHz",
		"STEAL is time the hypervisor",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	// Busiest first: CPU 0 (95%) is the first row, CPU 3 (no reading) last.
	rows := strings.Split(text, "\n")
	first, last := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "SKT") {
			first = i + 1
		}
		if strings.Contains(row, "above 70%") {
			last = i - 1
		}
	}
	if first < 0 || last < first || !strings.Contains(rows[first], "]0") || !strings.Contains(rows[last], "]3") {
		t.Fatalf("rows not sorted busiest first:\n%s", strings.Join(rows[first:last+1], "\n"))
	}
	if !strings.Contains(rows[last], "—") {
		t.Fatalf("a CPU without a reading must show dashes: %q", rows[last])
	}
	// By number keeps CPU 0 first too, but CPU 1 second.
	byNumber := renderCPUCores(p, "ascii", usage, nil, history, processes, true, false, 146, time.Now())
	if !strings.Contains(byNumber, "by number") {
		t.Fatal("sort label missing")
	}
	// A narrow view drops the trend and the process column but keeps every CPU.
	narrow := renderCPUCores(p, "ascii", usage, nil, history, nil, false, true, 90, time.Now())
	if strings.Contains(narrow, "TREND") || strings.Contains(narrow, "BUSIEST PROCESS") || !strings.Contains(narrow, "open Processes (4) then c") {
		t.Fatalf("narrow view: %s", narrow)
	}
	for _, cpu := range []string{"]0", "]1", "]2", "]3"} {
		if strings.Count(narrow, cpu+" ") < 1 {
			t.Errorf("narrow view lost CPU %s", cpu)
		}
	}
}

func TestRenderCPUCoresBeforeSamplesAndWithoutInventory(t *testing.T) {
	p := themes[0]
	waiting := renderCPUCores(p, "blocks", hostUtilisation{cpus: cpuInventory{logical: 2, ok: true}}, nil, nil, nil, false, true, 120, time.Now())
	if !strings.Contains(waiting, "need two samples") || strings.Count(waiting, "       —") < 2 {
		t.Fatalf("waiting view: %s", waiting)
	}
	empty := renderCPUCores(p, "blocks", hostUtilisation{}, nil, nil, nil, false, true, 120, time.Now())
	if !strings.Contains(empty, "no per-CPU reading") || !strings.Contains(empty, "CPU inventory not read yet") {
		t.Fatalf("empty view: %s", empty)
	}
}

func TestRecordHostUtilisationKeepsOneTrailPerCPU(t *testing.T) {
	w := &workspace{}
	w.recordHostUtilisation(hostUtilisation{cpuOK: true, cpuPercent: 10, coreBusy: []float64{10, 20}})
	w.recordHostUtilisation(hostUtilisation{cpuOK: true, cpuPercent: 10})
	w.recordHostUtilisation(hostUtilisation{cpuOK: true, cpuPercent: 10, coreBusy: []float64{30, 40}})
	if len(w.hostCoreHistory) != 2 {
		t.Fatalf("trails = %v", w.hostCoreHistory)
	}
	if got := w.hostCoreHistory[1]; len(got) != 3 || got[0] != 20 || got[1] != -1 || got[2] != 40 {
		t.Fatalf("cpu1 trail = %v, want [20 -1 40]", got)
	}
}

func TestShortCommandKeepsKernelThreadNames(t *testing.T) {
	if got := shortCommand("[kworker/3:0H-events_highpri]"); got != "[kworker/3:0H-events_hi…" {
		t.Errorf("kernel thread = %q", got)
	}
	if got := shortCommand("/usr/bin/node server.js"); got != "node" {
		t.Errorf("binary = %q", got)
	}
}
