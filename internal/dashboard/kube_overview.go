package dashboard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Overview is a readable projection of one pod. Environment values and
// annotation contents stay in the full manifest (c); only counts appear here.
func kubePodOverview(raw, eventsRaw string, eventsErr error) (string, error) {
	pod, err := decodePod(raw)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	section, field := kubeSectionWriter(&out)
	state, detail := podState(pod)
	section("POD")
	field("Name", pod.Metadata.Name)
	field("Namespace", pod.Metadata.Namespace)
	field("State", state+" · "+detail)
	phase := pod.Status.Phase
	if pod.Status.Reason != "" {
		phase += " · " + pod.Status.Reason
	}
	field("Phase", phase)
	if pod.Status.Message != "" {
		field("Message", pod.Status.Message)
	}
	owner := podOwner(pod)
	if owner == "" {
		owner = "none · bare pod"
	}
	field("Owner", owner)
	field("Node", pod.Spec.NodeName)
	field("Started", kubeTimestamp(pod.Status.StartTime))
	field("Created", kubeTimestamp(pod.Metadata.CreationTimestamp))
	if pod.Metadata.DeletionTimestamp != "" {
		field("Deleting", kubeTimestamp(pod.Metadata.DeletionTimestamp))
	}
	field("QoS class", pod.Status.QOSClass)
	field("Restart", pod.Spec.RestartPolicy)
	field("Service acct", pod.Spec.ServiceAccountName)
	if pod.Spec.PriorityClassName != "" {
		field("Priority", pod.Spec.PriorityClassName)
	}

	writePodContainers(section, field, pod, false)
	writePodVolumes(section, field, pod)

	section("CONDITIONS")
	if len(pod.Status.Conditions) == 0 {
		field("Conditions", "none reported")
	}
	for _, condition := range pod.Status.Conditions {
		value := condition.Status
		if condition.Reason != "" {
			value += " · " + condition.Reason
		}
		if condition.Message != "" {
			value += " · " + condition.Message
		}
		field(condition.Type, value)
	}

	section("RECENT EVENTS · newest first")
	writePodEvents(field, eventsRaw, eventsErr)

	writePodLabels(section, field, pod)
	out.WriteString("\n  z expand · arrows scroll · c full manifest · l logs · r Metrics API · d connections\n")
	return out.String(), nil
}

// Connections show how traffic reaches the pod: container ports, Services
// whose selector matches its labels, addresses and volumes.
func kubePodConnections(raw, servicesRaw string, servicesErr error) (string, error) {
	pod, err := decodePod(raw)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	section, field := kubeSectionWriter(&out)
	section("POD")
	field("Name", pod.Metadata.Name)
	field("Namespace", pod.Metadata.Namespace)
	field("Owner", available(podOwner(pod)))

	section("ADDRESSES")
	if len(pod.Status.PodIPs) == 0 {
		field("Pod IP", pod.Status.PodIP)
	}
	for _, ip := range pod.Status.PodIPs {
		field("Pod IP", ip.IP)
	}
	field("Host IP", pod.Status.HostIP)
	network := "pod network"
	if pod.Spec.HostNetwork {
		network = "host network · container ports bind on the node"
	}
	field("Network", network)

	writePodContainers(section, field, pod, true)

	section("SERVICES · selector matches pod labels")
	writePodServices(field, pod, servicesRaw, servicesErr)

	writePodVolumes(section, field, pod)
	writePodLabels(section, field, pod)
	out.WriteString("\n  A matching Service is a routing rule, not proof of live traffic. z expand · o overview · l logs\n")
	return out.String(), nil
}

func decodePod(raw string) (kubePod, error) {
	var pod kubePod
	if err := json.Unmarshal([]byte(raw), &pod); err != nil {
		return pod, fmt.Errorf("decode Kubernetes pod: %w", err)
	}
	if pod.Metadata.Name == "" {
		return pod, fmt.Errorf("the Kubernetes pod inspection did not return a pod")
	}
	return pod, nil
}

func kubeSectionWriter(out *strings.Builder) (func(string), func(string, string)) {
	section := func(title string) {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		fmt.Fprintf(out, "── %s\n", title)
	}
	field := func(label, value string) {
		value = strings.Join(strings.Fields(clean(value)), " ")
		fmt.Fprintf(out, "  %-12s %s\n", label, available(value))
	}
	return section, field
}

func kubeTimestamp(value string) string {
	if value == "" || strings.HasPrefix(value, "0001-01-01") {
		return "—"
	}
	return value
}

func containerStateText(status *kubeContainerStatus) string {
	if status == nil {
		return "no status reported"
	}
	state := status.State
	switch {
	case state.Running != nil:
		text := "running since " + kubeTimestamp(state.Running.StartedAt)
		if !status.Ready {
			text += " · not ready"
		}
		return text
	case state.Waiting != nil:
		text := "waiting · " + available(state.Waiting.Reason)
		if state.Waiting.Message != "" {
			text += " · " + state.Waiting.Message
		}
		return text
	case state.Terminated != nil:
		return fmt.Sprintf("terminated · %s · exit %d · %s", available(state.Terminated.Reason), state.Terminated.ExitCode, kubeTimestamp(state.Terminated.FinishedAt))
	}
	return "no state reported"
}

