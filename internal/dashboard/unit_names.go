package dashboard

import "strings"

func matchesUnitName(item workload, name string) bool {
	if item.ID == name || item.Name == name {
		return true
	}
	for _, alias := range strings.Fields(item.Aliases) {
		if alias == name {
			return true
		}
	}
	return false
}

func isTemplate(item workload) bool { return strings.HasSuffix(item.ID, "@.service") }
