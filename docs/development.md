# Development

[Documentation index](README.md) · [Contributing](../CONTRIBUTING.md)

Use Go 1.23 or newer and Make. Runtime backends are optional for fixture tests. Clone the repository and run:

```sh
make build
./bin/systemdoc
make check
make test
make cross
```

`make check` validates shell syntax, exercises the installer offline, runs `go vet`, and runs Go tests. `make test` runs the Go suite with race detection. `make cross` builds Linux amd64/arm64 and macOS arm64 executables with CGO disabled. `make build VERSION=v0.1.0` embeds an explicit version; ordinary local builds report `dev`.

Tests use fake backend executables and tcell simulation screens, not hidden application demo modes. They cover state parsing, aliases/templates, metrics, cancellation, input/focus, theme rendering, logs, non-overwriting writes, Docker inspection, command validation, and SSH argument handling. Authenticated SSH, live providers, and real lifecycle effects need disposable integration environments.

## Draw-loop benchmarks

The dashboard redraws on a one-second tick, so per-frame cost is idle CPU for every user. `go test ./internal/dashboard -run '^$' -bench 'Blend|Frame' -benchmem` measures a colour blend and a full 160x44 frame for the main dashboard and a host panel. Anything in the per-cell path of `paintSurfaces` or the panel draw functions should be checked against these before and after.

## Visual fixtures

Export actual terminal cells, using illustrative workloads:

```sh
SYSTEMDOC_VISUAL_REVIEW=/tmp/systemdoc-cells.json SYSTEMDOC_VISUAL_THEME='Cathedral' go test ./internal/dashboard -run TestVisualReview -count=1
python3 scripts/render-preview.py /tmp/systemdoc-cells.json 120x30 /tmp/systemdoc-preview.svg
```

For documentation images, capture the real binary instead of fixtures: `make build && scripts/capture-panels.sh` drives `bin/systemdoc` inside a detached 160×44 tmux server with 24-bit colour, walks every panel (dashboard, containers with pods, Docker overview, pod connections, processes, Process Activity, sysdig palette and stream, network speed, storage, constellation, Control Deck), converts each `tmux capture-pane -e` dump with `scripts/ansi-to-cells.py` into the same cell JSON, and renders PNGs into `docs/assets/` through `render-preview.py` and `rsvg-convert`. Run it with the k0s playground up (`playground/k0s.sh up`) so the container captures show a stack. It needs tmux, python3, rsvg-convert and JetBrainsMono Nerd Font.

Set `SYSTEMDOC_VISUAL_MODE=docker` for the container fixture or `approval` for the action card. The exporter includes 160×44, 120×30, and 80×24 frames. Python's standard library is sufficient to write the SVG. JetBrainsMono Nerd Font is required to review glyph spacing accurately. For README PNGs, convert the SVG with `rsvg-convert` from librsvg; this is a documentation-only dependency.

Review all themes and both modes. Screenshots published in documentation must say that workloads and measurements are fixtures. Never include credentials or real sensitive host configuration.

## Build layout

- `cmd/systemdoc`: flags, version, local/remote dispatch.
- `internal/dashboard`: UI, systemd/launchd/Docker backends, workflows, state, themes.
- `internal/remote`: OpenSSH launch, keys, compatibility checks.
- `scripts`: installer tests, release build, preview rendering.
- `docs`: user and maintainer guides, generated visual assets.
- `playground`: optional real Docker containers.

See [architecture](../SPEC.md) for data flow and [the release guide](releasing.md) for packaging.

The existing fake-systemd suite pins its service platform before workers start, including on macOS CI. Launchd adapters are tested directly, and Darwin routing runs in an isolated test process. On a Mac, `SYSTEMDOC_LIVE_LAUNCHD=1 go test ./internal/dashboard -run TestLiveLaunchdReadOnly` exercises read-only system-domain inventory.
