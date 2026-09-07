package dashboard

import (
	"encoding/json"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"os"
	"strings"
	"testing"
	"time"
)

// This opt-in test exports terminal cells for visual review, without adding any
// fixture mode or screenshot code to the application binary.
func TestVisualReview(t *testing.T) {
	path := os.Getenv("SYSTEMDOC_VISUAL_REVIEW")
	if path == "" {
		t.Skip("set SYSTEMDOC_VISUAL_REVIEW to export terminal cells")
	}
	w := testWorkspace()
	for i, theme := range themes {
		if theme.name == os.Getenv("SYSTEMDOC_VISUAL_THEME") {
			w.theme = i
		}
	}
	w.items[0] = []workload{
		{ID: "nginx.service", Name: "nginx.service", State: "active", Detail: "running", Description: "A high performance web server and reverse proxy", Enablement: "enabled", CPU: "12.4%", Memory: "148.0 MiB"},
		{ID: "worker.service", Name: "worker.service", State: "failed", Detail: "exit-code", Description: "Background queue worker", Enablement: "enabled", CPU: "", Memory: ""},
	}
	for i, name := range []string{"postgresql", "redis", "sshd", "docker", "containerd", "cron", "NetworkManager", "systemd-journald", "systemd-resolved", "backup", "prometheus", "grafana-server", "node-exporter"} {
		w.items[0] = append(w.items[0], workload{ID: name + ".service", Name: name + ".service", State: "active", Detail: "running", Enablement: "enabled", CPU: fmt.Sprintf("%.1f%%", float64(i)/3), Memory: fmt.Sprintf("%.1f MiB", float64(i+1)*11)})
	}
	w.selected[0] = "nginx.service"
	w.lastRefresh[0] = time.Now()
	for i := 0; i < 24; i++ {
		w.fleetHistory[0] = append(w.fleetHistory[0], fleetSample{cpuCount: 14, memoryCount: 14, cpu: float64((i * 7) % 19), memory: float64(100 + i*3)})
	}
	w.activity = []activityEvent{{mode: 0, id: "worker.service", text: "active → failed / exit-code", at: time.Now()}}
	w.applyTheme()
	w.detail.SetText(richOutput("● nginx.service - A high performance web server\n     Loaded: loaded (/usr/lib/systemd/system/nginx.service; enabled)\n     Active: active (running) since Sat 2026-09-05 10:15:22 BST\n   Main PID: 1248 (nginx)\n      Tasks: 5\n     Memory: 148.0 MiB\n        CPU: 3min 12.345s\n     CGroup: /system.slice/nginx.service\n             ├─1248 nginx: master process\n             ├─1250 nginx: worker process\n             ╰─1251 nginx: worker process\n\n2026-09-05T10:15:22 nginx[1248]: Started web server\n2026-09-05T10:15:23 nginx[1248]: Ready to accept connections\n2026-09-05T10:18:44 nginx[1250]: WARNING upstream response slow", 0, w.palette()))

	if os.Getenv("SYSTEMDOC_VISUAL_MODE") == "docker" {
		w.mode = 1
		w.items[1] = []workload{
			{ID: "web", Name: "website-web-1", State: "running", Detail: "Up 2 hours (healthy)", Description: "nginx:alpine", Project: "website", CPU: "3.2%", Memory: "48.0 MiB"},
			{ID: "redis", Name: "website-redis-1", State: "running", Detail: "Up 2 hours", Description: "redis:alpine", Project: "website", CPU: "0.4%", Memory: "12.0 MiB"},
			{ID: "worker", Name: "website-worker-1", State: "restarting", Detail: "Restarting (1)", Description: "worker:latest", Project: "website"},
		}
		w.selected[1] = "web"
		w.lastRefresh[1] = time.Now()
		w.fleetHistory[1] = w.fleetHistory[0]
		w.applyTheme()
		raw, err := os.ReadFile("testdata/docker-inspect.json")
		if err != nil {
			t.Fatal(err)
		}
		overview, err := dockerOverview(string(raw), false)
		if err != nil {
			t.Fatal(err)
		}
		w.detail.SetWrap(true).SetText(richOutput(overview, 0, w.palette()))
	}
	host, _ := os.Hostname()
	w.header.SetText(strings.ReplaceAll(w.header.GetText(false), host, "demo-host"))
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	type cell struct {
		Rune   string
		FG, BG int32
		Bold   bool
	}
	frames := map[string][][]cell{}
	for _, size := range [][2]int{{160, 44}, {120, 30}, {80, 24}} {
		screen.SetSize(size[0], size[1])
		w.app.ForceDraw()
		rows := [][]cell{}
		for y := 0; y < size[1]; y++ {
			row := []cell{}
			for x := 0; x < size[0]; x++ {
				r, _, style, _ := screen.GetContent(x, y)
				fg, bg, attributes := style.Decompose()
				row = append(row, cell{string(r), fg.Hex(), bg.Hex(), attributes&tcell.AttrBold != 0})
			}
			rows = append(rows, row)
		}
		frames[fmt.Sprintf("%dx%d", size[0], size[1])] = rows
	}
	data, err := json.Marshal(frames)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
