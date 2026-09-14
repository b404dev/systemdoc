package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type workload struct {
	PID                                           int
	ID, Name, State, Detail, Project, Description string
	Enablement                                    string
	LoadState, Aliases                            string
	Owner                                         string
	UnitFileOnly                                  bool
	CPU, Memory                                   string
	CPUCounter                                    uint64
	HasCPU                                        bool
	SampleAt                                      time.Time
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	return commandLimit(ctx, 0, name, args...)
}

// commandLimit runs a bounded command; limit 0 keeps the default 1 MiB tail.
func commandLimit(ctx context.Context, limit int, name string, args ...string) (string, error) {
	return runBounded(ctx, 8*time.Second, limit, name, args...)
}

// runBounded is the one place subprocess output and duration are capped.
func runBounded(ctx context.Context, timeout time.Duration, limit int, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := tailBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	err := cmd.Run()
	output := buffer.String()
	if err != nil {
		return output, fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(output))
	}
	return output, nil
}

func inventory(ctx context.Context, mode int, user bool) ([]workload, error) {
	items, err := listWorkloads(ctx, mode, user)
	if err != nil {
		return nil, err
	}
	return enrichInventory(ctx, mode, user, items), nil
}

// List identities and states without waiting for resource sampling.
func listWorkloads(ctx context.Context, mode int, user bool) ([]workload, error) {
	if mode == 1 {
		return listContainers(ctx)
	}
	if usesLaunchd() {
		return listLaunchServices(ctx, user)
	}
	args := []string{"list-units", "--all", "--type=service", "--output=json", "--no-pager"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	output, err := command(ctx, "systemctl", args...)
	if err != nil {
		return nil, err
	}
	var rows []struct{ Unit, Load, Active, Sub, Description string }
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		return nil, err
	}
	result := make([]workload, 0, len(rows))
	for _, row := range rows {
		result = append(result, workload{ID: row.Unit, Name: row.Unit, State: row.Active, Detail: row.Sub, Description: row.Description, LoadState: row.Load})
	}
	return result, nil
}

// listContainers merges Docker containers with the pods of a detected
// Kubernetes stack. Either backend alone is enough for the suite to work; the
// error is only returned when both are unavailable.
func listContainers(ctx context.Context) ([]workload, error) {
	type podResult struct {
		items []workload
		stack *kubeStack
		err   error
	}
	pods := make(chan podResult, 1)
	go func() {
		items, stack, err := listPods(ctx)
		pods <- podResult{items, stack, err}
	}()
	containers, dockerErr := listDockerContainers(ctx)
	setDockerError(dockerErr)
	var result podResult
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result = <-pods:
	}
	if dockerErr != nil && result.stack == nil {
		return nil, dockerErr
	}
	if dockerErr != nil && result.err != nil {
		return nil, fmt.Errorf("%w\n\nKubernetes: %v", dockerErr, result.err)
	}
	return append(containers, result.items...), nil
}

// containerProject names the Compose project, or the local Kubernetes cluster
// a node container belongs to, so kind and minikube nodes group with their stack.
func containerProject(labels string) string {
	project := "standalone"
	for _, label := range strings.Split(labels, ",") {
		key, value, _ := strings.Cut(label, "=")
		switch key {
		case "com.docker.compose.project":
			project = value
		case "io.x-k8s.kind.cluster":
			project = "kind:" + value
		case "k3d.cluster":
			project = "k3d:" + value
		case "name.minikube.sigs.k8s.io":
			project = "minikube:" + value
		}
	}
	return project
}

func listDockerContainers(ctx context.Context) ([]workload, error) {
	output, err := command(ctx, "docker", "ps", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var result []workload
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var row struct{ ID, Names, State, Status, Image, Labels string }
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		result = append(result, workload{ID: row.ID, Name: row.Names, State: row.State, Detail: row.Status, Project: containerProject(row.Labels), Description: row.Image})
	}
	return result, nil
}

type unitFile struct {
	UnitFile string `json:"unit_file"`
	State    string `json:"state"`
}

