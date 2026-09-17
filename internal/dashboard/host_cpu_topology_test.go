package dashboard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseProcStatKeepsPerCPULines(t *testing.T) {
	sample := parseProcStat("cpu  100 20 30 700 50 5 5 10 90 15\ncpu0 60 10 15 300 25 5 5 10 0 0\ncpu2 40 10 15 400 25 0 0 0 0 0\ncpu1 bad 1 2 3 4\nintr 1\n")
	if !sample.ok || len(sample.cores) != 3 {
		t.Fatalf("expected three core slots (highest index 2), got %+v", sample)
	}
	if sample.cores[0].total != 430 || sample.cores[0].busy != 105 {
		t.Fatalf("cpu0 = %+v", sample.cores[0])
	}
	// The malformed cpu1 line leaves a zero slot rather than rejecting the sample.
	if sample.cores[1].total != 0 || sample.cores[2].total != 490 {
		t.Fatalf("cpu1/cpu2 = %+v %+v", sample.cores[1], sample.cores[2])
	}
	if len(parseProcStat("cpu  1 2 3 4 5\n").cores) != 0 {
		t.Fatal("no per-CPU lines must mean no cores, not an empty slot")
	}
}

func TestCoreBusySharesLeaveGapsForUnusableCores(t *testing.T) {
	first := parseProcStat("cpu  10 0 0 90 0\ncpu0 10 0 0 90 0\ncpu1 5 0 0 95 0\ncpu2 5 0 0 95 0\n")
	second := parseProcStat("cpu  60 0 0 140 0\ncpu0 60 0 0 140 0\ncpu1 5 0 0 195 0\ncpu2 3 0 0 90 0\n")
	shares := coreBusyShares(first, second)
	if len(shares) != 3 {
		t.Fatalf("shares = %v", shares)
	}
	// cpu0: 50 busy of 100 elapsed; cpu1: idle; cpu2: counter went backwards.
	if shares[0] != 50 || shares[1] != 0 || shares[2] != -1 {
		t.Fatalf("shares = %v, want [50 0 -1]", shares)
	}
	if coreBusyShares(cpuTimes{}, second) != nil || coreBusyShares(first, first) != nil {
		t.Fatal("a missing baseline or a repeated sample must report no shares")
	}
}

func TestParseCPUInfoCountsCoresSocketsAndClocks(t *testing.T) {
	raw := `processor	: 0
model name	: Fake CPU 3000
physical id	: 0
core id	: 0
cpu MHz		: 3400.000

processor	: 1
model name	: Fake CPU 3000
physical id	: 0
core id	: 0
cpu MHz		: 1200.000

processor	: 2
model name	: Fake CPU 3000
physical id	: 1
core id	: 0
cpu MHz		: 3400.000

processor	: 3
model name	: Fake CPU 3000
physical id	: 1
core id	: 0
cpu MHz		: 3400.000
`
	inventory, clocks := parseCPUInfo(raw)
	if !inventory.ok || inventory.logical != 4 || inventory.cores != 2 || inventory.sockets != 2 || inventory.model != "Fake CPU 3000" {
		t.Fatalf("%+v", inventory)
	}
	if !clocks.ok || clocks.source != "cpuinfo" || len(clocks.mhz) != 4 || clocks.mhz[1] != 1200 {
		t.Fatalf("%+v", clocks)
	}
	if got := clockText(clocks); got != "1.2 GHz–3.4 GHz" {
		t.Errorf("spread clock text = %q", got)
	}
	// ARM boards carry only processor lines and one Model line, no clocks.
	arm, armClocks := parseCPUInfo("processor\t: 0\nBogoMIPS\t: 48.00\n\nprocessor\t: 1\nBogoMIPS\t: 48.00\n\nModel\t\t: Raspberry Pi 5\n")
	if !arm.ok || arm.logical != 2 || arm.cores != 0 || arm.model != "Raspberry Pi 5" || armClocks.ok {
		t.Fatalf("arm: %+v %+v", arm, armClocks)
	}
	if empty, _ := parseCPUInfo(""); empty.ok {
		t.Fatal("an empty file must not describe a CPU")
	}
}

