# The Observatory theme family

[Back to Systemdoc](README.md) · [Configuration](docs/configuration.md)

Systemdoc's five themes share a restrained gothic identity: near-black architectural surfaces, bone-white text, silver context, and selective stained-glass colour. The eye mark represents observation; operational labels remain direct and modern.

![Five Observatory theme colour previews](docs/assets/themes.svg)

| Theme | Background | Surface | Accent → glow | Character |
| --- | --- | --- | --- | --- |
| **Cathedral** · default | `#070a10` | `#101722` | Cold cyan `#64ddea` → violet `#a889e8` | Moonlit stone and stained glass |
| **Reliquary** | `#0b0908` | `#191511` | Tarnished gold `#d4b777` → crimson `#e06a7d` | Warm metal and dark wood |
| **Nocturne** | `#0d0912` | `#1a1220` | Lavender `#b9a1e8` → cyan `#64ddea` | Violet night and cold light |
| **Crypt** | `#070b09` | `#101812` | Sage `#9bbd9f` → gold `#d4b777` | Moss, old stone, low light |
| **Blood Moon** | `#100709` | `#201014` | Crimson `#e06a7d` → gold `#d4b777` | Controlled drama for late-night work |

All themes share bone-white foreground `#e9e4da`, muted silver `#9aa3b1`, error `#e06a7d`, warning `#d4b777`, and success `#64d7a1`. Decorative glow remains separate from severity, so a theme never changes the meaning of a failure or warning.

## Nerd Font identity

The interface is designed on a patched monospace cell grid and enables Nerd Font icons by default. JetBrainsMono Nerd Font is the recommended face. A single semantic vocabulary covers the eye identity, five suites, navigation, status, filters, metrics, approvals, and destructive actions.

Icons always accompany text. Suite numbers and keyboard shortcuts remain visible, status is never communicated by glyph or colour alone, and Preferences can switch to labeled Unicode/ASCII fallbacks when a patched font is unavailable.

## Choose a theme

Press `t`, move through the list to preview the live workspace, then Enter to save. Escape restores the original theme. Settings are local to the machine and user running Systemdoc.

Legacy Deep theme names migrate to the nearest Observatory palette when read. Unknown names resolve to Cathedral. The settings file is not rewritten until a preference is saved, and existing accent/background overrides remain in effect.

## How the light works

The header and selected row blend accent toward glow. All blending is done in CIE Lab, so the gradient steps evenly in perceived colour rather than in raw channel values. Open telemetry panels use semantic accent rails and tinted surfaces; the focused pane gets a brighter edge. The pointer, icon, and written label remain useful without colour. These are terminal-cell effects with no graphics protocol requirement.

A true-color terminal displays the intended gradients. Indexed terminals may quantize colours, so the exact result depends on the terminal and its configured font.
