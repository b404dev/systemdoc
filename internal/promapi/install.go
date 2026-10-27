package promapi

// InstallContext describes the host so InstallSteps can pick the right
// commands. PackageManager is one of "apt-get", "dnf", "pacman", "zypper",
// "brew" or "" when none was found. DistroID is the ID from /etc/os-release
// ("debian", "ubuntu", "fedora", "rhel", "rocky", "almalinux", "arch", ...).
type InstallContext struct {
	GOOS           string
	DistroID       string
	PackageManager string
	Docker         bool
	Proxmox        bool
}

// InstallStep is one command or snippet in the setup guide. Runnable steps
// carry an Argv the dashboard may execute after review; show-only steps
// carry an Argv for display or a Note with file contents.
type InstallStep struct {
	// Component groups steps: "prometheus", "node_exporter", "cadvisor",
	// "pve-exporter" or "scrape-config".
	Component string
	// Title is a one-line description.
	Title string
	// Argv is the command without a shell; empty for pure notes.
	Argv []string
	// Sudo marks commands that need root.
	Sudo bool
	// Runnable marks commands the dashboard may execute after review.
	Runnable bool
	// Note holds context, a file path or file contents to show.
	Note string
}

// Component names used by InstallSteps.
const (
	ComponentPrometheus   = "prometheus"
	ComponentNodeExporter = "node_exporter"
	ComponentCadvisor     = "cadvisor"
	ComponentPVEExporter  = "pve-exporter"
	ComponentScrapeConfig = "scrape-config"
)

// CadvisorImage is the pinned cAdvisor image used by the Docker step.
const CadvisorImage = "ghcr.io/google/cadvisor:v0.52.1"

// ScrapeSnippetNode is the prometheus.yml scrape job for node_exporter.
const ScrapeSnippetNode = `  - job_name: node
    static_configs:
      - targets: ["localhost:9100"]
`

// ScrapeSnippetCadvisor is the prometheus.yml scrape job for cAdvisor.
const ScrapeSnippetCadvisor = `  - job_name: cadvisor
    static_configs:
      - targets: ["localhost:8080"]
`

// ScrapeSnippetPVE is the prometheus.yml scrape job for pve-exporter. The
// target is the Proxmox node to query; the exporter itself listens on 9221.
const ScrapeSnippetPVE = `  - job_name: pve
    metrics_path: /pve
    params:
      module: [default]
      cluster: ["1"]
      node: ["1"]
    static_configs:
      - targets: ["localhost"]
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:9221
`

// PVEConfigSnippet is /etc/prometheus/pve.yml for pve-exporter (mode 0600).
const PVEConfigSnippet = `default:
  user: prometheus@pve
  token_name: monitoring
  token_value: "<secret printed by pveum user token add>"
  verify_ssl: false
`

// PVEExporterUnit is a systemd unit for a pipx-installed pve-exporter.
const PVEExporterUnit = `[Unit]
Description=Prometheus Proxmox VE exporter
After=network-online.target

[Service]
ExecStart=/root/.local/bin/pve_exporter --config.file /etc/prometheus/pve.yml --web.listen-address 127.0.0.1:9221
Restart=on-failure

[Install]
WantedBy=multi-user.target
`

const (
	promConfigPath    = "/etc/prometheus/prometheus.yml"
	pveConfigPath     = "/etc/prometheus/pve.yml"
	dockerDesktopNote = "Docker Desktop containers cannot see the host PID and network namespaces, so node_exporter and cAdvisor in Docker report the Linux VM, not this Mac. Install them with Homebrew instead."
)

func run(component, title string, sudo bool, note string, argv ...string) InstallStep {
	return InstallStep{Component: component, Title: title, Argv: argv, Sudo: sudo, Runnable: true, Note: note}
}

func show(component, title, note string, argv ...string) InstallStep {
	return InstallStep{Component: component, Title: title, Argv: argv, Note: note}
}

