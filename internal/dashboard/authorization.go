package dashboard

import "strings"

func (w *workspace) authorizedActions() {
	item := w.current()
	if item.ID == "" {
		w.message("Select a service", "Choose a system service first.")
		return
	}
	var choices []choice
	for _, verb := range serviceVerbs() {
		name, args, err := actionArgs(0, false, item, verb)
		if err != nil {
			continue
		}
		args = append([]string{"--", name}, args...)
		choices = append(choices, choice{verb, "sudo " + name + " · terminal authorization", func() {
			detail := "sudo " + strings.Join(args, " ")
			if usesLaunchd() {
				detail += "\n\nStop sends SIGTERM; launchd may relaunch the job. Enable/disable does not immediately load, unload or stop it."
			}
			w.confirm("System service · "+item.Name, detail, func() { w.native("sudo", args...) })
		}})
	}
	w.choose("authorized", "System service authorization · "+item.Name, choices)
}