func writeSysfsCPU(t *testing.T, root string, id int, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, "cpu"+strconv.Itoa(id), name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSysfsTopologyAndClocksReadAFakeTree(t *testing.T) {
	root := t.TempDir()
	// Two cores with two threads each on one package; cpu3 is offline and
	// exposes no cpufreq directory.
	writeSysfsCPU(t, root, 0, map[string]string{"topology/core_id": "0\n", "topology/physical_package_id": "0\n", "cpufreq/scaling_cur_freq": "3400000\n", "cpufreq/cpuinfo_min_freq": "800000\n", "cpufreq/cpuinfo_max_freq": "4200000\n"})
	writeSysfsCPU(t, root, 1, map[string]string{"topology/core_id": "0\n", "topology/physical_package_id": "0\n", "cpufreq/scaling_cur_freq": "3400000\n"})
	writeSysfsCPU(t, root, 2, map[string]string{"topology/core_id": "1\n", "topology/physical_package_id": "0\n", "cpufreq/scaling_cur_freq": "800000\n"})
	writeSysfsCPU(t, root, 3, map[string]string{"topology/core_id": "1\n", "topology/physical_package_id": "0\n"})
	if err := os.WriteFile(filepath.Join(root, "online"), []byte("0-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cores, sockets, ok := sysfsTopology(root)
	if !ok || cores != 2 || sockets != 1 {
		t.Fatalf("cores %d sockets %d ok %v", cores, sockets, ok)
	}
	lo, hi, ok := sysfsClockRange(root)
	if !ok || lo != 800 || hi != 4200 {
		t.Fatalf("range %v–%v ok %v", lo, hi, ok)
	}
	clocks := sysfsClocks(root, 4)
	if !clocks.ok || clocks.source != "cpufreq" || len(clocks.mhz) != 4 || clocks.mhz[2] != 800 || clocks.mhz[3] != 0 {
		t.Fatalf("%+v", clocks)
	}
	if got := clockText(clocks); got != "800 MHz–3.4 GHz" {
		t.Errorf("clock text = %q", got)
	}
	if empty := sysfsClocks(t.TempDir(), 4); empty.ok || empty.source != "" {
		t.Fatalf("a tree without cpufreq must report no clocks, got %+v", empty)
	}
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "stat"), []byte("cpu  1 2 3 4 5\ncpu0 1 1 1 1 1\ncpu1 1 1 1 1 1\ncpu2 1 1 1 1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inventory := collectLinuxCPUInventory(proc, root, 8)
	// Online CPUs come from /proc/stat, the topology from sysfs, and the
	// runtime fallback is not used when the kernel files answer.
	if !inventory.ok || inventory.logical != 3 || inventory.cores != 2 || inventory.sockets != 1 || inventory.maxMHz != 4200 {
		t.Fatalf("%+v", inventory)
	}
	if fallback := collectLinuxCPUInventory(t.TempDir(), t.TempDir(), 8); !fallback.ok || fallback.logical != 8 || fallback.cores != 0 {
		t.Fatalf("fallback: %+v", fallback)
	}
}

func TestCPUInventoryLabelsSayOnlyWhatIsKnown(t *testing.T) {
	usage := hostUtilisation{}
	if cpuInventoryLabel(usage) != "" || cpuInventoryDetail(usage) != "" {
		t.Fatal("an unknown inventory must add nothing")
	}
	usage.cpus = cpuInventory{logical: 12, cores: 6, sockets: 1, model: "Fake CPU", minMHz: 800, maxMHz: 4700, ok: true}
	if got := cpuInventoryLabel(usage); got != "12 CPUs" {
		t.Errorf("label without clocks = %q", got)
	}
	if got := cpuInventoryDetail(usage); got != "12 CPUs · 6 cores × 2 threads · Fake CPU · no clock reading · cpufreq and /proc/cpuinfo expose none" {
		t.Errorf("detail without clocks = %q", got)
	}
	usage.clocks = cpuClocks{mhz: []float64{3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3100}, source: "cpufreq", ok: true}
	if got := cpuInventoryLabel(usage); got != "12 CPUs · 3.0 GHz" {
		t.Errorf("label = %q", got)
	}
	if got := cpuInventoryDetail(usage); !strings.Contains(got, "3.0 GHz now · 800 MHz–4.7 GHz range · cpufreq") {
		t.Errorf("detail = %q", got)
	}
	usage.clocks.source = "cpuinfo"
	if got := cpuInventoryDetail(usage); !strings.Contains(got, "nominal inside a VM") {
		t.Errorf("cpuinfo clocks must be flagged as nominal, got %q", got)
	}
	single := hostUtilisation{cpus: cpuInventory{logical: 1, ok: true}}
	if got := cpuInventoryLabel(single); got != "1 CPU" {
		t.Errorf("single = %q", got)
	}
	two := hostUtilisation{cpus: cpuInventory{logical: 16, cores: 16, sockets: 2, ok: true}}
	if got := cpuInventoryDetail(two); !strings.HasPrefix(got, "16 CPUs · 2 sockets · no clock reading") {
		t.Errorf("two sockets = %q", got)
	}
}

func TestCoreStripDrawsOneBarPerCPUAndFallsBackToACount(t *testing.T) {
	p := themes[0]
	shares := []float64{0, 3, 50, 100, -1}
	strip := coreStrip(p, shares, 8, "ascii")
	for _, want := range []string{"]0[-]", "]0[-]", "]5[-]", "]9[-]", "]·[-]"} {
		if !strings.Contains(strip, want) {
			t.Errorf("ascii strip %q lacks %q", strip, want)
		}
	}
	blocks := coreStrip(p, shares, 8, "blocks")
	// A core is never drawn as nothing: idle and 3% are both the lowest bar
	// (idle in the muted hue), 100% the full one, and only the missing
	// reading is a dot.
	if strings.Count(blocks, "▁") != 2 || !strings.Contains(blocks, "█") || strings.Count(blocks, "·") != 1 || strings.Count(blocks, "[-]") != 5 || !strings.HasPrefix(blocks, "["+p.muted+"]▁") {
		t.Errorf("block strip = %q", blocks)
	}
	if coreStrip(p, shares, 4, "blocks") != "" {
		t.Error("a strip wider than its slot must be omitted, not clipped")
	}
	if got := coreStripPlain([]float64{90, 10, 75, -1}); got != "2/3 CPUs above 70%" {
		t.Errorf("plain summary = %q", got)
	}
	if coreStripPlain(nil) != "" || coreStripPlain([]float64{-1}) != "" {
		t.Error("no usable cores must summarise to nothing")
	}
}

func TestCPUListCountSizesKernelLists(t *testing.T) {
	for list, want := range map[string]int{"0-3": 4, "0-3,8,10-11": 7, "5": 1, "": 0} {
		if got := cpuListCount(list); got != want {
			t.Errorf("%q = %d, want %d", list, got, want)
		}
	}
	if cpuListCount("0-x") < 1<<30 {
		t.Error("an unparsable list must never look confined")
	}
}
