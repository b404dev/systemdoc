package dashboard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hostProcess struct {
	PID, PPID                     int
	User, State, Elapsed, Command string
	CPU                           float64
	RSS                           int64 // KiB; -1 means unavailable.
	// RateCPU is true when CPU is the share of one logical CPU used between the
	// previous poll and this one (Linux /proc sampling). When false, CPU is the
	// ps pcpu value: total CPU time divided by the process's lifetime.
	RateCPU bool
	// LastCPU is the logical CPU the process was last scheduled on, from
	// /proc/PID/stat on Linux; -1 where that is not read (macOS, vanished
	// PIDs). It says where the process ran, not where it is allowed to.
	LastCPU int
}

// joinNote concatenates the non-empty diagnostics with the status separator.
func joinNote(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, strings.TrimSpace(part))
		}
	}
	return strings.Join(kept, " · ")
}

// skippedNote describes tolerated parser rows; "" when nothing was skipped.
func skippedNote(skipped int, what string) string {
	if skipped <= 0 {
		return ""
	}
	return fmt.Sprintf("%d unrecognised %s skipped", skipped, what)
}

// allRowsFailed is the tolerant parsers' shared failure rule: a snapshot is
// rejected only when it had rows and none of them parsed.
func allRowsFailed(parsed, skipped int, what string) error {
	if parsed == 0 && skipped > 0 {
		return fmt.Errorf("no %s could be parsed (%d unrecognised)", what, skipped)
	}
	return nil
}

// Split fixed leading columns while retaining spaces in commands and paths.
func leadingFields(line string, count int) ([]string, string) {
	var fields []string
	for len(fields) < count {
		line = strings.TrimLeft(line, " \t")
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			return fields, line
		}
		fields = append(fields, line[:i])
		line = line[i:]
	}
	return fields, strings.TrimLeft(line, " \t")
}

// parseProcesses keeps the historical signature: it fails only when the
// snapshot had rows and none of them parsed.
func parseProcesses(raw string) ([]hostProcess, error) {
	rows, _, err := parseProcessesTolerant(raw)
	return rows, err
}

// parseProcessesTolerant skips rows it cannot read (a truncated line, a
// duplicate PID from a racing ps) and reports how many, so one odd row no
// longer blanks the whole Process Explorer.
func parseProcessesTolerant(raw string) (rows []hostProcess, skipped int, err error) {
	seen := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f, command := leadingFields(line, 7)
		if len(f) != 7 || command == "" {
			skipped++
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		cpu, e3 := strconv.ParseFloat(f[3], 64)
		rss, e4 := strconv.ParseInt(f[4], 10, 64)
		if f[3] == "-" {
			cpu, e3 = -1, nil
		}
		if f[4] == "-" {
			rss, e4 = -1, nil
		}
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || pid < 0 || ppid < 0 || cpu < -1 || rss < -1 || math.IsNaN(cpu) || math.IsInf(cpu, 0) || seen[pid] {
			skipped++
			continue
		}
		seen[pid] = true
		rows = append(rows, hostProcess{PID: pid, PPID: ppid, User: f[2], State: f[5], Elapsed: f[6], Command: clean(command), CPU: cpu, RSS: rss, LastCPU: -1})
	}
	return rows, skipped, allRowsFailed(len(rows), skipped, "ps rows")
}

func collectProcesses(ctx context.Context) ([]hostProcess, error) {
	raw, diagnostic, err := networkCommand(ctx, "ps", "-axww", "-o", "pid=,ppid=,user=,pcpu=,rss=,stat=,etime=,args=")
	if err != nil {
		return nil, fmt.Errorf("ps: %w %s", err, diagnostic)
	}
	rows, _, err := parseProcessesTolerant(raw)
	if err != nil {
		return nil, err
	}
	rows = omitProcessCollector(rows, os.Getpid())
	processCPURates(rows)
	return rows, nil
}

// processCPUSample is one /proc/PID/stat reading kept between polls.
type processCPUSample struct {
	ticks, start uint64
	at           time.Time
}

// processCPUSampler turns ps's lifetime-average pcpu into an interval rate.
// ps reports cputime/elapsed, so a process that was busy an hour ago outranks
// one busy now; comparing /proc/PID/stat ticks between polls fixes that.
// The map is keyed by PID and validated by start time so a reused PID never
// inherits another process's counters.
type processCPUSampler struct {
	mu      sync.Mutex
	root    string
	samples map[int]processCPUSample
}