func listUnitFiles(ctx context.Context, user bool) []unitFile {
	args := []string{"list-unit-files", "--type=service", "--output=json", "--no-pager"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	output, err := command(ctx, "systemctl", args...)
	if err != nil {
		return nil
	}
	var files []unitFile
	if json.Unmarshal([]byte(output), &files) != nil {
		return nil
	}
	return files
}

func enrichInventory(ctx context.Context, mode int, user bool, items []workload) []workload {
	if mode == 1 {
		hasPods := false
		for _, item := range items {
			if isPod(item) {
				hasPods = true
				break
			}
		}
		enrichDockerResources(ctx, items)
		if hasPods {
			// Sequential on purpose: both samplers walk the whole slice.
			enrichKubeResources(ctx, currentKubeStack(ctx), items)
		}
		return items
	}
	if usesLaunchd() {
		return enrichLaunchInventory(ctx, user, items)
	}
	// Unit-file discovery and resource accounting are independent queries.
	filesReady := make(chan []unitFile, 1)
	go func() { filesReady <- listUnitFiles(ctx, user) }()
	enrichServiceResources(ctx, user, items)
	var files []unitFile
	select {
	case <-ctx.Done():
		return items
	case files = <-filesReady:
	}
	return mergeUnitFiles(items, files)
}

func mergeUnitFiles(items []workload, files []unitFile) []workload {
	seen := make(map[string]int, len(items))
	aliases := map[string]bool{}
	for i, item := range items {
		seen[item.ID] = i
		for _, alias := range strings.Fields(item.Aliases) {
			aliases[alias] = true
		}
	}
	for _, file := range files {
		if i, ok := seen[file.UnitFile]; ok {
			items[i].Enablement = file.State
			continue
		}
		if aliases[file.UnitFile] {
			continue
		}
		state, detail := "unknown", "No runtime state in this snapshot"
		if strings.Contains(file.UnitFile, "@.service") {
			state, detail = "template", "Requires an instance name"
		} else if file.State == "alias" {
			state, detail = "alias", "Target not in this runtime snapshot"
		}
		items = append(items, workload{ID: file.UnitFile, Name: file.UnitFile, State: state, Detail: detail, Description: "Installed unit file · " + detail, Enablement: file.State, UnitFileOnly: true})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func inspect(ctx context.Context, mode int, user bool, item workload, tab int) (string, error) {
	if mode == 0 && usesLaunchd() {
		return inspectLaunchService(ctx, user, item, tab)
	}
	if mode == 0 && isTemplate(item) && tab != 2 {
		return "This is a systemd template, not a running service instance.\n\n" + item.ID + " defines configuration for named instances.\nUse c to read its configuration, or select an instance such as " + strings.Replace(item.ID, "@.", "@NAME.", 1) + " to inspect runtime state, logs and resources.", nil
	}
	if mode == 1 && isPod(item) {
		return inspectPod(ctx, item, tab)
	}
	if mode == 1 {
		if tab == 1 {
			return command(ctx, "docker", "logs", "--tail", "150", "--timestamps", item.ID)
		}
		if tab == 3 {
			return command(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", item.ID)
		}
		output, err := command(ctx, "docker", "inspect", "--type", "container", "--", item.ID)
		if err != nil {
			return output, err
		}
		if tab == 0 || tab == 4 {
			return dockerOverview(output, tab == 4)
		}
		return output, nil
	}
	args := []string{}
	if user {
		args = append(args, "--user")
	}
	if tab == 1 {
		args = append(args, "--unit", item.ID, "--lines", "150", "--no-pager", "--output=short-iso")
		return command(ctx, "journalctl", args...)
	}
	if tab == 4 {
		args = append(args, "list-dependencies", "--all", "--plain", "--no-pager", item.ID)
		return command(ctx, "systemctl", args...)
	}
	verb := "status"
	if tab == 2 {
		verb = "cat"
	}
	if tab == 3 {
		args = append(args, "show", item.ID, "--property=CPUUsageNSec,MemoryCurrent,TasksCurrent,IOReadBytes,IOWriteBytes,ControlGroup")
	} else {
		args = append(args, verb, "--no-pager", "--", item.ID)
	}
	// systemctl status returns a nonzero code for inactive/failed units, but its output is useful.
	output, err := command(ctx, "systemctl", args...)
	if err != nil && tab == 0 {
		var status *exec.ExitError
		if errors.As(err, &status) {
			if status.ExitCode() == 3 {
				return output, nil
			}
			if status.ExitCode() == 4 {
				scope := "system"
				if user {
					scope = "user"
				}
				return "Unit not found in the " + scope + " systemd manager: " + item.ID + "\n\nCheck the system/user scope (u). The unit may have been removed or renamed. This is not an inactive-service result.\n\n" + output, nil
			}
		}
		return err.Error(), nil
	}
	return output, err
}
