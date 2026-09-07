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
	ID, Name, State, Detail, Project, Description string
	Enablement                                    string
	LoadState, Aliases                            string
	UnitFileOnly                                  bool
	CPU, Memory                                   string
	CPUCounter                                    uint64
	HasCPU                                        bool
	SampleAt                                      time.Time
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var buffer tailBuffer
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
			project := "standalone"
			for _, label := range strings.Split(row.Labels, ",") {
				if value, ok := strings.CutPrefix(label, "com.docker.compose.project="); ok {
					project = value
				}
			}
			result = append(result, workload{ID: row.ID, Name: row.Names, State: row.State, Detail: row.Status, Project: project, Description: row.Image})
		}
		return result, nil
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
		enrichDockerResources(ctx, items)
		return items
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
	if mode == 0 && isTemplate(item) && tab != 2 {
		return "This is a systemd template, not a running service instance.\n\n" + item.ID + " defines configuration for named instances.\nUse c to read its configuration, or select an instance such as " + strings.Replace(item.ID, "@.", "@NAME.", 1) + " to inspect runtime state, logs and resources.", nil
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
