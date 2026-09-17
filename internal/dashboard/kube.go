package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A single-node Kubernetes stack (k0s, k3s, kind, minikube, microk8s or any
// reachable kubectl context) joins the Containers suite beside Docker. Pods
// are workloads whose ID carries the pod: prefix so every existing route can
// tell them apart from container IDs without a second inventory mode.
const podPrefix = "pod:"

type kubeStack struct {
	argv              []string
	distro, version   string
	nodes, readyNodes int
	nodeName          string
}

func (s kubeStack) label() string {
	if s.distro == "" {
		return ""
	}
	label := s.distro
	if s.version != "" {
		label += " " + s.version
	}
	switch {
	case s.nodes == 1 && s.readyNodes == 1:
		label += " · single node"
	case s.nodes == 1:
		label += " · single node · not ready"
	case s.nodes > 1 && s.readyNodes == s.nodes:
		label += fmt.Sprintf(" · %d nodes", s.nodes)
	case s.nodes > 1:
		label += fmt.Sprintf(" · %d of %d nodes ready", s.readyNodes, s.nodes)
	}
	return label
}

// kubectl is bounded twice: the request timeout stops a dead API server from
// holding the inventory, and the shared command timeout still applies.
func (s kubeStack) run(ctx context.Context, limit int, args ...string) (string, error) {
	if len(s.argv) == 0 {
		return "", fmt.Errorf("no Kubernetes stack detected")
	}
	full := append(append([]string{}, s.argv[1:]...), "--request-timeout=6s")
	full = append(full, args...)
	output, err := commandLimit(ctx, limit, s.argv[0], full...)
	if err == nil && jsonRequest(args) && len(output) >= outputLimit(limit) {
		return output, fmt.Errorf("kubectl %s returned more than %d MiB; the output was cut and cannot be decoded", strings.Join(args, " "), outputLimit(limit)>>20)
	}
	return output, err
}

// jsonRequest reports whether a kubectl invocation asked for JSON, which is
// useless when the tail buffer has dropped its beginning.
func jsonRequest(args []string) bool {
	for i, arg := range args {
		if (arg == "-o" || arg == "--output") && i+1 < len(args) && args[i+1] == "json" || arg == "-o=json" || arg == "--output=json" {
			return true
		}
	}
	return false
}

// Detection state is shared by the inventory worker and the UI thread.
var kube struct {
	mu        sync.Mutex
	stack     *kubeStack
	err       string
	dockerErr string
	probed    time.Time
	failures  int
}

func resetKubeState() {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	kube.stack, kube.err, kube.dockerErr, kube.probed, kube.failures = nil, "", "", time.Time{}, 0
}

// kubeLabel is the short stack description shown in the masthead and table.
func kubeLabel() string {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	if kube.stack == nil {
		return ""
	}
	return kube.stack.label()
}

func kubeStatus() (label, kubeErr, dockerErr string) {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	if kube.stack != nil {
		label = kube.stack.label()
	}
	return label, kube.err, kube.dockerErr
}

func setDockerError(err error) {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	kube.dockerErr = ""
	if err != nil {
		kube.dockerErr = firstLine(err.Error())
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

// kubeArgv is the detected kubectl invocation, or plain kubectl when nothing
// has been detected yet, so reviewed commands always name a real executable.
func kubeArgv() []string {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	if kube.stack != nil {
		return append([]string(nil), kube.stack.argv...)
	}
	if value := os.Getenv("SYSTEMDOC_KUBECTL"); strings.TrimSpace(value) != "" {
		return strings.Fields(value)
	}
	return []string{"kubectl"}
}

func kubectlCandidates() [][]string {
	if value := os.Getenv("SYSTEMDOC_KUBECTL"); strings.TrimSpace(value) != "" {
		return [][]string{strings.Fields(value)}
	}
	var candidates [][]string
	if _, err := exec.LookPath("kubectl"); err == nil {
		candidates = append(candidates, []string{"kubectl"})
	}
	for _, embedded := range [][]string{{"k0s", "kubectl"}, {"k3s", "kubectl"}, {"microk8s", "kubectl"}} {
		if _, err := exec.LookPath(embedded[0]); err == nil {
			candidates = append(candidates, embedded)
		}
	}
	return candidates
}

// currentKubeStack returns the detected stack, probing when nothing is cached.
// Failed probes back off so a host with kubectl but no cluster is not asked
// on every refresh; a stack that stops answering is dropped and re-probed.
func currentKubeStack(ctx context.Context) *kubeStack {
	kube.mu.Lock()
	if kube.stack != nil {
		stack := *kube.stack
		kube.mu.Unlock()
		return &stack
	}
	wait := min(time.Duration(30<<min(kube.failures, 5))*time.Second, 10*time.Minute)
	if !kube.probed.IsZero() && time.Since(kube.probed) < wait {
		kube.mu.Unlock()
		return nil
	}
	if len(kubectlCandidates()) == 0 {
		// Nothing to probe; a kubectl installed later is noticed at once.
		kube.err, kube.probed, kube.failures = "", time.Time{}, 0
		kube.mu.Unlock()
		return nil
	}
	kube.mu.Unlock()
	stack, err := probeKubeStack(ctx)
	if ctx.Err() != nil {
		return nil
	}
	kube.mu.Lock()
	defer kube.mu.Unlock()
	kube.probed = time.Now()
	if stack == nil {
		kube.failures++
		kube.err = ""
		if err != nil {
			kube.err = firstLine(err.Error())
		}
		return nil
	}
	kube.failures = 0
	kube.err = ""
	kube.stack = stack
	copied := *stack
	return &copied
}

// retryKubeProbe lets an explicit refresh probe again immediately instead of
// waiting out the back-off window.
func retryKubeProbe() {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	if kube.stack == nil {
		kube.probed = time.Time{}
	}
}

func dropKubeStack(err error) {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	kube.stack = nil
	kube.probed = time.Now()
	kube.err = firstLine(err.Error())
}

func probeKubeStack(ctx context.Context) (*kubeStack, error) {
	candidates := kubectlCandidates()
	if len(candidates) == 0 {
		return nil, nil
	}
	var firstErr error
	try := func(argv []string) *kubeStack {
		stack := kubeStack{argv: argv}
		output, err := stack.run(ctx, 0, "get", "nodes", "-o", "json")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if err := describeKubeNodes(output, &stack); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		return &stack
	}
	for _, argv := range candidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if stack := try(argv); stack != nil {
			return stack, nil
		}
		// kind writes a kubeconfig context per cluster; when the current context
		// is not the cluster, ask kind which clusters it manages.
		if len(argv) == 1 && argv[0] == "kubectl" {
			if _, err := exec.LookPath("kind"); err == nil {
				clusters, err := command(ctx, "kind", "get", "clusters")
				if err == nil {
					for _, name := range strings.Fields(clusters) {
						if stack := try([]string{"kubectl", "--context", "kind-" + name}); stack != nil {
							return stack, nil
						}
					}
				}
			}
		}
	}
	return nil, firstErr
}

type kubeNode struct {
	Metadata struct {
		Name   string
		Labels map[string]string
	}
	Spec   struct{ ProviderID string }
	Status struct {
		NodeInfo   struct{ KubeletVersion, ContainerRuntimeVersion, OSImage string }
		Conditions []struct{ Type, Status string }
	}
}

// describeKubeNodes recognises the distribution from what the node reports
// about itself rather than from which binary answered, so a kubeconfig that
// points at k3s through plain kubectl is still labelled k3s.
func describeKubeNodes(raw string, stack *kubeStack) error {
	var list struct{ Items []kubeNode }
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return fmt.Errorf("decode Kubernetes node list: %w", err)
	}
	if len(list.Items) == 0 {
		return fmt.Errorf("the Kubernetes API answered but reported no nodes")
	}
	stack.nodes = len(list.Items)
	stack.distro = "kubernetes"
	for i, node := range list.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				stack.readyNodes++
			}
		}
		if i > 0 {
			continue
		}
		stack.nodeName = node.Metadata.Name
		version := node.Status.NodeInfo.KubeletVersion
		if cut := strings.IndexAny(version, "+-"); cut > 0 {
			version = version[:cut]
		}
		stack.version = version
		labels := node.Metadata.Labels
		kubelet := node.Status.NodeInfo.KubeletVersion
		switch {
		case labels["minikube.k8s.io/name"] != "":
			stack.distro = "minikube"
		case labels["microk8s.io/cluster"] != "":
			stack.distro = "microk8s"
		case strings.HasPrefix(node.Spec.ProviderID, "kind://"):
			stack.distro = "kind"
		case strings.HasPrefix(node.Metadata.Name, "k3d-"):
			stack.distro = "k3d"
		case strings.Contains(kubelet, "+k3s") || labels["node.kubernetes.io/instance-type"] == "k3s":
			stack.distro = "k3s"
		case strings.Contains(kubelet, "+k0s"):
			stack.distro = "k0s"
		case node.Metadata.Name == "docker-desktop":
			stack.distro = "docker-desktop"
		}
	}
	return nil
}

