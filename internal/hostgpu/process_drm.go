package hostgpu

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type drmClient struct {
	pid        int
	device, id string
	memory     int64
	engines    map[string]uint64
	capacity   map[string]uint64
}

func parseDRMClient(raw string) (drmClient, bool) {
	c := drmClient{memory: -1, engines: map[string]uint64{}, capacity: map[string]uint64{}}
	driver := ""
	resident, legacy, system := int64(-1), int64(-1), int64(-1)
	for _, line := range strings.Split(raw, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value := strings.TrimSpace(rest)
		switch key {
		case "drm-driver":
			driver = value
		case "drm-pdev":
			c.device = value
		case "drm-client-id":
			c.id = value
		case "drm-resident-vram":
			resident = drmMemory(value)
		case "drm-memory-vram":
			legacy = drmMemory(value)
		case "drm-resident-memory":
			system = drmMemory(value)
		default:
			fields := strings.Fields(value)
			if len(fields) == 0 {
				continue
			}
			n, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil {
				continue
			}
			if engine, found := strings.CutPrefix(key, "drm-engine-capacity-"); found {
				if n > 0 {
					c.capacity[engine] = n
				}
			} else if engine, found := strings.CutPrefix(key, "drm-engine-"); found && len(fields) == 2 && fields[1] == "ns" {
				c.engines[engine] = n
			}
		}
	}
	// Skip NVIDIA to avoid counting its clients again through pmon. Without
	// a client identity duplicate FDs cannot safely be distinguished.
	if driver == "" || driver == "nvidia-drm" || driver == "nvidia" || c.id == "" {
		return c, false
	}
	c.memory = resident
	if c.memory < 0 {
		c.memory = legacy
	}
	if c.memory < 0 {
		c.memory = system
	}
	return c, true
}

func drmMemory(raw string) int64 {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 2 {
		return -1
	}
	n, err := strconv.ParseUint(fields[0], 10, 64)
	multiplier := uint64(1)
	if len(fields) == 2 {
		switch fields[1] {
		case "KiB":
			multiplier = 1024
		case "MiB":
			multiplier = 1024 * 1024
		default:
			return -1
		}
	}
	if err != nil || n > uint64(math.MaxInt64)/multiplier {
		return -1
	}
	return int64(n * multiplier)
}

func collectDRMProcesses(ctx context.Context, root string, pids []int, previous ProcessSample, now time.Time) (map[int]Process, ProcessSample) {
	result := map[int]Process{}
	next := ProcessSample{at: now, clients: map[string]drmClient{}}
	// First deduplicate shared/duplicated FDs within each process. The same
	// client can be visible in two processes; these are attributed, not summed
	// into a machine total, just like shared process RSS pages.
	for _, pid := range pids {
		if ctx.Err() != nil {
			return result, next
		}
		base := filepath.Join(root, strconv.Itoa(pid))
		fds, _ := os.ReadDir(filepath.Join(base, "fd"))
		if len(fds) == 0 {
			continue
		}
		// Start time prevents a reused PID from borrowing another process's interval.
		stat, _ := os.ReadFile(filepath.Join(base, "stat"))
		tail := ""
		// Commands can contain ') '; the final closing parenthesis terminates comm.
		if end := strings.LastIndex(string(stat), ") "); end >= 0 {
			tail = string(stat)[end+2:]
		}
		fields := strings.Fields(tail)
		if len(fields) <= 19 {
			continue
		}
		for _, fd := range fds {
			if ctx.Err() != nil {
				return result, next
			}
			target, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
			if err != nil || (!strings.HasPrefix(target, "/dev/dri/") && !strings.HasPrefix(target, "/dev/char/226:")) {
				continue
			}
			file, err := os.Open(filepath.Join(base, "fdinfo", fd.Name()))
			if err != nil {
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
			file.Close()
			if err != nil || len(raw) > 64*1024 {
				continue
			}
			client, ok := parseDRMClient(string(raw))
			if !ok {
				continue
			}
			client.pid = pid

			key := strconv.Itoa(pid) + ":" + fields[19] + ":" + client.device + ":" + client.id
			if _, duplicate := next.clients[key]; duplicate {
				continue
			}
			next.clients[key] = client
		}
	}
	// Sum each engine's work across this process's clients, then take the
	// busiest engine. Concurrent engines must not create a fictitious >100%.
	engineBusy := map[int]map[string]float64{}
	for key, client := range next.clients {
		reading := Process{Busy: -1, MemoryUsed: client.memory}
		if old, ok := result[client.pid]; ok {
			reading = combineProcess(old, reading)
		}
		result[client.pid] = reading
		before, found := previous.clients[key]
		if !found || !now.After(previous.at) {
			continue
		}
		for engine, value := range client.engines {
			old, ok := before.engines[engine]
			if !ok {
				continue
			}
			if value < old {
				// Drivers may temporarily regress; retain the high-water counter.
				client.engines[engine] = old
				next.clients[key] = client
				continue
			}
			capacity := max(uint64(1), client.capacity[engine])
			share := float64(value-old) / float64(now.Sub(previous.at).Nanoseconds()) / float64(capacity) * 100
			if engineBusy[client.pid] == nil {
				engineBusy[client.pid] = map[string]float64{}
			}
			engineBusy[client.pid][client.device+":"+engine] += share
		}
	}
	for pid, engines := range engineBusy {
		reading := result[pid]
		for _, share := range engines {
			reading.Busy = max(reading.Busy, min(100, share))
		}
		result[pid] = reading
	}
	return result, next
}
