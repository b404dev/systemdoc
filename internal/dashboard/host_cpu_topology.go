package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cpuInventory is what the machine has, read once at start: how many logical
// CPUs the scheduler can place a task on, how many physical cores and sockets
// those threads sit on, the model string, and the clock range cpufreq
// advertises. Nothing here changes while Systemdoc runs except through CPU
// hotplug, which is rare enough that a restart is the honest answer.
type cpuInventory struct {
	logical, cores, sockets int
	model                   string
	minMHz, maxMHz          float64
	ok                      bool
	// coreOf and socketOf map each logical CPU number to its physical core
	// and package id; -1 where the kernel did not say.
	coreOf, socketOf []int
}

// coreLabel names the core and socket a logical CPU sits on, or "" when the
// topology is unknown.
func (inv cpuInventory) coreLabel(cpu int) (core, socket string) {
	if cpu < 0 || cpu >= len(inv.coreOf) || inv.coreOf[cpu] < 0 {
		return "", ""
	}
	return strconv.Itoa(inv.coreOf[cpu]), strconv.Itoa(inv.socketOf[cpu])
}

// cpuClocks is the per-sample frequency reading: one current clock per
// logical CPU in MHz, zero where a CPU exposed none, and the file it came
// from so the panels can say how live the figure is. cpufreq reports what the
// governor is asking of the core right now; the "cpu MHz" lines of
// /proc/cpuinfo are the kernel's own estimate on bare metal and a fixed
// nominal figure inside most virtual machines.
type cpuClocks struct {
	mhz    []float64
	source string // "cpufreq", "cpuinfo" or ""
	ok     bool
}

