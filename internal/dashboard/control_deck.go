package dashboard

import "fmt"

// controlDeck makes the five feature suites explicit while routing to the
// existing live views, so discovery never creates a second source of truth.
func (w *workspace) controlDeck() {
	services := fleetTotals(w.items[0])
	containers := fleetTotals(w.items[1])
	choices := []choice{
		{
			"01  " + w.icon(iconServices) + "  SERVICES  ·  lifecycle & diagnosis",
			fmt.Sprintf("%d observed · %d active · %d need attention · status, logs, config, metrics, dependencies, timers and safe actions", len(w.items[0]), services.active, services.attention),
			func() { w.navigateToSuite(0) },
		},
		{
			"02  " + w.icon(iconContainers) + "  CONTAINERS  ·  Docker, Compose & Kubernetes",
			fmt.Sprintf("%d observed · %d running · %d need attention · health, ports, mounts, networks, logs, inspect JSON, Compose workflows and single-node Kubernetes pods (k0s, k3s, kind, minikube, microk8s)", len(w.items[1]), containers.active, containers.attention),
			func() { w.navigateToSuite(1) },
		},
		{
			"03  " + w.icon(iconNetwork) + "  NETWORK  ·  ports & connections",
			"TCP listeners, UDP bindings, live connections, interfaces, download/upload speed, process owners, precise filters and private snapshot export",
			func() { w.navigateToSuite(2) },
		},
		{
			"04  " + w.icon(iconProcesses) + "  PROCESSES  ·  resource explorer",
			"CPU and RSS ranking, parent trees, full commands, zombie detection, reviewed process signals, service ownership and one-key jumps to open ports",
			func() { w.navigateToSuite(3) },
		},
		{
			"05  " + w.icon(iconStorage) + "  STORAGE  ·  capacity & pressure",
			"Mount capacity, free space, inode pressure, deleted files held open, filters, sorting and private evidence export",
			func() { w.navigateToSuite(4) },
		},
	}
	w.choose("control-deck", w.iconLabel(iconEye, "CONTROL DECK · five operational suites"), choices)
}

func (w *workspace) navigateToSuite(target int) {
	_, page := w.pages.GetFrontPage()
	switch current := page.(type) {
	case *networkPage:
		if target == 2 {
			w.app.SetFocus(current.table)
			return
		}
		current.close()
	case *hostPage:
		if target == current.tab {
			w.app.SetFocus(current.table)
			return
		}
		current.close()
	}
	w.openHostTab(target)
}