func isPod(item workload) bool { return strings.HasPrefix(item.ID, podPrefix) }

func podRef(item workload) (namespace, name string) {
	namespace, name, _ = strings.Cut(strings.TrimPrefix(item.ID, podPrefix), "/")
	return namespace, name
}

func podID(namespace, name string) string { return podPrefix + namespace + "/" + name }

type kubeContainerSpec struct {
	Name, Image   string
	Command, Args []string
	Ports         []struct {
		Name, Protocol          string
		ContainerPort, HostPort int
	}
	Resources    struct{ Requests, Limits map[string]string }
	VolumeMounts []struct {
		Name, MountPath, SubPath string
		ReadOnly                 bool
	}
	Env                                         []struct{ Name string }
	LivenessProbe, ReadinessProbe, StartupProbe json.RawMessage
}

type kubeContainerState struct {
	Waiting    *struct{ Reason, Message string }
	Running    *struct{ StartedAt string }
	Terminated *struct {
		Reason, Message, StartedAt, FinishedAt string
		ExitCode, Signal                       int
	}
}

type kubeContainerStatus struct {
	Name, Image, ImageID, ContainerID string
	Ready, Started                    bool
	RestartCount                      int
	State, LastState                  kubeContainerState
}

type kubePod struct {
	Metadata struct {
		Name, Namespace, CreationTimestamp, DeletionTimestamp string
		Labels, Annotations                                   map[string]string
		OwnerReferences                                       []struct {
			Kind, Name string
			Controller bool
		}
	}
	Spec struct {
		NodeName, ServiceAccountName, RestartPolicy, PriorityClassName string
		HostNetwork                                                    bool
		Containers, InitContainers                                     []kubeContainerSpec
		Volumes                                                        []map[string]json.RawMessage
	}
	Status struct {
		Phase, Reason, Message, PodIP, HostIP, StartTime, QOSClass string
		PodIPs                                                     []struct{ IP string }
		Conditions                                                 []struct{ Type, Status, Reason, Message, LastTransitionTime string }
		ContainerStatuses, InitContainerStatuses                   []kubeContainerStatus
	}
}

