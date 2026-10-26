// Package hostnet collects and filters host sockets and interface telemetry.
package hostnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"systemdoc/internal/commandcapture"
	"systemdoc/internal/textutil"
)

type Socket struct {
	Protocol, Family, State, Local, Remote string
	Process, User, UID, FD, Inode, Cgroup  string
	PID                                    int
}

func (s Socket) Key() string {
	return fmt.Sprintf("%s/%s/%s/%s/%d/%s/%s", s.Protocol, s.Family, s.Local, s.Remote, s.PID, s.FD, s.Inode)
}
func (s Socket) Bound() bool {
	return s.State == "LISTEN" || s.Protocol == "UDP" && s.State == "UNCONN"
}
func Endpoint(value string) (string, string) {
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		return host, port
	}
	if i := strings.LastIndexByte(value, ':'); i >= 0 {
		return strings.Trim(value[:i], "[]"), value[i+1:]
	}
	return value, ""
}
func Binding(address string) string {
	host, _ := Endpoint(address)
	switch host {
	case "0.0.0.0":
		return "All IPv4 interfaces"
	case "::":
		return "All IPv6 interfaces"
	case "*":
		return "Wildcard bind"
	}
	host, _, _ = strings.Cut(host, "%")
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "Loopback address"
	}
	return "Specific local address"
}
func socketState(value string) string {
	switch strings.ToUpper(value) {
	case "ESTAB":
		return "ESTABLISHED"
	case "CLOSE_WAIT":
		return "CLOSE-WAIT"
	case "TIME_WAIT":
		return "TIME-WAIT"
	default:
		return strings.ToUpper(value)
	}
}

var ssOwner = regexp.MustCompile(`\("((?:[^"\\]|\\.)*)",pid=([0-9]+),fd=([0-9]+)\)`)

