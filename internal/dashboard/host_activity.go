package dashboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// Process Activity digs one level below the process table: which threads are
// busy, how often the process yields or is preempted, how fast it faults and
// does I/O, what it holds open, who its children are and what it has written
// to the journal. Everything comes from /proc and journalctl, sampled for one
// PID, so it is cheap enough to refresh every two seconds while open.

// Linux user-space ticks are 100 Hz on every mainstream architecture; Go has
// no cgo-free sysconf, and /proc/PID/stat times are reported in these ticks.
const clockTicksPerSecond = 100

type threadSample struct {
	TID         int
	Name, State string
	Ticks       uint64
	// CPU is the logical CPU the thread last ran on (stat field 39).
	CPU int
}

type activitySample struct {
	At                             time.Time
	Comm, State                    string
	Threads, Nice                  int
	StartTime                      uint64
	UserTicks, SystemTicks         uint64
	MinorFaults, MajorFaults       uint64
	RSSKiB, SwapKiB                int64
	VoluntarySwitches, Preemptions uint64
	IOOK                           bool
	IONote                         string
	ReadBytes, WriteBytes          uint64
	ReadChars, WriteChars          uint64
	FDOK                           bool
	FDNote                         string
	FDTotal, FDSockets             int
	FDFiles, FDPipes, FDOther      int
	FDDeleted                      int
	Executable, WorkingDir, Unit   string
	Tasks                          []threadSample
	// LastCPU is the logical CPU the main thread last ran on and CPUsAllowed
	// the affinity mask as a list, so a process pinned or confined by a
	// cgroup cpuset is seen as such rather than as one that will not spread.
	LastCPU     int
	CPUsAllowed string
}

// parseTaskStat reads one /proc/PID/stat or /proc/PID/task/TID/stat line. The
// command name may contain spaces and parentheses, so fields are located from
// the last closing parenthesis.
func parseTaskStat(line string) (comm string, fields []string, err error) {
	open, close := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
	if open < 0 || close < open {
		return "", nil, fmt.Errorf("unrecognized stat line")
	}
	fields = strings.Fields(line[close+1:])
	if len(fields) < 20 {
		return "", nil, fmt.Errorf("stat line has %d fields", len(fields))
	}
	return line[open+1 : close], fields, nil
}

func statUint(fields []string, index int) uint64 {
	// index is the documented 1-based field number; fields starts at field 3.
	i := index - 3
	if i < 0 || i >= len(fields) {
		return 0
	}
	n, _ := strconv.ParseUint(fields[i], 10, 64)
	return n
}

func statInt(fields []string, index int) int {
	i := index - 3
	if i < 0 || i >= len(fields) {
		return 0
	}
	n, _ := strconv.Atoi(fields[i])
	return n
}

func parseProcStatus(raw string, sample *activitySample) {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		number := func() int64 {
			n, _ := strconv.ParseInt(strings.Fields(value + " 0")[0], 10, 64)
			return n
		}
		switch key {
		case "State":
			sample.State = value
		case "VmRSS":
			sample.RSSKiB = number()
		case "VmSwap":
			sample.SwapKiB = number()
		case "voluntary_ctxt_switches":
			sample.VoluntarySwitches = uint64(number())
		case "nonvoluntary_ctxt_switches":
			sample.Preemptions = uint64(number())
		case "Cpus_allowed_list":
			sample.CPUsAllowed = value
		}
	}
}

func parseProcIO(raw string, sample *activitySample) {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "rchar":
			sample.ReadChars = n
		case "wchar":
			sample.WriteChars = n
		case "read_bytes":
			sample.ReadBytes = n
		case "write_bytes":
			sample.WriteBytes = n
		}
	}
	sample.IOOK = true
}

// classifyDescriptors counts what the descriptor targets point at.
func classifyDescriptors(targets []string, sample *activitySample) {
	sample.FDOK = true
	sample.FDTotal = len(targets)
	for _, target := range targets {
		switch {
		case strings.HasPrefix(target, "socket:"):
			sample.FDSockets++
		case strings.HasPrefix(target, "pipe:"):
			sample.FDPipes++
		case strings.HasPrefix(target, "/"):
			sample.FDFiles++
			if strings.HasSuffix(target, " (deleted)") {
				sample.FDDeleted++
			}
		default:
			sample.FDOther++
		}
	}
}

