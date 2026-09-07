package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestDockerOverviewDescribesContainerAndConnections(t *testing.T) {
	raw, err := os.ReadFile("testdata/docker-inspect.json")
	if err != nil {
		t.Fatal(err)
	}
	out, err := dockerOverview(string(raw), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"website-web-1", "nginx:alpine", "healthy", "unless-stopped", "127.0.0.1:8080 → 80/tcp", "[::1]:8080 → 80/tcp", "exposed only", "website_data → /data", "/srv/website/nginx.conf → /etc/nginx/nginx.conf", "read-only", "read-write", "tmpfs → /tmp", "172.20.0.4", "fd00::4", "website_default", "256.0 MiB", "1.00 CPUs", "app.owner"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "not-for-overview") || strings.Contains(out, "0001-01-01") {
		t.Fatal("overview leaked env value or zero timestamp")
	}
	connections, err := dockerOverview(string(raw), true)
	if err != nil || !strings.Contains(connections, "VOLUMES & MOUNTS") || strings.Contains(connections, "── RUNTIME") {
		t.Fatal(connections, err)
	}
}

func TestDockerOverviewHandlesStoppedAndMissingData(t *testing.T) {
	raw := `[{"Id":"abc","Name":"/stopped","State":{"Status":"exited","ExitCode":137,"OOMKilled":true},"HostConfig":{"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":"8080"}]}},"NetworkSettings":{"Ports":{"80/tcp":null},"Networks":{"missing":null}}}]`
	out, err := dockerOverview(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exited", "137", "OOM killed   yes", "configured; no live binding reported", "not configured", "none reported", "unavailable"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	for _, raw := range []string{"bad", "[]", "[{}]", "[{\"Id\":\"one\"},{\"Id\":\"two\"}]"} {
		if _, err := dockerOverview(raw, false); err == nil {
			t.Fatal("accepted invalid inspection", raw)
		}
	}
}

func TestDockerInspectionRoutesOverviewAndRawConfig(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs("testdata/docker-inspect.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_FIXTURE", fixture)
	script := `#!/bin/sh
[ "$1" = inspect ] && [ "$2" = --type ] && [ "$3" = container ] && [ "$4" = -- ] && [ "$5" = abc ] || exit 90
/bin/cat "$DOCKER_FIXTURE"
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, tab := range []int{0, 2, 4} {
		out, err := inspect(context.Background(), 1, false, workload{ID: "abc"}, tab)
		if err != nil {
			t.Fatal(err)
		}
		if tab == 2 && !strings.Contains(out, "DEMO_SECRET=not-for-overview") {
			t.Fatal("raw inspection was lost")
		}
		if tab != 2 && !strings.Contains(out, "VOLUMES & MOUNTS") {
			t.Fatal("structured view was not used")
		}
	}
	raw := `[{"Id":"abc","Name":"/[red]name","Config":{"Labels":{"label":"[blue]value"}}}]`
	out, err := dockerOverview(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	plain := tview.NewTextView().SetDynamicColors(true).SetText(richOutput(out, 0, themes[0])).GetText(true)
	if !strings.Contains(plain, "[red]name") || !strings.Contains(plain, "[blue]value") {
		t.Fatal("untrusted Docker markup was interpreted")
	}
}
