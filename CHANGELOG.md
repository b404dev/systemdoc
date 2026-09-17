# Changelog

Changes are recorded here before being assigned a release tag.

## Unreleased

### Interface

- Telemetry trails floor every non-zero reading at one level, so a host running at a few percent shows a visible line on the fixed 0–100 scale instead of an empty chart.
- Table meters draw only their filled part: the dotted track that appeared on every row of a 500-process table was noise. Figures in RSS, PID, size, inode and throughput columns are right-aligned so they read as columns.
- Overlays and rails no longer cut text mid-word: the Control Deck and sysdig palette widen to the terminal and end long descriptions with an ellipsis, and the Storyline rail drops the oldest event rather than truncating the newest.
- Host page footers are one status line plus one key line, so every page spends the same rows at the bottom. The Processes card says how many processes were on a CPU at the sample instant instead of "running".
- Syslog-style timestamps (`Sep 14 16:17:12`) are dimmed like ISO stamps, so the journal tail inside a status block reads like the Logs tab.
- System Constellation is a relationship map rather than a list: dependencies are grouped by unit kind, failed and attention units come first, each dependency carries its own state from the current inventory, and every unit is shown (capped per group) instead of stopping at 23 lines.
- The sysdig live stream collapses consecutive identical events into one line with a count, dims event numbers and timestamps, colours enter and exit markers and error results, and exports the collapsed text.

### Fixes

- The host and network command runner now sets a wait delay, so an `lsof` helper child holding the pipe after a timeout can no longer leave a page's busy flag stuck and stop it refreshing for the rest of the session.
- Telemetry card trails were drawn two cells wider than the card's inner area, clipping the newest samples on the right; the selected-workload band drew its full 60-sample CPU and memory history into a band that fits about a quarter of it, so the memory trail was usually invisible. Both now fit their panels.
- Per-workload trend and metric histories, and per-interface throughput trails, are dropped when the workload or interface leaves the inventory. Container IDs, pod names and Docker `veth` names churn, and a long session kept a trail for every ID it had ever seen.
- Two data races: the Process Activity pause flag was read by the sampler goroutine while the key handler wrote it, and the Constellation goroutine read settings the preferences dialog can write.
- Edit unit override, Pod shell and Container shell now show the exact command on the approval card before suspending the workspace, like every other action; `systemctl edit` also gains the `--` separator and leading-dash guard the other systemctl verbs already had.
- The approval card accepts `y` only after it has been drawn once, so a key typed while a card is still being prepared cannot approve it unseen.
- The `lsof` scan behind Deleted but open has the same 15-second floor as the process snapshot; it was the heaviest command in the tool and the only host view polling at the base interval.
- sysdig probes run in their own process group and are interrupted, then reaped, as a group, so a root `sysdig` that misses the interrupt cannot outlive the panel that started it.
- The `r` inspector tab is named Metrics everywhere; the Delete key and the Network page's `l` / `c` / `i` view keys are documented; the README no longer says resource totals ignore whole-host usage.
- CI no longer runs the test suite twice per platform. It gains gofmt and staticcheck, a govulncheck pass, a one-iteration benchmark smoke and a build against the Go 1.23 floor declared in go.mod.
- Blocky gradients over SSH. sshd forwards `TERM` but not `COLORTERM`, so a 24-bit terminal looked like 256 colours on the remote host and every blend snapped to palette blocks. `--ssh` now forwards the local terminal's colour capability as `COLORTERM=truecolor`. When a terminal really is limited to 256 colours, panels take a single flat tint instead of a gradient and the status line says `256 colours`; the troubleshooting guide covers manual SSH sessions and tmux.

## v0.1.1 - 2026-09-14

### Fixes

