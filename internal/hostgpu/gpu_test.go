package hostgpu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNVIDIAReadings(t *testing.T) {
	devices := parseNVIDIA("0, Example GPU, 42, 512, 8192, 63\n1, Other GPU, [N/A], [Not Supported], 4096, N/A\n2, Bad reading, NaN, Inf, -3, NaN\nmalformed\n")
	if len(devices) != 3 {
		t.Fatalf("devices: %+v", devices)
	}
	if d := devices[0]; d.Busy != 42 || d.MemoryUsed != 512*1024*1024 || d.MemoryTotal != 8192*1024*1024 || d.Temperature != 63 {
		t.Fatalf("reading: %+v", d)
	}
	if d := devices[1]; d.Busy != -1 || d.MemoryUsed != -1 || d.MemoryTotal != 4096*1024*1024 || d.Temperature != -1 {
		t.Fatalf("missing: %+v", d)
	}
	if d := devices[2]; d.Busy != -1 || d.MemoryUsed != -1 || d.MemoryTotal != -1 || d.Temperature != -1 {
		t.Fatalf("invalid: %+v", d)
	}
}

func TestDRMDevices(t *testing.T) {
	root := t.TempDir()
	for _, card := range []string{"card0", "card1", "card0-HDMI-A-1"} {
		dir := filepath.Join(root, card, "device")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"vendor": "0x1002", "gpu_busy_percent": "25", "mem_info_vram_used": "1024", "mem_info_vram_total": "8192"} {
			if card == "card1" && name == "vendor" {
				value = "0x10de"
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	devices := collectDRM(root)
	if len(devices) != 1 || devices[0].Busy != 25 || devices[0].MemoryTotal != 8192 {
		t.Fatalf("devices: %+v", devices)
	}
	if got := collectDRM(filepath.Join(root, "missing")); len(got) != 0 {
		t.Fatalf("missing: %+v", got)
	}
}