// ParseSS skips rows (or owner entries) it cannot read and counts
// them, so one odd row no longer blanks the whole socket table.
func ParseSS(raw string) (rows []Socket, skipped int, err error) {
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 || (f[0] != "tcp" && f[0] != "udp") {
			skipped++
			continue
		}
		s := Socket{Protocol: strings.ToUpper(f[0]), State: socketState(f[1]), Local: f[4], Remote: f[5]}
		host, _ := Endpoint(s.Local)
		if strings.Contains(host, ":") {
			s.Family = "IPv6"
		} else if host != "*" {
			s.Family = "IPv4"
		}
		for _, value := range f[6:] {
			key, v, ok := strings.Cut(value, ":")
			if !ok {
				continue
			}
			switch key {
			case "uid":
				s.UID = v
			case "ino":
				s.Inode = v
			case "cgroup":
				s.Cgroup = v
			case "v6only":
				s.Family = "IPv6"
			}
		}
		owners := ssOwner.FindAllStringSubmatch(line, -1)
		if len(owners) == 0 {
			rows = append(rows, s)
			continue
		}
		for _, owner := range owners {
			row := s
			row.Process = owner[1]
			if decoded, err := strconv.Unquote(`"` + owner[1] + `"`); err == nil {
				row.Process = decoded
			}
			pid, err := strconv.Atoi(owner[2])
			if err != nil || pid <= 0 {
				skipped++
				continue
			}
			row.PID = pid
			row.FD = owner[3]
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 && skipped > 0 {
		return nil, skipped, fmt.Errorf("no ss socket rows could be parsed (%d unrecognised)", skipped)
	}
	return rows, skipped, nil
}

// NUL field records keep process names separate from file records and endpoints.
// Newline is only stripped at record boundaries, never used as a field delimiter.
func parseLsofSockets(raw string) ([]Socket, error) {
	if raw == "" {
		return nil, nil
	}
	if !strings.HasSuffix(raw, "\x00\n") && !strings.HasSuffix(raw, "\x00") {
		return nil, fmt.Errorf("incomplete lsof field output")
	}
	var rows []Socket
	var process, current Socket
	hasFile := false
	seenProcess := false
	flush := func() error {
		if !hasFile {
			return nil
		}
		hasFile = false
		if current.Protocol != "TCP" && current.Protocol != "UDP" {
			return nil
		}
		if current.PID <= 0 || current.Local == "" {
			return fmt.Errorf("incomplete lsof socket record")
		}
		if current.State == "" && current.Protocol == "UDP" {
			current.State = "UNCONN"
			if current.Remote != "" {
				current.State = "CONNECTED"
			}
		}
		if current.State == "" {
			current.State = "UNKNOWN"
		}
		rows = append(rows, current)
		return nil
	}
	for _, field := range strings.Split(raw, "\x00") {
		field = strings.TrimLeft(field, "\n")
		if field == "" {
			continue
		}
		value := field[1:]
		switch field[0] {
		case 'p':
			seenProcess = true
			if err := flush(); err != nil {
				return nil, err
			}
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 0 {
				return nil, fmt.Errorf("invalid lsof PID")
			}
			process = Socket{PID: pid}
		case 'c':
			process.Process = value
		case 'u':
			process.UID = value
		case 'L':
			process.User = value
		case 'f':
			if err := flush(); err != nil {
				return nil, err
			}
			current = process
			current.FD = value
			hasFile = true
		case 't':
			current.Family = value
		case 'P':
			current.Protocol = strings.ToUpper(value)
		case 'n':
			current.Local, current.Remote, _ = strings.Cut(value, "->")
		case 'T':
			if state, ok := strings.CutPrefix(value, "ST="); ok {
				current.State = socketState(state)
			}
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if !seenProcess {
		return nil, fmt.Errorf("lsof output has no process records")
	}
	return rows, nil
}

type Interface struct {
	Name, Flags, Addresses, Hardware string
	MTU                              int
}
type Counter struct {
	Name     string
	Receive  uint64
	Transmit uint64
}
type Rate struct {
	Name                    string
	Receive, Transmit       float64
	ReceiveTotal, SendTotal uint64
	Ready                   bool
}
type Snapshot struct {
	InterfaceAt, CounterAt      time.Time
	Sockets                     []Socket
	Interfaces                  []Interface
	Counters                    []Counter
	Source, CounterSource, Note string
	At                          time.Time
}

func CollectSockets(ctx context.Context, platform string) (Snapshot, error) {
	result := Snapshot{At: time.Now()}
	var raw, diagnostic string
	var err error
	if platform != "darwin" {
		result.Source = "ss · host network namespace"
		raw, diagnostic, err = commandcapture.Capture(ctx, "ss", "-H", "-n", "-a", "-t", "-u", "-p", "-e")
		if err == nil {
			var skipped int
			result.Sockets, skipped, err = ParseSS(raw)
			if err != nil {
				return result, err
			}
			result.Note = textutil.JoinNote(diagnostic, textutil.SkippedNote(skipped, "ss rows"))
			enrichNetworkOwners(result.Sockets)
			return result, nil
		}
		if !errors.Is(err, exec.ErrNotFound) {
			return result, fmt.Errorf("ss: %w\n%s", err, diagnostic)
		}
	}
	result.Source = "lsof · visible process sockets"
	raw, diagnostic, err = commandcapture.Capture(ctx, "lsof", "-nP", "-iTCP", "-iUDP", "-F0pcuLftPnT")
	if err != nil {
		var status *exec.ExitError
		if !(errors.As(err, &status) && status.ExitCode() == 1 && (raw != "" || diagnostic == "")) {
			return result, fmt.Errorf("lsof: %w\n%s", err, diagnostic)
		}
	}
	if err != nil && raw != "" {
		diagnostic = "lsof returned partial results (exit 1). " + diagnostic
	}
	result.Sockets, err = parseLsofSockets(raw)
	if err != nil {
		return result, err
	}
	result.Note = diagnostic
	if platform != "darwin" {
		result.Note = "ss is unavailable; lsof shows visible process-owned sockets only.\n" + result.Note
	}
	return result, nil
}
func enrichNetworkOwners(rows []Socket) {
	users := map[string]string{}
	for i := range rows {
		if rows[i].User != "" || rows[i].UID == "" {
			continue
		}
		name, exists := users[rows[i].UID]
		if !exists {
			account, err := user.LookupId(rows[i].UID)
			if err == nil {
				name = account.Username
			}
			users[rows[i].UID] = name
		}
		rows[i].User = name
	}
}
func collectNetworkInterfaces() ([]Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var rows []Interface
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return rows, err
		}
		var values []string
		for _, address := range addresses {
			values = append(values, address.String())
		}
		rows = append(rows, Interface{Name: iface.Name, Flags: iface.Flags.String(), Addresses: strings.Join(values, ", "), Hardware: iface.HardwareAddr.String(), MTU: iface.MTU})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

func parseProcNetDev(raw string) ([]Counter, error) {
	var rows []Counter
	for _, line := range strings.Split(raw, "\n") {
		name, values, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 16 {
			return nil, fmt.Errorf("unrecognized /proc/net/dev row")
		}
		receive, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid receive counter")
		}
		transmit, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid transmit counter")
		}
		rows = append(rows, Counter{Name: strings.TrimSpace(name), Receive: receive, Transmit: transmit})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no interface counters in /proc/net/dev")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

func parseDarwinNetstat(raw string) ([]Counter, error) {
	indices := map[string]int{}
	rows := map[string]Counter{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Name" {
			indices = map[string]int{}
			for i, field := range fields {
				indices[field] = i
			}
			continue
		}
		rx, rxOK := indices["Ibytes"]
		tx, txOK := indices["Obytes"]
		if !rxOK || !txOK || rx >= len(fields) || tx >= len(fields) || len(fields) < 4 || !strings.HasPrefix(fields[2], "<Link#") {
			continue
		}
		receive, rxErr := strconv.ParseUint(fields[rx], 10, 64)
		transmit, txErr := strconv.ParseUint(fields[tx], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		current := rows[fields[0]]
		if receive >= current.Receive && transmit >= current.Transmit {
			rows[fields[0]] = Counter{Name: fields[0], Receive: receive, Transmit: transmit}
		}
	}
	result := make([]Counter, 0, len(rows))
	for _, row := range rows {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if len(result) == 0 {
		return nil, fmt.Errorf("netstat did not report interface byte counters")
	}
	return result, nil
}

func collectNetworkCounters(ctx context.Context, platform string) ([]Counter, error) {
	if platform == "darwin" {
		raw, diagnostic, err := commandcapture.Capture(ctx, "netstat", "-ibn")
		if err != nil {
			return nil, fmt.Errorf("netstat: %w\n%s", err, diagnostic)
		}
		return parseDarwinNetstat(raw)
	}
	raw, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	return parseProcNetDev(string(raw))
}

func Rates(previous []Counter, previousAt time.Time, current []Counter, currentAt time.Time) []Rate {
	before := map[string]Counter{}
	for _, row := range previous {
		before[row.Name] = row
	}
	elapsed := currentAt.Sub(previousAt).Seconds()
	rates := make([]Rate, 0, len(current))
	for _, row := range current {
		rate := Rate{Name: row.Name, ReceiveTotal: row.Receive, SendTotal: row.Transmit}
		old, ok := before[row.Name]
		if ok && elapsed > 0 && row.Receive >= old.Receive && row.Transmit >= old.Transmit {
			rate.Receive = float64(row.Receive-old.Receive) / elapsed
			rate.Transmit = float64(row.Transmit-old.Transmit) / elapsed
			rate.Ready = true
		}
		rates = append(rates, rate)
	}
	return rates
}

func CollectView(ctx context.Context, platform string, view int) (Snapshot, error) {
	var result Snapshot
	switch view {
	case 3:
		result.CounterSource = "/proc/net/dev · host interface counters"
		if platform == "darwin" {
			result.CounterSource = "netstat -ibn · host interface counters"
		}
		var err error
		result.Counters, err = collectNetworkCounters(ctx, platform)
		result.CounterAt = time.Now()
		return result, err
	case 2:
		var err error
		result.Interfaces, err = collectNetworkInterfaces()
		result.InterfaceAt = time.Now()
		return result, err
	default:
		var err error
		result, err = CollectSockets(ctx, platform)
		result.Interfaces, _ = collectNetworkInterfaces()
		result.InterfaceAt = time.Now()
		sortNetworkSockets(result.Sockets)
		return result, err
	}
}
func sortNetworkSockets(rows []Socket) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Bound() != b.Bound() {
			return a.Bound()
		}
		_, ap := Endpoint(a.Local)
		_, bp := Endpoint(b.Local)
		an, _ := strconv.Atoi(ap)
		bn, _ := strconv.Atoi(bp)
		if an != bn {
			return an < bn
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		return a.Key() < b.Key()
	})
}
func MatchesSocket(s Socket, query string) bool {
	_, localPort := Endpoint(s.Local)
	_, remotePort := Endpoint(s.Remote)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		key, value, field := strings.Cut(term, ":")
		if !field {
			if !strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s %s %s %s %s %d %s", s.Protocol, s.Family, s.State, s.Local, s.Remote, s.Process, s.User, s.PID, s.Cgroup)), term) {
				return false
			}
			continue
		}
		actual := ""
		switch key {
		case "port":
			if localPort != value && remotePort != value {
				return false
			}
			continue
		case "localport":
			if localPort != value {
				return false
			}
			continue
		case "remoteport":
			if remotePort != value {
				return false
			}
			continue
		case "pid":
			if s.PID <= 0 || strconv.Itoa(s.PID) != value {
				return false
			}
			continue
		case "proto":
			if strings.ToLower(s.Protocol) != value {
				return false
			}
			continue
		case "state":
			if !strings.EqualFold(s.State, socketState(value)) {
				return false
			}
			continue
		case "process":
			actual = s.Process
		case "user":
			actual = s.User
			if actual == "" {
				actual = s.UID
			}
		case "local":
			actual = s.Local
		case "remote":
			actual = s.Remote
		case "family":
			actual = s.Family
		case "service":
			actual = s.Cgroup
		default:
			return false
		}
		if !strings.Contains(strings.ToLower(actual), value) {
			return false
		}
	}
	return true
}
