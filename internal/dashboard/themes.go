package dashboard

// The Observatory family shares one near-black, bone-white foundation.
// Glow is decorative; it must not change the meaning of errors or warnings.
type palette struct {
	name, background, surface, text, muted, accent string
	error, warning, success, glow                  string
}

func darkTheme(name, background, surface, accent, glow string) palette {
	return palette{
		name: name, background: background, surface: surface,
		text: "#e9e4da", muted: "#9aa3b1", accent: accent, glow: glow,
		error: "#e06a7d", warning: "#d4b777", success: "#64d7a1",
	}
}

var themes = []palette{
	darkTheme("Cathedral", "#070a10", "#101722", "#64ddea", "#a889e8"),
	darkTheme("Reliquary", "#0b0908", "#191511", "#d4b777", "#e06a7d"),
	darkTheme("Nocturne", "#0d0912", "#1a1220", "#b9a1e8", "#64ddea"),
	darkTheme("Crypt", "#070b09", "#101812", "#9bbd9f", "#d4b777"),
	darkTheme("Blood Moon", "#100709", "#201014", "#e06a7d", "#d4b777"),
}

func themeIndex(name string) int {
	// Preserve old preferences while moving the product to the smaller,
	// purpose-built Observatory family. Reading settings remains non-mutating.
	aliases := map[string]string{
		"Deep Navy":      "Cathedral",
		"Deep Violet":    "Nocturne",
		"Deep Teal":      "Crypt",
		"Deep Ember":     "Reliquary",
		"Deep Rose":      "Blood Moon",
		"Deep Obsidian":  "Cathedral",
		"Deep Forest":    "Crypt",
		"Deep Aubergine": "Nocturne",
		"Deep Copper":    "Reliquary",
		"Deep Midnight":  "Nocturne",
	}
	if migrated, ok := aliases[name]; ok {
		name = migrated
	}
	for i, theme := range themes {
		if theme.name == name {
			return i
		}
	}
	return 0
}
