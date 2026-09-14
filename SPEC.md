# Systemdoc architecture

This document describes the implemented application. [The user guide](docs/usage.md) is the feature reference; [known limits](docs/troubleshooting.md#current-boundaries) separate current behaviour from possible future work.

## Scope

A Linux and macOS terminal workspace with two modes: native services (systemd on Linux, launchd on macOS) and containers. The container mode merges Docker containers with the pods of one detected Kubernetes stack on the same node (k0s, k3s, kind, k3d, minikube, microk8s or any reachable kubectl context). Compose project workflows live within the container mode. Optional AI assistance is contextual. SSH launches the full application on the target host rather than aggregating machines into a central dashboard.

## Components

| Area | Implementation |
| --- | --- |
| Entry point | `cmd/systemdoc`; CLI flags and SSH dispatch |
| Interface | Go, tview, tcell; one application loop and widget ownership on the UI thread |
| Theme system | Five application-owned Observatory palettes, Nerd Font-first semantic icons with readable fallbacks, shared status colours, separate decorative glow, block/braille/ASCII signal renderers |
| Systemd | `systemctl`, `journalctl`, `systemd-analyze`; system/user scope is explicit |
| Launchd | macOS `launchctl print` scoped jobs, `ps` process accounting, plist inspection, PID-scoped unified logs, reviewed lifecycle commands |
| Docker | Installed CLI, pinned endpoint, `ps`, `stats`, container-typed `inspect`, streaming logs |
| Docker overview | Typed projection of inspect JSON into identity, ports, mounts, networks, runtime, labels; raw JSON remains available |
| Kubernetes | Detected kubectl invocation (`kubectl`, `k0s kubectl`, `k3s kubectl`, `microk8s kubectl`, kind contexts, or `--kubectl`/`SYSTEMDOC_KUBECTL`); node list names the distribution; `get pods -A -o json`, `top pods`, pod JSON/YAML, events, Services, prefixed multi-container logs; `rollout restart` and `delete pod` as reviewed actions |
| Host network | TCP/UDP socket ownership, interfaces, and successive-counter download/upload rates from `/proc/net/dev` or `netstat -ibn` |
| Compose | Installed `docker compose`; project files, profiles, environment files, workdir, endpoint |
| Settings | JSON under XDG config, owner-only atomic saves |
| Operations | Exact-target review, cancellable subprocesses, bounded output, session history |
| Remote | OpenSSH control connection, native authentication, optional matching static ELF upload |
| AI | Installed external CLIs; user-reviewed context and separately validated drafts |

Versions are pinned in [go.mod](go.mod). Release binaries use `CGO_ENABLED=0` for Linux amd64/arm64 and macOS arm64.

## Data flow and responsiveness

Systemd and container inventory jobs start independently. Within the container job, Docker and Kubernetes are listed concurrently and either alone is sufficient; a failed kubectl probe backs off (30 s doubling to 10 min) so a host with kubectl but no cluster is not asked on every refresh, and a stack that stops answering is dropped and re-probed. Identity/state inventory is published before slower resource enrichment. Each backend has one in-flight job; cached inventory makes mode switches immediate while stale data refreshes in the background. Changed scopes discard obsolete results.

The selected inspector has a bounded cache and a short selection debounce. Fleet and per-workload signal histories are bounded and reuse completed inventory samples. The Storyline reuses bounded change observations. Constellation invokes the existing relationship inspection only when opened; it adds no background loop. Context cancellation stops obsolete subprocess work. Log streams retain bounded tail buffers, format snapshots away from the UI thread, and show a 500-line live window. The full retained buffer is available in history/export. Refresh interval is configurable from 2 to 300 seconds.

## Data contracts

Unknown values stay unknown. CPU samples come from actual counters, and resets do not produce invented rates. Workload totals are not whole-host metrics. Aliases resolve to canonical services without inflating active counts. Installed files absent from the runtime snapshot are not guessed inactive. Templates require instances for runtime operations.

Pods carry a `pod:` ID prefix so every route distinguishes them from container IDs without a third mode. Pod state is reduced to the shared vocabulary (running, pending, restarting, failed, succeeded, terminating) with the Kubernetes reason kept in the detail; a running pod with a container that is not ready counts as attention. Pod CPU is a share of one logical CPU derived from Metrics API millicores and stays unknown without metrics-server. The Deployment owning a pod is recovered from the ReplicaSet name and pod-template hash. Environment values and annotation contents are not copied into the pod overview.

Docker overview derives from a single selected-container inspection. Port rendering retains protocol and every host binding, including IPv6; exposed-only and configured-only mappings are identified. Mounts retain type, source/name, destination, and access. Networks list observed endpoint attributes. Environment values remain in full inspect JSON. Labels and command arguments can still contain user-provided data.

## Action boundaries

Lifecycle operations review the exact target and command. Successful command completion does not prove workload health. Draft validation and installation are separate; new service installation does not enable or start it. Compose deployment is separate from source creation. Command input supports a defined argv vocabulary, not an arbitrary shell.

Pod restart is a controller rollout; a bare pod cannot be restarted and the request is refused with the reason. The process inherits native permissions; it does not reconfigure Docker socket access or read root-only kubeconfigs. Remote actions execute on the remote host. Settings and project definitions belong to the user running the application. Untrusted output is cleaned and escaped before rich-text rendering. See [security](SECURITY.md).

## Verification and distribution

`make check` runs installer tests, vet, and Go tests; `make test` adds race detection. Fixture-based terminal tests cover focus, resize, overlays, theme changes, and input handling. Docker parser tests cover readable inspection and raw JSON routing. Release scripts build both architectures with an embedded version and SHA-256 manifest. GitHub Actions creates a draft release for maintainer review.

See [development](docs/development.md) and [releasing](docs/releasing.md) for reproducible commands. Live integration and distro/terminal coverage remain explicit release checks, not inferred from passing unit tests.