- Subprocess output is no longer merged with stderr, so a `WARNING:` line from docker or kubectl on a successful call cannot break JSON decoding of Compose projects, Kubernetes nodes or pods. Failures still report both streams.
- Cancelled or timed-out commands can no longer wedge the caller when a grandchild keeps the pipe open: bounded commands, Operations, log following and the AI provider now set a wait delay, and log following interrupts rather than kills so `sudo … kubectl logs -f` wrappers stop cleanly.
- Quitting while a sysdig probe is live now waits for the probe to stop, and the container runner interrupts the named container before the CLI, so a privileged `sysdig/sysdig` container is not left running.
- `:docker …` and `:systemd …` commands that change suite keep the loading flag and refresh schedule consistent with the new suite, so an in-flight job for the previous suite can no longer leave auto-refresh paused. Installing a service draft reloads the inventory for the new scope.
- Host CPU% skips the guest and guest_nice columns of `/proc/stat`, which the kernel already counts inside user and nice; VM hosts were reporting inflated busy time.
- launchd enablement accepts the `=> disabled` / `=> enabled` spelling newer launchctl releases print, and an unrecognised row no longer discards the whole table.
- Time-bounded log search now finds the timestamp behind kubectl's `[pod/…]` prefix and parses launchd's `YYYY-MM-DD HH:MM:SS.ffffff±ZZZZ` stamps, so pod and macOS lines are no longer dropped from bounded searches.
- A kubectl JSON read that exceeds its output cap reports the cap instead of a bare decode error.
- The SSH binary upload has its own 15-minute deadline (`SYSTEMDOC_UPLOAD_TIMEOUT` overrides) instead of the two-minute check timeout that killed transfers on slow links.
- Removed dead code and resolved staticcheck findings; socket state filtering compares case-insensitively without allocating.

## v0.1.0 - 2026-09-14

First public release.

### Added

- Documentation images are now captured from the real binary against live data. `scripts/capture-panels.sh` drives `bin/systemdoc` in a detached 24-bit-colour tmux server, walks every panel, and renders PNGs through `scripts/ansi-to-cells.py` and the existing SVG path; the k0s playground gained DaemonSet, StatefulSet-with-unbound-claim, OOM-killed, unschedulable and flapping-liveness workloads so the captures show every state the suite distinguishes.

- Reviewed sysdig tracing. `T` on a process, or **Trace with sysdig** on a container or pod, opens a palette of probes scoped to that exact target: live syscalls, descriptor I/O, stdout and stderr streams; 15-second summaries of failed syscalls, top syscalls by count and time, slow syscalls and slow file I/O, top files, file errors and top connections; and a 30-second compressed capture for offline analysis. Each probe shows its `sudo sysdig …` command before running and picks the CO-RE BPF driver on kernels 5.8+ (`SYSTEMDOC_SYSDIG_ENGINE` overrides). Nothing suspends the interface: live probes stream into a panel with pause, follow and export, and Escape stops sysdig with a relayed interrupt so the capture ends cleanly; timed probes collect under a cancellable overlay and export through the review flow; when sudo has no cached authorization a masked in-workspace field asks once and passes the password to `sudo -S -v` on stdin without storing it. When the installed sysdig is missing or its libraries fail to load (a frozen package set can ship one linked against another abseil or protobuf), Systemdoc runs the official `sysdig/sysdig` image through Docker instead, with the host's `/proc`, `/dev` and `/etc` and the BPF probe (no kernel module), and keeps only the final frame of top-style chisel output. A missing sysdig is reported with the install command for the distribution.

- Process Activity. Enter on a process in Process Explorer now opens a live two-second view of that PID from `/proc`: state in words, user/system CPU with a trend, RSS and swap, page-fault rates, voluntary versus preempted context switches, storage and syscall I/O rates, open descriptors by kind with deleted-but-open files flagged, service unit, executable, working directory, children, a busiest-first thread table with per-thread CPU and state, and the newest journal lines for the PID. Counters another user's process does not expose are named as not permitted rather than shown as zero; the view stops and says so when the process exits or the PID is reused. Space pauses, `f` streams the PID's journal in a panel without leaving the workspace, and `K`, `n`, `s` route to signals, ports and the service. The Actions entries for following a journal or container log now open the live Logs tab instead of suspending the interface.

