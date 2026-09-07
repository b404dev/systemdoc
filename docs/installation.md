# Installation

[Documentation index](README.md)

## Requirements

Release binaries target Linux x86_64 (`amd64`) and aarch64 (`arm64`). The installer needs `curl`, `sha256sum`, and standard Linux shell utilities. The binary is built with `CGO_ENABLED=0` and does not need Go on the target host.

Systemd features need `systemctl` and, for logs, `journalctl`. Docker features need the Docker CLI and daemon access; Compose uses `docker compose`. SSH uses OpenSSH. Optional AI tools are installed and authenticated separately. No backend is bundled or configured by the installer.

Use a Unicode monospace font. True-color terminals give the best gradients. A terminal around 120×30 or larger shows the full dashboard; 80×24 uses a compact layout. Nerd Font icons are off by default.

## One-line installation and updates

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/systemdoc/main/install.sh | sh
```

This URL becomes available when the public repository is configured and its first release is published. The installer resolves the latest release once, downloads the matching binary and `SHA256SUMS` from that tag, verifies the checksum, then replaces `~/.local/bin/systemdoc` atomically. Download or checksum failures leave the existing binary untouched. It never invokes sudo or edits shell settings.

If `systemdoc` is not found, add this to your shell's startup configuration:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Start a new shell, then run `systemdoc --version`.

## Download and inspect the installer first

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/systemdoc/main/install.sh -o install-systemdoc.sh
less install-systemdoc.sh
sh install-systemdoc.sh
```

## Pin a release or choose a directory

Replace the example version with a published tag:

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/systemdoc/main/install.sh | SYSTEMDOC_VERSION=v0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/OWNER/systemdoc/main/install.sh | SYSTEMDOC_INSTALL_DIR="$HOME/bin" sh
```

The install directory must be an absolute path writable by your user. `SYSTEMDOC_REPO=owner/repository` selects a fork with the same release asset layout. The checksum validates the binary against the release manifest; it is not an independent signature of the publisher.

## Manual download

Download `systemdoc-linux-amd64` or `systemdoc-linux-arm64` and `SHA256SUMS` from the repository's GitHub Releases page. Compare the result of `sha256sum systemdoc-linux-amd64` with the matching manifest entry (substitute `arm64` as needed). After verifying:

```sh
install -Dm755 systemdoc-linux-amd64 "$HOME/.local/bin/systemdoc"
systemdoc --version
```

## Build from source

Go 1.23 or newer, Git, and Make are needed for this route:

```sh
git clone https://github.com/OWNER/systemdoc.git
cd systemdoc
make build
./bin/systemdoc
```

`make install` builds and installs into `~/.local/bin`. `make install PREFIX=/your/prefix` changes the prefix; `DESTDIR` supports packaging. Use `make cross` for both supported Linux architectures.

## Uninstall

Remove the installed executable:

```sh
rm "$HOME/.local/bin/systemdoc"
```

If you used a custom installation directory, remove that copy instead. Settings and registered project definitions remain under `${XDG_CONFIG_HOME:-$HOME/.config}/systemdoc/`; delete that directory separately only if you want to discard them. Uninstalling does not remove services, containers, volumes, Compose sources, or SSH keys.