func cgroupUnit(raw string) string {
	unit := ""
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		parts := strings.Split(line, "/")
		part := parts[len(parts)-1]
		if strings.HasSuffix(part, ".service") || strings.HasSuffix(part, ".scope") {
			unit = part
		}
	}
	return unit
}

// sampleProcessActivity reads one process from procRoot (normally /proc).
// Fields another user's process does not expose are reported as such rather
// than as zero.
func sampleProcessActivity(procRoot string, pid int) (activitySample, error) {
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	sample := activitySample{At: time.Now()}
	raw, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return sample, err
	}
	comm, fields, err := parseTaskStat(strings.TrimSpace(string(raw)))
	if err != nil {
		return sample, err
	}
	sample.Comm = comm
	sample.State = fields[0]
	sample.MinorFaults, sample.MajorFaults = statUint(fields, 10), statUint(fields, 12)
	sample.UserTicks, sample.SystemTicks = statUint(fields, 14), statUint(fields, 15)
	sample.Nice, sample.Threads = statInt(fields, 19), statInt(fields, 20)
	sample.StartTime = statUint(fields, 22)
	sample.LastCPU = statInt(fields, 39)
	if raw, err := os.ReadFile(filepath.Join(dir, "status")); err == nil {
		parseProcStatus(string(raw), &sample)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "io")); err == nil {
		parseProcIO(string(raw), &sample)
	} else {
		sample.IONote = permissionNote(err, "I/O counters")
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "fd")); err == nil {
		targets := make([]string, 0, len(entries))
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(dir, "fd", entry.Name()))
			if err != nil {
				target = "unknown"
			}
			targets = append(targets, target)
		}
		classifyDescriptors(targets, &sample)
	} else {
		sample.FDNote = permissionNote(err, "open descriptors")
	}
	if target, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
		sample.Executable = target
	}
	if target, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
		sample.WorkingDir = target
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
		sample.Unit = cgroupUnit(string(raw))
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "task")); err == nil {
		for _, entry := range entries {
			tid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, "task", entry.Name(), "stat"))
			if err != nil {
				continue
			}
			name, fields, err := parseTaskStat(strings.TrimSpace(string(raw)))
			if err != nil {
				continue
			}
			sample.Tasks = append(sample.Tasks, threadSample{TID: tid, Name: name, State: fields[0], Ticks: statUint(fields, 14) + statUint(fields, 15), CPU: statInt(fields, 39)})
		}
	}
	return sample, nil
}

func permissionNote(err error, what string) string {
	if errors.Is(err, os.ErrPermission) {
		return what + " belong to another user · not permitted without elevation"
	}
	return what + " unavailable · " + firstLine(err.Error())
}

// activityRates derives per-second rates between two samples of the same
// process. Rates need two samples; the first render says so instead of
// showing zero.
type activityRates struct {
	Valid                          bool
	Seconds                        float64
	CPU, UserCPU, SystemCPU        float64
	MinorFaults, MajorFaults       float64
	VoluntarySwitches, Preemptions float64
	ReadBytes, WriteBytes          float64
	ReadChars, WriteChars          float64
	ThreadCPU                      map[int]float64
}

func computeActivityRates(previous, current *activitySample) activityRates {
	rates := activityRates{ThreadCPU: map[int]float64{}}
	if previous == nil || current == nil || !current.At.After(previous.At) || previous.StartTime != current.StartTime {
		return rates
	}
	seconds := current.At.Sub(previous.At).Seconds()
	if seconds <= 0 {
		return rates
	}
	delta := func(a, b uint64) float64 {
		if b < a {
			return 0
		}
		return float64(b-a) / seconds
	}
	rates.Valid, rates.Seconds = true, seconds
	rates.UserCPU = delta(previous.UserTicks, current.UserTicks) / clockTicksPerSecond * 100
	rates.SystemCPU = delta(previous.SystemTicks, current.SystemTicks) / clockTicksPerSecond * 100
	rates.CPU = rates.UserCPU + rates.SystemCPU
	rates.MinorFaults = delta(previous.MinorFaults, current.MinorFaults)
	rates.MajorFaults = delta(previous.MajorFaults, current.MajorFaults)
	rates.VoluntarySwitches = delta(previous.VoluntarySwitches, current.VoluntarySwitches)
	rates.Preemptions = delta(previous.Preemptions, current.Preemptions)
	if previous.IOOK && current.IOOK {
		rates.ReadBytes = delta(previous.ReadBytes, current.ReadBytes)
		rates.WriteBytes = delta(previous.WriteBytes, current.WriteBytes)
		rates.ReadChars = delta(previous.ReadChars, current.ReadChars)
		rates.WriteChars = delta(previous.WriteChars, current.WriteChars)
	}
	before := map[int]uint64{}
	for _, task := range previous.Tasks {
		before[task.TID] = task.Ticks
	}
	for _, task := range current.Tasks {
		if ticks, ok := before[task.TID]; ok {
			rates.ThreadCPU[task.TID] = delta(ticks, task.Ticks) / clockTicksPerSecond * 100
		}
	}
	return rates
}

