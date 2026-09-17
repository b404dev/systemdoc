package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type project struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
	State     string   `json:"-"`
	Endpoint  string   `json:"endpoint,omitempty"`
	EnvFiles  []string `json:"env_files,omitempty"`
	Profiles  []string `json:"profiles,omitempty"`
}

func dockerEndpoint() string {
	if value := os.Getenv("DOCKER_CONTEXT"); value != "" {
		return "context:" + value
	}
	if value := os.Getenv("DOCKER_HOST"); value != "" {
		return "host:" + value
	}
	return "context:default"
}

func projectArgs(p project, verb string) ([]string, error) {
	if p.Endpoint != "" && p.Endpoint != dockerEndpoint() {
		return nil, fmt.Errorf("project belongs to %s; current endpoint is %s", p.Endpoint, dockerEndpoint())
	}
	if p.Name == "" || p.Directory == "" || len(p.Files) == 0 {
		return nil, fmt.Errorf("project source context is missing; register its Compose file first")
	}
	args := []string{"compose", "--project-name", p.Name, "--project-directory", p.Directory}
	for _, file := range p.Files {
		if !filepath.IsAbs(file) {
			return nil, fmt.Errorf("the Compose path must be absolute")
		}
		if _, err := os.Stat(file); err != nil {
			return nil, err
		}
		args = append(args, "--file", file)
	}
	for _, file := range p.EnvFiles {
		if !filepath.IsAbs(file) {
			return nil, fmt.Errorf("environment file path must be absolute")
		}
		if _, err := os.Stat(file); err != nil {
			return nil, err
		}
		args = append(args, "--env-file", file)
	}
	for _, profile := range p.Profiles {
		args = append(args, "--profile", profile)
	}
	switch verb {
	case "up":
		args = append(args, "up", "--detach")
	case "logs":
		args = append(args, "logs", "--no-color", "--tail", "150", "--timestamps")
	case "validate":
		args = append(args, "config", "--quiet")
	case "config":
		args = append(args, "config", "--no-interpolate", "--no-env-resolution")
	case "ps":
		args = append(args, "ps", "--all")
	case "stop", "start", "restart", "down", "pull", "build":
		args = append(args, verb)
	default:
		return nil, fmt.Errorf("unsupported Compose action %q", verb)
	}
	return args, nil
}

func discoverProjects(ctx context.Context, registered []project) ([]project, error) {
	var result []project
	for _, p := range registered {
		if p.Endpoint == "" || p.Endpoint == dockerEndpoint() {
			result = append(result, p)
		}
	}
	for i := range result {
		result[i].State = "registered · not observed"
	}
	output, err := command(ctx, "docker", "compose", "ls", "--all", "--format", "json")
	if err != nil {
		return result, err
	}
	var rows []struct{ Name, Status, ConfigFiles string }
	if err = json.Unmarshal([]byte(output), &rows); err != nil {
		return result, err
	}
	for _, row := range rows {
		found := false
		for i := range result {
			if result[i].Name == row.Name {
				result[i].State = row.Status
				found = true
				break
			}
		}
		if found {
			continue
		}
		p := project{Name: row.Name, State: row.Status, Endpoint: dockerEndpoint()}
		if row.ConfigFiles != "" {
			p.Files = strings.Split(row.ConfigFiles, ",")
			p.Directory = filepath.Dir(p.Files[0])
		}
		result = append(result, p)
	}
	return result, nil
}
