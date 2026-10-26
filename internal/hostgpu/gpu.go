// Package hostgpu collects optional, read-only GPU telemetry.
package hostgpu

import (
	"context"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"systemdoc/internal/commandcapture"
	"systemdoc/internal/textutil"
)

// Device uses -1 for unavailable readings. Memory values are bytes.
type Device struct {
	ID                      string // Stable backend identity; names alone are not unique.
	Name                    string
	Busy, Temperature       float64
	MemoryUsed, MemoryTotal int64
}

// Collect bounds optional CLI work so a broken driver cannot stall sampling.
func Collect(ctx context.Context) []Device {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	raw, _, err := commandcapture.Capture(ctx, "nvidia-smi", "--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu", "--format=csv,noheader,nounits")
	var devices []Device
	if err == nil {
		devices = parseNVIDIA(raw)
	}
	return append(devices, collectDRM("/sys/class/drm")...)
}

func number(raw string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return -1
	}
	return n
}

func parseNVIDIA(raw string) []Device {
	r := csv.NewReader(strings.NewReader(raw))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	var devices []Device
	for {
		row, err := r.Read()
		if err != nil {
			break
		}
		if len(row) != 6 || number(row[0]) < 0 {
			continue
		}
		d := Device{ID: "nvidia:" + strings.TrimSpace(row[0]), Name: textutil.Clean(strings.TrimSpace(row[1])), Busy: number(row[2]), MemoryUsed: -1, MemoryTotal: -1, Temperature: number(row[5])}
		if d.Busy > 100 {
			d.Busy = -1
		}
		if n := number(row[3]); n >= 0 && n < float64(math.MaxInt64)/(1024*1024) {
			d.MemoryUsed = int64(n * 1024 * 1024)
		}
		if n := number(row[4]); n > 0 && n < float64(math.MaxInt64)/(1024*1024) {
			d.MemoryTotal = int64(n * 1024 * 1024)
		}
		devices = append(devices, d)
	}
	return devices
}

func collectDRM(root string) []Device {
	paths, _ := filepath.Glob(filepath.Join(root, "card*"))
	var devices []Device
	for _, path := range paths {
		if _, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "card")); err != nil {
			continue
		}
		device := filepath.Join(path, "device")
		vendor, _ := os.ReadFile(filepath.Join(device, "vendor"))
		// NVIDIA is queried above; never count it twice.
		if strings.TrimSpace(string(vendor)) == "0x10de" {
			continue
		}
		read := func(name string) float64 {
			raw, err := os.ReadFile(filepath.Join(device, name))
			if err != nil {
				return -1
			}
			return number(string(raw))
		}
		d := Device{ID: "drm:" + filepath.Base(path), Name: filepath.Base(path), Busy: read("gpu_busy_percent"), MemoryUsed: -1, MemoryTotal: -1, Temperature: -1}
		if d.Busy > 100 {
			d.Busy = -1
		}
		if n := read("mem_info_vram_used"); n >= 0 && n < float64(math.MaxInt64) {
			d.MemoryUsed = int64(n)
		}
		if n := read("mem_info_vram_total"); n > 0 && n < float64(math.MaxInt64) {
			d.MemoryTotal = int64(n)
		}
		if d.Busy >= 0 || d.MemoryTotal > 0 {
			devices = append(devices, d)
		}
	}
	return devices
}
