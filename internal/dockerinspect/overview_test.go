package dockerinspect

import (
	"os"
	"strings"
	"testing"
)

func TestDockerOverviewDescribesContainerAndConnections(t *testing.T) {
	raw, err := os.ReadFile("testdata/docker-inspect.json")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Overview(string(raw), false)
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
	connections, err := Overview(string(raw), true)
	if err != nil || !strings.Contains(connections, "VOLUMES & MOUNTS") || strings.Contains(connections, "── RUNTIME") {
		t.Fatal(connections, err)
	}
}

func TestDockerOverviewHandlesStoppedAndMissingData(t *testing.T) {
	raw := `[{"Id":"abc","Name":"/stopped","State":{"Status":"exited","ExitCode":137,"OOMKilled":true},"HostConfig":{"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":"8080"}]}},"NetworkSettings":{"Ports":{"80/tcp":null},"Networks":{"missing":null}}}]`
	out, err := Overview(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exited", "137", "OOM killed   yes", "configured; no live binding reported", "not configured", "none reported", "unavailable"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	for _, raw := range []string{"bad", "[]", "[{}]", "[{\"Id\":\"one\"},{\"Id\":\"two\"}]"} {
		if _, err := Overview(raw, false); err == nil {
			t.Fatal("accepted invalid inspection", raw)
		}
	}
}
