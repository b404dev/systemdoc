package dashboard

// Every theme shares Deep Navy's near-black foundation and semantic colours.
// Glow is decorative; it must not change the meaning of errors or warnings.
type palette struct {
	name, background, surface, text, muted, accent string
	error, warning, success, glow                  string
}

func darkTheme(name, background, surface, accent, glow string) palette {
	return palette{
		name: name, background: background, surface: surface,
		text: "#edf5ff", muted: "#8c9db8", accent: accent, glow: glow,
		error: "#ff5cac", warning: "#ffce70", success: "#53f5af",
	}
}

var themes = []palette{
	darkTheme("Deep Navy", "#070b14", "#0e1626", "#43e8ff", "#ff5cac"),
	darkTheme("Deep Violet", "#0b0914", "#171226", "#b99aff", "#5de6ff"),
	darkTheme("Deep Teal", "#060e12", "#0c1b23", "#44f2c4", "#62a8ff"),
	darkTheme("Deep Ember", "#100b0a", "#211613", "#ffbb66", "#ff6b9c"),
	darkTheme("Deep Rose", "#100a12", "#211322", "#ff83c7", "#b59aff"),
	// Quieter accents for low-light workspaces; severity colours stay consistent.
	darkTheme("Deep Obsidian", "#090b0e", "#14181e", "#b4becd", "#889bb8"),
	darkTheme("Deep Forest", "#080d0a", "#121c16", "#a3c49b", "#75b8ac"),
	darkTheme("Deep Aubergine", "#100b12", "#201624", "#c3a1bc", "#999dc8"),
	darkTheme("Deep Copper", "#100c09", "#211913", "#cca789", "#b98f96"),
	darkTheme("Deep Midnight", "#080b13", "#111a2a", "#94add8", "#9991bf"),
}

func themeIndex(name string) int {
	for i, theme := range themes {
		if theme.name == name {
			return i
		}
	}
	return 0
}