func writePodContainers(section func(string), field func(string, string), pod kubePod, connectionsOnly bool) {
	statuses := map[string]*kubeContainerStatus{}
	for i := range pod.Status.ContainerStatuses {
		statuses[pod.Status.ContainerStatuses[i].Name] = &pod.Status.ContainerStatuses[i]
	}
	for i := range pod.Status.InitContainerStatuses {
		statuses[pod.Status.InitContainerStatuses[i].Name] = &pod.Status.InitContainerStatuses[i]
	}
	groups := []struct {
		title      string
		containers []kubeContainerSpec
	}{{"CONTAINERS", pod.Spec.Containers}, {"INIT CONTAINERS", pod.Spec.InitContainers}}
	for _, group := range groups {
		if len(group.containers) == 0 {
			if group.title == "CONTAINERS" {
				section(group.title)
				field("Containers", "none reported")
			}
			continue
		}
		if connectionsOnly {
			group.title += " · ports"
		}
		section(group.title)
		for _, container := range group.containers {
			status := statuses[container.Name]
			field("Container", container.Name)
			if !connectionsOnly {
				field("Image", container.Image)
				field("State", containerStateText(status))
				if status != nil {
					field("Restarts", fmt.Sprint(status.RestartCount))
					if last := status.LastState.Terminated; last != nil {
						field("Last exit", fmt.Sprintf("%s · exit %d · %s", available(last.Reason), last.ExitCode, kubeTimestamp(last.FinishedAt)))
					}
				}
				if len(container.Command) > 0 {
					field("Command", dockerArgv(container.Command))
				}
				if len(container.Args) > 0 {
					field("Args", dockerArgv(container.Args))
				}
				field("Requests", kubeResources(container.Resources.Requests))
				field("Limits", kubeResources(container.Resources.Limits))
				probes := []string{}
				for name, probe := range map[string]json.RawMessage{"liveness": container.LivenessProbe, "readiness": container.ReadinessProbe, "startup": container.StartupProbe} {
					if len(probe) > 0 && string(probe) != "null" {
						probes = append(probes, name)
					}
				}
				sort.Strings(probes)
				if len(probes) == 0 {
					probes = []string{"none configured"}
				}
				field("Probes", strings.Join(probes, ", "))
				field("Environment", fmt.Sprintf("%d entries · c opens full manifest", len(container.Env)))
			}
			if len(container.Ports) == 0 {
				field("Ports", "none declared")
			}
			for _, port := range container.Ports {
				protocol := port.Protocol
				if protocol == "" {
					protocol = "TCP"
				}
				value := fmt.Sprintf("%d/%s", port.ContainerPort, protocol)
				if port.Name != "" {
					value += " · " + port.Name
				}
				if port.HostPort > 0 {
					value = fmt.Sprintf("host %d → %s", port.HostPort, value)
				}
				field("Port", value)
			}
		}
	}
}

func kubeResources(values map[string]string) string {
	if len(values) == 0 {
		return "none set"
	}
	parts := make([]string, 0, len(values))
	for _, key := range sortedKeys(values) {
		parts = append(parts, key+" "+values[key])
	}
	return strings.Join(parts, " · ")
}

func writePodVolumes(section func(string), field func(string, string), pod kubePod) {
	section("VOLUMES · source → mount")
	if len(pod.Spec.Volumes) == 0 {
		field("Volumes", "none declared")
		return
	}
	mounts := map[string][]string{}
	for _, container := range append(append([]kubeContainerSpec(nil), pod.Spec.Containers...), pod.Spec.InitContainers...) {
		for _, mount := range container.VolumeMounts {
			access := "read-write"
			if mount.ReadOnly {
				access = "read-only"
			}
			path := mount.MountPath
			if mount.SubPath != "" {
				path += " (subPath " + mount.SubPath + ")"
			}
			mounts[mount.Name] = append(mounts[mount.Name], fmt.Sprintf("%s → %s · %s", container.Name, path, access))
		}
	}
	for _, volume := range pod.Spec.Volumes {
		var name string
		json.Unmarshal(volume["name"], &name)
		kind, source := "unknown", ""
		for key, value := range volume {
			if key == "name" {
				continue
			}
			kind = key
			var detail map[string]json.RawMessage
			if json.Unmarshal(value, &detail) == nil {
				for _, candidate := range []string{"claimName", "secretName", "name", "path", "medium"} {
					var text string
					if json.Unmarshal(detail[candidate], &text) == nil && text != "" {
						source = text
						break
					}
				}
			}
		}
		value := kind
		if source != "" {
			value += " " + source
		}
		field(name, value)
		if len(mounts[name]) == 0 {
			field("Mount", "not mounted by any container")
		}
		for _, mount := range mounts[name] {
			field("Mount", mount)
		}
	}
}

