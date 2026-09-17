package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// enrichServiceResources fills accounting for every loaded unit from one
// systemctl show call. It returns a note when the call failed outright, so
// the summary line can say why the resource columns are blank.
func enrichServiceResources(ctx context.Context, user bool, items []workload) string {
	args := []string{"show", "--property=Id,Names,LoadState,ActiveState,SubState,MainPID,NRestarts,TasksCurrent,CPUUsageNSec,MemoryCurrent,MemoryPeak,IOReadBytes,IOWriteBytes,ControlGroup", "--"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	indexes := map[string]int{}
	for i, item := range items {
		if !item.UnitFileOnly && (item.LoadState == "" || item.LoadState == "loaded") {
			indexes[item.ID] = i
			args = append(args, item.ID)
		}
	}
	if len(indexes) == 0 {
		return ""
	}
	text, err := command(ctx, "systemctl", args...)
	if err != nil && text == "" {
		if ctx.Err() != nil {
			return ""
		}
		return "systemctl show: " + firstLine(err.Error())
	}
	now := time.Now()
	for _, block := range strings.Split(text, "\n\n") {
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				values[key] = value
			}
		}
		i, ok := indexes[values["Id"]]
		if !ok {
			continue
		}
		if state := values["ActiveState"]; state != "" {
			items[i].State = state
		}
		if sub := values["SubState"]; sub != "" {
			items[i].Detail = sub
		}
		if load := values["LoadState"]; load != "" {
			items[i].LoadState = load
		}
		var aliases []string
		for _, name := range strings.Fields(values["Names"]) {
			if name != items[i].ID {
				aliases = append(aliases, name)
			}
		}
		items[i].Aliases = strings.Join(aliases, " ")
		if n, err := strconv.ParseUint(values["CPUUsageNSec"], 10, 64); err == nil && n != ^uint64(0) {
			items[i].CPUCounter = n
			items[i].HasCPU = true
			items[i].SampleAt = now
		}
		if n, err := strconv.ParseUint(values["MemoryCurrent"], 10, 64); err == nil && n != ^uint64(0) {
			items[i].Memory = fmt.Sprintf("%.1f MiB", float64(n)/1024/1024)
		}
		if n, err := strconv.ParseUint(values["MemoryPeak"], 10, 64); err == nil && n != ^uint64(0) {
			items[i].MemoryPeak, items[i].HasMemoryPeak = n, true
		}
		items[i].Tasks = -1
		if n, err := strconv.ParseInt(values["TasksCurrent"], 10, 64); err == nil && n >= 0 {
			items[i].Tasks = n
		}
		if n, err := strconv.Atoi(values["NRestarts"]); err == nil && n >= 0 {
			items[i].Restarts = n
		}
		if n, err := strconv.Atoi(values["MainPID"]); err == nil && n > 0 {
			items[i].MainPID = n
		}
		read, readErr := strconv.ParseUint(values["IOReadBytes"], 10, 64)
		write, writeErr := strconv.ParseUint(values["IOWriteBytes"], 10, 64)
		if readErr == nil && writeErr == nil && read != ^uint64(0) && write != ^uint64(0) {
			items[i].IORead, items[i].IOWrite, items[i].HasIO = read, write, true
		}
		items[i].CGroup = values["ControlGroup"]
	}
	return ""
}

// bulkResourceText renders the accounting already on a systemd row in the
// key=value form the Metrics tab's counter parser reads, so selecting the tab
// costs no subprocess. It is empty when the bulk sample carried nothing.
func bulkResourceText(item workload) string {
	if !item.HasCPU {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "CPUUsageNSec=%d\n", item.CPUCounter)
	if n, ok := memoryValue(item.Memory); ok {
		fmt.Fprintf(&out, "MemoryCurrent=%d\n", uint64(n*1024*1024))
	}
	if item.HasMemoryPeak {
		fmt.Fprintf(&out, "MemoryPeak=%d\n", item.MemoryPeak)
	}
	if item.Tasks >= 0 {
		fmt.Fprintf(&out, "TasksCurrent=%d\n", item.Tasks)
	}
	if item.HasIO {
		fmt.Fprintf(&out, "IOReadBytes=%d\nIOWriteBytes=%d\n", item.IORead, item.IOWrite)
	}
	fmt.Fprintf(&out, "NRestarts=%d\n", item.Restarts)
	if item.MainPID > 0 {
		fmt.Fprintf(&out, "MainPID=%d\n", item.MainPID)
	}
	if item.CGroup != "" {
		fmt.Fprintf(&out, "ControlGroup=%s\n", item.CGroup)
	}
	return strings.TrimRight(out.String(), "\n")
}

// serviceAccountingLine is the one-line reading of the extra bulk fields.
func serviceAccountingLine(item workload) string {
	parts := []string{fmt.Sprintf("Restarts %d", item.Restarts)}
	if item.Tasks >= 0 {
		parts = append(parts, fmt.Sprintf("Tasks %d", item.Tasks))
	}
	if item.HasMemoryPeak {
		parts = append(parts, fmt.Sprintf("Peak memory %.1f MiB", float64(item.MemoryPeak)/1024/1024))
	}
	if item.HasIO {
		parts = append(parts, "I/O read "+hostBytes(int64(item.IORead), false)+" · written "+hostBytes(int64(item.IOWrite), false))
	}
	return strings.Join(parts, " · ")
}
func computeResourceRates(before, after []workload) {
	previous := map[string]workload{}
	for _, item := range before {
		previous[item.ID] = item
	}
	for i, item := range after {
		old, ok := previous[item.ID]
		if ok && old.HasCPU && item.HasCPU && item.CPUCounter >= old.CPUCounter && item.SampleAt.After(old.SampleAt) {
			after[i].CPU = fmt.Sprintf("%.1f%%", float64(item.CPUCounter-old.CPUCounter)/float64(item.SampleAt.Sub(old.SampleAt))*100)
		}
	}
}

// A single stats snapshot populates the container list and fleet cards together.
// Stats access is optional: inventory remains usable when accounting is unavailable.
func enrichDockerResources(ctx context.Context, items []workload) string {
	indexes := map[string]int{}
	for i, item := range items {
		if isActive(item) && !isPod(item) {
			indexes[item.ID] = i
		}
	}
	if len(indexes) == 0 {
		return ""
	}
	output, err := command(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		return "docker stats: " + firstLine(err.Error())
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var row struct{ ID, CPUPerc, MemUsage string }
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		i, ok := indexes[row.ID]
		if !ok {
			continue
		}
		// The full line feeds the Metrics tab, which used to run docker stats
		// again for the one selected container on every poll.
		items[i].Stats = line
		if _, ok := cpuValue(row.CPUPerc); ok {
			items[i].CPU = row.CPUPerc
		}
		if n, ok := memoryValue(row.MemUsage); ok {
			items[i].Memory = fmt.Sprintf("%.1f MiB", n)
		}
	}
	return ""
}