// podOwner names the controller that owns a pod. Deployments own pods through
// a ReplicaSet whose name carries the pod-template hash, so the Deployment is
// recovered by removing that suffix; anything else is reported as found.
func podOwner(pod kubePod) string {
	for _, owner := range pod.Metadata.OwnerReferences {
		if !owner.Controller {
			continue
		}
		kind := strings.ToLower(owner.Kind)
		if kind == "replicaset" {
			if hash := pod.Metadata.Labels["pod-template-hash"]; hash != "" && strings.HasSuffix(owner.Name, "-"+hash) {
				return "deployment/" + strings.TrimSuffix(owner.Name, "-"+hash)
			}
		}
		return kind + "/" + owner.Name
	}
	return ""
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// podState reduces phase, container states and readiness to the dashboard's
// state vocabulary. "running" is only reported when the pod is running; a
// crash loop is "restarting" and image or configuration errors are "failed",
// so the same attention rules apply to containers and pods.
func podState(pod kubePod) (state, detail string) {
	ready, total, restarts := 0, len(pod.Spec.Containers), 0
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	for _, status := range pod.Status.InitContainerStatuses {
		restarts += status.RestartCount
	}
	readiness := fmt.Sprintf("%d/%d ready", ready, total)
	suffix := ""
	if restarts > 0 {
		suffix = " · " + plural(restarts, "restart")
	}
	if pod.Metadata.DeletionTimestamp != "" {
		return "terminating", "Terminating · " + readiness + suffix
	}
	switch pod.Status.Phase {
	case "Succeeded":
		reason := pod.Status.Reason
		if reason == "" {
			reason = "Completed"
		}
		return "succeeded", reason + suffix
	case "Failed":
		reason := pod.Status.Reason
		if reason == "" {
			reason = "Failed"
		}
		return "failed", reason + " · " + readiness + suffix
	case "Unknown":
		return "unknown", "node not reporting · " + readiness + suffix
	}
	statuses := append(append([]kubeContainerStatus(nil), pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, status := range statuses {
		if waiting := status.State.Waiting; waiting != nil {
			switch waiting.Reason {
			case "CrashLoopBackOff":
				return "restarting", waiting.Reason + " · " + readiness + suffix
			case "ImagePullBackOff", "ErrImagePull", "ErrImageNeverPull", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError", "RunContainerError", "ConfigError":
				return "failed", waiting.Reason + " · " + readiness + suffix
			}
		}
		if terminated := status.State.Terminated; terminated != nil && terminated.ExitCode != 0 && pod.Status.Phase == "Running" {
			reason := terminated.Reason
			if reason == "" {
				reason = "Error"
			}
			return "restarting", fmt.Sprintf("%s exit %d · %s%s", reason, terminated.ExitCode, readiness, suffix)
		}
	}
	if pod.Status.Phase == "Pending" {
		reason := "Pending"
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "PodScheduled" && condition.Status == "False" && condition.Reason != "" {
				reason = condition.Reason
			}
		}
		for _, status := range statuses {
			if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
				reason = status.State.Waiting.Reason
			}
		}
		return "pending", reason + " · " + readiness
	}
	if pod.Status.Phase == "Running" && ready < total {
		return "running", "not ready · " + readiness + suffix
	}
	if pod.Status.Phase == "" {
		return "unknown", "no status reported"
	}
	return strings.ToLower(pod.Status.Phase), readiness + suffix
}

func podImages(pod kubePod) string {
	images := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		images = append(images, container.Image)
	}
	if len(images) > 2 {
		return strings.Join(images[:2], ", ") + fmt.Sprintf(" +%d more", len(images)-2)
	}
	return strings.Join(images, ", ")
}

