package promapi

import (
	"strings"
	"testing"
)

func findSteps(steps []InstallStep, component string, argvContains ...string) []InstallStep {
	var out []InstallStep
	for _, s := range steps {
		if s.Component != component {
			continue
		}
		joined := strings.Join(s.Argv, " ")
		ok := true
		for _, want := range argvContains {
			if !strings.Contains(joined, want) {
				ok = false
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

func anyRunnable(steps []InstallStep) bool {
	for _, s := range steps {
		if s.Runnable {
			return true
		}
	}
	return false
}

func TestInstallStepsContexts(t *testing.T) {
	cases := []struct {
		name  string
		ctx   InstallContext
		check func(t *testing.T, steps []InstallStep)
	}{
		{"apt", InstallContext{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get"}, func(t *testing.T, steps []InstallStep) {
			for _, want := range [][]string{
				{ComponentPrometheus, "apt-get install -y prometheus"},
				{ComponentPrometheus, "systemctl enable --now prometheus"},
				{ComponentNodeExporter, "apt-get install -y prometheus-node-exporter"},
				{ComponentNodeExporter, "systemctl enable --now prometheus-node-exporter"},
				{ComponentScrapeConfig, "systemctl reload prometheus"},
			} {
				got := findSteps(steps, want[0], want[1])
				if len(got) != 1 || !got[0].Sudo || !got[0].Runnable {
					t.Errorf("apt: want one sudo runnable %q step in %s, got %+v", want[1], want[0], got)
				}
			}
			if got := findSteps(steps, ComponentScrapeConfig, "promtool check config"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("apt: promtool step missing or not runnable: %+v", got)
			}
			if got := findSteps(steps, ComponentCadvisor); len(got) != 0 {
				t.Errorf("apt without docker: unexpected cadvisor steps %+v", got)
			}
			if anyRunnable(findSteps(steps, ComponentPVEExporter)) {
				t.Error("apt without proxmox: pve steps should be show-only")
			}
		}},
		{"brew", InstallContext{GOOS: "darwin", PackageManager: "brew"}, func(t *testing.T, steps []InstallStep) {
			for _, s := range steps {
				if s.Sudo {
					t.Errorf("brew: unexpected sudo step %+v", s)
				}
			}
			if got := findSteps(steps, ComponentPrometheus, "brew install prometheus"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("brew: install step %+v", got)
			}
			if got := findSteps(steps, ComponentNodeExporter, "brew services start node_exporter"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("brew: node_exporter service step %+v", got)
			}
			if got := findSteps(steps, ComponentScrapeConfig, "systemctl"); len(got) != 0 {
				t.Errorf("brew: unexpected systemctl steps %+v", got)
			}
		}},
		{"docker-only linux", InstallContext{GOOS: "linux", DistroID: "ubuntu", Docker: true}, func(t *testing.T, steps []InstallStep) {
			got := findSteps(steps, ComponentNodeExporter, "docker run", "node-exporter")
			if len(got) != 1 || !got[0].Runnable || got[0].Sudo {
				t.Errorf("docker linux: node-exporter step %+v", got)
			}
			if got := findSteps(steps, ComponentCadvisor, CadvisorImage); len(got) != 1 || !got[0].Runnable {
				t.Errorf("docker linux: cadvisor step %+v", got)
			}
			if got := findSteps(steps, ComponentPrometheus, "docker run", "prom/prometheus"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("docker linux: prometheus step %+v", got)
			}
			if got := findSteps(steps, ComponentScrapeConfig, "systemctl"); len(got) != 0 {
				t.Errorf("docker linux: unexpected systemctl steps %+v", got)
			}
		}},
		{"darwin docker", InstallContext{GOOS: "darwin", Docker: true}, func(t *testing.T, steps []InstallStep) {
			got := findSteps(steps, ComponentNodeExporter, "docker run", "node-exporter")
			if len(got) != 1 || got[0].Runnable || !strings.Contains(got[0].Note, "Docker Desktop") {
				t.Errorf("darwin docker: node-exporter step %+v", got)
			}
			if got := findSteps(steps, ComponentCadvisor); len(got) != 1 || got[0].Runnable {
				t.Errorf("darwin docker: cadvisor step %+v", got)
			}
		}},
		{"dnf fedora", InstallContext{GOOS: "linux", DistroID: "fedora", PackageManager: "dnf"}, func(t *testing.T, steps []InstallStep) {
			if got := findSteps(steps, ComponentPrometheus, "dnf install -y golang-github-prometheus"); len(got) != 1 || !got[0].Runnable || !got[0].Sudo {
				t.Errorf("fedora: server step %+v", got)
			}
			if got := findSteps(steps, ComponentNodeExporter, "dnf install -y node-exporter"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("fedora: node-exporter step %+v", got)
			}
		}},
		{"dnf rhel", InstallContext{GOOS: "linux", DistroID: "rhel", PackageManager: "dnf"}, func(t *testing.T, steps []InstallStep) {
			if anyRunnable(findSteps(steps, ComponentPrometheus)) {
				t.Errorf("rhel: server steps should be show-only: %+v", findSteps(steps, ComponentPrometheus))
			}
			if len(findSteps(steps, ComponentPrometheus)) == 0 {
				t.Error("rhel: no server guidance")
			}
			if got := findSteps(steps, ComponentNodeExporter, "dnf install -y node-exporter"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("rhel: node-exporter step %+v", got)
			}
		}},
		{"dnf rhel docker", InstallContext{GOOS: "linux", DistroID: "rocky", PackageManager: "dnf", Docker: true}, func(t *testing.T, steps []InstallStep) {
			if got := findSteps(steps, ComponentPrometheus, "docker run", "prom/prometheus"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("rocky docker: prometheus docker step %+v", got)
			}
		}},
		{"proxmox", InstallContext{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get", Proxmox: true}, func(t *testing.T, steps []InstallStep) {
			pve := findSteps(steps, ComponentPVEExporter)
			if len(pve) == 0 {
				t.Fatal("proxmox: no pve-exporter steps")
			}
			if got := findSteps(steps, ComponentPVEExporter, "pveum user add prometheus@pve"); len(got) != 1 || !got[0].Runnable || !got[0].Sudo {
				t.Errorf("proxmox: user add step %+v", got)
			}
			if got := findSteps(steps, ComponentPVEExporter, "pveum user token add"); len(got) != 1 || got[0].Runnable || got[0].Sudo {
				t.Errorf("proxmox: token step must be show-only: %+v", got)
			}
			if got := findSteps(steps, ComponentPVEExporter, "--tokens", "prometheus@pve!monitoring"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("proxmox: token acl step %+v", got)
			}
			if got := findSteps(steps, ComponentPVEExporter, "pipx install prometheus-pve-exporter"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("proxmox: pipx step %+v", got)
			}
			found := false
			for _, s := range pve {
				if strings.Contains(s.Note, PVEConfigSnippet) {
					found = true
				}
			}
			if !found {
				t.Error("proxmox: no step shows pve.yml")
			}
			last := steps[len(steps)-1]
			if last.Component != ComponentScrapeConfig {
				t.Errorf("proxmox: last step is %s, want scrape-config", last.Component)
			}
		}},
		{"proxmox docker", InstallContext{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get", Proxmox: true, Docker: true}, func(t *testing.T, steps []InstallStep) {
			if got := findSteps(steps, ComponentPVEExporter, "prompve/prometheus-pve-exporter"); len(got) != 1 || !got[0].Runnable {
				t.Errorf("proxmox docker: exporter container step %+v", got)
			}
			if got := findSteps(steps, ComponentPVEExporter, "pipx"); len(got) != 0 {
				t.Errorf("proxmox docker: unexpected pipx steps %+v", got)
			}
		}},
		{"nothing", InstallContext{GOOS: "linux", DistroID: "alpine"}, func(t *testing.T, steps []InstallStep) {
			if len(steps) == 0 {
				t.Fatal("nothing: no guidance at all")
			}
			for _, s := range steps {
				if s.Runnable || s.Sudo {
					t.Errorf("nothing: unexpected runnable step %+v", s)
				}
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.check(t, InstallSteps(c.ctx))
		})
	}
}

func TestInstallStepsHygiene(t *testing.T) {
	contexts := []InstallContext{
		{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get", Docker: true, Proxmox: true},
		{GOOS: "linux", DistroID: "fedora", PackageManager: "dnf", Docker: true},
		{GOOS: "linux", DistroID: "rhel", PackageManager: "dnf"},
		{GOOS: "linux", DistroID: "arch", PackageManager: "pacman", Docker: true},
		{GOOS: "linux", DistroID: "opensuse-tumbleweed", PackageManager: "zypper"},
		{GOOS: "linux", Docker: true},
		{GOOS: "linux"},
		{GOOS: "darwin", PackageManager: "brew", Docker: true},
		{GOOS: "darwin", Docker: true},
		{GOOS: "linux", DistroID: "debian", PackageManager: "apt-get", Proxmox: true},
	}
	order := map[string]int{ComponentPrometheus: 0, ComponentNodeExporter: 1, ComponentCadvisor: 2, ComponentPVEExporter: 3, ComponentScrapeConfig: 4}
	for _, ctx := range contexts {
		steps := InstallSteps(ctx)
		if len(steps) == 0 {
			t.Errorf("%+v: no steps", ctx)
		}
		prev := -1
		for _, s := range steps {
			rank, ok := order[s.Component]
			if !ok {
				t.Errorf("%+v: unknown component %q", ctx, s.Component)
			}
			if rank < prev {
				t.Errorf("%+v: component %s out of order", ctx, s.Component)
			}
			prev = rank
			if s.Title == "" {
				t.Errorf("%+v: step without title: %+v", ctx, s)
			}
			if len(s.Argv) == 0 && s.Note == "" {
				t.Errorf("%+v: step %q has neither argv nor note", ctx, s.Title)
			}
			if s.Sudo && !s.Runnable {
				t.Errorf("%+v: sudo step %q is not runnable", ctx, s.Title)
			}
			if !s.Runnable {
				continue
			}
			if len(s.Argv) == 0 {
				t.Errorf("%+v: runnable step %q has empty argv", ctx, s.Title)
			}
			for _, a := range s.Argv {
				if a == "" || strings.ContainsAny(a, "|;>") || strings.Contains(a, "&&") || strings.Contains(a, "$(") {
					t.Errorf("%+v: runnable step %q has shell-like argv element %q", ctx, s.Title, a)
				}
			}
			if s.Argv[0] == "sudo" {
				t.Errorf("%+v: runnable step %q embeds sudo; use the Sudo flag", ctx, s.Title)
			}
			switch s.Argv[0] {
			case "apt-get", "dnf", "pacman", "zypper", "systemctl", "pveum":
				if !s.Sudo {
					t.Errorf("%+v: step %q should be sudo", ctx, s.Title)
				}
			case "brew", "docker":
				if s.Sudo {
					t.Errorf("%+v: step %q should not be sudo", ctx, s.Title)
				}
			}
		}
	}
}
