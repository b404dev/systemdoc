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

Compose needs the `docker compose` plugin. Registered projects belong to their recorded endpoint. Source-dependent actions need the original Compose files and working directory. A stopped container can have configured port mappings without a live binding; the overview labels that distinction.

## Resources show dashes or unexpected totals

A dash means unavailable, not zero. Systemd accounting must expose the corresponding counters. CPU needs consecutive samples and uses 100% for one logical CPU, so totals may exceed 100%. Telemetry sums reporting workloads, not the host. Trends use up to 32 inventory samples and an automatic scale.

## The layout or colours look wrong

Start with a true-color terminal, JetBrainsMono Nerd Font, and Cathedral (`t`). Clear accent/background overrides under Preferences. Legacy Deep theme names migrate to the nearest Observatory palette. If icons appear as boxes, either configure the patched font in the terminal displaying Systemdoc—including the local terminal for SSH—or disable the Nerd Font interface under Preferences for labeled fallbacks.

At 110 columns or more, automatic layout shows list and inspector side by side. Compact terminals show the focused pane; Enter/Tab reaches the inspector. `z` expands, Escape restores. `L` opens the independent log drawer. Resize for more telemetry and content.

## Logs stop following or seem shorter than expected

Space and upward scrolling pause follow; `g` resumes. Each stream retains at most 1 MiB and the live display shows the newest 500 matching lines. `h` opens full retained history. `e` exports retained logs for the focused log pane. An application restart discards session buffers and activity history.

## Remote upload fails

`--upload` requires a matching static Linux ELF or macOS Mach-O executable. A `/tmp` mounted with `noexec` prevents execution: install remotely in an executable location and use `--remote-bin`. Host-key and authentication prompts belong to OpenSSH. See [remote hosts](remote.md).

## Current boundaries

Inventory uses CLI adapters and polling, not direct D-Bus or Docker Engine event subscriptions. Polling can miss short-lived transitions. Activity and operation history are bounded and session-local. There is no batch management, persistent audit database, advanced chart cursor, central multi-host dashboard, automatic reconnect, socket inventory, or built-in existing-file diff editor. Timer browsing lists loaded timers and requires JSON-capable `systemctl list-timers`; it does not include all installed timer files. Rich log search operates on retained buffers and does not fetch older logs. Docker Connections reports container attachments; it is not a Compose topology graph or proof of live traffic.

Automated tests use fixtures and fake executables. They do not establish compatibility with every terminal, distribution, live AI provider, or authenticated SSH host. Before a release, run the manual checks in [the release guide](releasing.md). Report problems with version, OS/architecture, terminal, backend versions, exact steps, and sanitized output.

## macOS service access

Use `launchctl print system` to check system scope, or `launchctl print gui/$(id -u)` for your logged-in GUI session. A missing GUI domain is reported rather than replaced with another scope. Native Apple permissions apply. Diagnostic output formats can change; an unrecognized services table is reported as unavailable. See [macOS support and limits](macos.md).
