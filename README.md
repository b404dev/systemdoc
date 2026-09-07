# Systemdoc

**Your Linux services and Docker containers, in one terminal workspace.**

Find a failing workload, follow its logs, inspect its configuration, and take action without juggling terminals. Systemdoc brings live telemetry, keyboard navigation, and a dark, glowing interface to the native tools you already use.

![Systemdoc's Deep Navy dashboard with service inventory, telemetry, and inspector](docs/assets/dashboard.png)

*Representative fixture rendered by the actual TUI. Workloads and measurements are illustrative.*

## Install

Linux **x86_64** and **arm64**. No Go installation or sudo required.

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/systemdoc/main/install.sh | sh
```

The installer downloads the latest published release, verifies its SHA-256 checksum, and installs `~/.local/bin/systemdoc`. Add that directory to your `PATH` if needed. Run the same command to update.

> Release preparation: the public repository URL and first published release must be in place before this command is live. See [the release guide](docs/releasing.md).

Prefer to inspect the script first, pin a version, install elsewhere, or build from source? See [installation](docs/installation.md).

## Start here

```sh
systemdoc                       # system services
systemdoc --user                 # your user services
systemdoc --docker               # Docker containers
systemdoc --filter state:failed  # focus on failures
```

- **Understand the workload.** Systemd state, boot enablement, configuration, dependencies, and accounting. Docker identity, image, health, ports, volumes, bind mounts, networks, runtime settings, and labels.
- **Follow what changes.** CPU/memory trends, session activity, streaming logs, a separate log drawer, retained log history, and export.
- **Act deliberately.** Searchable actions, exact-target lifecycle review, service drafts, and Compose project workflows.
- **Keep your context.** System/user scope, Docker contexts, favourites, filters, and SSH sessions running the full application remotely.
- **Make it yours.** Ten Deep themes built for near-black surfaces, luminous edges, and gradients. Mouse support is optional; the workspace is keyboard navigable.

| Key | Use it for |
| --- | --- |
| `1` / `2` | Services / containers |
| `/` | Filter the inventory |
| `Enter` / `Tab` | Inspect / move between panes |
| `o` / `l` / `c` / `r` / `d` | Overview / logs / configuration / metrics / dependencies or connections |
| `L` / `z` | Open log drawer / expand focused pane |
| `a` / `R` | Actions / review restart |
| `V` / `T` / `E` | Saved views / systemd timers / troubleshooting snapshot |
| `t` / `?` / `q` | Themes / help / quit |

[Full keymap and workflows →](docs/usage.md)

## Built for the dark

Deep Navy is the default. Nine alternatives share its contrast and layout, including the quieter Obsidian, Forest, Aubergine, Copper, and Midnight palettes. Error, warning, and success colours stay consistent.

![Ten Deep theme previews](docs/assets/themes.svg)

Press `t` to preview; Enter saves and Escape restores. A standard Unicode monospace font works. Nerd Font icons are optional. A true-color terminal gives the intended gradient treatment. [Theme catalogue →](THEMES.md)

## What you need

| Feature | Runtime tools |
| --- | --- |
| System services | Linux with systemd and `systemctl`; `journalctl` for logs |
| Containers | Docker CLI and access to the chosen Docker daemon |
| Compose projects | Docker Compose plugin (`docker compose`) |
| Remote sessions | OpenSSH client; a remote binary or `--upload` |
| Optional AI help | An installed, separately authenticated Codex or Claude CLI |

An unavailable backend is shown in the UI; it does not disable the other mode. Run Systemdoc as your regular user. Native systemd, journal, Docker, and SSH permissions still apply.

## Documentation

| Guide | Contents |
| --- | --- |
| [Installation](docs/installation.md) | One-line install, updates, pinned versions, source builds, uninstall |
| [User guide](docs/usage.md) | Navigation, logs, Docker overview, actions, Compose, AI assistance |
| [Configuration](docs/configuration.md) | Settings, themes, overrides, layouts, and migration |
| [Remote hosts](docs/remote.md) | SSH login, key setup, Docker contexts, temporary upload |
| [Troubleshooting](docs/troubleshooting.md) | Backend access, missing metrics, terminal display, installer failures |
| [Contributing](CONTRIBUTING.md) | Local checks, fixtures, and change workflow |
| [Release guide](docs/releasing.md) | GitHub setup, static binaries, checksums, draft releases |
| [Security](SECURITY.md) | Permissions, configuration data, reporting vulnerabilities |

Try the optional [container playground](playground/README.md) for a small, real Compose project.

## Project status

Systemdoc is preparing its first public release. Implemented features are described in the user guide; known limits and manual test gaps are recorded in [troubleshooting](docs/troubleshooting.md) and [the release guide](docs/releasing.md). Inventory uses CLI polling, and activity history is session-local. Resource totals describe reporting workloads, not whole-host usage.

[Architecture](SPEC.md) · [Visual system](VISUAL-DESIGN.md) · [Changelog](CHANGELOG.md)
