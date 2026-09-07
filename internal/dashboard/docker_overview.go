package dashboard

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

type dockerPortBinding struct{ HostIP, HostPort string }
type dockerNetwork struct {
	IPAddress, GlobalIPv6Address, Gateway, IPv6Gateway, MacAddress string
	Aliases                                                        []string
}
type dockerMount struct {
	Type, Name, Source, Destination, Driver, Mode, Propagation string
	RW                                                         bool
}
type dockerInspection struct {
	ID, Name, Created, Image, Path, Platform, Driver, LogPath string
	Args                                                      []string
	RestartCount                                              int
	State                                                     *struct {
		Status, StartedAt, FinishedAt, Error string
		Running, OOMKilled                   bool
		Pid, ExitCode                        int
		Health                               *struct {
			Status        string
			FailingStreak int
		}
	}
	Config *struct {
		Image, Hostname, User, WorkingDir string
		Entrypoint, Cmd, Env              []string
		Labels                            map[string]string
		ExposedPorts                      map[string]json.RawMessage
	}
	HostConfig *struct {
		NetworkMode, Runtime                   string
		Privileged, ReadonlyRootfs, AutoRemove bool
		RestartPolicy                          struct {
			Name              string
			MaximumRetryCount int
		}
		PortBindings        map[string][]dockerPortBinding
		LogConfig           struct{ Type string }
		Memory, NanoCpus    int64
		CpuQuota, CpuPeriod int64
	}
	Mounts          []dockerMount
	NetworkSettings *struct {
		Ports    map[string][]dockerPortBinding
		Networks map[string]*dockerNetwork
	}
}

