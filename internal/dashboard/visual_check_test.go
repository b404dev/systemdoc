package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// This opt-in test exports terminal cells for visual review, without adding any
// fixture mode or screenshot code to the application binary.
func TestVisualReview(t *testing.T) {
	path := os.Getenv("SYSTEMDOC_VISUAL_REVIEW")
	if path == "" {
		t.Skip("set SYSTEMDOC_VISUAL_REVIEW to export terminal cells")
	}
	w := testWorkspace()
	w.hostLabel = "demo-host"
	for i, theme := range themes {
		if theme.name == os.Getenv("SYSTEMDOC_VISUAL_THEME") {
			w.theme = i
		}
	}
	w.items[0] = []workload{
		{ID: "nginx.service", Name: "nginx.service", State: "active", Detail: "running", Description: "A high performance web server and reverse proxy", Enablement: "enabled", CPU: "12.4%", Memory: "148.0 MiB", PID: 1248},
		{ID: "worker.service", Name: "worker.service", State: "failed", Detail: "exit-code", Description: "Background queue worker", Enablement: "enabled", CPU: "", Memory: ""},
	}
	for i, name := range []string{"postgresql", "redis", "sshd", "docker", "containerd", "cron", "NetworkManager", "systemd-journald", "systemd-resolved", "backup", "prometheus", "grafana-server", "node-exporter"} {
		w.items[0] = append(w.items[0], workload{ID: name + ".service", Name: name + ".service", State: "active", Detail: "running", Enablement: "enabled", CPU: fmt.Sprintf("%.1f%%", float64(i)/3), Memory: fmt.Sprintf("%.1f MiB", float64(i+1)*11)})
	}
	w.selected[0] = "nginx.service"
	w.lastRefresh[0] = time.Now()
	for i := 0; i < 24; i++ {
		w.fleetHistory[0] = append(w.fleetHistory[0], fleetSample{cpuCount: 14, memoryCount: 14, cpu: float64((i * 7) % 19), memory: float64(100 + i*3)})
		w.sampleWorkloads(0, []workload{{ID: "nginx.service", Name: "nginx.service", State: "active", CPU: fmt.Sprintf("%.1f%%", float64((i*5)%17)), Memory: fmt.Sprintf("%.1f MiB", 120+float64(i))}})
	}
	w.activity = []activityEvent{
		{mode: 0, id: "nginx.service", text: "activating → active / running", at: time.Now().Add(-3 * time.Minute)},
		{mode: 0, id: "worker.service", text: "active → failed / exit-code", at: time.Now().Add(-90 * time.Second)},
		{mode: 0, id: "nginx.service", text: "CPU crossed 10%", at: time.Now().Add(-20 * time.Second)},
	}
	for i := 0; i < 24; i++ {
		w.recordHostUtilisation(hostUtilisation{cpuPercent: float64(18 + (i*7)%34), cpuOK: true, memPercent: float64(52 + (i*3)%11), memUsed: 10960000000, memTotal: 16 * 1024 * 1024 * 1024, memOK: true})
	}
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
		for i := 0; i < 24; i++ {
			w.sampleWorkloads(1, []workload{{ID: "web", Name: "website-web-1", State: "running", CPU: fmt.Sprintf("%.1f%%", float64((i*3)%9)), Memory: fmt.Sprintf("%.1f MiB", 40+float64(i)/2)}})
		}
		w.activity = []activityEvent{{mode: 1, id: "website-worker-1", text: "running → restarting", at: time.Now().Add(-time.Minute)}, {mode: 1, id: "website-web-1", text: "health starting → healthy", at: time.Now().Add(-15 * time.Second)}}
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
	if mode := os.Getenv("SYSTEMDOC_VISUAL_MODE"); mode == "network" || mode == "network-speed" {
		w.networkPage()
		_, page := w.pages.GetFrontPage()
		n := page.(*networkPage)
		n.snapshot = networkSnapshot{Source: "fixture · illustrative data", CounterSource: "fixture · host interface counters", At: time.Now(), CounterAt: time.Now(), Sockets: []networkSocket{
			{Protocol: "TCP", Family: "IPv4", State: "LISTEN", Local: "127.0.0.1:3000", Remote: "0.0.0.0:*", Process: "node", PID: 1842, User: "demo", FD: "21", Cgroup: "/user.slice/dev-server.service"},
			{Protocol: "TCP", Family: "IPv6", State: "LISTEN", Local: "[::]:5432", Remote: "[::]:*", Process: "postgres", PID: 2201, User: "postgres", FD: "6"},
			{Protocol: "TCP", Family: "IPv4", State: "LISTEN", Local: "0.0.0.0:8080", Remote: "0.0.0.0:*", Process: "python", PID: 3244, User: "demo", FD: "3"},
			{Protocol: "UDP", Family: "IPv4", State: "UNCONN", Local: "0.0.0.0:5353", Remote: "0.0.0.0:*", Process: "", PID: 0, UID: "70"},
		}}
		n.rates = []networkRate{
			{Name: "eth0", Receive: 24.8 * 1024 * 1024, Transmit: 3.7 * 1024 * 1024, ReceiveTotal: 18 * 1024 * 1024 * 1024, SendTotal: 7 * 1024 * 1024 * 1024, Ready: true},
			{Name: "tailscale0", Receive: 1.2 * 1024 * 1024, Transmit: 840 * 1024, ReceiveTotal: 4 * 1024 * 1024 * 1024, SendTotal: 2 * 1024 * 1024 * 1024, Ready: true},
			{Name: "lo", Receive: 128 * 1024, Transmit: 128 * 1024, ReceiveTotal: 900 * 1024 * 1024, SendTotal: 900 * 1024 * 1024, Ready: true},
		}
		n.rateInterval = 5 * time.Second
		for sample := 0; sample < 18; sample++ {
			for _, rate := range n.rates {
				n.rateHistory[rate.Name] = appendSignal(n.rateHistory[rate.Name], (rate.Receive+rate.Transmit)*float64(8+(sample*7)%9)/12)
			}
			n.downloadHistory = appendSignal(n.downloadHistory, float64(10+(sample*5)%17)*1024*1024)
			n.uploadHistory = appendSignal(n.uploadHistory, float64(2+(sample*3)%8)*1024*1024)
		}
		n.header.SetText(" PORTS & NETWORK · demo-host\n Host sockets · numeric addresses · independent of Docker context")
		if mode == "network-speed" {
			n.switchView(3)
		} else {
			n.render()
		}
	}
	if mode := os.Getenv("SYSTEMDOC_VISUAL_MODE"); mode == "processes" || mode == "storage" {
		tab := 3
		if mode == "storage" {
			tab = 4
		}
		w.hostPage(tab)
		_, page := w.pages.GetFrontPage()
		h := page.(*hostPage)
		h.processes, _ = parseProcesses(processFixture)
		h.mounts, _ = parseMounts(macDiskFixture, "darwin", false)
		h.at[0] = time.Now()
		h.render()
	}
	if os.Getenv("SYSTEMDOC_VISUAL_MODE") == "constellation" {
		body := constellationText(w.current(), 0, "nginx.service\nnetwork-online.target\npostgresql.service\nredis.service\nsystem.slice\n", w.settings.NerdIcons)
		view := textView().SetDynamicColors(true).SetScrollable(true).SetWrap(false)
		view.SetText(richOutput(body, 4, w.palette())).SetBorder(true).SetTitle(" SYSTEM CONSTELLATION · sampled 18:53:51 · Esc returns ")
		w.pages.AddPage("constellation-fixture", centered(view, 124, 34), true, true)
	}
	if os.Getenv("SYSTEMDOC_VISUAL_MODE") == "approval" {
		w.confirm("restart · nginx.service", "systemctl restart -- nginx.service\n\nThe service may be briefly unavailable. Systemdoc will retain the operation result for review.", func() {})
	}
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
