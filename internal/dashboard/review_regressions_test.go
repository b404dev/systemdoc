package dashboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// A tool that warns on stderr while succeeding must not corrupt the output
// that JSON and table consumers decode.
func TestRunBoundedKeepsStderrOutOfSuccessfulOutput(t *testing.T) {
	output, err := runBounded(context.Background(), 5*time.Second, 0, "sh", "-c", `echo 'WARNING: noise' >&2; echo '{"ok":true}'`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != `{"ok":true}` {
		t.Fatalf("stdout was contaminated: %q", output)
	}
	_, err = runBounded(context.Background(), 5*time.Second, 0, "sh", "-c", `echo 'detail on stderr' >&2; exit 3`)
	if err == nil || !strings.Contains(err.Error(), "detail on stderr") {
		t.Fatalf("failure should carry stderr: %v", err)
	}
}

// A cancelled command whose grandchild keeps the pipe open must still return
// promptly instead of wedging the caller until the grandchild exits.
func TestRunBoundedReturnsWhenGrandchildHoldsPipe(t *testing.T) {
	started := time.Now()
	_, err := runBounded(context.Background(), 300*time.Millisecond, 0, "sh", "-c", `sleep 30 & wait`)
	if err == nil {
		t.Fatal("expected the timeout to be reported")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("runBounded blocked for %s on a pipe-holding grandchild", elapsed)
	}
}

func TestKubeJSONReadReportsTruncation(t *testing.T) {
	if !jsonRequest([]string{"get", "pods", "-o", "json"}) || !jsonRequest([]string{"get", "nodes", "--output=json"}) {
		t.Fatal("JSON requests not recognised")
	}
	if jsonRequest([]string{"logs", "--tail", "200", "-o"}) {
		t.Fatal("non-JSON request treated as JSON")
	}
	if outputLimit(0) != 1024*1024 || outputLimit(8) != 8 {
		t.Fatal("outputLimit default is wrong")
	}
}

func TestLogSearchTimeBoundsSeePodPrefixedStamps(t *testing.T) {
	at, _ := time.ParseInLocation(time.RFC3339, "2026-09-14T10:00:00Z", time.Local)
	raw := "[pod/web/web-1/nginx] 2026-09-14T10:00:00Z error one\n[pod/web/web-1/nginx] 2026-09-14T12:00:00Z error two\n"
	_, hits, err := searchRetainedLogs(raw, logSearchOptions{Query: "error", Since: at, Until: at.Add(time.Hour)})
	if err != nil || len(hits) != 1 {
		t.Fatalf("pod-prefixed line inside the window should match once: hits=%v err=%v", hits, err)
	}
	if _, err := parseLogTime("2026-09-14 10:00:00.123456+0100"); err != nil {
		t.Fatalf("launchd log stamp rejected: %v", err)
	}
}

// A 256-colour terminal takes the flat paint path and says so in the status.
func TestLimitedColourTerminalDrawsFlatAndSaysSo(t *testing.T) {
	w := testWorkspace()
	w.colourDepthKnown = false
	screen := tcell.NewSimulationScreen("UTF-8")
	w.app.SetScreen(screen)
	defer screen.Fini()
	screen.SetSize(160, 44)
	w.app.ForceDraw()
	if !w.limitedColours {
		t.Fatal("simulation screen reports 256 colours; limitedColours should be set")
	}
	w.updateDashboard()
	w.app.ForceDraw()
	if !strings.Contains(w.summary.GetText(true), "256 colours") {
		t.Fatalf("status should mention the colour limit: %q", w.summary.GetText(true))
	}
}
