package dashboard

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// hostUtilisation is whole-machine utilisation. It is deliberately separate
// from fleetSample, which only ever sums the workloads Systemdoc observed, so
// a missing host reading can never be mistaken for a workload measurement.
type hostUtilisation struct {
	cpuPercent float64
	cpuOK      bool
	memPercent float64
	memUsed    int64 // bytes
	memTotal   int64 // bytes
	memOK      bool
	// Signals the CPU and memory percentages do not carry: run-queue
	// pressure, swap in use, and the kernel's own stall accounting.
	load1, load5, load15 float64
	loadOK               bool
	swapUsed, swapTotal  int64 // bytes
	swapOK               bool
	pressure             hostPressure
	// What the CPU figure is a share of: the machine's logical CPUs and
	// cores, the clock each is running at right now, and each one's own busy
	// share over the same interval as cpuPercent. coreBusy is nil until two
	// samples exist or where /proc/stat carries no per-CPU lines.
	cpus     cpuInventory
	clocks   cpuClocks
	coreBusy []float64
}

// hostPressure is the "some" 10-second average from /proc/pressure: the share
// of time at least one task was stalled waiting for that resource.
type hostPressure struct {
	cpu, memory, io float64
	ok              bool
}

// parseLoadavg reads the three load averages from /proc/loadavg.
func parseLoadavg(raw string) (load1, load5, load15 float64, ok bool) {
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	values := [3]float64{}
	for i := range values {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || value < 0 {
			return 0, 0, 0, false
		}
		values[i] = value
	}
	return values[0], values[1], values[2], true
}

// parsePressureSome reads the "some avg10" figure of one /proc/pressure file.
func parsePressureSome(raw string) (float64, bool) {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			if value, found := strings.CutPrefix(field, "avg10="); found {
				n, err := strconv.ParseFloat(value, 64)
				return max(0, min(100, n)), err == nil
			}
		}
	}
	return 0, false
}

// parseSwap returns swap in use and configured, in bytes, from /proc/meminfo.
// A machine without swap reports ok with a zero total.
func parseSwap(raw string) (used, total int64, ok bool) {
	values := map[string]int64{}
	for _, line := range strings.Split(raw, "\n") {
		name, rest, found := strings.Cut(line, ":")
		if !found || (name != "SwapTotal" && name != "SwapFree") {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || value < 0 {
			return 0, 0, false
		}
		values[name] = value * 1024
	}
	total, found := values["SwapTotal"]
	free, foundFree := values["SwapFree"]
	if !found || !foundFree || free > total {
		return 0, 0, false
	}
	return total - free, total, true
}

// cpuTimes is one cumulative CPU accounting sample. A percentage needs two.
// cores holds the same counters for each online logical CPU, indexed by its
// cpuN number, so the per-core strip is measured over exactly the interval
// the aggregate figure is.
type cpuTimes struct {
	busy, total uint64
	ok          bool
	cores       []coreTimes
}

type coreTimes struct {
	busy, total uint64
}

// parseProcStat reads the aggregate "cpu" line of /proc/stat and the cpuN
// lines beneath it. Idle and iowait are both non-busy time; every other
// column counts as busy. The guest and guest_nice columns are already
// included in user and nice by the kernel, so they are skipped rather than
// counted twice. A malformed aggregate line rejects the sample; a malformed
// per-CPU line only drops that CPU.
func parseProcStat(raw string) cpuTimes {
	sample := cpuTimes{}
	cores := map[int]coreTimes{}
	highest := -1
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		busy, total, ok := procStatBusy(fields[1:])
		if fields[0] == "cpu" {
			if !ok {
				return cpuTimes{}
			}
			sample.busy, sample.total, sample.ok = busy, total, true
			continue
		}
		index, err := strconv.Atoi(fields[0][3:])
		if err != nil || index < 0 || !ok {
			continue
		}
		cores[index] = coreTimes{busy: busy, total: total}
		highest = max(highest, index)
	}
	if !sample.ok {
		return cpuTimes{}
	}
	if highest >= 0 {
		sample.cores = make([]coreTimes, highest+1)
		for index, core := range cores {
			sample.cores[index] = core
		}
	}
	return sample
}

