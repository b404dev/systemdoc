# Working in Systemdoc

This is a Go terminal application. Start with the files for the feature being
changed; the dashboard package is large, but a task rarely needs all of it.
Use `rg` to follow specific symbols and read individual functions. Avoid dumping
the entire package, all tests, screenshots, or the changelog into context.

## Where to look

All paths below are relative to `internal/dashboard/` unless stated otherwise.

| Task | Starting points |
| --- | --- |
| CLI flags and startup | `cmd/systemdoc/main.go` (repository root), `ui.go` |
| Inventory and refresh | `data.go`, `refresh.go`, `detail_cache.go`, `metrics.go` |
| Navigation and layout | `dashboard.go`, `ui.go`, `control_deck.go`, `chooser.go` |
| Service backends | `data.go`, `unit_names.go`, `service_states_test.go`, `launchd*.go` |
| Docker inspection | `internal/dockerinspect/` (repository root); `data.go` routes inspector tabs |
| Compose | `compose.go`, `compose_draft.go`, `data.go` |
| Kubernetes | `kube.go`, `kube_overview.go`, `kube*_test.go`, `testdata/kube-*.json` |
| Host processes and storage | `host_data.go`, `host_ui.go`, `host_mounts_*.go` |
| Host CPU and memory | `host_metrics.go`, `host_cpu*.go`, `host_cores_ui.go` |
| Network collection, parsing and filtering | `internal/hostnet/` (repository root) |
| Network UI | `network_ui.go`, `network_test.go` |
| Structured command capture | `internal/commandcapture/` (repository root) |
| Plain-text sanitization and formatting | `internal/textutil/` (repository root) |
| Process activity and tracing | `host_activity*.go`, `sysdig*.go` |
| Logs | `logs.go`, `log_*.go` |
| Reviewed actions and commands | `authorization.go`, `operations.go`, `commands.go`, `sudo.go` |
| Settings and themes | `settings.go`, `preferences.go`, `themes.go`, `style.go`, `icons.go` |
| Prometheus and the Metrics suite | `metrics_*.go`, `prometheus*.go`, `internal/promapi/` (repository root) |
| SSH | `remote.go`, `internal/remote/` (repository root) |

Read related `*_test.go` files as needed. `helpers_test.go` contains adapters
used only by tests; production uses staged inventory and tolerant parsers.
Platform-specific files use `_linux.go` / `_other.go` suffixes or build tags;
check both implementations when changing shared contracts.

The extracted packages do not import `dashboard`, tview or tcell. Keep that
boundary: collection and parsing belong outside the UI. Their tests run with
`go test ./internal/hostnet ./internal/dockerinspect ./internal/commandcapture ./internal/promapi`;
they do not require loading dashboard fixtures or starting terminal screens.

## Documentation and assets

Read `SPEC.md` for architecture or the relevant guide in `docs/` for behaviour.
`VISUAL-DESIGN.md` and `THEMES.md` matter for visual changes. `CHANGELOG.md`,
`docs/visual-roadmap.md`, and `docs/assets/` are not prerequisites for ordinary
code changes. Assets are documentation screenshots and design references.
`playground/` provides optional live Docker/Kubernetes environments.

## Checks and constraints

- Use `gofmt` on changed Go files and run focused tests while developing.
- Before finishing code changes, run `go test ./...`; use `make test` for race
  detection, `make check` for shell/installer/vet checks, and `make cross` when
  changing platform-dependent code. See `CONTRIBUTING.md` for the full workflow.
- Keep backend calls off the UI thread, preserve cancellation and explicit
  action review, and clean/escape external output.
- Do not delete code just because it lacks direct textual callers: tview uses
  interface methods, and some helpers are test-only or platform-specific.