func processJournal(ctx context.Context, pid, lines int) string {
	if usesLaunchd() {
		return "journal lookup is Linux-only; use the unified log for macOS processes"
	}
	output, err := command(ctx, "journalctl", "_PID="+strconv.Itoa(pid), "-n", strconv.Itoa(lines), "--no-pager", "--output=short-iso")
	if err != nil {
		return "journal unavailable · " + firstLine(strings.TrimPrefix(err.Error(), "journalctl: "))
	}
	if strings.TrimSpace(output) == "" || strings.HasPrefix(output, "-- No entries --") {
		return "no journal entries carry this PID · a daemon logging through its service unit still appears under s service"
	}
	return strings.TrimRight(output, "\n")
}

func perSecond(value float64, unit string) string {
	switch {
	case value >= 100:
		return fmt.Sprintf("%.0f%s/s", value, unit)
	case value >= 10:
		return fmt.Sprintf("%.1f%s/s", value, unit)
	}
	return fmt.Sprintf("%.2f%s/s", value, unit)
}

func bytesPerSecond(value float64) string {
	return hostBytes(int64(value), false) + "/s"
}

func stateWord(state string) string {
	switch strings.TrimSpace(strings.SplitN(state, " ", 2)[0]) {
	case "R":
		return "running"
	case "S":
		return "sleeping · interruptible"
	case "D":
		return "waiting on disk or device · uninterruptible"
	case "T", "t":
		return "stopped"
	case "Z":
		return "zombie"
	case "I":
		return "idle kernel thread"
	case "X":
		return "dead"
	}
	return state
}