var processCPU = newProcessCPUSampler("/proc")

func newProcessCPUSampler(root string) *processCPUSampler {
	return &processCPUSampler{root: root, samples: map[int]processCPUSample{}}
}

// apply rewrites CPU for every row that has a comparable previous sample and
// sets RateCPU on it. First-seen processes, restarted PIDs and unreadable stat
// files keep the ps estimate so the table is never blank. Entries for PIDs
// absent from rows are pruned.
func (s *processCPUSampler) apply(rows []hostProcess, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[int]processCPUSample, len(rows))
	for i := range rows {
		raw, err := os.ReadFile(filepath.Join(s.root, strconv.Itoa(rows[i].PID), "stat"))
		if err != nil {
			continue
		}
		_, fields, err := parseTaskStat(strings.TrimSpace(string(raw)))
		if err != nil {
			continue
		}
		current := processCPUSample{ticks: statUint(fields, 14) + statUint(fields, 15), start: statUint(fields, 22), at: now}
		next[rows[i].PID] = current
		rows[i].LastCPU = statInt(fields, 39)
		previous, ok := s.samples[rows[i].PID]
		if !ok || previous.start != current.start || !now.After(previous.at) || current.ticks < previous.ticks {
			continue
		}
		seconds := now.Sub(previous.at).Seconds()
		rows[i].CPU = float64(current.ticks-previous.ticks) / clockTicksPerSecond / seconds * 100
		rows[i].RateCPU = true
	}
	s.samples = next
}

func omitProcessCollector(rows []hostProcess, parentPID int) []hostProcess {
	result := rows[:0]
	for _, row := range rows {
		fields := strings.Fields(row.Command)
		if row.PPID == parentPID && len(fields) > 0 && filepath.Base(fields[0]) == "ps" {
			continue
		}
		result = append(result, row)
	}
	return result
}

func matchesProcess(p hostProcess, query string) bool {
	for _, term := range strings.Fields(strings.ToLower(query)) {
		key, value, field := strings.Cut(term, ":")
		actual := fmt.Sprintf("%d %d %s %s %s", p.PID, p.PPID, p.User, p.State, p.Command)
		if field {
			switch key {
			case "pid":
				if strconv.Itoa(p.PID) != value {
					return false
				}
				continue
			case "ppid":
				if strconv.Itoa(p.PPID) != value {
					return false
				}
				continue
			case "user":
				actual = p.User
			case "state":
				actual = p.State
			case "command", "process":
				actual = p.Command
			default:
				value = term
			}
		} else {
			value = term
		}
		if !strings.Contains(strings.ToLower(actual), value) {
			return false
		}
	}
	return true
}

type processBranch struct {
	Process hostProcess
	Depth   int
}

func processTree(rows []hostProcess) []processBranch {
	rows = append([]hostProcess(nil), rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].PID < rows[j].PID })
	byPID := map[int]hostProcess{}
	children := map[int][]hostProcess{}
	for _, p := range rows {
		byPID[p.PID] = p
		children[p.PPID] = append(children[p.PPID], p)
	}
	var result []processBranch
	visited := map[int]bool{}
	walk := func(root hostProcess) {
		stack := []processBranch{{Process: root}}
		for len(stack) > 0 {
			b := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[b.Process.PID] {
				continue
			}
			visited[b.Process.PID] = true
			result = append(result, b)
			kids := children[b.Process.PID]
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, processBranch{kids[i], b.Depth + 1})
			}
		}
	}
	for _, p := range rows {
		if _, ok := byPID[p.PPID]; !ok || p.PPID == p.PID {
			walk(p)
		}
	}
	// PID reuse or a changing snapshot may leave cycles; keep every row visible.
	for _, p := range rows {
		if !visited[p.PID] {
			walk(p)
		}
	}
	return result
}

type hostMount struct {
	Source, Path                 string
	Size, Used, Free             int64 // KiB
	Percent                      int
	Inodes, InodeUsed, InodeFree int64
	InodePercent                 int // -1 = unavailable / filesystem does not report inodes.
}

func (m hostMount) key() string { return m.Source + "\x00" + m.Path }

var dfRow = regexp.MustCompile(`^(.+?)\s+([0-9]+|-)\s+([0-9]+|-)\s+(-?[0-9]+|-)\s+([0-9]+%|-)\s+(.+)$`)

func dfNumber(s string) (int64, error) {
	if s == "-" {
		return -1, nil
	}
	return strconv.ParseInt(strings.TrimSuffix(s, "%"), 10, 64)
}

