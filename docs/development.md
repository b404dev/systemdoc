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

`make check` validates shell syntax, exercises the installer offline, runs `go vet`, and runs Go tests. `make test` runs the Go suite with race detection. `make cross` builds static Linux amd64 and arm64 executables. `make build VERSION=v0.1.0` embeds an explicit version; ordinary local builds report `dev`.

Tests use fake backend executables and tcell simulation screens, not hidden application demo modes. They cover state parsing, aliases/templates, metrics, cancellation, input/focus, theme rendering, logs, non-overwriting writes, Docker inspection, command validation, and SSH argument handling. Authenticated SSH, live providers, and real lifecycle effects need disposable integration environments.

## Visual fixtures

Export actual terminal cells, using illustrative workloads:

```sh
SYSTEMDOC_VISUAL_REVIEW=/tmp/systemdoc-cells.json SYSTEMDOC_VISUAL_THEME='Deep Navy' go test ./internal/dashboard -run TestVisualReview -count=1
python3 scripts/render-preview.py /tmp/systemdoc-cells.json 120x30 /tmp/systemdoc-preview.svg
```

Set `SYSTEMDOC_VISUAL_MODE=docker` for the container fixture. The exporter includes 160×44, 120×30, and 80×24 frames. Python's standard library is sufficient to write the SVG. A monospace font such as JetBrains Mono improves local rendering fidelity. For README PNGs, convert the SVG with `rsvg-convert` from librsvg; this is a documentation-only dependency.

Review all themes and both modes. Screenshots published in documentation must say that workloads and measurements are fixtures. Never include credentials or real sensitive host configuration.

## Build layout

- `cmd/systemdoc`: flags, version, local/remote dispatch.
- `internal/dashboard`: UI, backends, workflows, state, themes.
- `internal/remote`: OpenSSH launch, keys, compatibility checks.
- `scripts`: installer tests, release build, preview rendering.
- `docs`: user and maintainer guides, generated visual assets.
- `playground`: optional real Docker containers.

See [architecture](../SPEC.md) for data flow and [the release guide](releasing.md) for packaging.
