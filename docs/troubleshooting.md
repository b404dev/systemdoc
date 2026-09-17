# Troubleshooting and known limits

[Documentation index](README.md)

## The installer cannot find a release

The public repository must exist and have a published, non-draft release containing `systemdoc-linux-amd64`, `systemdoc-linux-arm64`, and `SHA256SUMS`. An unpublished first release cannot be installed with the one-liner. For a pinned version, check the exact `v…` tag and asset names. Network failures and checksum mismatches stop installation before the current binary is replaced.

If `systemdoc` is not found after installation, add the install directory to PATH or run `~/.local/bin/systemdoc` directly. Linux amd64/arm64 and macOS arm64 are build targets; Windows and 32-bit ARM are not. macOS support still requires live verification.

## Systemd or journal access fails

Try `systemctl list-units --type=service` and `journalctl -n 20` as the same user. Use `--user` or `u` for your user manager. Environments without a running systemd manager cannot provide systemd inventory. **Actions → System service authorization** opens the native authorization workflow for system operations.

An installed unit with no observed runtime state is `unknown`. A bare template (`worker@.service`) needs a named instance for runtime actions. Missing units and inactive units are distinct results.

## Docker is unavailable or points to the wrong host

Check `docker ps -a` and `docker context show`. `DOCKER_HOST`, `DOCKER_CONTEXT`, and `--docker-context` select the endpoint; Systemdoc pins the current CLI context at startup when neither environment variable is set. The application does not change Docker socket permissions or group membership.

## sysdig probes fail or ask for a driver