// renderProcessActivity lays out one refresh of the view. Untrusted values
// (command names, paths, journal lines) are escaped; colour tags are trusted.
func renderProcessActivity(p palette, glyphs string, process hostProcess, previous, current *activitySample, history []float64, journal string, children []hostProcess, paused bool, note string, host hostUtilisation) string {
	var out strings.Builder
	hue := panelHue(p, 3)
	title := strings.TrimSpace(process.Command)
	if current != nil && current.Comm != "" && !strings.Contains(title, current.Comm) {
		title = strings.TrimSuffix(current.Comm+" · "+title, " · ")
	}
	fmt.Fprintf(&out, "[%s::b]%s[-::-]\n", hue, tview.Escape(clean(title)))
	if note != "" {
		fmt.Fprintf(&out, "[%s::b]%s[-::-]\n\n", p.error, tview.Escape(clean(note)))
	}
	if current == nil {
		fmt.Fprintf(&out, "[%s]Reading /proc/%d…[-]\n", p.muted, process.PID)
		return out.String()
	}
	rates := computeActivityRates(previous, current)
	stamp := "sampled " + current.At.Format("15:04:05") + " · every 2s"
	if paused {
		stamp = "PAUSED · Space resumes · sampled " + current.At.Format("15:04:05")
	}
	fmt.Fprintf(&out, "[%s]PID %d · %s · %s[-]\n\n", p.muted, process.PID, tview.Escape(clean(process.User)), stamp)
	section := func(name string) { fmt.Fprintf(&out, "[%s::b]── %s[-::-]\n", hue, name) }
	field := func(label, value string) { fmt.Fprintf(&out, "  [%s]%-10s[-] %s\n", p.muted, label, value) }
	muted := func(text string) string { return fmt.Sprintf("[%s]%s[-]", p.muted, tview.Escape(clean(text))) }

	section("VITALS")
	stateHue := p.muted
	if strings.HasPrefix(current.State, "R") {
		stateHue = p.success
	}
	if strings.HasPrefix(current.State, "D") || strings.HasPrefix(current.State, "Z") {
		stateHue = p.warning
	}
	field("State", fmt.Sprintf("[%s::b]%s[-::-]  %s", stateHue, tview.Escape(clean(stateWord(current.State))), muted(fmt.Sprintf("%d threads · nice %d", current.Threads, current.Nice))))
	logical := host.cpus.logical
	if rates.Valid {
		cpuHue := pressureHue(p, int(rates.CPU))
		scale := "100% = one logical CPU"
		if logical > 1 {
			scale = fmt.Sprintf("100%% = one of %d logical CPUs · %.1f%% of the machine", logical, rates.CPU/float64(logical))
		}
		field("CPU", fmt.Sprintf("[%s::b]%5.1f%%[-::-]  [%s]%s[-]  %s", cpuHue, rates.CPU, hue, signalChart(history, glyphs), muted(fmt.Sprintf("user %.1f%% · system %.1f%% · %s", rates.UserCPU, rates.SystemCPU, scale))))
	} else {
		field("CPU", muted("rates need a second sample · ps estimate "+fmt.Sprintf("%.1f%%", max(0, process.CPU))))
	}
	// Where the process runs, on what: the CPU it was last scheduled on and
	// that CPU's clock and busy share, the set it is allowed to use, then the
	// machine's own inventory so the shares above have a denominator.
	placement := fmt.Sprintf("last on CPU %d", current.LastCPU)
	if current.LastCPU >= 0 && current.LastCPU < len(host.clocks.mhz) && host.clocks.mhz[current.LastCPU] > 0 {
		placement += " · " + formatClock(host.clocks.mhz[current.LastCPU])
	}
	if current.LastCPU >= 0 && current.LastCPU < len(host.coreBusy) && host.coreBusy[current.LastCPU] >= 0 {
		placement += fmt.Sprintf(" · that CPU %.0f%% busy", host.coreBusy[current.LastCPU])
	}
	if current.CPUsAllowed != "" {
		allowed := "allowed " + current.CPUsAllowed
		if logical > 0 && cpuListCount(current.CPUsAllowed) < logical {
			allowed = fmt.Sprintf("[%s]confined to CPUs %s[-]", p.warning, tview.Escape(clean(current.CPUsAllowed)))
		}
		placement += "  " + allowed
	}
	field("CPUs", placement)
	if detail := cpuInventoryDetail(host); detail != "" {
		field("Host", muted(detail))
	}
	memory := hostBytes(current.RSSKiB, true) + " RSS"
	if current.SwapKiB > 0 {
		memory += " · " + hostBytes(current.SwapKiB, true) + " swapped"
	}
	if rates.Valid {
		memory += "  " + muted(fmt.Sprintf("page faults %s · major %s", perSecond(rates.MinorFaults, ""), perSecond(rates.MajorFaults, "")))
	}
	field("Memory", memory)
	if rates.Valid {
		field("Switches", fmt.Sprintf("voluntary %s · preempted %s  %s", perSecond(rates.VoluntarySwitches, ""), perSecond(rates.Preemptions, ""), muted("voluntary = waits for I/O, locks or timers · preempted = wanted CPU and lost it")))
	}
	switch {
	case current.IOOK && rates.Valid:
		field("I/O", fmt.Sprintf("read %s · write %s  %s", bytesPerSecond(rates.ReadBytes), bytesPerSecond(rates.WriteBytes), muted(fmt.Sprintf("storage · syscalls read %s · write %s", bytesPerSecond(rates.ReadChars), bytesPerSecond(rates.WriteChars)))))
	case current.IOOK:
		field("I/O", muted(fmt.Sprintf("lifetime read %s · write %s · rates after the next sample", hostBytes(int64(current.ReadBytes), false), hostBytes(int64(current.WriteBytes), false))))
	default:
		field("I/O", muted(current.IONote))
	}
	if current.FDOK {
		fds := fmt.Sprintf("%d open  %s", current.FDTotal, muted(fmt.Sprintf("%d sockets · %d files · %d pipes · %d other", current.FDSockets, current.FDFiles, current.FDPipes, current.FDOther)))
		if current.FDDeleted > 0 {
			fds += fmt.Sprintf("  [%s]%d deleted-but-open[-]", p.warning, current.FDDeleted)
		}
		field("Files", fds)
	} else {
		field("Files", muted(current.FDNote))
	}
	if current.Unit != "" {
		field("Service", tview.Escape(clean(current.Unit))+"  "+muted("s opens it in Services"))
	}
	if current.Executable != "" {
		field("Executable", tview.Escape(clean(current.Executable)))
	}
	if current.WorkingDir != "" {
		field("Workdir", tview.Escape(clean(current.WorkingDir)))
	}
	if len(children) > 0 {
		names := make([]string, 0, len(children))
		for _, child := range children {
			if len(names) == 6 {
				names = append(names, fmt.Sprintf("+%d more", len(children)-6))
				break
			}
			names = append(names, fmt.Sprintf("%d %s", child.PID, shortCommand(child.Command)))
		}
		field("Children", tview.Escape(clean(strings.Join(names, " · "))))
	}

	out.WriteByte('\n')
	section("THREADS · busiest first")
	tasks := append([]threadSample(nil), current.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := rates.ThreadCPU[tasks[i].TID], rates.ThreadCPU[tasks[j].TID]
		if a != b {
			return a > b
		}
		if tasks[i].State != tasks[j].State {
			return tasks[i].State == "R"
		}
		return tasks[i].TID < tasks[j].TID
	})
	if len(tasks) == 0 {
		field("Threads", muted("no thread list readable"))
	}
	busy := 0
	for i, task := range tasks {
		if i == 12 {
			field("", muted(fmt.Sprintf("… %d more threads; the busiest are listed", len(tasks)-i)))
			break
		}
		cpu := "     —"
		if rates.Valid {
			share := rates.ThreadCPU[task.TID]
			cpu = fmt.Sprintf("[%s]%5.1f%%[-]", pressureHue(p, int(share)), share)
			if share >= 0.5 {
				busy++
			}
		}
		stateHue := p.muted
		if task.State == "R" {
			stateHue = p.success
		}
		if task.State == "D" {
			stateHue = p.warning
		}
		on := ""
		if logical > 1 {
			on = muted(fmt.Sprintf("  on CPU %d", task.CPU))
		}
		fmt.Fprintf(&out, "  [%s]%-10d[-] %-20s [%s]%s[-]  %s%s\n", p.muted, task.TID, tview.Escape(clean(task.Name)), stateHue, task.State, cpu, on)
	}
	if rates.Valid && len(tasks) > 0 {
		fmt.Fprintf(&out, "  %s\n", muted(fmt.Sprintf("%d of %d threads used CPU in the last sample", busy, len(tasks))))
	}

	out.WriteByte('\n')
	section("JOURNAL · newest last · _PID=" + strconv.Itoa(process.PID))
	if strings.Contains(journal, "\n") || strings.HasPrefix(journal, "20") {
		out.WriteString(richOutput(journal, 1, p))
	} else {
		fmt.Fprintf(&out, "  %s", muted(available(journal)))
	}
	out.WriteString("\n\n")
	fmt.Fprintf(&out, "[%s]Space pause · r sample now · T sysdig · f follow journal · K signals · n ports · s service · G graphics · Esc returns[-]\n", p.muted)
	return out.String()
}

// cpuListCount sizes a kernel CPU list such as "0-3,8,10-11". An unparsable
// list counts as covering everything, so nothing is called confined by
// mistake.
func cpuListCount(list string) int {
	count := 0
	for _, part := range strings.Split(strings.TrimSpace(list), ",") {
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(lo)
		if err != nil {
			return int(^uint(0) >> 1)
		}
		end := start
		if isRange {
			if end, err = strconv.Atoi(hi); err != nil || end < start {
				return int(^uint(0) >> 1)
			}
		}
		count += end - start + 1
	}
	return count
}

func shortCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command
	}
	name := filepath.Base(fields[0])
	if len([]rune(name)) > 24 {
		return string([]rune(name)[:23]) + "…"
	}
	return name
}
