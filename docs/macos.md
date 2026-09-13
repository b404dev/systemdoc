# macOS services

Systemdoc now has an initial launchd backend for macOS, alongside systemd on Linux and Docker on either platform. Apple uses launchd to manage system daemons and per-user agents, including jobs launched on demand. [Apple’s launchd guide](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).

Intel Macs and Intel/Rosetta builds are unsupported. Run the arm64 binary from a native Apple Silicon terminal.

## Run it

Build from source on a Mac with Go and Make:

```sh
make build
./bin/systemdoc                 # loaded system launchd jobs
./bin/systemdoc --user          # current user's GUI-domain agents
./bin/systemdoc --active-only   # jobs with a running PID
./bin/systemdoc --docker        # containers on the configured Docker endpoint
```

`u` switches system/user service scope. `--user` selects `gui/<uid>`, so it requires that user's GUI login domain to exist; a headless SSH login alone may not create one. Systemdoc does not silently fall back to another domain. Homebrew services appear when registered with launchd in the selected domain; Systemdoc does not manage Homebrew installation or packages.

Cross-builds are available at `bin/systemdoc-darwin-arm64` (Apple Silicon only) after `make cross`. `make install` works on both platforms. Release packaging includes these assets, and the installer supports macOS's `shasum` and BSD `mv`. A published release is still required for release installation; no release is created by building locally. No Developer ID signing/notarization workflow is included.

## What is available

- Loaded-job inventory from `launchctl print`, with PID, state and explicit enable/disable overrides. Unloaded plist files are not enumerated.
- Overview and Runtime tabs show the exact scoped job's native launchctl output.
- Config reads the reported plist with `plutil`, including binary plists. Dynamically registered jobs without a reported path show an explanation.
- CPU and resident memory use `ps` for the observed process. These exclude children. CPU follows macOS's averaged process accounting, not systemd CPU counter deltas. Missing process samples stay unavailable.
- Logs use macOS unified logging filtered by the current PID. Live streams reconnect when a changed PID is observed. Historical inspection requests the last 15 minutes and retains up to 150 lines. File-based stdout/stderr logs are separate; inspect `StandardOutPath` and `StandardErrorPath` in Config. PID reuse may include unrelated historical entries, and private/absent log messages are not reconstructed.
- Saved views, filters, favourites, themes, activity, retained-log search and reviewed snapshot exports work with launchd. Views record the service manager and reject Linux/macOS mismatches. Legacy views without a manager are treated as systemd views.
- Reviewed start/restart, SIGTERM stop, enable and disable actions. Start uses `kickstart`; restart uses `kickstart -k`. Stop may be followed by launchd restarting an on-demand or KeepAlive job. Enable/disable changes future eligibility and does not immediately load, unload or stop a job. System actions may require native authorization; Actions → System service authorization opens a terminal sudo workflow. Apple-protected services may reject changes.

A job with no PID and no nonzero last exit status is shown as inactive/on-demand. A nonzero last exit without a running PID is flagged for attention; this reports the previous exit, not a continuous health check. `override:disabled` finds explicit disabled overrides; `default` means no explicit override was returned and does not infer the plist's Disabled value. Unavailable override queries stay unknown.

The command bar accepts scoped `launchctl print`, `kickstart [-k]`, `kill SIGTERM`, `enable`, and `disable` commands. It rejects another user's domain and unsupported verbs. Systemd timers, unit drafts, unit overrides, and journalctl are Linux-only. Scheduled launchd jobs can be inspected through their plist keys; there is no computed launchd schedule browser yet.

## Settings and SSH

Without `XDG_CONFIG_HOME`, macOS settings use `~/Library/Application Support/systemdoc/settings.json`. An absolute `XDG_CONFIG_HOME` overrides this on either platform. Preferences remain private to the user running the app.

SSH can run a preinstalled executable on Linux or macOS. Temporary uploads check ELF/static architecture for Linux and Mach-O executable architecture for macOS. A Linux executable cannot be uploaded as a Mac executable; supply the matching cross-build with `--upload-binary`.

## Validation and current limits

This implementation was developed on Linux. Fixture tests cover launchd parsing, missing data, scopes, command construction, PID changes and platform routing. The Apple Silicon build cross-compiles. CI includes a macOS runner plus an opt-in read-only live launchd inventory check. A CI configuration is not evidence that its jobs have run or passed.

`launchctl print` is diagnostic text rather than a stable machine-readable interface. Unrecognized/truncated output fails visibly instead of inventing service data. Live GUI-agent scope, permissions, unified logs, lifecycle actions, installation and terminal rendering still need verification on a real Mac before declaring release readiness.

On a disposable Mac, verify system and GUI scopes, one Homebrew/third-party agent, idle jobs, one controlled restart, PID/log reconnection, config inspection, snapshots, Docker and installation. Use `man launchctl`, `man log`, and `man ps` on that Mac for the installed command versions. See also [Apple’s terminal guidance](https://support.apple.com/guide/terminal/apdc6c1077b-5d5d-4d35-9c19-60f2397b2369/mac).
