package dashboard

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func dockerResourceOutput(raw string) string {
	var stats struct{ Name, CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string }
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &stats) != nil {
		return raw
	}
	return fmt.Sprintf("%s · live container resources\n\nCPU       %s\nMemory    %s  (%s)\nNetwork   %s  (received / sent)\nBlock I/O %s  (read / written)\nProcesses %s\n\nMemory calculation follows Docker CLI accounting.\nCPU is percentage of one logical CPU and can exceed 100%%.", stats.Name, stats.CPUPerc, stats.MemUsage, stats.MemPerc, stats.NetIO, stats.BlockIO, stats.PIDs)
}

type metricSample struct {
	at                    time.Time
	cpu                   uint64
	percent               float64
	memory                uint64
	validCPU, validMemory bool
}

func parseCounters(text string, previous *metricSample, now time.Time) metricSample {
	s := metricSample{at: now}
	for _, line := range strings.Split(text, "\n") {
		key, value, _ := strings.Cut(line, "=")
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil || n == ^uint64(0) {
			continue
		}
		switch key {
		case "CPUUsageNSec":
			s.cpu = n
			s.validCPU = true
		case "MemoryCurrent":
			s.memory = n
			s.validMemory = true
		}
	}
	if previous != nil && previous.validCPU && s.validCPU && s.cpu >= previous.cpu && now.After(previous.at) {
		s.percent = float64(s.cpu-previous.cpu) / float64(now.Sub(previous.at)) * 100
	} else {
		s.percent = -1
	}
	return s
}

func sparkline(values []float64) string {
	const bars = "▁▂▃▄▅▆▇█"
	runes := []rune(bars)
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range values {
		if v < 0 {
			b.WriteRune('·')
			continue
		}
		index := 0
		if max > 0 {
			index = int(v / max * 7)
		}
		b.WriteRune(runes[index])
	}
	return b.String()
}

func (w *workspace) resourceOutput(id, raw string) string {
	if w.metrics == nil {
		w.metrics = map[string][]metricSample{}
	}
	key := fmt.Sprintf("%t/%s", w.user, id)
	history := w.metrics[key]
	var previous *metricSample
	if len(history) > 0 {
		previous = &history[len(history)-1]
	}
	sample := parseCounters(raw, previous, time.Now())
	history = append(history, sample)
	if len(history) > 60 {
		history = history[len(history)-60:]
	}
	w.metrics[key] = history
	cpu, memory := []float64{}, []float64{}
	for _, s := range history {
		cpu = append(cpu, s.percent)
		m := -1.0
		if s.validMemory {
			m = float64(s.memory) / 1024 / 1024
		}
		memory = append(memory, m)
	}
	cpulabel, memlabel := "—", "—"
	if sample.percent >= 0 {
		cpulabel = fmt.Sprintf("%.1f%%", sample.percent)
	}
	if sample.validMemory {
		memlabel = fmt.Sprintf("%.1f MiB", float64(sample.memory)/1024/1024)
	}
	return fmt.Sprintf("CPU · %% of one logical CPU   %s\n%s\n\nMemory   %s\n%s\n\nSelected-workload history · %d samples · automatic scale\nUnavailable samples: ·\n\nBackend counters\n%s", cpulabel, sparkline(cpu), memlabel, sparkline(memory), len(history), raw)
}
