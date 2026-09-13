package dashboard

import (
	"github.com/rivo/tview"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActionArguments(t *testing.T) {
	_, args, err := actionArgs(0, true, workload{ID: "worker.service"}, "restart")
	if err != nil || strings.Join(args, " ") != "--user restart -- worker.service" {
		t.Fatal(args, err)
	}
	for _, id := range []string{"", "--all"} {
		if _, _, err := actionArgs(1, false, workload{ID: id}, "stop"); err == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	if _, _, err := actionArgs(1, false, workload{ID: "abc"}, "prune"); err == nil {
		t.Fatal("unsupported action accepted")
	}
}
func TestProjectContextAndDown(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	os.WriteFile(file, []byte("services: {}"), 0600)
	args, err := projectArgs(project{Name: "site", Directory: dir, Files: []string{file}}, "down")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if !strings.Contains(got, "--project-directory "+dir) || strings.Contains(got, "--volumes") {
		t.Fatal(got)
	}
	if _, err := projectArgs(project{Name: "site"}, "up"); err == nil {
		t.Fatal("missing source accepted")
	}
}
func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := settings{Theme: "Nocturne", RefreshSeconds: 3, GraphMode: "braille", Favorites: []string{"0/true/foo.service"}}
	if err := writeSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := readSettings()
	if err != nil || got.Theme != s.Theme || got.GraphMode != "braille" || len(got.Favorites) != 1 {
		t.Fatal(got, err)
	}
	path, _ := settingsPath()
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("settings are world readable")
	}
}
func TestBoundedOutput(t *testing.T) {
	var b tailBuffer
	b.Write([]byte(strings.Repeat("x", 2*1024*1024)))
	b.Write([]byte("END"))
	if len(b.String()) != 1024*1024 || !strings.HasSuffix(b.String(), "END") {
		t.Fatal("output retention broken")
	}
}
func TestRichOutputEscapesInput(t *testing.T) {
	raw := "2026-09-05T12:30:00 ERROR [red]bad[-] \x1b[2J"
	rich := richOutput(raw, 1, themes[0])
	plain := tview.NewTextView().SetDynamicColors(true).SetText(rich).GetText(true)
	if !strings.Contains(plain, "[red]bad[-]") || strings.Contains(plain, "\x1b") {
		t.Fatal(plain)
	}
	if !strings.Contains(rich, themes[0].error) {
		t.Fatal("severity not highlighted")
	}
}
func TestCounterRatesAndReset(t *testing.T) {
	at := time.Now()
	a := parseCounters("CPUUsageNSec=1000000000\nMemoryCurrent=1024", nil, at)
	b := parseCounters("CPUUsageNSec=1500000000\nMemoryCurrent=2048", &a, at.Add(time.Second))
	if b.percent != 50 || b.memory != 2048 {
		t.Fatal(b)
	}
	c := parseCounters("CPUUsageNSec=0", &b, at.Add(2*time.Second))
	if c.percent != -1 {
		t.Fatal("restart produced bogus CPU")
	}
}
func TestUnitDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.service")
	if err := installNewUnit(path, "original"); err != nil {
		t.Fatal(err)
	}
	if err := installNewUnit(path, "changed"); err == nil {
		t.Fatal("overwrote existing unit")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("original lost")
	}
}
func TestProviderRestrictions(t *testing.T) {
	args, _ := providerArgs("claude")
	if !strings.Contains(strings.Join(args, "|"), "--tools||") {
		t.Fatal(args)
	}
	args, _ = providerArgs("codex")
	if !strings.Contains(strings.Join(args, " "), "--sandbox read-only") {
		t.Fatal(args)
	}
}
