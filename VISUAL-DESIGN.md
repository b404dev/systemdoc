# Systemdoc visual system

The implemented direction is a dark control console with sharp text, selective lighting, and clear workload context. [The theme catalogue](THEMES.md) defines the five palettes. This document supersedes the earlier visual explorations; unimplemented mockups are not release promises.

## Shared foundation

All presets derive from Deep Navy: near-black background, a slightly raised surface, bright primary text, readable muted labels, and luminous accents. Error, warning, and success retain one meaning across themes. Decorative glow has its own token, so a colour variant does not redefine failure.

The header and selected row blend accent toward glow. Telemetry has open panels with fading top rails and a light surface tint. Focused panes retain brighter rounded borders. Other borders recede. There is no zebra striping. A muted, single-glyph footer spinner animates only during current-backend inventory work.

Lighting is drawn with tcell colours inside the owning panel's rectangle. Content and empty cells share the surface treatment. Selected-row styling, explicit log/match backgrounds, and overlays have separate handling. The main-page drawing wrapper completes before overlays draw. Geometry, mouse input, and focus remain owned by tview.

## Hierarchy

1. Header: Systemdoc, workload control, host/scope; a taller header appears when space allows.
2. Mode/action bar: services, containers, filters, actions, themes, expansion.
3. Telemetry: active and attention counts, reporting workload CPU/memory, real sample trends.
4. Inventory and inspector: a gradient row and pointer identify selection; the inspector keeps workload identity above tabs.
5. Optional logs and activity: a separate drawer supports troubleshooting without losing the inspected context.
6. Footer: short, discoverable keyboard hints and a two-cell loading indicator; no full-width live/activity bar.

Docker Overview and Connections use titled, wrapping sections so long volume paths, port mappings, and network values remain readable. Full inspect JSON is a separate tab. Native systemd output retains its familiar structure.

## Responsive behaviour

Automatic layout shows side-by-side panes at 110 columns or wider. Tall narrow terminals stack them; compact terminals display the focused pane. Telemetry collapses when space is tight. `z` expands the focused pane and Escape restores it. The log drawer participates in the Tab focus cycle.

## Presentation contracts

- Data is observed, never decorative: missing samples stay missing and CPU/memory trends use real inventory history.
- Labels and the selected pointer supplement colour. Optional Nerd Font icons never replace mode names.
- Standard Unicode monospace fonts work. True-color terminals show the intended gradients; indexed palettes may quantize them.
- All themes are dark. Removed palette names fall back to Deep Navy; user overrides are retained.
- Escape cancels theme preview. Theme changes do not restart the inspector's backend work unnecessarily.
- External text is cleaned and escaped; it cannot inject tview markup.

## Visual review

The [development guide](docs/development.md) explains the terminal-cell fixture exporter. Review all themes at 160×44, 120×30, and 80×24, with both backends, long values, a log drawer, and an overlay. Tiny-screen tests also exercise geometry and selection. Fixture screenshots use the actual renderer and are labeled as illustrative data.

## Proposed next changes

The [visual roadmap](docs/visual-roadmap.md) scopes a compact workspace, timer timeline and troubleshooting layout. Its [interactive concept](docs/assets/workspace-concept.html) uses illustrative data and is a proposal, not the implemented terminal UI.
