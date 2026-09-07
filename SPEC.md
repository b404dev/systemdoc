# Systemdoc architecture

This document describes the implemented application. [The user guide](docs/usage.md) is the feature reference; [known limits](docs/troubleshooting.md#current-boundaries) separate current behaviour from possible future work.

## Scope

A Linux terminal workspace with two modes: systemd services and Docker containers. Compose project workflows live within Docker. Optional AI assistance is contextual. SSH launches the full application on the target host rather than aggregating machines into a central dashboard.

## Components

| Area | Implementation |
| --- | --- |
| Entry point | `cmd/systemdoc`; CLI flags and SSH dispatch |
| Interface | Go, tview, tcell; one application loop and widget ownership on the UI thread |
| Theme system | Ten application-owned Deep palettes, shared semantic colours, separate decorative glow |
| Systemd | `systemctl`, `journalctl`, `systemd-analyze`; system/user scope is explicit |
| Docker | Installed CLI, pinned endpoint, `ps`, `stats`, container-typed `inspect`, streaming logs |
| Docker overview | Typed projection of inspect JSON into identity, ports, mounts, networks, runtime, labels; raw JSON remains available |
| Compose | Installed `docker compose`; project files, profiles, environment files, workdir, endpoint |
| Settings | JSON under XDG config, owner-only atomic saves |
| Operations | Exact-target review, cancellable subprocesses, bounded output, session history |
| Remote | OpenSSH control connection, native authentication, optional matching static ELF upload |
| AI | Installed external CLIs; user-reviewed context and separately validated drafts |

Versions are pinned in [go.mod](go.mod). Release binaries use `CGO_ENABLED=0` for Linux amd64 and arm64.

## Data flow and responsiveness

Systemd and Docker inventory jobs start independently. Identity/state inventory is published before slower resource enrichment. Each backend has one in-flight job; cached inventory makes mode switches immediate while stale data refreshes in the background. Changed scopes discard obsolete results.

The selected inspector has a bounded cache and a short selection debounce. Context cancellation stops obsolete subprocess work. Log streams retain bounded tail buffers, format snapshots away from the UI thread, and show a 500-line live window. The full retained buffer is available in history/export. Refresh interval is configurable from 2 to 300 seconds.

## Data contracts

Unknown values stay unknown. CPU samples come from actual counters, and resets do not produce invented rates. Workload totals are not whole-host metrics. Aliases resolve to canonical services without inflating active counts. Installed files absent from the runtime snapshot are not guessed inactive. Templates require instances for runtime operations.

Docker overview derives from a single selected-container inspection. Port rendering retains protocol and every host binding, including IPv6; exposed-only and configured-only mappings are identified. Mounts retain type, source/name, destination, and access. Networks list observed endpoint attributes. Environment values remain in full inspect JSON. Labels and command arguments can still contain user-provided data.

## Action boundaries

Lifecycle operations review the exact target and command. Successful command completion does not prove workload health. Draft validation and installation are separate; new service installation does not enable or start it. Compose deployment is separate from source creation. Command input supports a defined argv vocabulary, not an arbitrary shell.

The process inherits native permissions; it does not reconfigure Docker socket access. Remote actions execute on the remote host. Settings and project definitions belong to the user running the application. Untrusted output is cleaned and escaped before rich-text rendering. See [security](SECURITY.md).

## Verification and distribution

`make check` runs installer tests, vet, and Go tests; `make test` adds race detection. Fixture-based terminal tests cover focus, resize, overlays, theme changes, and input handling. Docker parser tests cover readable inspection and raw JSON routing. Release scripts build both architectures with an embedded version and SHA-256 manifest. GitHub Actions creates a draft release for maintainer review.

See [development](docs/development.md) and [releasing](docs/releasing.md) for reproducible commands. Live integration and distro/terminal coverage remain explicit release checks, not inferred from passing unit tests.
