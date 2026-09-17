package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLoadavgReadsThreeAverages(t *testing.T) {
	l1, l5, l15, ok := parseLoadavg("0.52 1.25 2.00 3/1234 56789\n")
	if !ok || l1 != 0.52 || l5 != 1.25 || l15 != 2 {
		t.Fatalf("load averages = %v %v %v %v", l1, l5, l15, ok)
	}
	if _, _, _, ok := parseLoadavg("garbage"); ok {
		t.Fatal("malformed loadavg must not report a reading")
	}
}

func TestParseSwapDistinguishesNoSwapFromUnknown(t *testing.T) {
	used, total, ok := parseSwap("MemTotal: 100 kB\nSwapTotal:       2097148 kB\nSwapFree:        1048576 kB\n")
	if !ok || total != 2097148*1024 || used != (2097148-1048576)*1024 {
		t.Fatalf("swap = %d/%d %v", used, total, ok)
	}
	if used, total, ok := parseSwap("SwapTotal: 0 kB\nSwapFree: 0 kB\n"); !ok || used != 0 || total != 0 {
		t.Fatal("a machine without swap is a valid zero reading")
	}
	if _, _, ok := parseSwap("MemTotal: 100 kB\n"); ok {
		t.Fatal("missing swap lines must not report a reading")
	}
}

func TestPressureReadsSomeAvg10AndSurvivesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "io"), []byte("some avg10=12.50 avg60=3.00 avg300=1.00 total=123\nfull avg10=2.00 avg60=1.00 avg300=0.50 total=45\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pressure := readPressure(dir)
	if !pressure.ok || pressure.io != 12.5 || pressure.cpu != 0 {
		t.Fatalf("pressure = %+v", pressure)
	}
	if readPressure(filepath.Join(dir, "absent")).ok {
		t.Fatal("no pressure files must read as missing, not zero")
	}
	p := themes[0]
	if signal := pressureSignal(p, pressure); !strings.Contains(signal, "io 12%") || !strings.Contains(signal, p.warning) {
		t.Fatalf("io stall above 5%% must be named in the warning hue: %q", signal)
	}
	if pressureSignal(p, hostPressure{ok: true, cpu: 2}) != "" {
		t.Fatal("stalls under 5% are noise and stay silent")
	}
	if signal := pressureSignal(p, hostPressure{ok: true, memory: 40}); !strings.Contains(signal, p.error) {
		t.Fatalf("a 40%% memory stall takes the error hue: %q", signal)
	}
}

func TestHostHeadlinesCarryLoadAndSwap(t *testing.T) {
	usage := hostUtilisation{cpuOK: true, cpuPercent: 12, load1: 3.5, loadOK: true, memOK: true, memPercent: 40, memUsed: 4 << 30, memTotal: 10 << 30, swapOK: true, swapUsed: 1 << 30, swapTotal: 2 << 30}
	if got := hostCPUHeadline(usage, "3.0%"); !strings.Contains(got, "load 3.50") {
		t.Fatalf("cpu headline lacks load: %q", got)
	}
	if got := hostMemoryHeadline(usage, "1 MiB"); !strings.Contains(got, "swap 1.0/2.0 GiB") {
		t.Fatalf("memory headline lacks swap: %q", got)
	}
	usage.swapUsed = 0
	if got := hostMemoryHeadline(usage, "1 MiB"); strings.Contains(got, "swap") {
		t.Fatalf("unused swap is not worth a word: %q", got)
	}
}