func writePodLabels(section func(string), field func(string, string), pod kubePod) {
	section("LABELS")
	if len(pod.Metadata.Labels) == 0 {
		field("Labels", "none")
	}
	for _, key := range sortedKeys(pod.Metadata.Labels) {
		field(key, pod.Metadata.Labels[key])
	}
	field("Annotations", fmt.Sprintf("%d · c opens full manifest", len(pod.Metadata.Annotations)))
}

type kubeEvent struct {
	Type, Reason, Message, FirstTimestamp, LastTimestamp, EventTime string
	Count                                                           int
	Series                                                          *struct {
		Count            int
		LastObservedTime string
	}
	Source             struct{ Component string }
	ReportingComponent string
}

func (e kubeEvent) when() string {
	switch {
	case e.Series != nil && e.Series.LastObservedTime != "":
		return e.Series.LastObservedTime
	case e.LastTimestamp != "":
		return e.LastTimestamp
	case e.EventTime != "":
		return e.EventTime
	}
	return e.FirstTimestamp
}

func writePodEvents(field func(string, string), raw string, err error) {
	if err != nil {
		field("Events", "unavailable · "+firstLine(err.Error()))
		return
	}
	var list struct{ Items []kubeEvent }
	if json.Unmarshal([]byte(raw), &list) != nil {
		field("Events", "unavailable · event list could not be decoded")
		return
	}
	if len(list.Items) == 0 {
		field("Events", "none retained · the API server keeps events for about an hour")
		return
	}
	sort.SliceStable(list.Items, func(i, j int) bool { return list.Items[i].when() > list.Items[j].when() })
	shown := 0
	for _, event := range list.Items {
		if shown == 12 {
			field("More", fmt.Sprintf("%d older events in kubectl get events", len(list.Items)-shown))
			break
		}
		count := event.Count
		if event.Series != nil && event.Series.Count > count {
			count = event.Series.Count
		}
		label := event.Type
		if label == "" {
			label = "Event"
		}
		value := fmt.Sprintf("%s · %s · %s", kubeTimestamp(event.when()), available(event.Reason), event.Message)
		if count > 1 {
			value += fmt.Sprintf(" (×%d)", count)
		}
		component := event.Source.Component
		if component == "" {
			component = event.ReportingComponent
		}
		if component != "" {
			value += " · " + component
		}
		field(label, value)
		shown++
	}
}

type kubeService struct {
	Metadata struct{ Name, Namespace string }
	Spec     struct {
		Type, ClusterIP string
		ExternalIPs     []string
		Selector        map[string]string
		Ports           []struct {
			Name, Protocol string
			Port, NodePort int
			TargetPort     json.RawMessage
		}
	}
	Status struct {
		LoadBalancer struct {
			Ingress []struct{ IP, Hostname string }
		}
	}
}

func serviceSelectsPod(service kubeService, pod kubePod) bool {
	if len(service.Spec.Selector) == 0 {
		return false
	}
	for key, value := range service.Spec.Selector {
		if pod.Metadata.Labels[key] != value {
			return false
		}
	}
	return true
}

func writePodServices(field func(string, string), pod kubePod, raw string, err error) {
	if err != nil {
		field("Services", "unavailable · "+firstLine(err.Error()))
		return
	}
	var list struct{ Items []kubeService }
	if json.Unmarshal([]byte(raw), &list) != nil {
		field("Services", "unavailable · service list could not be decoded")
		return
	}
	matched := 0
	for _, service := range list.Items {
		if !serviceSelectsPod(service, pod) {
			continue
		}
		matched++
		kind := service.Spec.Type
		if kind == "" {
			kind = "ClusterIP"
		}
		field("Service", service.Metadata.Name+" · "+kind)
		field("Cluster IP", service.Spec.ClusterIP)
		for _, ip := range service.Spec.ExternalIPs {
			field("External IP", ip)
		}
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			address := ingress.IP
			if address == "" {
				address = ingress.Hostname
			}
			field("Load balancer", address)
		}
		for _, port := range service.Spec.Ports {
			protocol := port.Protocol
			if protocol == "" {
				protocol = "TCP"
			}
			target := strings.Trim(string(port.TargetPort), "\"")
			if target == "" || target == "null" {
				target = fmt.Sprint(port.Port)
			}
			value := fmt.Sprintf("%d/%s → %s", port.Port, protocol, target)
			if port.Name != "" {
				value += " · " + port.Name
			}
			if port.NodePort > 0 {
				value += fmt.Sprintf(" · node port %d", port.NodePort)
			}
			field("Port", value)
		}
	}
	if matched == 0 {
		field("Services", "none select this pod's labels")
	}
}