func podWorkload(pod kubePod) workload {
	state, detail := podState(pod)
	return workload{
		ID:          podID(pod.Metadata.Namespace, pod.Metadata.Name),
		Name:        pod.Metadata.Namespace + "/" + pod.Metadata.Name,
		State:       state,
		Detail:      detail,
		Project:     pod.Metadata.Namespace,
		Description: podImages(pod),
		Owner:       podOwner(pod),
	}
}

func parsePodList(raw string) ([]workload, error) {
	var list struct{ Items []kubePod }
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("decode Kubernetes pod list: %w", err)
	}
	result := make([]workload, 0, len(list.Items))
	for _, pod := range list.Items {
		result = append(result, podWorkload(pod))
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// listPods returns every pod in the detected stack. A stack that stops
// answering is dropped so the next inventory probes again.
func listPods(ctx context.Context) ([]workload, *kubeStack, error) {
	stack := currentKubeStack(ctx)
	if stack == nil {
		return nil, nil, nil
	}
	output, err := stack.run(ctx, 8*1024*1024, "get", "pods", "--all-namespaces", "-o", "json")
	if err != nil {
		if ctx.Err() == nil {
			dropKubeStack(err)
		}
		return nil, stack, err
	}
	items, err := parsePodList(output)
	if err != nil {
		return nil, stack, err
	}
	return items, stack, nil
}

// kubeCPUPercent converts a Kubernetes CPU quantity to a share of one
// logical CPU, the unit the rest of the dashboard already uses.
func kubeCPUPercent(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	scale := 100.0
	switch {
	case strings.HasSuffix(value, "m"):
		value, scale = strings.TrimSuffix(value, "m"), 0.1
	case strings.HasSuffix(value, "u"):
		value, scale = strings.TrimSuffix(value, "u"), 0.0001
	case strings.HasSuffix(value, "n"):
		value, scale = strings.TrimSuffix(value, "n"), 0.0000001
	}
	n, err := strconv.ParseFloat(value, 64)
	return n * scale, err == nil && n >= 0
}

// kubeMemoryMiB converts a Kubernetes memory quantity to MiB.
func kubeMemoryMiB(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		scale  float64
	}{
		{"Ei", 1 << 40}, {"Pi", 1 << 30}, {"Ti", 1 << 20}, {"Gi", 1024}, {"Mi", 1}, {"Ki", 1.0 / 1024},
		{"E", 1e18 / (1024 * 1024)}, {"P", 1e15 / (1024 * 1024)}, {"T", 1e12 / (1024 * 1024)}, {"G", 1e9 / (1024 * 1024)}, {"M", 1e6 / (1024 * 1024)}, {"k", 1e3 / (1024 * 1024)},
	}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			return n * unit.scale, err == nil && n >= 0
		}
	}
	n, err := strconv.ParseFloat(value, 64)
	return n / (1024 * 1024), err == nil && n >= 0
}

