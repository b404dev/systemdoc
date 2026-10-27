package dashboard

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"systemdoc/internal/promapi"
)

// detectInstall learns what this host can install with and fills the setup
// steps; it runs once per page, off the UI thread.
func (m *metricsPage) detectInstall() {
	c := promapi.InstallContext{GOOS: runtime.GOOS, DistroID: distroID(), Docker: dockerUsable(m.ctx), Proxmox: isProxmox()}
	for _, manager := range []string{"apt-get", "dnf", "pacman", "zypper", "brew"} {
		if _, err := exec.LookPath(manager); err == nil {
			c.PackageManager = manager
			break
		}
	}
	steps := promapi.InstallSteps(c)
	m.w.queue(func() {
		if m.ctx.Err() == nil {
			m.install = steps
			m.render()
		}
	})
}

func distroID() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

func isProxmox() bool {
	if _, err := os.Stat("/etc/pve"); err == nil {
		return true
	}
	_, err := exec.LookPath("pveversion")
	return err == nil
}

// renderSetup shows the connection state, where the URL comes from, and the
// install steps for this host. Runnable steps are numbered; the rest are
// shown for the user to apply by hand.
func (m *metricsPage) renderSetup() {
	p := m.w.palette()
	var out strings.Builder
	head := func(text string) { fmt.Fprintf(&out, "\n[%s::b]%s[-::-]\n", panelHue(p, metricsTab), text) }
	muted := func(text string) { fmt.Fprintf(&out, "[%s]%s[-]\n", p.muted, tview.Escape(text)) }
	head("CONNECTION")
	switch m.state {
	case metricsUnconfigured:
		muted("No Prometheus URL is configured and none was detected among running services or containers.")
	case metricsProbing:
		muted("Probing " + m.endpoint.URL + " …")
	case metricsUnreachable:
		if m.endpoint.Source == "default" {
			muted("Nothing is configured or detected, and nothing answers on the conventional " + m.endpoint.URL)
		} else {
			fmt.Fprintf(&out, "[%s]%s is not answering: %s[-]\n", p.error, tview.Escape(m.endpoint.URL), tview.Escape(m.failure))
		}
		muted("Retries back off up to ten minutes · r probes again now")
	default:
		fmt.Fprintf(&out, "[%s]%s answers[-] · prometheus %s · %s\n", p.success, tview.Escape(m.endpoint.URL), tview.Escape(m.build.Version), tview.Escape(m.endpoint.Source))
	}
	path, _ := settingsPath()
	muted("Set the URL with --prometheus, SYSTEMDOC_PROMETHEUS or prometheus_url in " + path)
	muted("A bearer token is read from SYSTEMDOC_PROMETHEUS_TOKEN and never stored")
	if len(m.install) == 0 {
		head("INSTALL")
		muted("Detecting package manager and Docker …")
		m.setup.SetText(out.String())
		return
	}
	component := ""
	for i, step := range m.install {
		if step.Component != component {
			component = step.Component
			head(strings.ToUpper(strings.NewReplacer("-", " ", "_", " ").Replace(component)))
		}
		marker, colour := fmt.Sprintf(" %2d ", i+1), p.text
		if i == m.installStep {
			marker, colour = fmt.Sprintf("▸%2d ", i+1), p.accent
		}
		fmt.Fprintf(&out, "[%s]%s%s[-]", colour, marker, tview.Escape(step.Title))
		if step.Sudo {
			fmt.Fprintf(&out, " [%s]sudo[-]", p.warning)
		}
		if !step.Runnable {
			fmt.Fprintf(&out, " [%s]show only[-]", p.muted)
		}
		out.WriteString("\n")
		if len(step.Argv) > 0 {
			fmt.Fprintf(&out, "    [%s]%s[-]\n", p.muted, tview.Escape(strings.Join(step.Argv, " ")))
		}
		if i == m.installStep && step.Note != "" {
			fmt.Fprintf(&out, "    [%s]%s[-]\n", p.muted, tview.Escape(strings.ReplaceAll(step.Note, "\n", "\n    ")))
		}
	}
	m.setup.SetText(out.String())
}

func (m *metricsPage) moveStep(key tcell.Key) {
	if key == tcell.KeyUp {
		m.installStep = max(0, m.installStep-1)
	} else {
		m.installStep = min(len(m.install)-1, m.installStep+1)
	}
	m.renderSetup()
}

// runSelectedStep reviews the chosen install command and runs it through the
// same approval, sudo and operation history as every other action.
func (m *metricsPage) runSelectedStep() {
	if m.installStep >= len(m.install) {
		return
	}
	step := m.install[m.installStep]
	if !step.Runnable || len(step.Argv) == 0 {
		m.w.message(step.Title, step.Note)
		return
	}
	w := m.w
	detail := strings.Join(step.Argv, " ") + "\n\n" + step.Note
	run := func() { w.execute(step.Component, step.Argv[0], step.Argv[1:]) }
	if step.Sudo {
		detail = "sudo -n -- " + detail
		run = func() {
			w.ensureSudo(func() { w.execute(step.Component, "sudo", append([]string{"-n", "--"}, step.Argv...)) })
		}
	}
	w.confirm("install · "+step.Title, detail, run)
}