func procStatBusy(fields []string) (busy, total uint64, ok bool) {
	var idle uint64
	for i, field := range fields {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		if i >= 8 {
			break
		}
		total += value
		if i == 3 || i == 4 {
			idle += value
		}
	}
	if total == 0 {
		return 0, 0, false
	}
	return total - idle, total, true
}

// cpuPercent compares two cumulative samples. The counters only move forward,
// so a reset or a repeated sample reports nothing rather than a false zero.
func cpuPercent(previous, current cpuTimes) (float64, bool) {
	if !previous.ok || !current.ok || current.total <= previous.total || current.busy < previous.busy {
		return 0, false
	}
	busy := float64(current.busy - previous.busy)
	total := float64(current.total - previous.total)
	return max(0, min(100, busy/total*100)), true
}

// parseMeminfo returns used and total bytes. MemAvailable is the kernel's own
// estimate of what a new workload could claim, which is a truer "used" than
// MemFree; older kernels without it fall back to free plus reclaimable cache.
func parseMeminfo(raw string) (used, total int64, ok bool) {
	values := map[string]int64{}
	for _, line := range strings.Split(raw, "\n") {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || value < 0 {
			continue
		}
		values[name] = value * 1024
	}
	total = values["MemTotal"]
	if total <= 0 {
		return 0, 0, false
	}
	available, found := values["MemAvailable"]
	if !found {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return max(0, min(total, total-available)), total, true
}

// parseVMStat reads macOS page accounting. Active, wired and compressed pages
// are the resident footprint; free and speculative pages are reclaimable.
func parseVMStat(raw string) (used int64, ok bool) {
	pageSize := int64(4096)
	if _, rest, found := strings.Cut(raw, "page size of "); found {
		if fields := strings.Fields(rest); len(fields) > 0 {
			if value, err := strconv.ParseInt(fields[0], 10, 64); err == nil && value > 0 {
				pageSize = value
			}
		}
	}
	counted := false
	var pages int64
	for _, line := range strings.Split(raw, "\n") {
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(name) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
		default:
			continue
		}
		value, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(rest), "."), 10, 64)
		if err != nil || value < 0 {
			continue
		}
		pages += value
		counted = true
	}
	if !counted {
		return 0, false
	}
	return pages * pageSize, true
}

// parseProcessCPU sums per-process CPU shares. Each value is a share of one
// core, so the total is divided by the core count to describe the machine.
func parseProcessCPU(raw string, cores int) (float64, bool) {
	if cores <= 0 {
		return 0, false
	}
	var total float64
	counted := false
	for _, line := range strings.Split(raw, "\n") {
		field := strings.TrimSpace(line)
		if field == "" {
			continue
		}
		value, err := strconv.ParseFloat(field, 64)
		if err != nil || value < 0 {
			continue
		}
		total += value
		counted = true
	}
	if !counted {
		return 0, false
	}
	return max(0, min(100, total/float64(cores))), true
}

// collectHostUtilisation returns the current reading plus the CPU sample to
// compare the next reading against. Each half fails independently: an
// unavailable memory source never suppresses a good CPU reading.
func collectHostUtilisation(ctx context.Context, platform string, cores int, previous cpuTimes) (hostUtilisation, cpuTimes) {
	if platform == "darwin" {
		return collectDarwinUtilisation(ctx, cores), cpuTimes{}
	}
	var result hostUtilisation
	current := cpuTimes{}
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		current = parseProcStat(string(raw))
		result.cpuPercent, result.cpuOK = cpuPercent(previous, current)
		result.coreBusy = coreBusyShares(previous, current)
	}
	result.clocks = collectLinuxClocks("/proc", "/sys/devices/system/cpu", cores)
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		if used, total, ok := parseMeminfo(string(raw)); ok {
			result.memUsed, result.memTotal, result.memOK = used, total, true
			result.memPercent = float64(used) / float64(total) * 100
		}
		result.swapUsed, result.swapTotal, result.swapOK = parseSwap(string(raw))
	}
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		result.load1, result.load5, result.load15, result.loadOK = parseLoadavg(string(raw))
	}
	result.pressure = readPressure("/proc/pressure")
	return result, current
}