// enrichKubeResources fills pod CPU and memory from the Metrics API. Metrics
// are optional: without metrics-server the figures stay unknown.
func enrichKubeResources(ctx context.Context, stack *kubeStack, items []workload) string {
	indexes := map[string]int{}
	for i, item := range items {
		if isPod(item) && isActive(item) {
			indexes[item.ID] = i
		}
	}
	if len(indexes) == 0 || stack == nil {
		return ""
	}
	output, err := stack.run(ctx, 0, "top", "pods", "--all-namespaces", "--no-headers")
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		return "kubectl top: " + firstLine(err.Error())
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		i, ok := indexes[podID(fields[0], fields[1])]
		if !ok {
			continue
		}
		if cpu, ok := kubeCPUPercent(fields[2]); ok {
			items[i].CPU = fmt.Sprintf("%.1f%%", cpu)
		}
		if memory, ok := kubeMemoryMiB(fields[3]); ok {
			items[i].Memory = fmt.Sprintf("%.1f MiB", memory)
		}
	}
	return ""
}

// kubeResourceOutput formats one pod's kubectl top reading, per container.
func kubeResourceOutput(item workload, raw string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s · live pod resources from the Metrics API\n\n", item.Name)
	total := 0.0
	totalMemory := 0.0
	rows := 0
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		cpu, cpuOK := kubeCPUPercent(fields[2])
		memory, memoryOK := kubeMemoryMiB(fields[3])
		cpuText, memoryText := fields[2], fields[3]
		if cpuOK {
			total += cpu
			cpuText = fmt.Sprintf("%.1f%% (%s)", cpu, fields[2])
		}
		if memoryOK {
			totalMemory += memory
			memoryText = fmt.Sprintf("%.1f MiB", memory)
		}
		fmt.Fprintf(&out, "Container %-24s CPU %-18s Memory %s\n", fields[1], cpuText, memoryText)
		rows++
	}
	if rows == 0 {
		return item.Name + " · no metrics reported\n\n" + strings.TrimSpace(raw) + "\n\nkubectl top needs metrics-server (or the distribution's metrics add-on) and a pod that has run long enough to be sampled."
	}
	fmt.Fprintf(&out, "\nPod total    CPU %.1f%% of one logical CPU · Memory %.1f MiB\n\nCPU is the Metrics API's recent average, not an instantaneous reading. Working-set memory follows kubelet accounting.", total, totalMemory)
	return out.String()
}

