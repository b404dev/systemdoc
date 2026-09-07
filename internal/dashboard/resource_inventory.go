package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func enrichServiceResources(ctx context.Context, user bool, items []workload) {
	args := []string{"show", "--property=Id,Names,LoadState,ActiveState,SubState,CPUUsageNSec,MemoryCurrent", "--"}
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
		return
	}
	text, err := command(ctx, "systemctl", args...)
	if err != nil && text == "" {
		return
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
	}
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
func enrichDockerResources(ctx context.Context, items []workload) {
	indexes := map[string]int{}
	for i, item := range items {
		if isActive(item) {
			indexes[item.ID] = i
		}
	}
	if len(indexes) == 0 {
		return
	}
	output, err := command(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return
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
		if _, ok := cpuValue(row.CPUPerc); ok {
			items[i].CPU = row.CPUPerc
		}
		if n, ok := memoryValue(row.MemUsage); ok {
			items[i].Memory = fmt.Sprintf("%.1f MiB", n)
		}
	}
}