// readPressure reads the three PSI files. The directory is absent on kernels
// without PSI or with it disabled, and then the reading simply stays missing.
func readPressure(dir string) hostPressure {
	var result hostPressure
	any := false
	for name, target := range map[string]*float64{"cpu": &result.cpu, "memory": &result.memory, "io": &result.io} {
		raw, err := os.ReadFile(dir + "/" + name)
		if err != nil {
			continue
		}
		if value, ok := parsePressureSome(string(raw)); ok {
			*target = value
			any = true
		}
	}
	result.ok = any
	return result
}

// pressurePlain is the escaped-text form of the stall reading for card notes.
func pressurePlain(pressure hostPressure) string {
	if !pressure.ok {
		return ""
	}
	parts := []string{}
	for _, axis := range []struct {
		name  string
		value float64
	}{{"cpu", pressure.cpu}, {"mem", pressure.memory}, {"io", pressure.io}} {
		if axis.value >= 5 {
			parts = append(parts, axis.name+" "+strconv.FormatFloat(axis.value, 'f', 0, 64)+"%")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " · stall " + strings.Join(parts, " ")
}

// pressureSignal names the stalled resources worth a glance: below 5% the
// kernel is merely busy, from 5% tasks are waiting, from 25% they are
// waiting often enough to explain slowness.
func pressureSignal(p palette, pressure hostPressure) string {
	if !pressure.ok {
		return ""
	}
	parts := []string{}
	for _, axis := range []struct {
		name  string
		value float64
	}{{"cpu", pressure.cpu}, {"mem", pressure.memory}, {"io", pressure.io}} {
		if axis.value < 5 {
			continue
		}
		hue := p.warning
		if axis.value >= 25 {
			hue = p.error
		}
		parts = append(parts, "["+hue+"::b]"+axis.name+" "+strconv.FormatFloat(axis.value, 'f', 0, 64)+"%[-::-]")
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + p.muted + "]STALL[-] " + strings.Join(parts, " ")
}

// macOS exposes no cumulative aggregate counter comparable to /proc/stat, so
// CPU is summed from live process shares and memory from page accounting.
func collectDarwinUtilisation(ctx context.Context, cores int) hostUtilisation {
	var result hostUtilisation
	if raw, _, err := networkCommand(ctx, "ps", "-A", "-o", "%cpu="); err == nil {
		result.cpuPercent, result.cpuOK = parseProcessCPU(raw, cores)
	}
	total := int64(0)
	if raw, _, err := networkCommand(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
		if value, convErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); convErr == nil {
			total = value
		}
	}
	if total <= 0 {
		return result
	}
	if raw, _, err := networkCommand(ctx, "vm_stat"); err == nil {
		if used, ok := parseVMStat(raw); ok {
			used = max(0, min(total, used))
			result.memUsed, result.memTotal, result.memOK = used, total, true
			result.memPercent = float64(used) / float64(total) * 100
		}
	}
	return result
}

// collectCPUInventory picks the platform reader. The fallback count is Go's
// view of the schedulable CPUs, which is right whenever the kernel files are
// unreadable.
func collectCPUInventory(ctx context.Context, platform string, fallbackLogical int) (cpuInventory, cpuClocks) {
	if platform == "darwin" {
		return collectDarwinCPUInventory(ctx, fallbackLogical)
	}
	return collectLinuxCPUInventory("/proc", "/sys/devices/system/cpu", fallbackLogical), cpuClocks{}
}

// Reading /proc is cheap enough to sample often; the macOS path shells out, so
// it samples less frequently. Neither cadence follows the inventory interval,
// which keeps this loop off the settings the UI goroutine owns.
func hostSampleInterval() time.Duration {
	if servicePlatform == "darwin" {
		return 5 * time.Second
	}
	return 2 * time.Second
}

func (w *workspace) hostMetricsLoop(cores int) {
	var previous cpuTimes
	// The inventory is read once: counts and the advertised clock range do
	// not move between samples, and on macOS each is a sysctl subprocess.
	inventory, darwinClocks := collectCPUInventory(w.ctx, servicePlatform, cores)
	ticker := time.NewTicker(hostSampleInterval())
	defer ticker.Stop()
	for {
		usage, sample := collectHostUtilisation(w.ctx, servicePlatform, cores, previous)
		previous = sample
		usage.cpus = inventory
		if servicePlatform == "darwin" {
			usage.clocks = darwinClocks
		}
		w.queueQuiet(func() { w.recordHostUtilisation(usage) })
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// History keeps a bounded trail for the telemetry cards. Readings that failed
// are stored as -1 so the renderer leaves a gap instead of drawing a zero.
func (w *workspace) recordHostUtilisation(usage hostUtilisation) {
	w.hostUsage = usage
	w.telemetryDirty.Store(true)
	cpu, memory := -1.0, -1.0
	if usage.cpuOK {
		cpu = usage.cpuPercent
	}
	if usage.memOK {
		memory = usage.memPercent
	}
	w.hostCPUHistory = appendBounded(w.hostCPUHistory, cpu)
	w.hostMemoryHistory = appendBounded(w.hostMemoryHistory, memory)
}

// trimHistory keeps the most recent samples that fit the card's chart width.
func trimHistory(values []float64, width int) []float64 {
	if width > 0 && len(values) > width {
		return values[len(values)-width:]
	}
	return values
}

// hostBytesPair drops the repeated unit so a card can show both numbers.
func hostBytesPair(used, total int64) string {
	usedText, totalText := hostBytes(used, false), hostBytes(total, false)
	usedValue, usedUnit, _ := strings.Cut(usedText, " ")
	totalValue, totalUnit, _ := strings.Cut(totalText, " ")
	if usedUnit == totalUnit {
		return usedValue + "/" + totalValue + " " + totalUnit
	}
	return usedText + " / " + totalText
}

// An unavailable host reading stays visibly missing; the tracked workload sum
// beside it is a different measurement and is never substituted for it.
func hostCPUHeadline(usage hostUtilisation, tracked string) string {
	share := "—"
	if usage.cpuOK {
		share = strconv.FormatFloat(usage.cpuPercent, 'f', 0, 64) + "%"
	}
	if usage.loadOK {
		// The one-minute load average says how many tasks wanted a CPU, which
		// a utilisation percentage alone cannot show on a saturated machine.
		return share + " · load " + strconv.FormatFloat(usage.load1, 'f', 2, 64) + " · " + tracked + " tracked"
	}
	return share + " · " + tracked + " tracked"
}

func hostMemoryHeadline(usage hostUtilisation, tracked string) string {
	if !usage.memOK {
		return "— · " + tracked + " tracked"
	}
	headline := strconv.FormatFloat(usage.memPercent, 'f', 0, 64) + "% · " + hostBytesPair(usage.memUsed, usage.memTotal)
	if usage.swapOK && usage.swapUsed > 0 {
		headline += " · swap " + hostBytesPair(usage.swapUsed, usage.swapTotal)
	}
	return headline
}

func appendBounded(values []float64, value float64) []float64 {
	values = append(values, value)
	if len(values) > 64 {
		values = values[len(values)-64:]
	}
	return values
}
