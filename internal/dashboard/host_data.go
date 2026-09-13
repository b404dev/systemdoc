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
)

type hostProcess struct {
	PID, PPID                     int
	User, State, Elapsed, Command string
	CPU                           float64
	RSS                           int64 // KiB; -1 means unavailable.
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

func parseProcesses(raw string) ([]hostProcess, error) {
	var rows []hostProcess
	seen := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f, command := leadingFields(line, 7)
		if len(f) != 7 || command == "" {
			return nil, fmt.Errorf("unrecognized ps row")
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
			return nil, fmt.Errorf("invalid ps values")
		}
		seen[pid] = true
		rows = append(rows, hostProcess{pid, ppid, f[2], f[5], f[6], clean(command), cpu, rss})
	}
	return rows, nil
}

func collectProcesses(ctx context.Context) ([]hostProcess, error) {
	raw, diagnostic, err := networkCommand(ctx, "ps", "-axww", "-o", "pid=,ppid=,user=,pcpu=,rss=,stat=,etime=,args=")
	if err != nil {
		return nil, fmt.Errorf("ps: %w %s", err, diagnostic)
	}
	rows, err := parseProcesses(raw)
	if err != nil {
		return nil, err
	}
	return omitProcessCollector(rows, os.Getpid()), nil
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

func parseMounts(raw, platform string, inodeOnly bool) ([]hostMount, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Filesystem") {
		return nil, fmt.Errorf("missing df header")
	}
	var rows []hostMount
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := dfRow.FindStringSubmatch(strings.TrimSpace(line))
		if f == nil {
			return nil, fmt.Errorf("unrecognized df row")
		}
		values := make([]int64, 4)
		for i := range values {
			v, err := dfNumber(f[i+2])
			if err != nil {
				return nil, fmt.Errorf("invalid df count: %w", err)
			}
			values[i] = v
		}
		m := hostMount{Source: clean(f[1]), Path: clean(f[6]), Size: values[0], Used: values[1], Free: values[2], Percent: int(values[3]), InodePercent: -1, Inodes: -1, InodeUsed: -1, InodeFree: -1}
		if inodeOnly {
			m.Inodes, m.InodeUsed, m.InodeFree, m.InodePercent = m.Size, m.Used, m.Free, m.Percent
		} else if platform == "darwin" {
			inodes, path := leadingFields(f[6], 3)
			if len(inodes) != 3 || path == "" {
				return nil, fmt.Errorf("missing macOS inode columns")
			}
			for i, s := range inodes {
				v, err := dfNumber(s)
				if err != nil {
					return nil, fmt.Errorf("invalid macOS inode count: %w", err)
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
		rows = append(rows, m)
	}
	return rows, nil
}

func collectMounts(ctx context.Context, platform string) ([]hostMount, string, error) {
	args := []string{"-Pk"}
	if platform == "darwin" {
		args = []string{"-Pki"}
	}
	raw, diagnostic, err := networkCommand(ctx, "df", args...)
	if err != nil {
		return nil, "", fmt.Errorf("df: %w %s", err, diagnostic)
	}
	rows, err := parseMounts(raw, platform, false)
	if err != nil || platform == "darwin" {
		return rows, diagnostic, err
	}
	raw, inodeNote, inodeErr := networkCommand(ctx, "df", "-Pi")
	if inodeErr != nil {
		return rows, "Inodes unavailable: " + inodeErr.Error() + " " + inodeNote, nil
	}
	inodes, inodeErr := parseMounts(raw, platform, true)
	if inodeErr != nil {
		return rows, inodeErr.Error(), nil
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
	return rows, diagnostic, nil
}

type deletedFile struct {
	PID                                    int
	Process, User, FD, Device, Inode, Path string
	Size                                   int64
}

func (f deletedFile) key() string { return fmt.Sprintf("%d/%s/%s/%s", f.PID, f.FD, f.Device, f.Inode) }

func parseDeletedFiles(raw string) ([]deletedFile, error) {
	if strings.TrimSpace(raw) != "" && (!strings.Contains(raw, "\x00") || !strings.HasPrefix(strings.TrimLeft(raw, "\n"), "p")) {
		return nil, fmt.Errorf("unrecognized lsof field output")
	}
	var rows []deletedFile
	owner := deletedFile{}
	file := deletedFile{Size: -1}
	kind := ""
	flush := func() {
		if file.FD != "" && kind == "REG" {
			rows = append(rows, file)
		}
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
			if err != nil || pid <= 0 {
				return nil, fmt.Errorf("invalid lsof PID")
			}
			owner = deletedFile{PID: pid}
			file = deletedFile{Size: -1}
			kind = ""
		case 'c':
			owner.Process = clean(v)
		case 'u':
			owner.User = clean(v)
		case 'f':
			if owner.PID == 0 {
				return nil, fmt.Errorf("lsof file has no owner")
			}
			flush()
			file = owner
			file.FD = v
			file.Size = -1
			kind = ""
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
				return nil, fmt.Errorf("invalid lsof size")
			}
			file.Size = size
		}
	}
	flush()
	return rows, nil
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
	rows, parseErr := parseDeletedFiles(raw)
	if err != nil && raw != "" {
		note = "Partial lsof result. " + note
	}
	return rows, note, parseErr
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