// InstallSteps returns the setup guide for the host described by c. Steps
// are ordered Prometheus server, node_exporter, cAdvisor (Docker hosts),
// pve-exporter, then scrape configuration. Runnable steps never need a
// shell; show-only steps are documentation.
func InstallSteps(c InstallContext) []InstallStep {
	linux := c.GOOS == "linux"
	var steps []InstallStep
	steps = append(steps, serverSteps(c)...)
	steps = append(steps, nodeExporterSteps(c)...)
	if c.Docker {
		if linux {
			steps = append(steps, run(ComponentCadvisor, "Run cAdvisor in Docker", false,
				"Exposes container metrics on 127.0.0.1:8080. Newer image tags exist; bump the pin after checking the release notes.",
				"docker", "run", "-d", "--name", "cadvisor", "--restart", "unless-stopped",
				"-p", "127.0.0.1:8080:8080",
				"-v", "/:/rootfs:ro", "-v", "/var/run:/var/run:rw", "-v", "/sys:/sys:ro", "-v", "/var/lib/docker/:/var/lib/docker:ro",
				CadvisorImage))
		} else {
			steps = append(steps, show(ComponentCadvisor, "cAdvisor in Docker (Linux hosts)", dockerDesktopNote,
				"docker", "run", "-d", "--name", "cadvisor", "--restart", "unless-stopped",
				"-p", "127.0.0.1:8080:8080",
				"-v", "/:/rootfs:ro", "-v", "/var/run:/var/run:rw", "-v", "/sys:/sys:ro", "-v", "/var/lib/docker/:/var/lib/docker:ro",
				CadvisorImage))
		}
	}
	steps = append(steps, pveSteps(c)...)
	steps = append(steps, scrapeSteps(c)...)
	return steps
}

func serverSteps(c InstallContext) []InstallStep {
	const comp = ComponentPrometheus
	var steps []InstallStep
	switch c.PackageManager {
	case "apt-get":
		steps = append(steps,
			run(comp, "Install Prometheus", true, "Debian's package ships a config that already scrapes localhost:9100.",
				"apt-get", "install", "-y", "prometheus"),
			run(comp, "Enable and start Prometheus", true, "", "systemctl", "enable", "--now", "prometheus"),
			show(comp, "Configuration files", promConfigPath+" holds scrape jobs; /etc/default/prometheus holds command-line flags such as retention."))
	case "dnf":
		if c.DistroID == "fedora" {
			steps = append(steps,
				run(comp, "Install Prometheus", true, "", "dnf", "install", "-y", "golang-github-prometheus"),
				run(comp, "Enable and start Prometheus", true, "", "systemctl", "enable", "--now", "prometheus"),
				show(comp, "Configuration file", promConfigPath))
		} else {
			steps = append(steps, show(comp, "Prometheus server on RHEL-family systems",
				"No packaged server is available. Run the upstream container (podman run -d --name prometheus -p 127.0.0.1:9090:9090 -v "+promConfigPath+":"+promConfigPath+":ro -v prometheus-data:/prometheus docker.io/prom/prometheus) or install the release tarball from prometheus.io/download."))
		}
	case "pacman":
		steps = append(steps,
			run(comp, "Install Prometheus", true, "", "pacman", "-S", "--noconfirm", "--needed", "prometheus"),
			run(comp, "Enable and start Prometheus", true, "", "systemctl", "enable", "--now", "prometheus"),
			show(comp, "Configuration files", promConfigPath+" holds scrape jobs; /etc/conf.d/prometheus holds command-line flags."))
	case "zypper":
		steps = append(steps,
			run(comp, "Install Prometheus", true, "Package names follow the openSUSE server:monitoring repository.",
				"zypper", "install", "-y", "golang-github-prometheus-prometheus"),
			run(comp, "Enable and start Prometheus", true, "", "systemctl", "enable", "--now", "prometheus"),
			show(comp, "Configuration file", promConfigPath))
	case "brew":
		steps = append(steps,
			run(comp, "Install Prometheus", false, "", "brew", "install", "prometheus"),
			run(comp, "Start Prometheus as a service", false, "", "brew", "services", "start", "prometheus"),
			show(comp, "Configuration files", "$(brew --prefix)/etc/prometheus.yml holds scrape jobs; $(brew --prefix)/etc/prometheus.args holds command-line flags."))
	}
	if c.Docker && dockerServer(c) {
		dockerNote := "Create " + promConfigPath + " first; the container reads it read-only."
		if c.GOOS != "linux" {
			dockerNote = "Create prometheus.yml first and adjust the bind mount to its path; Docker Desktop can only share directories listed in its file-sharing settings."
		}
		steps = append(steps,
			run(comp, "Create a volume for Prometheus data", false, "", "docker", "volume", "create", "prometheus-data"),
			run(comp, "Run Prometheus in Docker", false, dockerNote,
				"docker", "run", "-d", "--name", "prometheus", "--restart", "unless-stopped",
				"-p", "127.0.0.1:9090:9090",
				"-v", promConfigPath+":"+promConfigPath+":ro",
				"-v", "prometheus-data:/prometheus",
				"prom/prometheus"),
			show(comp, "Scrape configuration", "Minimal "+promConfigPath+":\n\nglobal:\n  scrape_interval: 15s\nscrape_configs:\n"+ScrapeSnippetNode))
	}
	if len(steps) == 0 {
		steps = append(steps, show(comp, "Install Prometheus",
			"No supported package manager or Docker was found. Download the release tarball from prometheus.io/download, unpack it, and run ./prometheus --config.file=prometheus.yml."))
	}
	return steps
}