`sysdig: symbol lookup error: … libscap_engine_gvisor.so.0: undefined symbol: _ZN4absl…` means the sysdig libraries were compiled against a different abseil release than the one installed. Distributions that serve a frozen package database (Omarchy's stable mirror, for example) can list a `falcosecurity-libs` build that predates their own abseil bump; the fix is the rebuilt package set, for Arch from the [Arch Linux Archive](https://archive.archlinux.org/packages/) with `pacman -U`, choosing the `falcosecurity-libs`, `scap-dkms` and `sysdig` builds whose abseil matches `pacman -Q abseil-cpp`. `ldd -r /usr/bin/sysdig` must print no `undefined symbol` lines afterwards. When the installed binary does not load and Docker is usable, Systemdoc falls back to the official `sysdig/sysdig` container image automatically and says why in the review dialog; `SYSTEMDOC_SYSDIG=native` disables the fallback.

sysdig runs as root through `sudo`; a timed probe that reports "authorization expired" only needs to be run again so the terminal can ask for the password. On kernels 5.8 and newer Systemdoc passes `--modern-bpf`, which needs no kernel module but does need BTF (`CONFIG_DEBUG_INFO_BTF`, present on mainstream distributions). If that fails, set `SYSTEMDOC_SYSDIG_ENGINE=kmod` to use the `scap` kernel module (`scap-dkms` or `sysdig-dkms`, built against your running kernel) or `bpf` for the legacy probe. Pod filters depend on the container runtime labelling containers with `io.kubernetes.pod.name`; a cluster nested inside another container, such as the k0s playground, is seen by sysdig as that outer container, so trace the container instead. Tracing adds overhead to the traced workload, and live streams from a busy process are verbose; prefer the 15-second summaries first.

## Kubernetes pods are missing or the masthead says kubernetes unreachable

Systemdoc probes `kubectl get nodes` through `kubectl`, then `k0s kubectl`, `k3s kubectl` and `microk8s kubectl`, and asks `kind get clusters` for a context when plain `kubectl` cannot connect. k0s, k3s and microk8s keep their admin kubeconfig root-only, so a non-root session sees no pods from them unless you export a readable kubeconfig (`k0s kubeconfig admin`, `/etc/rancher/k3s/k3s.yaml`, `microk8s config`) or run Systemdoc with the permissions you use for `kubectl`. Pin an exact invocation with `--kubectl "kubectl --context kind-dev"` or `SYSTEMDOC_KUBECTL`. A failed probe backs off from 30 seconds towards 10 minutes; `r` still requests an immediate inventory, and the masthead reports `kubernetes unreachable` while a kubectl is installed but no cluster answers. Empty CPU and memory for pods mean the Metrics API is not installed (metrics-server, `k0s` metrics add-on, `minikube addons enable metrics-server`, `microk8s enable metrics-server`), not that usage is zero.

Compose needs the `docker compose` plugin. Registered projects belong to their recorded endpoint. Source-dependent actions need the original Compose files and working directory. A stopped container can have configured port mappings without a live binding; the overview labels that distinction.

## Resources show dashes or unexpected totals

A dash means unavailable, not zero. Systemd accounting must expose the corresponding counters. CPU needs consecutive samples and uses 100% for one logical CPU, so totals may exceed 100%. The tracked figure beside a host percentage sums reporting workloads only. When the accounting command itself fails, the summary line under the cards says `accounting unavailable` and names it (`systemctl show`, `docker stats` or `kubectl top`); fix the permission or the tool and the columns fill on the next poll. Trends use up to 32 inventory samples and an automatic scale.

## The layout or colours look wrong

**Gradients look blocky or banded, usually over SSH.** The terminal on the far side is being treated as 256 colours, so every smooth blend snaps to the nearest palette entry. sshd forwards `TERM` but not `COLORTERM`, and tmux and screen hide the outer terminal's capability. Systemdoc detects this, draws flat tints instead of gradients, and shows `256 colours` in the status line so you know why. To get 24-bit colour back:

- `systemdoc --ssh host` forwards your local terminal's capability automatically.
- For a manual SSH session, run `export COLORTERM=truecolor` on the remote shell, or add it to the remote `~/.bashrc`. `TCELL_TRUECOLOR=1 systemdoc` forces it for one run.
- Inside tmux, add `set -as terminal-features ',xterm-256color:RGB'` (tmux 3.2+) or `set -ga terminal-overrides ',xterm-256color:Tc'` to `~/.tmux.conf`.
- If the remote lacks terminfo for your terminal (Ghostty and Kitty on older Ubuntu, for example) and you have set `TERM=xterm-256color` to work around it, `COLORTERM=truecolor` restores full colour.

Start with a true-color terminal, JetBrainsMono Nerd Font, and Cathedral (`t`). Clear accent/background overrides under Preferences. Legacy Deep theme names migrate to the nearest Observatory palette. If icons appear as boxes, either configure the patched font in the terminal displaying Systemdoc—including the local terminal for SSH—or disable the Nerd Font interface under Preferences for labeled fallbacks.

At 110 columns or more, automatic layout shows list and inspector side by side. Compact terminals show the focused pane; Enter/Tab reaches the inspector. `z` expands, Escape restores. `L` opens the independent log drawer. Resize for more telemetry and content.

## Logs stop following or seem shorter than expected

Space and upward scrolling pause follow; `g` resumes. Each stream retains at most 1 MiB and the live display shows the newest 500 matching lines. `h` opens full retained history. `e` exports retained logs for the focused log pane. An application restart discards session buffers and activity history.

## Remote upload fails

`--upload` requires a matching static Linux ELF or macOS Mach-O executable. A `/tmp` mounted with `noexec` prevents execution: install remotely in an executable location and use `--remote-bin`. Host-key and authentication prompts belong to OpenSSH. See [remote hosts](remote.md).

## Current boundaries

Inventory uses CLI adapters and polling, not direct D-Bus or Docker Engine event subscriptions. Polling can miss short-lived transitions. Activity and operation history are bounded and session-local. There is no batch management, persistent audit database, advanced chart cursor, central multi-host dashboard, automatic reconnect, socket inventory, or built-in existing-file diff editor. Timer browsing lists loaded timers and requires JSON-capable `systemctl list-timers`; it does not include all installed timer files. Rich log search operates on retained buffers and does not fetch older logs. Docker Connections reports container attachments; it is not a Compose topology graph or proof of live traffic. Pod Connections lists Services whose selectors match the pod's labels; that is a routing rule, not proof of traffic, and Ingress, NetworkPolicy and multi-cluster views are not included. Kubernetes support targets one stack per node; several clusters are shown only through the current kubectl context or `--kubectl`.

Automated tests use fixtures and fake executables. They do not establish compatibility with every terminal, distribution, live AI provider, or authenticated SSH host. Before a release, run the manual checks in [the release guide](releasing.md). Report problems with version, OS/architecture, terminal, backend versions, exact steps, and sanitized output.

## macOS service access

Use `launchctl print system` to check system scope, or `launchctl print gui/$(id -u)` for your logged-in GUI session. A missing GUI domain is reported rather than replaced with another scope. Native Apple permissions apply. Diagnostic output formats can change; an unrecognized services table is reported as unavailable. See [macOS support and limits](macos.md).
