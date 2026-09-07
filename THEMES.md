# The Deep theme family

[Back to Systemdoc](README.md) · [Configuration](docs/configuration.md)

Ten original Systemdoc themes, all built around Deep Navy's near-black surfaces and high-contrast text. The layout and lighting are shared; the accent changes the character of the workspace.

![Ten Deep theme colour previews](docs/assets/themes.svg)

| Theme | Background | Surface | Accent → glow |
| --- | --- | --- | --- |
| **Deep Navy** · default | `#070b14` | `#0e1626` | Cyan `#43e8ff` → pink `#ff5cac` |
| **Deep Violet** | `#0b0914` | `#171226` | Violet `#b99aff` → ice `#5de6ff` |
| **Deep Teal** | `#060e12` | `#0c1b23` | Mint `#44f2c4` → blue `#62a8ff` |
| **Deep Ember** | `#100b0a` | `#211613` | Amber `#ffbb66` → rose `#ff6b9c` |
| **Deep Rose** | `#100a12` | `#211322` | Pink `#ff83c7` → lilac `#b59aff` |
| **Deep Obsidian** | `#090b0e` | `#14181e` | Silver `#b4becd` → steel `#889bb8` |
| **Deep Forest** | `#080d0a` | `#121c16` | Sage `#a3c49b` → faded teal `#75b8ac` |
| **Deep Aubergine** | `#100b12` | `#201624` | Dusty mauve `#c3a1bc` → lavender `#999dc8` |
| **Deep Copper** | `#100c09` | `#211913` | Copper `#cca789` → muted rose `#b98f96` |
| **Deep Midnight** | `#080b13` | `#111a2a` | Moonlit blue `#94add8` → slate violet `#9991bf` |

The five quieter additions use softer, less saturated accents: Obsidian for charcoal and silver, Forest for mossy greens, Aubergine for smoky plum, Copper for warm brown, and Midnight for cool ink blue. They keep the same readable text and state colours.

All themes share foreground `#edf5ff`, muted text `#8c9db8`, error `#ff5cac`, warning `#ffce70`, and success `#53f5af`. Decorative glow is a separate token from error severity.

## Choose a theme

Press `t`, move through the list to preview the live workspace, then Enter to save. Escape restores your original theme. Settings are local to the machine and user running Systemdoc.

The former third-party and light presets have been removed. A saved name that is no longer available falls back to Deep Navy when settings are read. The file is not rewritten until you save a preference. Existing accent/background overrides remain in effect; clear those under Actions → Preferences to see a preset as designed.

## How the light works

The header and selected row blend accent toward glow. Open telemetry panels use semantic accent rails and tinted surfaces; the focused pane gets a brighter edge. The pointer and text labels remain useful without colour. These are terminal-cell effects, with no animation loop or graphics protocol requirement.

A true-color terminal displays the intended gradients. Indexed terminals may quantize colours, so the exact result depends on the terminal. Standard Unicode fonts work; Nerd Font icons are optional.