// parseCPUInfo reads the per-processor blocks of /proc/cpuinfo. Field names
// differ by architecture: x86 has "model name", "physical id" and "core id";
// ARM boards often carry only a "processor" line per CPU plus one "Model" or
// "Hardware" line. Only what is present is used.
func parseCPUInfo(raw string) (inventory cpuInventory, clocks cpuClocks) {
	type block struct {
		pkg, core string
		mhz       float64
	}
	var blocks []block
	current := block{pkg: "0", core: ""}
	seenProcessor := false
	flush := func() {
		if seenProcessor {
			blocks = append(blocks, current)
		}
		current = block{pkg: "0", core: ""}
		seenProcessor = false
	}
	for _, line := range strings.Split(raw, "\n") {
		key, value, found := strings.Cut(line, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found {
			if key == "" {
				flush()
			}
			continue
		}
		switch key {
		case "processor":
			if seenProcessor {
				flush()
			}
			seenProcessor = true
		case "model name", "Model", "Hardware", "cpu model":
			if inventory.model == "" && value != "" {
				inventory.model = clean(value)
			}
		case "physical id":
			current.pkg = value
		case "core id":
			current.core = value
		case "cpu MHz", "clock":
			if n, err := strconv.ParseFloat(strings.TrimSuffix(value, "MHz"), 64); err == nil && n > 0 {
				current.mhz = n
			}
		}
	}
	flush()
	if len(blocks) == 0 {
		return inventory, clocks
	}
	inventory.logical, inventory.ok = len(blocks), true
	cores, sockets := map[string]bool{}, map[string]bool{}
	for _, b := range blocks {
		sockets[b.pkg] = true
		if b.core != "" {
			cores[b.pkg+"/"+b.core] = true
		}
	}
	inventory.sockets = len(sockets)
	if len(cores) > 0 {
		inventory.cores = len(cores)
		inventory.coreOf, inventory.socketOf = make([]int, len(blocks)), make([]int, len(blocks))
		for i, b := range blocks {
			inventory.coreOf[i], inventory.socketOf[i] = -1, -1
			if core, err := strconv.Atoi(b.core); err == nil {
				inventory.coreOf[i] = core
			}
			if pkg, err := strconv.Atoi(b.pkg); err == nil {
				inventory.socketOf[i] = pkg
			}
		}
	}
	clocks.mhz = make([]float64, len(blocks))
	for i, b := range blocks {
		clocks.mhz[i] = b.mhz
		if b.mhz > 0 {
			clocks.ok = true
		}
	}
	if clocks.ok {
		clocks.source = "cpuinfo"
	}
	return inventory, clocks
}

// sysfsCPUs lists the cpuN directories under a sysfs cpu root in numeric
// order. Offline CPUs keep their directory, so the list is the possible set;
// callers compare against /proc/stat, which only carries online CPUs.
func sysfsCPUs(root string) []int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var ids []int
	for _, entry := range entries {
		rest, found := strings.CutPrefix(entry.Name(), "cpu")
		if !found {
			continue
		}
		if id, err := strconv.Atoi(rest); err == nil && id >= 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func readSysfsInt(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return n, err == nil && n >= 0
}

// sysfsTopology counts distinct (package, core) pairs and packages from the
// topology directory. It is more reliable than /proc/cpuinfo on ARM, where
// the core id lines are usually absent.
func sysfsTopology(root string) (cores, sockets int, coreOf, socketOf []int, ok bool) {
	pairs, packages := map[string]bool{}, map[int64]bool{}
	ids := sysfsCPUs(root)
	if len(ids) == 0 {
		return 0, 0, nil, nil, false
	}
	coreOf, socketOf = make([]int, ids[len(ids)-1]+1), make([]int, ids[len(ids)-1]+1)
	for i := range coreOf {
		coreOf[i], socketOf[i] = -1, -1
	}
	for _, id := range ids {
		dir := filepath.Join(root, "cpu"+strconv.Itoa(id), "topology")
		core, coreOK := readSysfsInt(filepath.Join(dir, "core_id"))
		pkg, pkgOK := readSysfsInt(filepath.Join(dir, "physical_package_id"))
		if !coreOK || !pkgOK {
			continue
		}
		pairs[strconv.FormatInt(pkg, 10)+"/"+strconv.FormatInt(core, 10)] = true
		packages[pkg] = true
		coreOf[id], socketOf[id] = int(core), int(pkg)
	}
	if len(pairs) == 0 {
		return 0, 0, nil, nil, false
	}
	return len(pairs), len(packages), coreOf, socketOf, true
}

// sysfsClockRange reads the hardware clock limits cpufreq advertises for the
// first CPU that has them, in MHz. A machine without cpufreq (most virtual
// machines) has none, and the range simply stays unknown.
func sysfsClockRange(root string) (minMHz, maxMHz float64, ok bool) {
	for _, id := range sysfsCPUs(root) {
		dir := filepath.Join(root, "cpu"+strconv.Itoa(id), "cpufreq")
		lo, loOK := readSysfsInt(filepath.Join(dir, "cpuinfo_min_freq"))
		hi, hiOK := readSysfsInt(filepath.Join(dir, "cpuinfo_max_freq"))
		if !loOK || !hiOK || hi <= 0 {
			continue
		}
		return float64(lo) / 1000, float64(hi) / 1000, true
	}
	return 0, 0, false
}

// sysfsClocks reads scaling_cur_freq for every CPU that exposes it. The value
// is in kHz. Missing files leave a zero, so a partial reading still says
// which cores it covers.
func sysfsClocks(root string, logical int) cpuClocks {
	ids := sysfsCPUs(root)
	if len(ids) == 0 {
		return cpuClocks{}
	}
	size := max(logical, ids[len(ids)-1]+1)
	clocks := cpuClocks{mhz: make([]float64, size)}
	for _, id := range ids {
		khz, ok := readSysfsInt(filepath.Join(root, "cpu"+strconv.Itoa(id), "cpufreq", "scaling_cur_freq"))
		if !ok || khz <= 0 {
			continue
		}
		clocks.mhz[id] = float64(khz) / 1000
		clocks.ok = true
	}
	if !clocks.ok {
		return cpuClocks{}
	}
	clocks.source = "cpufreq"
	return clocks
}

// collectLinuxCPUInventory merges /proc/cpuinfo with sysfs. The logical count
// comes from the online cpuN lines of /proc/stat when available, because that
// is the set the per-core utilisation is measured on.
func collectLinuxCPUInventory(procRoot, sysRoot string, fallbackLogical int) cpuInventory {
	var inventory cpuInventory
	if raw, err := os.ReadFile(filepath.Join(procRoot, "cpuinfo")); err == nil {
		inventory, _ = parseCPUInfo(string(raw))
	}
	if raw, err := os.ReadFile(filepath.Join(procRoot, "stat")); err == nil {
		if sample := parseProcStat(string(raw)); sample.ok && len(sample.cores) > 0 {
			inventory.logical, inventory.ok = len(sample.cores), true
		}
	}
	if inventory.logical <= 0 && fallbackLogical > 0 {
		inventory.logical, inventory.ok = fallbackLogical, true
	}
	if cores, sockets, coreOf, socketOf, ok := sysfsTopology(sysRoot); ok {
		inventory.cores, inventory.sockets, inventory.coreOf, inventory.socketOf = cores, sockets, coreOf, socketOf
	}
	if lo, hi, ok := sysfsClockRange(sysRoot); ok {
		inventory.minMHz, inventory.maxMHz = lo, hi
	}
	return inventory
}

// collectLinuxClocks prefers cpufreq and falls back to /proc/cpuinfo.
func collectLinuxClocks(procRoot, sysRoot string, logical int) cpuClocks {
	if clocks := sysfsClocks(sysRoot, logical); clocks.ok {
		return clocks
	}
	if raw, err := os.ReadFile(filepath.Join(procRoot, "cpuinfo")); err == nil {
		if _, clocks := parseCPUInfo(string(raw)); clocks.ok {
			return clocks
		}
	}
	return cpuClocks{}
}

// collectDarwinCPUInventory asks sysctl for the counts and the brand string.
// hw.cpufrequency exists only on Intel Macs; Apple Silicon publishes no clock
// through sysctl, so the frequency stays unknown there rather than guessed.
func collectDarwinCPUInventory(ctx context.Context, fallbackLogical int) (cpuInventory, cpuClocks) {
	inventory := cpuInventory{logical: fallbackLogical, ok: fallbackLogical > 0}
	number := func(key string) (int64, bool) {
		raw, _, err := networkCommand(ctx, "sysctl", "-n", key)
		if err != nil {
			return 0, false
		}
		n, convErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		return n, convErr == nil && n > 0
	}
	if n, ok := number("hw.logicalcpu"); ok {
		inventory.logical, inventory.ok = int(n), true
	}
	if n, ok := number("hw.physicalcpu"); ok {
		inventory.cores = int(n)
	}
	if n, ok := number("hw.packages"); ok {
		inventory.sockets = int(n)
	}
	if raw, _, err := networkCommand(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		inventory.model = clean(strings.TrimSpace(raw))
	}
	var clocks cpuClocks
	if hz, ok := number("hw.cpufrequency"); ok && inventory.logical > 0 {
		clocks = cpuClocks{mhz: make([]float64, inventory.logical), source: "sysctl", ok: true}
		for i := range clocks.mhz {
			clocks.mhz[i] = float64(hz) / 1e6
		}
		inventory.maxMHz = float64(hz) / 1e6
	}
	return inventory, clocks
}

// coreBusyShares compares the per-CPU lines of two /proc/stat samples. A CPU
// missing from either sample, or whose counter went backwards, reports -1 so
// the strip leaves a gap for it instead of drawing an idle core.
func coreBusyShares(previous, current cpuTimes) []float64 {
	if !previous.ok || !current.ok || len(current.cores) == 0 {
		return nil
	}
	shares := make([]float64, len(current.cores))
	any := false
	for i, now := range current.cores {
		shares[i] = -1
		if i >= len(previous.cores) {
			continue
		}
		before := previous.cores[i]
		if now.total <= before.total || now.busy < before.busy {
			continue
		}
		shares[i] = max(0, min(100, float64(now.busy-before.busy)/float64(now.total-before.total)*100))
		any = true
	}
	if !any {
		return nil
	}
	return shares
}

// coreLoadShares splits each CPU's interval the way coreBusyShares does, so
// the cores view can say whether a busy CPU is running user code, the
// kernel, or a hypervisor's other guests, and whether an idle one is waiting
// on I/O.
func coreLoadShares(previous, current cpuTimes) []coreLoad {
	if !previous.ok || !current.ok || len(current.cores) == 0 {
		return nil
	}
	loads := make([]coreLoad, len(current.cores))
	for i, now := range current.cores {
		if i >= len(previous.cores) {
			continue
		}
		before := previous.cores[i]
		if now.total <= before.total || now.busy < before.busy || now.user < before.user || now.system < before.system || now.iowait < before.iowait || now.steal < before.steal {
			continue
		}
		elapsed := float64(now.total - before.total)
		share := func(a, b uint64) float64 { return max(0, min(100, float64(b-a)/elapsed*100)) }
		loads[i] = coreLoad{user: share(before.user, now.user), system: share(before.system, now.system), iowait: share(before.iowait, now.iowait), steal: share(before.steal, now.steal), ok: true}
	}
	return loads
}

// clockSummary reduces the per-core clocks to the figures a headline can
// carry: the mean over cores with a reading, and the spread.
func clockSummary(clocks cpuClocks) (mean, low, high float64, ok bool) {
	count := 0
	for _, mhz := range clocks.mhz {
		if mhz <= 0 {
			continue
		}
		if count == 0 || mhz < low {
			low = mhz
		}
		high = max(high, mhz)
		mean += mhz
		count++
	}
	if count == 0 {
		return 0, 0, 0, false
	}
	return mean / float64(count), low, high, true
}

// formatClock prints a clock the way a spec sheet does: GHz with one decimal
// from 1 GHz, whole MHz below.
func formatClock(mhz float64) string {
	if mhz >= 1000 {
		return strconv.FormatFloat(mhz/1000, 'f', 1, 64) + " GHz"
	}
	return strconv.FormatFloat(mhz, 'f', 0, 64) + " MHz"
}

// clockText is the headline form of the clock reading: the mean when the
// cores agree within about 5%, otherwise the spread, so a chip with two
// cores at turbo and the rest parked is not reported as one number.
func clockText(clocks cpuClocks) string {
	mean, low, high, ok := clockSummary(clocks)
	if !ok {
		return ""
	}
	if high-low > mean*0.05 {
		return formatClock(low) + "–" + formatClock(high)
	}
	return formatClock(mean)
}

// cpuCountText names the count the way people say it: logical CPUs first,
// then physical cores and sockets when they add information.
func cpuCountText(inventory cpuInventory, long bool) string {
	if !inventory.ok || inventory.logical <= 0 {
		return ""
	}
	text := strconv.Itoa(inventory.logical) + " CPU"
	if inventory.logical != 1 {
		text += "s"
	}
	if !long {
		return text
	}
	if inventory.cores > 0 && inventory.cores != inventory.logical {
		text += " · " + strconv.Itoa(inventory.cores) + " cores"
		if inventory.logical%inventory.cores == 0 && inventory.logical/inventory.cores > 1 {
			text += " × " + strconv.Itoa(inventory.logical/inventory.cores) + " threads"
		}
	}
	if inventory.sockets > 1 {
		text += " · " + strconv.Itoa(inventory.sockets) + " sockets"
	}
	return text
}

// cpuInventoryLabel is the compact "6 CPUs · 2.9 GHz" that fits a card title
// or the masthead. Either half is omitted when it is unknown, and an empty
// string means the panel has nothing to add.
func cpuInventoryLabel(usage hostUtilisation) string {
	parts := []string{}
	if count := cpuCountText(usage.cpus, false); count != "" {
		parts = append(parts, count)
	}
	if clock := clockText(usage.clocks); clock != "" {
		parts = append(parts, clock)
	}
	return strings.Join(parts, " · ")
}

// cpuInventoryDetail is the long form for the activity view and the process
// card: counts, model, current clock and the advertised range with its source.
func cpuInventoryDetail(usage hostUtilisation) string {
	parts := []string{}
	if count := cpuCountText(usage.cpus, true); count != "" {
		parts = append(parts, count)
	}
	if usage.cpus.model != "" {
		parts = append(parts, usage.cpus.model)
	}
	if clock := clockText(usage.clocks); clock != "" {
		clock += " now"
		if usage.cpus.maxMHz > 0 {
			clock += " · " + formatClock(usage.cpus.minMHz) + "–" + formatClock(usage.cpus.maxMHz) + " range"
		}
		parts = append(parts, clock+" · "+clockSourceNote(usage.clocks.source))
	} else if usage.cpus.ok {
		parts = append(parts, "no clock reading · cpufreq and /proc/cpuinfo expose none")
	}
	return strings.Join(parts, " · ")
}

func clockSourceNote(source string) string {
	switch source {
	case "cpufreq":
		return "cpufreq"
	case "cpuinfo":
		return "/proc/cpuinfo · nominal inside a VM"
	case "sysctl":
		return "sysctl nominal"
	}
	return ""
}

// coreStrip draws one bar per logical CPU at its busy share, coloured by the
// shared severity ramp, and returns "" when the strip would not fit in width.
// The strip is trusted text: the glyphs are fixed and the tags are ours.
func coreStrip(p palette, shares []float64, width int, mode string) string {
	if len(shares) == 0 || len(shares) > width {
		return ""
	}
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	if graphMode(mode) == "braille" {
		blocks = []rune(" ⡀⡄⡆⡇⣇⣧⣷⣿")
	}
	var out strings.Builder
	for _, share := range shares {
		if share < 0 {
			out.WriteString("[" + p.muted + "]·[-]")
			continue
		}
		// An idle core is still a core: it is drawn at the lowest level in the
		// muted hue, so only a missing reading leaves a dot.
		level := max(1, min(8, int(share/100*8)))
		glyph, hue := string(blocks[level]), pressureHue(p, int(share))
		if share == 0 {
			hue = p.muted
		}
		if graphMode(mode) == "ascii" {
			glyph = strconv.Itoa(min(9, int(share/10)))
		}
		out.WriteString("[" + hue + "]" + glyph + "[-]")
	}
	return out.String()
}

// coreStripPlain is the escaped-text summary for places without colour: how
// many cores were busy past the amber threshold at the last sample.
func coreStripPlain(shares []float64) string {
	if len(shares) == 0 {
		return ""
	}
	busy, counted := 0, 0
	for _, share := range shares {
		if share < 0 {
			continue
		}
		counted++
		if share >= 70 {
			busy++
		}
	}
	if counted == 0 {
		return ""
	}
	return strconv.Itoa(busy) + "/" + strconv.Itoa(counted) + " CPUs above 70%"
}
