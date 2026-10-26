package hostgpu

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"systemdoc/internal/commandcapture"
)

// Process readings use -1 for unavailable metrics. Busy is the busiest GPU
// engine/device, not a sum of overlapping engine execution times.
type Process struct {
	Busy       float64
	MemoryUsed int64 // bytes; may include buffers shared with other processes
}

// ProcessSample holds DRM counters for the next interval. Owned by one collector.
type ProcessSample struct {
	at      time.Time
	clients map[string]drmClient
}

// CollectProcesses runs only with the Process Explorer's background refresh.
// NVIDIA pmon reports compute and graphics clients; DRM is read without sudo.
func CollectProcesses(ctx context.Context, pids []int, previous ProcessSample) (map[int]Process, ProcessSample) {
	result, next := collectDRMProcesses(ctx, "/proc", pids, previous, time.Now())
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, _, err := commandcapture.Capture(ctx, "nvidia-smi", "pmon", "-c", "1", "-s", "um")
	if err == nil {
		for pid, reading := range parsePMON(raw) {
			// NVIDIA's own readings are authoritative for its clients.
			if existing, found := result[pid]; found {
				reading = combineProcess(existing, reading)
			}
			result[pid] = reading
		}
	}
	return result, next
}

func combineProcess(a, b Process) Process {
	memory := int64(-1)
	if a.MemoryUsed >= 0 && b.MemoryUsed >= 0 && a.MemoryUsed <= math.MaxInt64-b.MemoryUsed {
		memory = a.MemoryUsed + b.MemoryUsed
	}
	return Process{Busy: max(a.Busy, b.Busy), MemoryUsed: memory}
}

// Locate columns by header: older drivers omit JPEG/OFA/CCPM columns. The
// "mem" column is bandwidth utilisation; only "fb" measures allocated memory.
func parsePMON(raw string) map[int]Process {
	result := map[int]Process{}
	columns := map[string]int{}
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "#" {
			if len(fields) > 2 && fields[1] == "gpu" && fields[2] == "pid" {
				columns = map[string]int{}
				for i, name := range fields[1:] {
					columns[name] = i
				}
			}
			continue
		}
		get := func(name string) string {
			if index, ok := columns[name]; ok && index < len(fields) {
				return fields[index]
			}
			return ""
		}
		pid, err := strconv.Atoi(get("pid"))
		if err != nil || pid <= 0 || number(get("gpu")) < 0 {
			continue
		}
		key := get("gpu") + ":" + get("pid")
		if seen[key] {
			continue
		}
		seen[key] = true
		reading := Process{Busy: number(get("sm")), MemoryUsed: -1}
		if reading.Busy > 100 {
			reading.Busy = -1
		}
		if memory := number(get("fb")); memory >= 0 && memory < float64(math.MaxInt64)/(1024*1024) {
			reading.MemoryUsed = int64(memory * 1024 * 1024)
		}
		if old, ok := result[pid]; ok {
			reading = combineProcess(old, reading)
		}
		result[pid] = reading
	}
	return result
}