- Single-node Kubernetes in the Containers suite. When k0s, k3s, kind, k3d, minikube, microk8s, Docker Desktop or any reachable `kubectl` context is detected, its pods join the container list as `namespace/name` rows with the shared state vocabulary (running, pending, restarting for `CrashLoopBackOff`, failed for image and configuration errors, succeeded, terminating), Metrics API CPU and memory, a pod overview with containers, volumes, conditions, newest-first events and labels, a Connections tab with addresses and the Services whose selectors match, prefixed multi-container logs, the YAML manifest, and reviewed `rollout restart` and `delete pod` actions. The masthead names the distribution, version and node count; kind, k3d and minikube node containers are grouped under their cluster. Detection tries `kubectl`, the embedded `k0s kubectl`, `k3s kubectl` and `microk8s kubectl`, and kind's contexts, and can be pinned with `--kubectl` or `SYSTEMDOC_KUBECTL`. Docker alone, Kubernetes alone or both keep the suite usable; unreachable clusters back off rather than delaying every refresh.

- Whole-machine CPU and memory utilisation. The two resource telemetry cards now lead with host percentages on a fixed 0-100% scale and keep the tracked workload sum beside them, and the masthead repeats both figures where the terminal is wide enough. Linux reads `/proc/stat` and `/proc/meminfo`; macOS uses `vm_stat` with `sysctl` and per-process CPU shares. Unavailable readings stay visibly missing rather than reporting zero.

- Networking, Process Explorer and Disk & Storage are now visually distinct rather than the same table three times. Each carries a signature hue from the theme's decorative accent-to-glow range (severity colours keep their single meaning), a selection band named for what it shows - Socket exposure, Process vitals, Capacity, Open handle - and inline severity-coloured meters in percentage columns at 100 columns or wider.

- Process Explorer plots host CPU and memory trends on its cards, beside the summed `ps` snapshot figures, and shows the selected process's CPU and resident memory as gauges. Disk & Storage shows space and inode gauges for the selected filesystem. Networking leads its band with an exposure chip read from the bind address - loopback, single address, host-wide or an established flow - while keeping the caveat that a bind address is not a firewall rule.

- Process Explorer and Disk & Storage each gained a view rail naming their own two views - Flat/Tree and Filesystems/Deleted but open - so the two panels no longer present as the same table. `f`/`t` and `m`/`d` select a view directly; the original toggle keys still work.

- Pulse telemetry charts for health and resources, per-workload Focus Lens trails, an always-visible incident Storyline, an on-demand System Constellation relationship map, and persisted block/braille/ASCII signal graphics. All histories are bounded and reuse existing polls.

- A searchable Control Deck documents and opens five explicit operational suites: Services, Containers, Network, Processes, and Storage. It is available from the visible toolbar or with `0` across live pages.

- Process Explorer (`4`) with CPU/RSS sorting, parent trees, full command lines, filtering, and port/service navigation. Disk & Storage (`5`) with filesystem and inode pressure, deleted-open-file inspection, and reviewed exports. Both use the shared gradient dashboard surfaces and support Linux/macOS collectors.
- Process Explorer now offers a reviewed signal palette for terminate, interrupt, hangup, suspend, resume and force-kill. It protects PID 1 and Systemdoc, revalidates command identity before signalling, and uses existing user permissions without implicit elevation.

- Host Ports & Networking page (`3`) with TCP/UDP listeners, connections, process/PID ownership, interfaces, a live download/upload speed toggle, field filters, pause/refresh, and reviewed exports. Linux uses `ss` plus `/proc/net/dev`; macOS uses structured `lsof` plus `netstat -ibn` output.
- Reduced bursty CPU use: Network now collects only its active view, Speed reads counters without scanning sockets or processes, Linux socket ownership no longer invokes `ps`, and Process Explorer limits full snapshots to once every 15 seconds unless the configured interval is slower.

- Initial macOS launchd service backend: system/GUI scope, loaded-job states, process metrics, plist inspection, PID-scoped unified logs and reviewed lifecycle actions.
- Apple Silicon builds, macOS installer support, matching Mach-O SSH uploads, and a macOS CI job. Live Mac validation remains required.

