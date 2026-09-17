package dashboard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Tests pin the existing fixture suite to Linux before starting any workers.
var servicePlatform = runtime.GOOS

func usesLaunchd() bool { return servicePlatform == "darwin" }
func serviceManager() string {
	if usesLaunchd() {
		return "launchd"
	}
	return "systemd"
}
func launchDomain(user bool) string {
	if user {
		return fmt.Sprintf("gui/%d", os.Getuid())
	}
	return "system"
}
func launchTarget(user bool, id string) (string, error) {
	if id == "" || strings.HasPrefix(id, "-") || strings.ContainsAny(id, "/\x00\r\n\t *?[]{}") {
		return "", fmt.Errorf("select a valid launchd service label")
	}
	return launchDomain(user) + "/" + id, nil
}

var launchServiceRow = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(.+)$`)

// launchctl print is diagnostic text, not a stable API. Parse only its services
// table and reject truncated output rather than inventing inventory. A row the
// parser does not recognise is skipped; the table fails only when every row is.
func parseLaunchServices(raw string) ([]workload, error) {
	items, _, err := parseLaunchServicesTolerant(raw)
	return items, err
}

func parseLaunchServicesTolerant(raw string) (items []workload, skipped int, err error) {
	inTable, complete := false, false
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !inTable {
			if line == "services = {" {
				inTable = true
			}
			continue
		}
		if line == "}" {
			complete = true
			break
		}
		if line == "" {
			continue
		}
		item, ok := parseLaunchServiceRow(line)
		if !ok || seen[item.ID] {
			skipped++
			continue
		}
		seen[item.ID] = true
		items = append(items, item)
	}
	if !inTable || !complete {
		return nil, skipped, fmt.Errorf("launchctl did not return a complete services table; its diagnostic format may have changed")
	}
	if len(items) == 0 && skipped > 0 {
		return nil, skipped, fmt.Errorf("no launchctl services rows could be parsed (%d unrecognised)", skipped)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, skipped, nil
}

func parseLaunchServiceRow(line string) (workload, bool) {
	fields := launchServiceRow.FindStringSubmatch(line)
	if fields == nil {
		return workload{}, false
	}
	fields = fields[1:]
	pid := 0
	if fields[0] != "-" {
		var err error
		pid, err = strconv.Atoi(fields[0])
		if err != nil || pid < 0 {
			return workload{}, false
		}
	}
	status := fields[1]
	exitStatus := int64(0)
	if status != "-" {
		var err error
		exitStatus, err = strconv.ParseInt(status, 0, 64)
		if err != nil {
			return workload{}, false
		}
	}
	id := fields[2]
	item := workload{ID: id, Name: id, PID: pid, State: "inactive", Detail: "idle / on demand", LoadState: "loaded", Description: "launchd job"}
	if pid > 0 {
		item.State = "active"
		item.Detail = fmt.Sprintf("running · PID %d", pid)
	} else if exitStatus != 0 {
		item.State = "failed"
		item.Detail = "last exit status " + status + " · not running"
	}
	return item, true
}
func listLaunchServices(ctx context.Context, user bool) ([]workload, error) {
	raw, err := command(ctx, "launchctl", "print", launchDomain(user))
	if err != nil {
		return nil, err
	}
	return parseLaunchServices(raw)
}
func enrichLaunchResources(raw string, items []workload) {
	indexes := map[int][]int{}
	for i, item := range items {
		if item.PID > 0 {
			indexes[item.PID] = append(indexes[item.PID], i)
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cpu, cpuErr := strconv.ParseFloat(fields[1], 64)
		rss, memErr := strconv.ParseUint(fields[2], 10, 64)
		for _, i := range indexes[pid] {
			if cpuErr == nil && cpu >= 0 {
				items[i].CPU = fmt.Sprintf("%.1f%%", cpu)
			}
			if memErr == nil {
				items[i].Memory = fmt.Sprintf("%.1f MiB", float64(rss)/1024)
			}
		}
	}
}
func enrichLaunchInventory(ctx context.Context, user bool, items []workload) []workload {
	if raw, err := command(ctx, "ps", "-axo", "pid=,pcpu=,rss="); err == nil {
		enrichLaunchResources(raw, items)
	}
	if raw, err := command(ctx, "launchctl", "print-disabled", launchDomain(user)); err == nil {
		if overrides, err := parseLaunchDisabled(raw); err == nil {
			for i := range items {
				items[i].Enablement = "default"
				if state := overrides[items[i].ID]; state != "" {
					items[i].Enablement = state
				}
			}
		}
	}

	return items
}

// Only top-level fields belong to this service. Nested environment dictionaries
// may contain arbitrary keys/values, including strings resembling these fields.
func launchProperties(raw string) map[string]string {
	values := map[string]string{}
	depth := 0
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if depth == 1 {
			if key, value, ok := strings.Cut(line, " = "); ok && value != "{" {
				values[key] = value
			}
		}
		if strings.HasSuffix(line, "{") {
			depth++
		}
		if line == "}" {
			depth--
		}
	}
	return values
}
func launchLogArgs(ctx context.Context, user bool, item workload, follow bool) ([]string, error) {
	target, err := launchTarget(user, item.ID)
	if err != nil {
		return nil, err
	}
	raw, err := command(ctx, "launchctl", "print", target)
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(launchProperties(raw)["pid"])
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("no running PID for %s; unified logs cannot be scoped to this job. Check its configured StandardOutPath/StandardErrorPath in Config", target)
	}
	args := []string{"show", "--last", "15m", "--style", "compact"}
	if follow {
		args = []string{"stream", "--style", "compact"}
	}
	return append(args, "--predicate", fmt.Sprintf("processIdentifier == %d", pid)), nil
}
func inspectLaunchService(ctx context.Context, user bool, item workload, tab int) (string, error) {
	target, err := launchTarget(user, item.ID)
	if err != nil {
		return "", err
	}
	if tab == 1 {
		args, err := launchLogArgs(ctx, user, item, false)
		if err != nil {
			return "", err
		}
		raw, err := command(ctx, "log", args...)
		return "Unified logs · current PID · last 15 minutes · latest 150 retained lines\nPID reuse can include another process; file-based stdout/stderr logs are separate.\n\n" + recentLogLines(raw, 150), err
	}
	raw, err := command(ctx, "launchctl", "print", target)
	if err != nil {
		return raw, err
	}
	properties := launchProperties(raw)
	switch tab {
	case 2:
		path := properties["path"]
		if !filepath.IsAbs(path) {
			return "This loaded job has no absolute plist path in launchctl output. It may be registered dynamically.\n\n" + raw, nil
		}
		return command(ctx, "plutil", "-convert", "xml1", "-o", "-", "--", path)
	case 3:
		pid, err := strconv.Atoi(properties["pid"])
		if err != nil || pid <= 0 {
			return "No running process; CPU and memory are unavailable.", nil
		}
		sample, err := command(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid=,pcpu=,rss=")
		if err != nil {
			return sample, err
		}
		rows := []workload{{PID: pid}}
		enrichLaunchResources(sample, rows)
		return fmt.Sprintf("%s\n\nPID       %d\nCPU       %s\nMemory    %s (resident set)\n\nmacOS ps process samples; excludes child processes. CPU is ps's averaged percentage, not a systemd counter delta.\nPID and resource observations are collected separately.", target, pid, available(rows[0].CPU), available(rows[0].Memory)), nil
	default:
		return target + "\n\n" + raw, nil
	}
}
func launchActionArgs(user bool, item workload, verb string) (string, []string, error) {
	target, err := launchTarget(user, item.ID)
	if err != nil {
		return "", nil, err
	}
	switch verb {
	case "start":
		return "launchctl", []string{"kickstart", target}, nil
	case "restart":
		return "launchctl", []string{"kickstart", "-k", target}, nil
	case "stop":
		return "launchctl", []string{"kill", "SIGTERM", target}, nil
	case "enable", "disable":
		return "launchctl", []string{verb, target}, nil
	}
	return "", nil, fmt.Errorf("%s is not supported for launchd", verb)
}
func serviceVerbs() []string {
	if usesLaunchd() {
		return []string{"start", "stop", "restart", "enable", "disable"}
	}
	return []string{"start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask", "reset-failed"}
}

func parseLaunchDisabled(raw string) (map[string]string, error) {
	result := map[string]string{}
	opened := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !opened {
			if line == "disabled services = {" {
				opened = true
			}
			continue
		}
		if line == "}" {
			return result, nil
		}
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " => ")
		if !ok {
			return nil, fmt.Errorf("unrecognized disabled-services row")
		}
		id, err := strconv.Unquote(key)
		if err != nil {
			return nil, err
		}
		// Older launchctl prints true/false, newer releases print
		// disabled/enabled. An unknown value drops that row, not the table.
		switch value {
		case "true", "disabled":
			result[id] = "disabled"
		case "false", "enabled":
			result[id] = "enabled"
		}
	}
	return nil, fmt.Errorf("incomplete disabled-services table")
}