func kubeLogArgs(item workload, follow bool, tail string) []string {
	namespace, name := podRef(item)
	args := []string{"logs", "--namespace", namespace, "--all-containers=true", "--prefix", "--tail", tail, "--timestamps", "--", name}
	if follow {
		args = append([]string{"logs", "--follow"}, args[1:]...)
	}
	return args
}

// inspectPod serves the inspector tabs for a pod: overview with events, logs,
// the full manifest, Metrics API readings and connections with Services.
func inspectPod(ctx context.Context, item workload, tab int) (string, error) {
	stack := currentKubeStack(ctx)
	if stack == nil {
		return "", fmt.Errorf("the Kubernetes stack that reported %s is no longer reachable", item.Name)
	}
	namespace, name := podRef(item)
	switch tab {
	case 1:
		return stack.run(ctx, 0, kubeLogArgs(item, false, "150")...)
	case 2:
		return stack.run(ctx, 0, "get", "pod", "--namespace", namespace, "-o", "yaml", "--", name)
	case 3:
		output, err := stack.run(ctx, 0, "top", "pod", "--namespace", namespace, "--containers", "--no-headers", "--", name)
		if err != nil {
			return "", err
		}
		return kubeResourceOutput(item, output), nil
	}
	pod, err := stack.run(ctx, 0, "get", "pod", "--namespace", namespace, "-o", "json", "--", name)
	if err != nil {
		return pod, err
	}
	if tab == 4 {
		services, err := stack.run(ctx, 0, "get", "services", "--namespace", namespace, "-o", "json")
		if err != nil {
			services = ""
		}
		return kubePodConnections(pod, services, err)
	}
	events, err := stack.run(ctx, 0, "get", "events", "--namespace", namespace, "--field-selector", "involvedObject.kind=Pod,involvedObject.name="+name, "-o", "json")
	if err != nil {
		events = ""
	}
	return kubePodOverview(pod, events, err)
}

// podActionArgs maps reviewed lifecycle verbs to kubectl. Restart is a
// controller rollout because a bare pod cannot be restarted in place; delete
// removes the pod and lets its controller, if any, replace it.
func podActionArgs(item workload, verb string) (string, []string, error) {
	namespace, name := podRef(item)
	if namespace == "" || name == "" {
		return "", nil, fmt.Errorf("select a valid pod")
	}
	argv := kubeArgv()
	rest := argv[1:]
	switch verb {
	case "restart":
		kind, _, _ := strings.Cut(item.Owner, "/")
		switch kind {
		case "deployment", "statefulset", "daemonset":
			return argv[0], append(append([]string{}, rest...), "rollout", "restart", "--namespace", namespace, item.Owner), nil
		case "":
			return "", nil, fmt.Errorf("%s has no controller; delete recreates nothing, so restart is unavailable", item.Name)
		}
		return "", nil, fmt.Errorf("%s is owned by %s, which has no rollout restart; delete the pod to let the controller replace it", item.Name, item.Owner)
	case "delete", "rm":
		return argv[0], append(append([]string{}, rest...), "delete", "pod", "--namespace", namespace, "--", name), nil
	}
	return "", nil, fmt.Errorf("%s is a Kubernetes pod; use restart (controller rollout) or delete", item.Name)
}

func podVerbs(item workload) []string {
	kind, _, _ := strings.Cut(item.Owner, "/")
	switch kind {
	case "deployment", "statefulset", "daemonset":
		return []string{"restart", "delete"}
	}
	return []string{"delete"}
}
