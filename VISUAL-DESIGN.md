# Systemdoc visual system

The implemented direction is a gothic system observatory: watchful, architectural, restrained, and operationally clear. [The theme catalogue](THEMES.md) defines the five Observatory palettes. This document supersedes the earlier visual explorations; unimplemented mockups are not release promises.

## Shared foundation

All presets derive from Cathedral: near-black background, a slightly raised surface, bone-white primary text, readable silver labels, and restrained stained-glass accents. Error, warning, and success retain one meaning across themes. Decorative glow has its own token, so a colour variant does not redefine failure.

The eye is the primary identity mark. In the terminal it uses the Nerd Fonts Font Awesome eye glyph and appears only at identity and observation points: the header, splash, Control Deck, workload lens, and high-trust dialogs. It is not repeated as decoration.

The header and selected row blend accent toward glow. Telemetry has open panels with fading top rails and a light surface tint. Focused panes retain brighter rounded borders. Other borders recede. There is no zebra striping. A muted, single-glyph footer spinner animates only during current-backend inventory work.

Lighting is drawn with tcell colours inside the owning panel's rectangle. Content and empty cells share the surface treatment. Selected-row styling, explicit log/match backgrounds, and overlays have separate handling. The main-page drawing wrapper completes before overlays draw. Geometry, mouse input, and focus remain owned by tview.

## Hierarchy

1. Header: one masthead shared by every page - identity line, host readout and glyph-mode hint, then a gradient rule - at one height rule, so moving between suites never shifts the top of the screen. The main dashboard adds health, sample time and its signature tools on the second line; compact terminals retain one identity row everywhere.
2. Suite/action rail: five numbered operational areas plus Control Deck, saved views, actions, themes and expansion when width permits.
3. Pulse telemetry: active, attention, host CPU and host memory become real bounded two-row area signals after their baseline sample and state the latest direction. The two resource cards read whole-machine utilisation on a fixed 0–100% scale and name the tracked workload sum beside it, so a host figure is never confused with the sum of observed workloads. The CPU card's title names what the share is a share of - the logical CPU count and current clock - so the rows stay for the trend. Where the terminal allows, the masthead repeats the host CPU and memory percentages and the same count and clock.
4. Inventory and Focus Lens: a gradient row and pointer identify selection; the inspector identity band includes selected-workload CPU/memory trails above its tabs.
5. Storyline: one live rail exposes the latest observed transitions and opens a bounded suite-wide incident history.
6. Host suites: Networking, Process Explorer and Disk & Storage each carry a signature hue from the decorative accent-to-glow range, their own view rail, a selection band named for what it shows, and inline severity-coloured meters in the columns that carry a percentage. Severity colours are never spent on panel identity.
7. Constellation: an on-demand map projects service dependencies or container connections around the selected workload without adding background polling.
8. Optional logs: a separate drawer supports troubleshooting without losing the inspected context.
9. Footer: short, discoverable keyboard hints and a two-cell loading indicator.

Docker Overview and Connections use titled, wrapping sections so long volume paths, port mappings, and network values remain readable. Full inspect JSON is a separate tab. Native systemd output retains its familiar structure.

## Responsive behaviour

Automatic layout shows side-by-side panes at 110 columns or wider. `[` / `]` adjust the inventory share from 30–70%; saved views restore that ratio. Tall narrow terminals stack the panes; compact terminals display the focused pane and hide the selected-workload band. Telemetry collapses when space is tight. `z` expands the focused pane and Escape restores it. The log drawer participates in the Tab focus cycle.

## Presentation contracts

- Data is observed, never decorative: missing samples stay missing and CPU/memory trends use real inventory history.
- One colour ramp for pressure, everywhere: any share of the machine - host CPU and memory in the masthead and cards, a workload's or process's CPU share, filesystem space and inodes - is coloured green below 70%, amber from 70% and red from 85%. Memory figures without an honest ceiling keep the identity hue rather than a graded one.
- Colours interpolate in CIE Lab (via go-colorful) through a single `blend` function, so every gradient, surface tint, selection row and frame highlight steps evenly in perceived colour and clamps back into the sRGB gamut.
- Blocks, braille and ASCII render the same samples. Glyph mode changes presentation, never measurement.
- Nerd Font icons are enabled by default and come from one semantic vocabulary. Every icon is paired with a written label; suite numbers and keyboard shortcuts remain visible. Unicode/ASCII fallback mode never changes meaning or layout hierarchy.
- Standard Unicode monospace fonts work. True-color terminals show the intended gradients; indexed palettes may quantize them.
- All themes are dark. Legacy Deep theme names migrate to the nearest Observatory palette; unknown names fall back to Cathedral and user overrides are retained.
- Escape cancels theme preview. Theme changes do not restart the inspector's backend work unnecessarily.
- External text is cleaned and escaped; it cannot inject tview markup.
- Action approvals use a bounded, theme-aware card over the live workspace, keeping the selected workload in context while the exact command or change remains scrollable.

## Visual review

The [development guide](docs/development.md) explains the terminal-cell fixture exporter. Review all themes at 160×44, 120×30, and 80×24, with both backends, long values, a log drawer, and an overlay. Tiny-screen tests also exercise geometry and selection. Fixture screenshots use the actual renderer and are labeled as illustrative data.

## Further exploration

The compact workspace, Pulse cards, Focus Lens, Storyline, Constellation and selectable signal glyphs are implemented. The remaining [visual roadmap](docs/visual-roadmap.md) scopes a timer timeline and integrated troubleshooting layout. Its [interactive concept](docs/assets/workspace-concept.html) uses illustrative data and remains a design reference rather than a product screenshot.
