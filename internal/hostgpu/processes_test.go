package hostgpu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPMONColumnsAndMissingReadings(t *testing.T) {
	// New and old drivers differ in JPEG/OFA/CCPM columns. mem is a
	// bandwidth percentage and must never be presented as allocated VRAM.
	for _, raw := range []string{
		"# gpu pid type sm mem enc dec fb command\n# Idx # C/G % % % % MB name\n0 42 C 25 70 0 0 512 worker\n1 42 G 60 12 0 0 128 worker\n1 42 G 60 12 0 0 128 worker\n0 43 G - - - - - compositor\n0 - - - - - - - -\n",
		"# gpu pid type sm mem enc dec jpg ofa fb ccpm command\n0 42 C 25 70 0 0 0 0 512 0 worker\n1 42 G 60 12 0 0 0 0 128 0 worker\n0 43 G - - - - - - - - compositor\n",
	} {
		got := parsePMON(raw)
		if len(got) != 2 || got[42].Busy != 60 || got[42].MemoryUsed != 640*1024*1024 {
			t.Fatalf("process readings: %+v", got)
		}
		if got[43].Busy != -1 || got[43].MemoryUsed != -1 {
			t.Fatalf("missing: %+v", got[43])
		}
	}
	got := parsePMON("# gpu pid type sm mem command\n0 42 C 0 90 worker\n")
	if got[42].Busy != 0 || got[42].MemoryUsed != -1 {
		t.Fatalf("memory utilisation was mistaken for VRAM: %+v", got)
	}
}

func TestDRMProcessIntervalsDeduplicateFDsAndHandleCounterRegression(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "42")
	for _, name := range []string{"fd", "fdinfo"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// stat's starttime is field 22; comm can contain spaces and parentheses.
	stat := "42 (a command) ) " + strings.Repeat("0 ", 19) + "12345 0\n"
	if err := os.WriteFile(filepath.Join(base, "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fd := range []string{"3", "4"} {
		if err := os.Symlink("/dev/dri/renderD128", filepath.Join(base, "fd", fd)); err != nil {
			t.Fatal(err)
		}
	}
	write := func(counter uint64) {
		t.Helper()
		raw := fmt.Sprintf("drm-driver: amdgpu\ndrm-pdev: 0000:01:00.0\ndrm-client-id: 7\ndrm-memory-vram: 2048 KiB\ndrm-resident-vram: 1024 KiB\ndrm-engine-gfx: %d ns\ndrm-engine-compute: %d ns\ndrm-engine-capacity-compute: 2\n", counter, counter)
		for _, fd := range []string{"3", "4"} {
			if err := os.WriteFile(filepath.Join(base, "fdinfo", fd), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	at := time.Unix(100, 0)
	write(1_000_000_000)
	first, before := collectDRMProcesses(context.Background(), root, []int{42}, ProcessSample{}, at)
	if len(first) != 1 || first[42].Busy != -1 || first[42].MemoryUsed != 1024*1024 || len(before.clients) != 1 {
		t.Fatalf("first sample/deduplication: %+v (%d clients)", first, len(before.clients))
	}
	write(1_500_000_000)
	second, before := collectDRMProcesses(context.Background(), root, []int{42}, before, at.Add(time.Second))
	if second[42].Busy != 50 {
		t.Fatalf("overlapping engines must not be summed: %+v", second)
	}
	write(1_200_000_000)
	regressed, before := collectDRMProcesses(context.Background(), root, []int{42}, before, at.Add(2*time.Second))
	if regressed[42].Busy != -1 {
		t.Fatalf("regression invented a rate: %+v", regressed)
	}
	write(1_600_000_000)
	third, before := collectDRMProcesses(context.Background(), root, []int{42}, before, at.Add(3*time.Second))
	if third[42].Busy != 10 {
		t.Fatalf("regression lost high-water counter: %+v", third)
	}
	if err := os.WriteFile(filepath.Join(base, "stat"), []byte(strings.Replace(stat, "12345", "54321", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	reused, _ := collectDRMProcesses(context.Background(), root, []int{42}, before, at.Add(4*time.Second))
	if reused[42].Busy != -1 {
		t.Fatalf("reused PID borrowed an interval: %+v", reused)
	}
}

func TestDRMMalformedFieldsStayUnknown(t *testing.T) {
	client, ok := parseDRMClient("drm-driver: amdgpu\ndrm-client-id: 7\ndrm-resident-vram: 9223372036854775807 MiB\ndrm-engine-gfx: broken ns\n")
	if !ok || client.memory != -1 || len(client.engines) != 0 {
		t.Fatalf("malformed: %+v, %v", client, ok)
	}
	if _, ok := parseDRMClient("drm-driver: amdgpu\ndrm-engine-gfx: 123 ns\n"); ok {
		t.Fatal("missing client identity accepted")
	}
}
