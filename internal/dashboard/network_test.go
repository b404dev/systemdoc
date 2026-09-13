package dashboard

import (
	"context"
	"fmt"
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
)

const ssFixture = `tcp LISTEN 0 511 127.0.0.1:3000 0.0.0.0:* users:(("node server",pid=42,fd=3),("worker",pid=43,fd=4)) uid:1000 ino:700 cgroup:/user.slice/dev.service
udp UNCONN 0 0 [::]:5353 [::]:* uid:70 ino:701 v6only:1
 tcp ESTAB 0 0 [fe80::1%en0]:50000 [fe80::2%en0]:443 users:(("browser",pid=44,fd=5)) uid:1000 ino:702
 tcp TIME-WAIT 0 0 10.0.0.1:50001 10.0.0.2:443 ino:0
`
const lsofFixture = "p42\x00cnode server\x00u501\x00Lalice\x00\nf3\x00tIPv6\x00PTCP\x00n[::1]:3000\x00TST=LISTEN\x00\nf4\x00tIPv4\x00PTCP\x00n127.0.0.1:55000->127.0.0.1:5432\x00TST=ESTABLISHED\x00\np44\x00cmDNSResponder\x00u65\x00L_mdnsresponder\x00\nf5\x00tIPv6\x00PUDP\x00n*:5353\x00\nf6\x00tIPv4\x00PUDP\x00n10.0.0.1:5000->10.0.0.2:5000\x00\n"
const procNetDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
   lo: 1024 10 0 0 0 0 0 0 2048 20 0 0 0 0 0 0
 eth0: 4096 40 0 0 0 0 0 0 8192 80 0 0 0 0 0 0