// parseMounts keeps the historical signature: a missing header, or a table
// whose every row is unreadable, is an error; a single odd row is skipped.
func parseMounts(raw, platform string, inodeOnly bool) ([]hostMount, error) {
	rows, _, err := parseMountsTolerant(raw, platform, inodeOnly)
	return rows, err
}

// parseMountsTolerant reads POSIX df output and skips rows it cannot read.
func parseMountsTolerant(raw, platform string, inodeOnly bool) (rows []hostMount, skipped int, err error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Filesystem") {
		return nil, 0, fmt.Errorf("missing df header")
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m, ok := parseDFRow(strings.TrimSpace(line), platform, inodeOnly)
		if !ok {
			skipped++
			continue
		}
		rows = append(rows, m)
	}
	return rows, skipped, allRowsFailed(len(rows), skipped, "df rows")
}

func parseDFRow(line, platform string, inodeOnly bool) (hostMount, bool) {
	f := dfRow.FindStringSubmatch(line)
	if f == nil {
		return hostMount{}, false
	}
	values := make([]int64, 4)
	for i := range values {
		v, err := dfNumber(f[i+2])
		if err != nil {
			return hostMount{}, false
		}
		values[i] = v
	}
	m := hostMount{Source: clean(f[1]), Path: clean(f[6]), Size: values[0], Used: values[1], Free: values[2], Percent: int(values[3]), InodePercent: -1, Inodes: -1, InodeUsed: -1, InodeFree: -1}
	if inodeOnly {
		m.Inodes, m.InodeUsed, m.InodeFree, m.InodePercent = m.Size, m.Used, m.Free, m.Percent
	} else if platform == "darwin" {
		inodes, path := leadingFields(f[6], 3)
		if len(inodes) != 3 || path == "" {
			return hostMount{}, false
		}
		for i, s := range inodes {
			v, err := dfNumber(s)
			if err != nil {
				return hostMount{}, false
			}
			values[i] = v
		}
		m.InodeUsed, m.InodeFree, m.InodePercent, m.Path = values[0], values[1], int(values[2]), clean(path)
		if values[0] >= 0 && values[1] >= 0 && values[0] <= math.MaxInt64-values[1] {
			m.Inodes = values[0] + values[1]
		}
	}
	if m.Inodes == 0 {
		m.InodePercent = -1
	}
	return m, true
}

// mountEntry is one row of /proc/self/mountinfo that Storage should show.
type mountEntry struct {
	Source, Path, Type string
}

// mountUsage is the platform-neutral subset of statfs(2) the Storage view
// needs. BlockSize is the fragment size df uses (f_frsize).
type mountUsage struct {
	BlockSize, Blocks, Bfree, Bavail, Files, Ffree uint64
}

// pseudoFilesystems are kernel interfaces, not storage. tmpfs, devtmpfs and
// efivarfs are deliberately absent because df lists them.
var pseudoFilesystems = map[string]bool{
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "configfs": true,
	"fusectl": true, "pstore": true, "bpf": true, "autofs": true, "mqueue": true,
	"hugetlbfs": true, "binfmt_misc": true, "rpc_pipefs": true, "nsfs": true,
	"selinuxfs": true, "fuse.portal": true, "fuse.gvfsd-fuse": true, "ramfs": true,
}

// unescapeMountField decodes the octal escapes (\040 space, \011 tab, \012
// newline, \134 backslash) the kernel uses for paths in mountinfo.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			out.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// parseMountInfo lists the mounts df would show, in mount order. Pseudo
// filesystems are dropped, and when several mounts share a mount point only
// the last one is kept, because it is the one shadowing the others.
func parseMountInfo(raw string) []mountEntry {
	var entries []mountEntry
	index := map[string]int{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+2 >= len(fields) {
			continue
		}
		entry := mountEntry{Path: unescapeMountField(fields[4]), Type: fields[separator+1], Source: unescapeMountField(fields[separator+2])}
		if pseudoFilesystems[entry.Type] || entry.Path == "" {
			continue
		}
		if i, ok := index[entry.Path]; ok {
			entries[i] = entry
			continue
		}
		index[entry.Path] = len(entries)
		entries = append(entries, entry)
	}
	return entries
}

// dfPercent reproduces df's Capacity column: used/(used+avail) rounded up,
// or -1 when there is nothing to divide by.
func dfPercent(used, avail int64) int {
	if used < 0 || avail < 0 || used+avail <= 0 {
		return -1
	}
	return int((used*100 + used + avail - 1) / (used + avail))
}

