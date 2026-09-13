package dashboard

import (
	"fmt"
	"strings"
)

func parseLaunchCommand(parts []string) (parsedCommand, error) {
	result := parsedCommand{tab: -1}
	if len(parts) < 2 {
		return result, fmt.Errorf("use launchctl print DOMAIN/LABEL or a supported lifecycle command")
	}
	verb, target := parts[0], parts[len(parts)-1]
	switch {
	case verb == "print" && len(parts) == 2:
		result.tab = 0
	case verb == "kickstart" && len(parts) == 2:
		result.verb = "start"
	case verb == "kickstart" && len(parts) == 3 && parts[1] == "-k":
		result.verb = "restart"
	case verb == "kill" && len(parts) == 3 && parts[1] == "SIGTERM":
		result.verb = "stop"
	case (verb == "enable" || verb == "disable") && len(parts) == 2:
		result.verb = verb
	default:
		return result, fmt.Errorf("supported launchctl commands: print, kickstart [-k], kill SIGTERM, enable, disable")
	}
	if label, ok := strings.CutPrefix(target, "system/"); ok {
		result.target = label
	} else if label, ok := strings.CutPrefix(target, launchDomain(true)+"/"); ok {
		result.user = true
		result.target = label
	} else {
		return result, fmt.Errorf("use system/LABEL or %s/LABEL; other users/domains are not selected implicitly", launchDomain(true))
	}
	_, err := launchTarget(result.user, result.target)
	return result, err
}
