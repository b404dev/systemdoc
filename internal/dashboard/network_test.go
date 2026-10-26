package dashboard

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"systemdoc/internal/hostnet"
)

const ssFixture = `tcp LISTEN 0 511 127.0.0.1:3000 0.0.0.0:* users:(("node server",pid=42,fd=3),("worker",pid=43,fd=4)) uid:1000 ino:700 cgroup:/user.slice/dev.service
udp UNCONN 0 0 [::]:5353 [::]:* uid:70 ino:701 v6only:1
 tcp ESTAB 0 0 [fe80::1%en0]:50000 [fe80::2%en0]:443 users:(("browser",pid=44,fd=5)) uid:1000 ino:702
 tcp TIME-WAIT 0 0 10.0.0.1:50001 10.0.0.2:443 ino:0
`

func TestNetworkPageFilteringFocusAndResize(t *testing.T) {
	w := testWorkspace()
	w.networkPage()
	_, page := w.pages.GetFrontPage()
	n := page.(*networkPage)
	n.snapshot = hostnet.Snapshot{Sockets: mustSS(t), Source: "fixture", At: time.Now(), Interfaces: []hostnet.Interface{{Name: "en0", Addresses: "10.0.0.1/24", Flags: "up", MTU: 1500}}}
	n.rates = []hostnet.Rate{{Name: "en0", Receive: 1536, Transmit: 2048, ReceiveTotal: 4096, SendTotal: 8192, Ready: true}}
	n.render()
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	for _, size := range [][2]int{{160, 44}, {120, 30}, {80, 24}, {40, 12}} {
		screen.SetSize(size[0], size[1])
		w.app.ForceDraw()
		if n.table.GetRowCount() != 4 {
			t.Fatal("ports view should show three owner rows", n.table.GetRowCount())
		}
	}
	n.search.SetText("port:3000")
	if len(n.visible) != 2 {
		t.Fatal("filter failed")
	}
	n.table.Select(2, 0)
	selected := n.selected
	n.snapshot.Sockets = append([]hostnet.Socket{{Protocol: "TCP", State: "LISTEN", Local: "*:80", PID: 1}}, n.snapshot.Sockets...)
	n.render()
	if n.selected != selected {
		t.Fatal("refresh changed selected socket")
	}
	w.app.SetFocus(n.search)
	e := tcell.NewEventKey(tcell.KeyRune, 'c', 0)
	if n.GetInputCapture()(e) != e || n.view != 0 {
		t.Fatal("typing switched views")
	}
	n.switchView(1)
	if len(n.visible) != 6 {
		t.Fatal("connections missing sockets")
	}
	n.switchView(2)
	if len(n.interfaces) != 1 {
		t.Fatal("interface missing")
	}
	n.switchView(3)
	if len(n.visibleRates) != 1 || !strings.Contains(n.table.GetTitle(), "SPEED") || !strings.Contains(n.detail.GetText(true), "DOWNLOAD") {
		t.Fatal("speed toggle did not show live interface rates")
	}
	w.app.SetFocus(n.table)
	w.reviewSnapshot(n.report())
	_, review := w.pages.GetFrontPage()
	review.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if !n.table.HasFocus() {
		t.Fatal("closing export lost network focus")
	}
	n.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), func(p tview.Primitive) { w.app.SetFocus(p) })
	if front, _ := w.pages.GetFrontPage(); front != "main" {
		t.Fatal("network did not close")
	}
}
func mustSS(t *testing.T) []hostnet.Socket {
	t.Helper()
	rows, err := parseSS(ssFixture)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestNetworkRefreshPreservesSnapshotOnFailure(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output")
	if err := os.WriteFile(output, []byte(ssFixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETWORK_FIXTURE", output)
	failed := filepath.Join(dir, "failed")
	t.Setenv("NETWORK_FAILURE", failed)
	script := "#!/bin/sh\nif [ -f \"$NETWORK_FAILURE\" ]; then echo failed >&2; exit 1; fi\n/bin/cat \"$NETWORK_FIXTURE\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ss"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorkspace(ctx, false)
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- w.app.Run() }()
	defer func() { cancel(); w.app.Stop(); <-done }()
	var n *networkPage
	w.app.QueueUpdateDraw(func() { w.networkPage(); _, page := w.pages.GetFrontPage(); n = page.(*networkPage) })
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			busy := true
			w.app.QueueUpdateDraw(func() { busy = n.busy })
			if !busy {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("network refresh timed out")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait()
	if err := os.WriteFile(failed, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	w.app.QueueUpdateDraw(func() { n.table.Select(2, 0); n.refresh() })
	wait()
	w.app.QueueUpdateDraw(func() {
		if len(n.snapshot.Sockets) != 5 || n.failure[n.view] == "" || n.visible[1].PID != 43 {
			t.Error("refresh failure discarded snapshot or owner")
		}
	})
}

// Run explicitly on a host with socket enumeration permissions. No remote hosts
// are contacted; an ephemeral loopback listener provides a known local port.
func TestLiveNetworkReadOnly(t *testing.T) {
	if os.Getenv("SYSTEMDOC_LIVE_NETWORK") != "1" {
		t.Skip("set SYSTEMDOC_LIVE_NETWORK=1 to verify host sockets")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port := hostnet.Endpoint(listener.Addr().String())
	platforms := []string{runtime.GOOS}
	if runtime.GOOS != "darwin" {
		if _, err := exec.LookPath("lsof"); err == nil {
			platforms = append(platforms, "darwin")
		}
	}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			result, err := hostnet.CollectSockets(context.Background(), platform)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range result.Sockets {
				_, p := hostnet.Endpoint(s.Local)
				if p == port && s.State == "LISTEN" && s.PID == os.Getpid() {
					return
				}
			}
			t.Fatalf("test listener on port %s with current PID not found", port)
		})
	}
}