// dockerServer reports whether the Docker server steps should be offered:
// when no package manager provides a runnable server.
func dockerServer(c InstallContext) bool {
	switch c.PackageManager {
	case "":
		return true
	case "dnf":
		return c.DistroID != "fedora"
	}
	return false
}

func nodeExporterSteps(c InstallContext) []InstallStep {
	const comp = ComponentNodeExporter
	var steps []InstallStep
	switch c.PackageManager {
	case "apt-get":
		steps = append(steps,
			run(comp, "Install node_exporter", true, "", "apt-get", "install", "-y", "prometheus-node-exporter"),
			run(comp, "Enable and start node_exporter", true, "Listens on :9100.", "systemctl", "enable", "--now", "prometheus-node-exporter"),
			show(comp, "Collector flags", "/etc/default/prometheus-node-exporter"))
	case "dnf":
		steps = append(steps,
			run(comp, "Install node_exporter", true, "Fedora ships node-exporter; RHEL-family systems need EPEL 9 or 10.",
				"dnf", "install", "-y", "node-exporter"),
			run(comp, "Enable and start node_exporter", true, "Listens on :9100.", "systemctl", "enable", "--now", "node_exporter"))
	case "pacman":
		steps = append(steps,
			run(comp, "Install node_exporter", true, "", "pacman", "-S", "--noconfirm", "--needed", "prometheus-node-exporter"),
			run(comp, "Enable and start node_exporter", true, "Listens on :9100.", "systemctl", "enable", "--now", "prometheus-node-exporter"),
			show(comp, "Collector flags", "/etc/conf.d/prometheus-node-exporter"))
	case "zypper":
		steps = append(steps,
			run(comp, "Install node_exporter", true, "", "zypper", "install", "-y", "golang-github-prometheus-node_exporter"),
			run(comp, "Enable and start node_exporter", true, "Listens on :9100.", "systemctl", "enable", "--now", "prometheus-node_exporter"))
	case "brew":
		steps = append(steps,
			run(comp, "Install node_exporter", false, "", "brew", "install", "node_exporter"),
			run(comp, "Start node_exporter as a service", false, "Listens on :9100.", "brew", "services", "start", "node_exporter"),
			show(comp, "Collector flags", "$(brew --prefix)/etc/node_exporter.args"))
	}
	if c.Docker && (c.PackageManager == "" || c.GOOS != "linux") {
		argv := []string{"docker", "run", "-d", "--name", "node-exporter", "--restart", "unless-stopped",
			"--net=host", "--pid=host", "-v", "/:/host:ro,rslave",
			"quay.io/prometheus/node-exporter:latest", "--path.rootfs=/host"}
		if c.GOOS == "linux" {
			steps = append(steps, run(comp, "Run node_exporter in Docker", false, "Uses the host network and PID namespaces so it sees the host, not the container.", argv...))
		} else {
			steps = append(steps, show(comp, "node_exporter in Docker (Linux hosts)", dockerDesktopNote, argv...))
		}
	}
	if len(steps) == 0 {
		steps = append(steps, show(comp, "Install node_exporter",
			"Download the release tarball from prometheus.io/download and run ./node_exporter; it listens on :9100."))
	}
	return steps
}