// mount converts a statfs reading into the KiB-based row df would print.
func (u mountUsage) mount(entry mountEntry) hostMount {
	kib := func(blocks uint64) int64 { return int64(blocks * u.BlockSize / 1024) }
	m := hostMount{Source: clean(entry.Source), Path: clean(entry.Path), Size: kib(u.Blocks), Used: kib(u.Blocks - min(u.Blocks, u.Bfree)), Free: kib(u.Bavail)}
	m.Percent = dfPercent(m.Used, m.Free)
	m.Inodes, m.InodeFree, m.InodeUsed = int64(u.Files), int64(u.Ffree), int64(u.Files-min(u.Files, u.Ffree))
	m.InodePercent = -1
	if u.Files > 0 {
		m.InodePercent = dfPercent(m.InodeUsed, m.InodeFree)
	}
	return m
}

// statMounts reads every mount concurrently. Each mount gets its own
// goroutine so a stale NFS or FUSE mount that never answers statfs cannot hang
// the poll: after timeout it is reported as unreachable and omitted, and its
// goroutine is abandoned (a blocked statfs cannot be interrupted; the leak is
// bounded by the number of stuck mounts, and it returns whenever the kernel
// finally gives up). Mounts with no blocks are dropped, as df does without -a.
func statMounts(ctx context.Context, entries []mountEntry, timeout time.Duration, stat func(string) (mountUsage, error)) ([]hostMount, string) {
	type reply struct {
		usage mountUsage
		err   error
	}
	replies := make([]chan reply, len(entries))
	for i, entry := range entries {
		replies[i] = make(chan reply, 1)
		go func(path string, out chan reply) {
			usage, err := stat(path)
			out <- reply{usage, err}
		}(entry.Path, replies[i])
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var rows []hostMount
	var unreachable, unreadable []string
	accept := func(entry mountEntry, r reply) {
		if r.err != nil {
			unreadable = append(unreadable, entry.Path)
			return
		}
		if r.usage.Blocks == 0 {
			return
		}
		rows = append(rows, r.usage.mount(entry))
	}
	expired := false
	for i, entry := range entries {
		if expired {
			// The shared deadline has passed: take whatever has already
			// answered, report the rest without waiting.
			select {
			case r := <-replies[i]:
				accept(entry, r)
			default:
				unreachable = append(unreachable, entry.Path)
			}
			continue
		}
		select {
		case r := <-replies[i]:
			accept(entry, r)
		case <-deadline.C:
			expired = true
			unreachable = append(unreachable, entry.Path)
		case <-ctx.Done():
			expired = true
			unreachable = append(unreachable, entry.Path)
		}
	}
	var notes []string
	if len(unreachable) > 0 {
		notes = append(notes, "unreachable: "+strings.Join(unreachable, ", "))
	}
	if len(unreadable) > 0 {
		notes = append(notes, "unreadable: "+strings.Join(unreadable, ", "))
	}
	return rows, joinNote(notes...)
}

// mountStatTimeout bounds how long one poll waits for any single mount.
const mountStatTimeout = 2 * time.Second

func collectMounts(ctx context.Context, platform string) ([]hostMount, string, error) {
	if platform != "darwin" && nativeMountsSupported {
		raw, err := os.ReadFile("/proc/self/mountinfo")
		if err != nil {
			return nil, "", fmt.Errorf("mountinfo: %w", err)
		}
		entries := parseMountInfo(string(raw))
		if len(entries) == 0 {
			return nil, "", fmt.Errorf("mountinfo listed no filesystems")
		}
		rows, note := statMounts(ctx, entries, mountStatTimeout, statfsMount)
		if len(rows) == 0 && ctx.Err() != nil {
			return nil, note, ctx.Err()
		}
		return rows, note, nil
	}
	args := []string{"-Pk"}
	if platform == "darwin" {
		args = []string{"-Pki"}
	}
	raw, diagnostic, err := networkCommand(ctx, "df", args...)
	if err != nil {
		return nil, "", fmt.Errorf("df: %w %s", err, diagnostic)
	}
	rows, skipped, err := parseMountsTolerant(raw, platform, false)
	note := joinNote(diagnostic, skippedNote(skipped, "df rows"))
	if err != nil || platform == "darwin" {
		return rows, note, err
	}
	raw, inodeNote, inodeErr := networkCommand(ctx, "df", "-Pi")
	if inodeErr != nil {
		return rows, joinNote(note, "Inodes unavailable: "+inodeErr.Error()+" "+inodeNote), nil
	}
	inodes, inodeSkipped, inodeErr := parseMountsTolerant(raw, platform, true)
	if inodeErr != nil {
		return rows, joinNote(note, inodeErr.Error()), nil
	}
	index := map[string]hostMount{}
	for _, m := range inodes {
		index[m.key()] = m
	}
	for i, m := range rows {
		if v, ok := index[m.key()]; ok {
			rows[i].Inodes, rows[i].InodeUsed, rows[i].InodeFree, rows[i].InodePercent = v.Inodes, v.InodeUsed, v.InodeFree, v.InodePercent
		}
	}
	return rows, joinNote(note, skippedNote(inodeSkipped, "df -i rows")), nil
}

type deletedFile struct {
	PID                                    int
	Process, User, FD, Device, Inode, Path string
	Size                                   int64
}

func (f deletedFile) key() string { return fmt.Sprintf("%d/%s/%s/%s", f.PID, f.FD, f.Device, f.Inode) }

// parseDeletedFiles keeps the historical signature: output that is not lsof
// field format, or whose every record is unreadable, is an error.
func parseDeletedFiles(raw string) ([]deletedFile, error) {
	rows, _, err := parseDeletedFilesTolerant(raw)
	return rows, err
}

// parseDeletedFilesTolerant skips a process record with a bad PID (and the
// files under it), a file record with no owner, and a file with an unreadable
// size, counting each once, so one odd record cannot blank the view.
func parseDeletedFilesTolerant(raw string) (rows []deletedFile, skipped int, err error) {
	if strings.TrimSpace(raw) != "" && (!strings.Contains(raw, "\x00") || !strings.HasPrefix(strings.TrimLeft(raw, "\n"), "p")) {
		return nil, 0, fmt.Errorf("unrecognized lsof field output")
	}
	owner := deletedFile{}
	badOwner := false
	file := deletedFile{Size: -1}
	kind := ""
	flush := func() {
		if file.FD != "" && kind == "REG" {
			rows = append(rows, file)
		}
	}
	drop := func() {
		if file.FD != "" {
			skipped++
		}
		file = deletedFile{Size: -1}
	}
	for _, field := range strings.Split(raw, "\x00") {
		field = strings.TrimLeft(field, "\n")
		if field == "" {
			continue
		}
		v := field[1:]
		switch field[0] {
		case 'p':
			flush()
			pid, err := strconv.Atoi(v)
			badOwner = err != nil || pid <= 0
			if badOwner {
				skipped++
				pid = 0
			}
			owner = deletedFile{PID: pid}
			file = deletedFile{Size: -1}
			kind = ""
		case 'c':
			owner.Process = clean(v)
		case 'u':
			owner.User = clean(v)
		case 'f':
			flush()
			file = deletedFile{Size: -1}
			kind = ""
			if owner.PID == 0 {
				// Already counted when the owner record itself was bad.
				if !badOwner {
					skipped++
				}
				continue
			}
			file = owner
			file.FD = v
			file.Size = -1
		case 't':
			kind = v
		case 'D':
			file.Device = v
		case 'i':
			file.Inode = v
		case 'n':
			file.Path = clean(v)
		case 's':
			size, err := strconv.ParseInt(v, 10, 64)
			if err != nil || size < 0 {
				drop()
				continue
			}
			file.Size = size
		}
	}
	flush()
	return rows, skipped, allRowsFailed(len(rows), skipped, "lsof records")
}

func collectDeletedFiles(ctx context.Context) ([]deletedFile, string, error) {
	raw, note, err := networkCommand(ctx, "lsof", "-nP", "+L1", "-F0pcuftDins")
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, "", fmt.Errorf("lsof: %w %s", err, note)
	}
	if err != nil && raw == "" && note != "" {
		return nil, "", fmt.Errorf("lsof: %s", note)
	}
	rows, skipped, parseErr := parseDeletedFilesTolerant(raw)
	if err != nil && raw != "" {
		note = "Partial lsof result. " + note
	}
	return rows, joinNote(note, skippedNote(skipped, "lsof records")), parseErr
}

func hostBytes(value int64, kib bool) string {
	if value < 0 {
		return "—"
	}
	v := float64(value)
	if kib {
		v *= 1024
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func hostCount(value int64) string {
	if value < 0 {
		return "—"
	}
	return strconv.FormatInt(value, 10)
}