- Named saved views with filters, sorting, scope, layout, inspector tab and drawer state; Docker endpoint checks.
- Retained log search with regex, inclusive time bounds, merged context and next/previous matches.
- Read-only systemd timer browser with next/last triggers and timer/activated-unit inspection.
- Editable troubleshooting snapshot reports with optional logs/configuration and private, non-overwriting export.
- Visual roadmap and an interactive browser concept for compact workspace, timer timeline and troubleshooting layouts.

### Fixes

- Idle CPU was climbing because colour interpolation moved to CIE Lab, which costs about a microsecond per call and runs for every cell of every frame (a 160x44 frame went from roughly 4 ms to 19-36 ms). Blends are now memoised per colour pair with the amount quantised to 256 steps, `tcell.GetColor` string parsing is hoisted out of the per-cell surface loop, and the host utilisation sampler and the per-page poll tickers apply state without forcing extra frames - the screen is drawn by the existing one-second dashboard tick. Frame and blend benchmarks were added as a regression guard.

- Workspace rail buttons (Deck, Views, Actions, Themes, Expand) were sized to their exact labels, so adjacent controls ran together as `Viewsa`, `Actionst` and `Themesz`, and `Expand` could be clipped at the right edge. Button widths now derive from the rendered label.

- Background inventory refresh no longer resets focus from the inspector, log drawer, search field, or an active overlay.
- Slow inspector responses preserve the latest reading position instead of restoring the scroll offset from the start of the request.

### Interface

- Colour interpolation now runs through CIE Lab using go-colorful (already present as an indirect dependency, now direct) via the single `blend` function, so every gradient, surface tint, selection row and frame highlight steps evenly in perceived colour. One severity ramp - green below 70%, amber from 70%, red from 85% - now colours every share-of-machine figure across the application: the masthead readout, the host cards, workload and process CPU, and filesystem space and inodes. The host memory card's rail moved from the warning colour to a decorative hue so severity colours keep a single meaning.

- The masthead and suite rail are now uniform across every page. Networking, Process Explorer and Disk & Storage previously collapsed to a one-line header without the gradient rule and with different wording, and their suite rail had no controls on the right; all pages now share one height rule, one first line, the host readout and glyph-mode hint on the second line, the rule beneath, and a Control Deck button on the rail.

- The splash carries a large block-letter wordmark; the masthead keeps a compact one so no working rows are lost. The interface no longer calls itself a "gothic" observatory.

- Reframed the visual system as a restrained gothic observatory: an eye-led identity, a centralized Nerd Font vocabulary with icon-plus-text labels, and five cohesive Cathedral, Reliquary, Nocturne, Crypt, and Blood Moon palettes. Legacy Deep theme preferences migrate without rewriting their files.

- Command and change approvals now appear in a compact, theme-aware action card over the live workspace instead of replacing the entire screen.

- Responsive workspace overhaul: a wide live-operations masthead, genuine two-row Pulse area charts, a compact one-row identity fallback, denser selected-workload bands, visible Saved Views and Actions, and uppercase numbered suite navigation.
- Adjustable 30–70% inventory/inspector split with `[` / `]`, preference persistence, and named-view restoration.

- Added a themed startup splash and an always-visible polling control. The 2–300 second interval can be changed from any live page and now updates open Network, Process, and Storage pollers immediately.

- Replaced the three-row live/activity bar with a muted bottom-right refresh spinner; sample timestamps stay fixed between refreshes and activity remains available with `v`.
- Independent decorative glow colours, shared severity colours, gradient header/selection, open telemetry rails, and dark continuous inventory surfaces.
- Docker Overview now includes identity, image, health, restart policy, ports, volumes/bind mounts, networks, runtime settings, limits, and labels. Connections focuses on attachments; raw inspect JSON remains available.

### Distribution and documentation

- Linux amd64/arm64 static release builds with embedded version and SHA-256 checksums.
- User-local curl installer with pinned-version support and atomic replacement after verification.
- CI, draft-release automation, and offline installer tests.
- Public-facing README, installation, usage, configuration, SSH, troubleshooting, development, release, contribution, and security guides.

### Existing functionality included in the first release

- Systemd and Docker inventory, accounting, logs, inspection, filters, favourites, and reviewed lifecycle actions.
- Compose project registration and workflows, service drafts, SSH launching/upload, and optional external AI assistance.
