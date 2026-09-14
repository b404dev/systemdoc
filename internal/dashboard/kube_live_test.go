package dashboard

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveKubernetesStack is an explicit release check against a real
// single-node cluster. Set SYSTEMDOC_LIVE_KUBE=1 with a reachable kubectl (or
// SYSTEMDOC_KUBECTL) to run it; it is skipped otherwise.
func TestLiveKubernetesStack(t *testing.T) {
	if os.Getenv("SYSTEMDOC_LIVE_KUBE") == "" {
		t.Skip("set SYSTEMDOC_LIVE_KUBE=1 to exercise a real cluster")
	}
	resetKubeState()
	t.Cleanup(resetKubeState)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := inventory(ctx, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stack: %s", kubeLabel())
	t.Logf("context: %s", containerContextLabel(os.Getenv("DOCKER_CONTEXT")))
	if kubeLabel() == "" {
		t.Fatal("no Kubernetes stack detected")
	}
	var pod workload
	for _, item := range items {
		t.Logf("%-52s %-12s %-8s %-10s %-24s %s", item.Name, item.State, available(item.CPU), available(item.Memory), item.Project, item.Detail)
		if isPod(item) && isActive(item) && pod.ID == "" {
			pod = item
		}
	}
	if pod.ID == "" {
		t.Fatal("no running pod to inspect")
	}
	for tab, name := range []string{"overview", "logs", "manifest", "metrics", "connections"} {
		out, err := inspect(ctx, 1, false, pod, tab)
		if err != nil && tab != 3 {
			t.Fatalf("%s: %v", name, err)
		}
		if err != nil {
			t.Logf("%s unavailable (expected without metrics-server): %v", name, err)
			continue
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) > 40 {
			lines = append(lines[:40], "…")
		}
		t.Logf("── %s of %s\n%s", name, pod.Name, strings.Join(lines, "\n"))
	}
}
