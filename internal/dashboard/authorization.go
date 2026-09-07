package dashboard

import "strings"

func (w *workspace) authorizedActions() {
	item := w.current()
	if item.ID == "" {
		w.message("Select a service", "Choose a system service first.")
		return
	}
	var choices []choice
	for _, verb := range []string{"start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask", "reset-failed"} {
		_, args, err := actionArgs(0, false, item, verb)
		if err != nil {
			continue
		}
		args = append([]string{"--", "systemctl"}, args...)
		choices = append(choices, choice{verb, "sudo systemctl · terminal authorization", func() {
			w.confirm("System service · "+item.Name, "sudo "+strings.Join(args, " "), func() { w.native("sudo", args...) })
		}})
	}
	w.choose("authorized", "System service authorization · "+item.Name, choices)
}
