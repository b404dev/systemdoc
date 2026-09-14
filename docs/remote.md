# SSH and remote hosts

[Documentation index](README.md)


With Systemdoc installed remotely:

```sh
systemdoc --ssh alice@server
systemdoc --ssh server --ssh-user alice
systemdoc --ssh my-ssh-config-alias --remote-bin /home/alice/.local/bin/systemdoc
systemdoc --ssh alice@server --user
systemdoc --ssh alice@server --docker --docker-context default
systemdoc --ssh alice@server --ssh-port 2222 --identity ~/.ssh/server_key
```

**Actions → Run on remote host** asks for the host and SSH username in separate fields. Leave the username blank to use SSH configuration (or the username in `user@host`). The CLI equivalent is `--ssh-user`; this is separate from `--user`, which selects remote user services (systemd or the launchd GUI domain). Conflicting usernames are rejected before connecting. OpenSSH handles password/key-passphrase prompts, host keys, authentication, agent use, and settings such as ProxyJump. No credentials are copied. The entire application runs remotely, so settings, native service access, Docker access, and AI authentication belong to the remote user. Exiting the remote app returns to the local shell, or the local application when launched through **Actions → Run on remote host**.

On the first connection to each host/login, the app offers **Set up key**, **Use existing SSH**, or **Cancel**. Key setup uses `ssh-copy-id` to install only the public key on the remote account. With no selected identity, it creates `~/.ssh/systemdoc_ed25519` if needed using an interactive `ssh-keygen` passphrase prompt. It preserves existing keys. The chosen identity and whether the offer was shown are saved locally; passwords and private-key contents are never stored in Systemdoc settings. The connection form also has **Set up key** for repeating the workflow later.

For CLI key setup:

```sh
systemdoc --ssh server --ssh-user alice --setup-ssh-key --upload
# Use an existing key instead:
systemdoc --ssh server --ssh-user alice --setup-ssh-key --identity /home/alice/.ssh/id_ed25519
```

For subsequent CLI sessions, pass the same `--identity` path (the GUI remembers it). Key setup can ask for the remote account password and a local key passphrase; these are distinct authentication steps.

The remote command inherits your local terminal's colour capability: when the local terminal is 24-bit, Systemdoc sets `COLORTERM=truecolor` for the remote process, because sshd forwards `TERM` but not `COLORTERM` and the interface would otherwise render its gradients in 256-colour blocks. See [troubleshooting](troubleshooting.md#the-layout-or-colours-look-wrong) for manual SSH sessions and tmux.

Each remote session uses a private [OpenSSH control connection](https://man.openbsd.org/ssh_config#ControlMaster). Host checks, uploads, execution and cleanup reuse that authenticated connection, avoiding a fresh password prompt for each command. Systemdoc closes its own control connection on exit, with a 60-second idle fallback, and leaves your SSH configuration unchanged.

To upload a temporary executable for this session:

```sh
systemdoc --ssh alice@server --upload
```

Upload checks OS/CPU compatibility and requires a static ELF binary for Linux or a matching Mach-O executable for macOS. It creates a private `/tmp/systemdoc.*` directory, transfers the executable over SSH, runs it with a terminal, and attempts cleanup on exit. It never installs globally. If `/tmp` is mounted `noexec`, install the binary in an executable location and use `--remote-bin`. A failed connection may leave the temporary directory; the application reports the path when cleanup fails.

For another architecture:

```sh
make cross
systemdoc --ssh alice@arm-server --upload --upload-binary ./bin/systemdoc-linux-arm64
```

Upload supports Linux amd64/arm64 and macOS arm64. The installed-binary path works wherever a compatible Systemdoc build runs. No remote host was available for an authenticated end-to-end test during development; SSH argument construction, quoting, and validation have automated coverage.

## macOS hosts

The remote app selects launchd when running on macOS. `--upload` accepts a matching Apple Silicon Mach-O executable; use `--upload-binary` when the local binary targets a different OS/architecture. User service scope means the remote user’s GUI domain, which may be absent on headless SSH hosts. See [macOS services](macos.md).