func pveSteps(c InstallContext) []InstallStep {
	const comp = ComponentPVEExporter
	var steps []InstallStep
	if !c.Proxmox {
		return []InstallStep{
			show(comp, "Proxmox VE (not detected here)", "Run pve-exporter on a Proxmox node or point it at the PVE API. It needs a read-only API token and "+pveConfigPath+":\n\n"+PVEConfigSnippet),
			show(comp, "Scrape job for pve-exporter", ScrapeSnippetPVE),
		}
	}
	steps = append(steps,
		run(comp, "Create a read-only API user", true, "", "pveum", "user", "add", "prometheus@pve", "--comment", "Prometheus metrics"),
		run(comp, "Grant PVEAuditor to the user", true, "", "pveum", "acl", "modify", "/", "--users", "prometheus@pve", "--roles", "PVEAuditor"),
		show(comp, "Create an API token", "Prints the token secret once; copy it into "+pveConfigPath+".",
			"pveum", "user", "token", "add", "prometheus@pve", "monitoring", "--privsep", "1"),
		run(comp, "Grant PVEAuditor to the token", true, "", "pveum", "acl", "modify", "/", "--tokens", "prometheus@pve!monitoring", "--roles", "PVEAuditor"),
		show(comp, "Write "+pveConfigPath, "Mode 0600, owned by the user running the exporter:\n\n"+PVEConfigSnippet),
	)
	if c.Docker {
		steps = append(steps, run(comp, "Run pve-exporter in Docker", false, "Start after "+pveConfigPath+" exists.",
			"docker", "run", "-d", "--init", "--name", "prometheus-pve-exporter", "--restart", "unless-stopped",
			"-p", "127.0.0.1:9221:9221",
			"-v", pveConfigPath+":"+pveConfigPath+":ro",
			"prompve/prometheus-pve-exporter"))
	} else {
		steps = append(steps,
			run(comp, "Install pipx", true, "", "apt-get", "install", "-y", "pipx"),
			run(comp, "Install pve-exporter", false, "Installs pve_exporter into ~/.local/bin for the current user.", "pipx", "install", "prometheus-pve-exporter"),
			show(comp, "Systemd unit for pve-exporter", "Save as /etc/systemd/system/prometheus-pve-exporter.service, adjust ExecStart to the pipx path, then systemctl enable --now prometheus-pve-exporter:\n\n"+PVEExporterUnit))
	}
	steps = append(steps, show(comp, "Scrape job for pve-exporter", "Add to scrape_configs in "+promConfigPath+":\n\n"+ScrapeSnippetPVE))
	return steps
}

func scrapeSteps(c InstallContext) []InstallStep {
	const comp = ComponentScrapeConfig
	configPath := promConfigPath
	if c.PackageManager == "brew" {
		configPath = "$(brew --prefix)/etc/prometheus.yml"
	}
	steps := []InstallStep{
		show(comp, "Scrape node_exporter", "Add to scrape_configs in "+configPath+":\n\n"+ScrapeSnippetNode),
	}
	if c.Docker {
		steps = append(steps, show(comp, "Scrape cAdvisor", "Add to scrape_configs in "+configPath+":\n\n"+ScrapeSnippetCadvisor))
	}
	switch {
	case c.GOOS == "linux" && c.PackageManager != "":
		steps = append(steps,
			run(comp, "Check the configuration", false, "", "promtool", "check", "config", promConfigPath),
			run(comp, "Reload Prometheus", true, "", "systemctl", "reload", "prometheus"))
	case c.PackageManager == "brew":
		steps = append(steps, run(comp, "Restart Prometheus", false, "Homebrew's service picks up the edited "+configPath+".", "brew", "services", "restart", "prometheus"))
	case c.Docker && c.PackageManager == "":
		steps = append(steps,
			run(comp, "Check the configuration", false, "", "docker", "exec", "prometheus", "promtool", "check", "config", promConfigPath),
			run(comp, "Reload Prometheus", false, "", "docker", "kill", "--signal=HUP", "prometheus"))
	}
	return steps
}