// Overview is a readable projection of one container inspection. Config keeps
// the complete JSON; environment values are not copied into the overview.
func dockerOverview(raw string, connectionsOnly bool) (string, error) {
	var rows []dockerInspection
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return "", fmt.Errorf("decode Docker container inspection: %w", err)
	}
	if len(rows) != 1 || rows[0].ID == "" {
		return "", fmt.Errorf("Docker inspection did not return exactly one container")
	}
	c := rows[0]
	var out strings.Builder
	section := func(title string) {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		fmt.Fprintf(&out, "── %s\n", title)
	}
	field := func(label, value string) {
		value = strings.Join(strings.Fields(clean(value)), " ")
		fmt.Fprintf(&out, "  %-12s %s\n", label, available(value))
	}
	yesNo := func(value bool) string {
		if value {
			return "yes"
		}
		return "no"
	}
	section("CONTAINER")
	field("Name", strings.TrimPrefix(c.Name, "/"))
	if connectionsOnly {
		field("ID", c.ID)
	}
	if !connectionsOnly {
		if c.Config != nil {
			field("Image", c.Config.Image)
		}
		if c.State != nil {
			field("State", c.State.Status)
			health := "not configured"
			if c.State.Health != nil {
				health = fmt.Sprintf("%s · failing streak %d", c.State.Health.Status, c.State.Health.FailingStreak)
			}
			field("Health", health)
			if !c.State.Running && c.State.Status != "created" {
				field("Exit code", fmt.Sprint(c.State.ExitCode))
			}
			if c.State.Error != "" {
				field("Error", c.State.Error)
			}
		} else {
			field("State", "unavailable")
		}
		if c.HostConfig != nil {
			policy := c.HostConfig.RestartPolicy.Name
			if policy == "on-failure" {
				policy += fmt.Sprintf(" · max retries %d (0 = unlimited)", c.HostConfig.RestartPolicy.MaximumRetryCount)
			}
			field("Restart", policy)
		}
		if c.Config != nil {
			project, service := c.Config.Labels["com.docker.compose.project"], c.Config.Labels["com.docker.compose.service"]
			if project == "" {
				project = "standalone"
			}
			field("Project", project)
			if service != "" {
				field("Service", service)
			}
		}
	}

	section("PORTS · host → container")
	ports := map[string]bool{}
	if c.Config != nil {
		for port := range c.Config.ExposedPorts {
			ports[port] = true
		}
	}
	if c.HostConfig != nil {
		for port := range c.HostConfig.PortBindings {
			ports[port] = true
		}
	}
	if c.NetworkSettings != nil {
		for port := range c.NetworkSettings.Ports {
			ports[port] = true
		}
	}
	if len(ports) == 0 {
		field("Ports", "none reported")
	}
	for _, port := range sortedKeys(ports) {
		var bindings []dockerPortBinding
		configured := false
		if c.NetworkSettings != nil {
			bindings = c.NetworkSettings.Ports[port]
		}
		if len(bindings) == 0 && c.HostConfig != nil {
			bindings = c.HostConfig.PortBindings[port]
			configured = len(bindings) > 0
		}
		if len(bindings) == 0 {
			field(port, "exposed only · no host binding")
			continue
		}
		for _, binding := range bindings {
			host := binding.HostIP
			if host == "" {
				host = "*"
			}
			hostPort := binding.HostPort
			if hostPort == "" {
				hostPort = "automatic"
			}
			detail := net.JoinHostPort(host, hostPort) + " → " + port
			if configured {
				detail += " (configured; no live binding reported)"
			}
			field("Binding", detail)
		}
	}
	if c.HostConfig != nil {
		field("Network mode", c.HostConfig.NetworkMode)
	}

	section("VOLUMES & MOUNTS · source → destination")
	if len(c.Mounts) == 0 {
		field("Mounts", "none reported")
	}
	sort.SliceStable(c.Mounts, func(i, j int) bool { return c.Mounts[i].Destination < c.Mounts[j].Destination })
	for _, mount := range c.Mounts {
		access := "read-only"
		if mount.RW {
			access = "read-write"
		}
		source := mount.Source
		if mount.Name != "" {
			source = mount.Name
		}
		if source == "" {
			source = mount.Type
		}
		field(mount.Type, source+" → "+mount.Destination)
		detail := access
		if mount.Driver != "" {
			detail += " · driver " + mount.Driver
		}
		if mount.Propagation != "" {
			detail += " · " + mount.Propagation
		}
		if mount.Mode != "" {
			detail += " · mode " + mount.Mode
		}
		field("Access", detail)
		if mount.Name != "" && mount.Source != "" {
			field("Host path", mount.Source)
		}
	}

	section("NETWORKS")
	if c.NetworkSettings == nil {
		field("Networks", "unavailable")
	} else {
		if len(c.NetworkSettings.Networks) == 0 {
			field("Networks", "none reported")
		}
		for _, name := range sortedKeys(c.NetworkSettings.Networks) {
			field("Network", name)
			network := c.NetworkSettings.Networks[name]
			if network == nil {
				field("Endpoint", "unavailable")
				continue
			}
			field("IPv4", network.IPAddress)
			if network.GlobalIPv6Address != "" {
				field("IPv6", network.GlobalIPv6Address)
			}
			if network.Gateway != "" {
				field("Gateway", network.Gateway)
			}
			if network.IPv6Gateway != "" {
				field("IPv6 gateway", network.IPv6Gateway)
			}
			if network.MacAddress != "" {
				field("MAC", network.MacAddress)
			}
			if len(network.Aliases) > 0 {
				field("Aliases", strings.Join(network.Aliases, ", "))
			}
		}
	}
	if !connectionsOnly {
		section("RUNTIME")
		field("ID", c.ID)
		field("Created", dockerTimestamp(c.Created))
		field("Restarts", fmt.Sprint(c.RestartCount))
		if c.State != nil {
			field("Started", dockerTimestamp(c.State.StartedAt))
			if !c.State.Running && c.State.Status != "created" {
				field("Finished", dockerTimestamp(c.State.FinishedAt))
			}
			field("PID", fmt.Sprint(c.State.Pid))
			field("OOM killed", yesNo(c.State.OOMKilled))
		}
		if c.Config != nil {
			field("Entrypoint", dockerArgv(c.Config.Entrypoint))
			field("Command", dockerArgv(c.Config.Cmd))
			field("Hostname", c.Config.Hostname)
			user := c.Config.User
			if user == "" {
				user = "image default (typically root)"
			}
			field("User", user)
			field("Workdir", c.Config.WorkingDir)
			field("Environment", fmt.Sprintf("%d entries · c opens full inspect JSON", len(c.Config.Env)))
		}
		field("Image ID", c.Image)
		field("Platform", c.Platform)
		field("Storage", c.Driver)
		if c.HostConfig != nil {
			field("Runtime", c.HostConfig.Runtime)
			field("Log driver", c.HostConfig.LogConfig.Type)
			field("Privileged", yesNo(c.HostConfig.Privileged))
			field("Read-only FS", yesNo(c.HostConfig.ReadonlyRootfs))
			field("Auto-remove", yesNo(c.HostConfig.AutoRemove))
			memory := "no explicit limit"
			if c.HostConfig.Memory > 0 {
				memory = fmt.Sprintf("%.1f MiB", float64(c.HostConfig.Memory)/(1024*1024))
			}
			field("Memory limit", memory)
			cpu := "no explicit quota"
			if c.HostConfig.NanoCpus > 0 {
				cpu = fmt.Sprintf("%.2f CPUs", float64(c.HostConfig.NanoCpus)/1e9)
			} else if c.HostConfig.CpuQuota > 0 && c.HostConfig.CpuPeriod > 0 {
				cpu = fmt.Sprintf("%.2f CPUs", float64(c.HostConfig.CpuQuota)/float64(c.HostConfig.CpuPeriod))
			}
			field("CPU quota", cpu)
		}
		if c.LogPath != "" {
			field("Log path", c.LogPath)
		}
	}
	if c.Config != nil {
		section("LABELS")
		if len(c.Config.Labels) == 0 {
			field("Labels", "none")
		}
		for _, key := range sortedKeys(c.Config.Labels) {
			field(key, c.Config.Labels[key])
		}
	}
	out.WriteString("\n  z expand · arrows scroll · c full inspect JSON · l logs · r live metrics\n")
	return out.String(), nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func dockerTimestamp(value string) string {
	if strings.HasPrefix(value, "0001-01-01") {
		return "—"
	}
	return value
}

func dockerArgv(args []string) string {
	if len(args) == 0 {
		return "—"
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = fmt.Sprintf("%q", arg)
	}
	return strings.Join(parts, " ")
}