`
const darwinNetstatFixture = `Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll
lo0 16384 <Link#1> 00:00:00:00:00:00 100 0 1024 100 0 2048 0
en0 1500 <Link#6> aa:bb:cc:dd:ee:ff 200 0 4096 300 0 8192 0
en0 1500 192.0.2 192.0.2.5 200 - 4096 300 - 8192 -
`

func TestSSPreservesOwnersAndIPv6(t *testing.T) {
	rows, err := parseSS(ssFixture)
	if err != nil || len(rows) != 5 {
		t.Fatal(rows, err)
	}
	if rows[0].Process != "node server" || rows[1].PID != 43 || rows[0].Local != rows[1].Local {
		t.Fatal("shared ownership lost", rows)
	}
	if rows[2].PID != 0 || rows[2].Family != "IPv6" || !rows[2].bound() {
		t.Fatal("unowned UDP socket lost", rows[2])
	}
	host, port := socketEndpoint(rows[3].Local)
	if host != "fe80::1%en0" || port != "50000" || rows[3].State != "ESTABLISHED" {
		t.Fatal(rows[3], host, port)
	}
	if rows[4].PID != 0 || rows[4].bound() {
		t.Fatal("TIME-WAIT invented an owner", rows[4])
	}
	if _, err := parseSS("Cannot open netlink socket"); err == nil {
		t.Fatal("diagnostics parsed as sockets")
	}
}
func TestLsofRecordsRetainProcessAndSocketBoundaries(t *testing.T) {
	rows, err := parseLsofSockets(lsofFixture)
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	if rows[0].Process != "node server" || rows[0].User != "alice" || rows[0].State != "LISTEN" || rows[1].PID != 42 || rows[1].Remote != "127.0.0.1:5432" {
		t.Fatal(rows)
	}
	if rows[2].PID != 44 || rows[2].Process != "mDNSResponder" || rows[2].FD != "5" || rows[2].State != "UNCONN" || rows[3].State != "CONNECTED" {
		t.Fatal(rows)
	}
	for _, raw := range []string{"p42\x00\nf3\x00PTCP\x00npartial", "f3\x00PTCP\x00n*:80\x00\n"} {
		if _, err := parseLsofSockets(raw); err == nil {
			t.Fatal("broken records accepted")
		}
	}
	if rows, err := parseLsofSockets(""); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
func TestNetworkFiltersMatchExactPortsAndKeepIPv6(t *testing.T) {
	rows, _ := parseSS(ssFixture)
	for _, test := range []struct {
		query string
		want  int
	}{{"port:3000", 2}, {"port:300", 0}, {"remoteport:443", 2}, {"localport:443", 0}, {"process:node pid:42", 1}, {"proto:udp", 1}, {"state:estab", 1}, {"local:[fe80::1%en0]:50000", 1}, {"family:ipv6", 2}, {"service:dev.service", 2}, {"unknown:x", 0}} {
		count := 0
		for _, row := range rows {
			if matchesNetworkSocket(row, test.query) {
				count++
			}
		}
		if count != test.want {
			t.Fatal(test.query, count, test.want)
		}
	}
	if socketBinding("[::1]:80") != "Loopback address" || socketBinding("0.0.0.0:80") != "All IPv4 interfaces" {
		t.Fatal("bind classification incorrect")
	}
}
func TestNetworkCollectorSeparatesDiagnosticsAndEmptyResults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lsof")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	result, err := collectNetworkSockets(context.Background(), "darwin")
	if err != nil || len(result.Sockets) != 0 {
		t.Fatal(result, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'permission denied' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := collectNetworkSockets(context.Background(), "darwin"); err == nil {
		t.Fatal("permission failure became an empty table")
	}
	fixture := filepath.Join(dir, "data")
	if err := os.WriteFile(fixture, []byte(lsofFixture), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETWORK_FIXTURE", fixture)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n/bin/cat \"$NETWORK_FIXTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err = collectNetworkSockets(context.Background(), "linux")
	if err != nil || len(result.Sockets) != 4 || !strings.Contains(result.Note, "ss is unavailable") {
		t.Fatal(result, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n/bin/cat \"$NETWORK_FIXTURE\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err = collectNetworkSockets(context.Background(), "darwin")
	if err != nil || len(result.Sockets) != 4 || !strings.Contains(result.Note, "partial results") {
		t.Fatal("lsof partial selection discarded", result, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ss"), []byte("#!/bin/sh\necho 'netlink denied' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := collectNetworkSockets(context.Background(), "linux"); err == nil {
		t.Fatal("ss failure silently fell back")
	}
}
func TestNetworkOutputOverflowCannotLoseHeaders(t *testing.T) {
	var b networkBuffer
	b.Write([]byte("p42\x00"))
	b.Write([]byte(strings.Repeat("x", 4*1024*1024)))
	if !b.overflow || b.Len() != 4*1024*1024 || !strings.HasPrefix(b.String(), "p42\x00") {
		t.Fatal("bounded output lost record origin")
	}
}

func TestNetworkSpeedCountersAndRates(t *testing.T) {
	linux, err := parseProcNetDev(procNetDevFixture)
	if err != nil || len(linux) != 2 || linux[0].Name != "eth0" || linux[0].Receive != 4096 || linux[0].Transmit != 8192 {
		t.Fatal(linux, err)
	}
	darwin, err := parseDarwinNetstat(darwinNetstatFixture)
	if err != nil || len(darwin) != 2 || darwin[0].Name != "en0" || darwin[0].Receive != 4096 || darwin[0].Transmit != 8192 {
		t.Fatal(darwin, err)
	}
	start := time.Date(2026, time.September, 12, 12, 0, 0, 0, time.UTC)
	rates := networkRates([]networkCounter{{Name: "eth0", Receive: 1000, Transmit: 2000}}, start, []networkCounter{{Name: "eth0", Receive: 4000, Transmit: 6000}, {Name: "new0", Receive: 500, Transmit: 600}}, start.Add(2*time.Second))
	if len(rates) != 2 || !rates[0].Ready || rates[0].Receive != 1500 || rates[0].Transmit != 2000 || rates[1].Ready {
		t.Fatal(rates)
	}
	reset := networkRates([]networkCounter{{Name: "eth0", Receive: 5000, Transmit: 5000}}, start, []networkCounter{{Name: "eth0", Receive: 1, Transmit: 2}}, start.Add(time.Second))
	if reset[0].Ready {
		t.Fatal("counter reset produced an invented speed", reset)
	}
}

func TestNetworkSpeedCollectorDoesNotRequireCommands(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux speed counters come directly from /proc/net/dev")
	}
	t.Setenv("PATH", t.TempDir())
	snapshot, err := collectNetworkView(context.Background(), "linux", 3)
	if err != nil || len(snapshot.Counters) == 0 || snapshot.CounterAt.IsZero() {
		t.Fatalf("speed collector unexpectedly required an external command: counters=%d err=%v", len(snapshot.Counters), err)
	}
	if len(snapshot.Sockets) != 0 || len(snapshot.Interfaces) != 0 {
		t.Fatal("speed collector performed unrelated network collection")
	}
}

func TestNetworkPageFilteringFocusAndResize(t *testing.T) {
	w := testWorkspace()
	w.networkPage()
	_, page := w.pages.GetFrontPage()
	n := page.(*networkPage)
	n.snapshot = networkSnapshot{Sockets: mustSS(t), Source: "fixture", At: time.Now(), Interfaces: []networkInterface{{Name: "en0", Addresses: "10.0.0.1/24", Flags: "up", MTU: 1500}}}
	n.rates = []networkRate{{Name: "en0", Receive: 1536, Transmit: 2048, ReceiveTotal: 4096, SendTotal: 8192, Ready: true}}
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
	n.snapshot.Sockets = append([]networkSocket{{Protocol: "TCP", State: "LISTEN", Local: "*:80", PID: 1}}, n.snapshot.Sockets...)
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
func mustSS(t *testing.T) []networkSocket {
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
	_, port := socketEndpoint(listener.Addr().String())
	platforms := []string{runtime.GOOS}
	if runtime.GOOS != "darwin" {
		if _, err := exec.LookPath("lsof"); err == nil {
			platforms = append(platforms, "darwin")
		}
	}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			result, err := collectNetworkSockets(context.Background(), platform)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range result.Sockets {
				_, p := socketEndpoint(s.Local)
				if p == port && s.State == "LISTEN" && s.PID == os.Getpid() {
					return
				}
			}
			t.Fatal(fmt.Sprintf("test listener on port %s with current PID not found", port))
		})
	}
}
