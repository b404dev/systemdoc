package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKubeNodeDetectionNamesTheDistribution(t *testing.T) {
	for _, test := range []struct {
		raw, distro, version string
	}{
		{`{"items":[{"metadata":{"name":"lab"},"status":{"nodeInfo":{"kubeletVersion":"v1.31.2+k0s"},"conditions":[{"type":"Ready","status":"True"}]}}]}`, "k0s", "v1.31.2"},
		{`{"items":[{"metadata":{"name":"lab"},"status":{"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}}]}`, "k3s", "v1.30.4"},
		{`{"items":[{"metadata":{"name":"dev-control-plane"},"spec":{"providerID":"kind://docker/dev/dev-control-plane"},"status":{"nodeInfo":{"kubeletVersion":"v1.31.0"}}}]}`, "kind", "v1.31.0"},
		{`{"items":[{"metadata":{"name":"minikube","labels":{"minikube.k8s.io/name":"minikube"}},"status":{"nodeInfo":{"kubeletVersion":"v1.31.0"}}}]}`, "minikube", "v1.31.0"},
		{`{"items":[{"metadata":{"name":"lab","labels":{"microk8s.io/cluster":"true"}},"status":{"nodeInfo":{"kubeletVersion":"v1.31.0"}}}]}`, "microk8s", "v1.31.0"},
		{`{"items":[{"metadata":{"name":"k3d-dev-server-0"},"status":{"nodeInfo":{"kubeletVersion":"v1.30.4+k3s1"}}}]}`, "k3d", "v1.30.4"},
		{`{"items":[{"metadata":{"name":"docker-desktop"},"status":{"nodeInfo":{"kubeletVersion":"v1.30.2"}}}]}`, "docker-desktop", "v1.30.2"},
		{`{"items":[{"metadata":{"name":"node-a"},"status":{"nodeInfo":{"kubeletVersion":"v1.29.0"}}}]}`, "kubernetes", "v1.29.0"},
	} {
		var stack kubeStack
		if err := describeKubeNodes(test.raw, &stack); err != nil {
			t.Fatal(test.raw, err)
		}
		if stack.distro != test.distro || stack.version != test.version || stack.nodes != 1 {
			t.Fatalf("%s: %+v", test.raw, stack)
		}
	}
	var stack kubeStack
	if err := describeKubeNodes(`{"items":[]}`, &stack); err == nil {
		t.Fatal("empty node list accepted")
	}
	if err := describeKubeNodes(`not json`, &stack); err == nil {
		t.Fatal("invalid node list accepted")
	}
	ready := kubeStack{distro: "k0s", version: "v1.31.2", nodes: 1, readyNodes: 1}
	if ready.label() != "k0s v1.31.2 · single node" {
		t.Fatal(ready.label())
	}
	degraded := kubeStack{distro: "k3s", nodes: 3, readyNodes: 2}
	if degraded.label() != "k3s · 2 of 3 nodes ready" {
		t.Fatal(degraded.label())
	}
}

func TestPodListUsesTheDashboardStateVocabulary(t *testing.T) {
	raw, err := os.ReadFile("testdata/kube-pods.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parsePodList(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]workload{}
	for _, item := range items {
		byName[item.Name] = item
	}
	for _, test := range []struct {
		name, state, detail, owner string
		attention                  bool
	}{
		{"shop/web-6d4cf56db6-x7k2p", "running", "2/2 ready · 2 restarts", "deployment/web", false},
		{"shop/worker-0", "restarting", "CrashLoopBackOff · 0/1 ready · 7 restarts", "statefulset/worker", true},
		{"default/broken-image", "failed", "ImagePullBackOff · 0/1 ready", "", true},
		{"shop/migrate-abc12", "succeeded", "Completed", "job/migrate", false},
		{"shop/cache-0", "pending", "ContainerCreating · 0/1 ready", "", false},
		{"shop/api-7f9c8b5d4-q2w3e", "running", "not ready · 1/2 ready", "deployment/api", true},
		{"default/unschedulable", "pending", "Unschedulable · 0/1 ready", "", false},
		{"default/leaving", "terminating", "Terminating · 1/1 ready", "", false},
	} {
		item, ok := byName[test.name]
		if !ok {
			t.Fatalf("missing %s in %v", test.name, items)
		}
		if item.State != test.state || item.Detail != test.detail || item.Owner != test.owner || needsAttention(item) != test.attention {
			t.Fatalf("%s: %+v (attention %v)", test.name, item, needsAttention(item))
		}
		if !isPod(item) || item.ID != podID(item.Project, strings.TrimPrefix(item.Name, item.Project+"/")) {
			t.Fatalf("%s: unexpected identity %q", test.name, item.ID)
		}
	}
	web := byName["shop/web-6d4cf56db6-x7k2p"]
	if web.Description != "nginx:alpine, busybox:1.36" || web.Project != "shop" || !isActive(web) {
		t.Fatal(web)
	}
	if isActive(byName["shop/migrate-abc12"]) || isActive(byName["shop/cache-0"]) {
		t.Fatal("completed or pending pods counted as active")
	}
	if !strings.HasPrefix(strings.ToLower(byName["shop/worker-0"].Name), "shop/") {
		t.Fatal("namespace missing from the display name")
	}
}

func TestKubeQuantitiesConvertToDashboardUnits(t *testing.T) {
	for _, test := range []struct {
		value string
		cpu   float64
	}{{"250m", 25}, {"1", 100}, {"1500m", 150}, {"0", 0}, {"2500000n", 0.25}} {
		if got, ok := kubeCPUPercent(test.value); !ok || got != test.cpu {
			t.Fatalf("%s → %v %v", test.value, got, ok)
		}
	}
	for _, test := range []struct {
		value string
		mib   float64
	}{{"12Mi", 12}, {"1Gi", 1024}, {"512Ki", 0.5}, {"1048576", 1}} {
		if got, ok := kubeMemoryMiB(test.value); !ok || got != test.mib {
			t.Fatalf("%s → %v %v", test.value, got, ok)
		}
	}
	if _, ok := kubeCPUPercent("lots"); ok {
		t.Fatal("garbage CPU accepted")
	}
	if _, ok := memoryValue("12.0 MiB"); !ok {
		t.Fatal("formatted pod memory must feed the fleet totals")
	}
	output := kubeResourceOutput(workload{Name: "shop/web"}, "web-abc nginx 3m 15Mi\nweb-abc sidecar 250m 1Gi\n")
	for _, want := range []string{"0.3% (3m)", "25.0% (250m)", "15.0 MiB", "1024.0 MiB", "CPU 25.3%", "1039.0 MiB"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
	if !strings.Contains(kubeResourceOutput(workload{Name: "shop/web"}, ""), "metrics-server") {
		t.Fatal("missing metrics guidance")
	}
}

func TestKubePodOverviewAndConnections(t *testing.T) {
	pod, err := os.ReadFile("testdata/kube-pod.json")
	if err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile("testdata/kube-events.json")
	if err != nil {
		t.Fatal(err)
	}
	services, err := os.ReadFile("testdata/kube-services.json")
	if err != nil {
		t.Fatal(err)
	}
	overview, err := kubePodOverview(string(pod), string(events), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"web-6d4cf56db6-x7k2p", "deployment/web", "2/2 ready · 2 restarts", "Burstable", "nginx:alpine", "running since 2026-09-14T08:00:01Z", "OOMKilled · exit 137", "cpu 100m · memory 64Mi", "liveness, readiness", "2 entries · c opens full manifest", "wait-db", "terminated · Completed · exit 0", "80/TCP · http", "host 8443 → 8443/TCP", "persistentVolumeClaim web-data", "nginx → /etc/nginx/nginx.conf (subPath nginx.conf) · read-only", "configMap web-config", "secret web-tls", "emptyDir", "hostPath /srv/unused", "not mounted by any container", "BackOff", "(×3)", "kubelet", "default-scheduler", "app          web", "Annotations  2"} {
		if !strings.Contains(overview, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(overview, "not-for-overview") || strings.Contains(overview, "restartedAt") {
		t.Fatal("overview leaked an environment value or annotation content")
	}
	if strings.Index(overview, "BackOff") > strings.Index(overview, "Pulled") {
		t.Fatal("events are not newest first")
	}
	connections, err := kubePodConnections(string(pod), string(services), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"10.244.0.12", "fd00:10:244::c", "192.168.1.20", "pod network", "web · NodePort", "10.96.12.5", "80/TCP → http · http · node port 30080", "SERVICES"} {
		if !strings.Contains(connections, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(connections, "api · ClusterIP") || strings.Contains(connections, "headless-no-selector") || strings.Contains(connections, "── RECENT EVENTS") {
		t.Fatal("connections listed a service that does not select the pod, or mixed in events")
	}
	degraded, err := kubePodOverview(string(pod), "", os.ErrPermission)
	if err != nil || !strings.Contains(degraded, "Events       unavailable") {
		t.Fatal("events failure must be reported, not fatal", err)
	}
	for _, raw := range []string{"bad", "{}", `{"metadata":{}}`} {
		if _, err := kubePodOverview(raw, "", nil); err == nil {
			t.Fatal("accepted invalid pod", raw)
		}
	}
}

// fakeContainerBackends installs docker and kubectl stand-ins on PATH.
func fakeContainerBackends(t *testing.T, docker, kubectl string) {
	t.Helper()
	dir := t.TempDir()
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIXTURES", fixtures)
	if docker != "" {
		if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if kubectl != "" {
		if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(kubectl), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("SYSTEMDOC_KUBECTL", "")
	resetKubeState()
	t.Cleanup(resetKubeState)
}

const fakeKubectl = `#!/bin/sh
case "$*" in
 *"get nodes"*) /bin/cat "$FIXTURES/kube-nodes.json" ;;
 *"get pods --all-namespaces"*) /bin/cat "$FIXTURES/kube-pods.json" ;;
 *"top pods --all-namespaces"*) printf 'shop web-6d4cf56db6-x7k2p 250m 96Mi\nshop worker-0 1m 8Mi\n' ;;
 *"top pod --namespace shop --containers"*) printf 'web-6d4cf56db6-x7k2p nginx 3m 15Mi\n' ;;
 *"get pod --namespace shop -o json"*) /bin/cat "$FIXTURES/kube-pod.json" ;;
 *"get pod --namespace shop -o yaml"*) printf 'apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-6d4cf56db6-x7k2p\n' ;;
 *"get services --namespace shop"*) /bin/cat "$FIXTURES/kube-services.json" ;;
 *"get events --namespace shop"*) /bin/cat "$FIXTURES/kube-events.json" ;;
 *"logs --namespace shop"*) printf '[pod/web-6d4cf56db6-x7k2p/nginx] 2026-09-14T08:00:01Z ready\n' ;;
 *) echo "unexpected: $*" >&2; exit 90 ;;
esac
`

const fakeDocker = `#!/bin/sh
case "$*" in
 *"ps --all"*) printf '%s\n' '{"ID":"abc","Names":"dev-control-plane","State":"running","Status":"Up 3 hours","Image":"kindest/node:v1.31.0","Labels":"io.x-k8s.kind.cluster=dev,io.x-k8s.kind.role=control-plane"}' '{"ID":"def","Names":"website-web-1","State":"running","Status":"Up 1 hour (healthy)","Image":"nginx:alpine","Labels":"com.docker.compose.project=website"}' ;;
 *stats*) printf '%s\n' '{"ID":"abc","CPUPerc":"12.5%","MemUsage":"1.2GiB / 16GiB"}' ;;
 *) exit 90 ;;
esac
`

func TestContainerInventoryMergesDockerAndKubernetes(t *testing.T) {
	fakeContainerBackends(t, fakeDocker, fakeKubectl)
	items, err := inventory(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 || items[0].ID != "abc" || items[0].Project != "kind:dev" || items[1].Project != "website" {
		t.Fatalf("containers were not listed first with their projects: %+v", items[:2])
	}
	if items[0].CPU != "12.5%" || items[0].Memory != "1228.8 MiB" {
		t.Fatalf("docker resources lost: %+v", items[0])
	}
	var web, worker workload
	for _, item := range items {
		switch item.Name {
		case "shop/web-6d4cf56db6-x7k2p":
			web = item
		case "shop/worker-0":
			worker = item
		}
	}
	if web.CPU != "25.0%" || web.Memory != "96.0 MiB" {
		t.Fatalf("pod metrics not applied: %+v", web)
	}
	if worker.CPU != "" || worker.Memory != "" {
		t.Fatalf("metrics were applied to a pod that is not running: %+v", worker)
	}
	if kubeLabel() != "k0s v1.31.2 · single node" {
		t.Fatal(kubeLabel())
	}
	if label := containerContextLabel("default"); label != "default + k0s v1.31.2 · single node" {
		t.Fatal(label)
	}
	totals := fleetTotals(items)
	if totals.active != 4 || totals.attention != 3 {
		t.Fatalf("fleet totals: %+v", totals)
	}
}

func TestKubernetesKeepsTheSuiteUsableWithoutDocker(t *testing.T) {
	fakeContainerBackends(t, "", fakeKubectl)
	items, err := listWorkloads(context.Background(), 1, false)
	if err != nil {
		t.Fatal("pods alone should satisfy the Containers suite:", err)
	}
	if len(items) != 8 || !isPod(items[0]) {
		t.Fatal(items)
	}
	if label := containerContextLabel("default"); label != "docker unavailable + k0s v1.31.2 · single node" {
		t.Fatal(label)
	}
}

func TestDockerAloneStillWorksAndUnreachableKubectlBacksOff(t *testing.T) {
	fakeContainerBackends(t, fakeDocker, "#!/bin/sh\necho 'The connection to the server localhost:8080 was refused' >&2\nexit 1\n")
	items, err := listWorkloads(context.Background(), 1, false)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if kubeLabel() != "" {
		t.Fatal("unreachable cluster was reported as a stack")
	}
	if label := containerContextLabel("default"); label != "default · kubernetes unreachable" {
		t.Fatal(label)
	}
	kube.mu.Lock()
	probed, failures := kube.probed, kube.failures
	kube.mu.Unlock()
	if probed.IsZero() || failures != 1 {
		t.Fatal("failed probe was not recorded for back-off")
	}
	if _, err := listWorkloads(context.Background(), 1, false); err != nil {
		t.Fatal(err)
	}
	kube.mu.Lock()
	again := kube.probed
	kube.mu.Unlock()
	if !again.Equal(probed) {
		t.Fatal("kubectl was probed again inside the back-off window")
	}
	fakeContainerBackends(t, "#!/bin/sh\necho 'Cannot connect to the Docker daemon' >&2\nexit 1\n", "#!/bin/sh\nexit 1\n")
	if _, err := listWorkloads(context.Background(), 1, false); err == nil || !strings.Contains(err.Error(), "Docker daemon") {
		t.Fatal("both backends failing must surface the Docker error", err)
	}
}

func TestPodInspectionRoutesEveryTab(t *testing.T) {
	fakeContainerBackends(t, fakeDocker, fakeKubectl)
	pod := workload{ID: podID("shop", "web-6d4cf56db6-x7k2p"), Name: "shop/web-6d4cf56db6-x7k2p", Project: "shop", Owner: "deployment/web"}
	for tab, want := range []string{"── RECENT EVENTS", "[pod/web-6d4cf56db6-x7k2p/nginx]", "kind: Pod", "0.3% (3m)", "web · NodePort"} {
		out, err := inspect(context.Background(), 1, false, pod, tab)
		if err != nil {
			t.Fatal(tab, err)
		}
		if !strings.Contains(out, want) {
			t.Fatalf("tab %d missing %q:\n%s", tab, want, out)
		}
	}
	// A container in the same suite still routes to Docker.
	if _, err := inspect(context.Background(), 1, false, workload{ID: "abc"}, 0); err == nil || !strings.Contains(err.Error(), "docker") {
		t.Fatal("container inspection did not use docker", err)
	}
}

func TestPodActionsAndCommands(t *testing.T) {
	t.Setenv("SYSTEMDOC_KUBECTL", "k0s kubectl")
	resetKubeState()
	t.Cleanup(resetKubeState)
	deployment := workload{ID: podID("shop", "web-abc"), Name: "shop/web-abc", Owner: "deployment/web"}
	name, args, err := actionArgs(1, false, deployment, "restart")
	if err != nil || name != "k0s" || strings.Join(args, " ") != "kubectl rollout restart --namespace shop deployment/web" {
		t.Fatal(name, args, err)
	}
	name, args, err = actionArgs(1, false, deployment, "delete")
	if err != nil || name != "k0s" || strings.Join(args, " ") != "kubectl delete pod --namespace shop -- web-abc" {
		t.Fatal(name, args, err)
	}
	for _, verb := range []string{"start", "stop", "pause", "unpause"} {
		if _, _, err := actionArgs(1, false, deployment, verb); err == nil {
			t.Fatal("docker verb accepted for a pod:", verb)
		}
	}
	bare := workload{ID: podID("default", "solo"), Name: "default/solo"}
	if _, _, err := actionArgs(1, false, bare, "restart"); err == nil {
		t.Fatal("bare pod restart accepted")
	}
	job := workload{ID: podID("shop", "migrate-1"), Name: "shop/migrate-1", Owner: "job/migrate"}
	if _, _, err := actionArgs(1, false, job, "restart"); err == nil || !strings.Contains(err.Error(), "job/migrate") {
		t.Fatal("job-owned pod restart must explain the owner", err)
	}
	if verbs := podVerbs(deployment); strings.Join(verbs, " ") != "restart delete" {
		t.Fatal(verbs)
	}
	if verbs := podVerbs(bare); strings.Join(verbs, " ") != "delete" {
		t.Fatal(verbs)
	}
	if name, args, _ := actionArgs(1, false, workload{ID: "abc"}, "restart"); name != "docker" || strings.Join(args, " ") != "restart abc" {
		t.Fatal("container actions changed", name, args)
	}
	for _, test := range []struct {
		text, target string
		tab          int
		verb         string
	}{
		{"kubectl logs web-abc", podID("default", "web-abc"), 1, ""},
		{"kubectl logs -f web-abc -n shop", podID("shop", "web-abc"), 1, ""},
		{"kubectl logs shop/web-abc", podID("shop", "web-abc"), 1, ""},
		{"kubectl describe pod web-abc --namespace=shop", podID("shop", "web-abc"), 0, ""},
		{"kubectl get pod web-abc -o yaml -n shop", podID("shop", "web-abc"), 2, ""},
		{"kubectl top pod web-abc -n shop", podID("shop", "web-abc"), 3, ""},
		{"kubectl delete pod web-abc -n shop", podID("shop", "web-abc"), -1, "delete"},
	} {
		got, err := parseCommand(test.text, false)
		if err != nil || got.mode != 1 || got.target != test.target || got.tab != test.tab || got.verb != test.verb {
			t.Fatal(test.text, got, err)
		}
	}
	for _, text := range []string{"kubectl delete deployment web", "kubectl get pods -A", "kubectl exec -it web -- sh", "kubectl logs web;id", "kubectl apply -f x.yaml", "kubectl logs -n shop", "kubectl delete pod 'web*' -n shop"} {
		if _, err := parseCommand(text, false); err == nil {
			t.Fatal("accepted unsupported kubectl command", text)
		}
	}
}

func TestPodLogFollowUsesTheDetectedKubectl(t *testing.T) {
	t.Setenv("SYSTEMDOC_KUBECTL", "kubectl --context kind-dev")
	resetKubeState()
	t.Cleanup(resetKubeState)
	pod := workload{ID: podID("shop", "web-abc"), Name: "shop/web-abc"}
	args := kubeLogArgs(pod, true, "150")
	if strings.Join(args, " ") != "logs --follow --namespace shop --all-containers=true --prefix --tail 150 --timestamps -- web-abc" {
		t.Fatal(args)
	}
	if argv := kubeArgv(); strings.Join(argv, " ") != "kubectl --context kind-dev" {
		t.Fatal(argv)
	}
}

func TestKubernetesPodsInTheWorkspace(t *testing.T) {
	w := testWorkspace()
	w.items[1] = []workload{
		{ID: "abc", Name: "dev-control-plane", State: "running", Project: "kind:dev", Description: "kindest/node:v1.31.0"},
		{ID: podID("shop", "web-abc"), Name: "shop/web-abc", State: "running", Detail: "2/2 ready", Project: "shop", Owner: "deployment/web", Description: "nginx:alpine"},
		{ID: podID("shop", "worker-0"), Name: "shop/worker-0", State: "restarting", Detail: "CrashLoopBackOff · 0/1 ready · 7 restarts", Project: "shop", Owner: "statefulset/worker"},
	}
	w.switchMode(1)
	w.search.SetText("project:shop")
	if len(w.visible) != 2 {
		t.Fatal("namespace filter did not select the pods", w.visible)
	}
	w.search.SetText("")
	w.selected[1] = podID("shop", "web-abc")
	w.renderTable()
	w.tab = 2
	if w.inspectorTabName() != "Manifest" {
		t.Fatal(w.inspectorTabName())
	}
	w.selected[1] = "abc"
	w.renderTable()
	if w.inspectorTabName() != "Inspect JSON" {
		t.Fatal(w.inspectorTabName())
	}
	w.selected[1] = podID("shop", "web-abc")
	w.renderTable()
	w.updateSelectionCard()
	if card := w.selectionCard.GetText(true); !strings.Contains(card, "namespace shop · deployment/web") {
		t.Fatal(card)
	}
	text := constellationText(w.current(), 1, "", false)
	if !strings.Contains(text, "namespace  shop") || !strings.Contains(text, "owner      deployment/web") {
		t.Fatal(text)
	}
	report := collectSnapshot(context.Background(), snapshotRequest{Item: w.current(), Mode: 1, Endpoint: "context:default"})
	if !strings.Contains(report, "Backend: Kubernetes") || !strings.Contains(report, "Scope: namespace shop") {
		t.Fatal(report[:200])
	}
}
