# Configuration

[Documentation index](README.md)

Settings live at `$XDG_CONFIG_HOME/systemdoc/settings.json`, normally `~/.config/systemdoc/settings.json` on Linux. On macOS without an absolute `XDG_CONFIG_HOME`, the path is `~/Library/Application Support/systemdoc/settings.json`. Saves use an atomic replacement and owner-only file permissions. Invalid JSON is reported and not overwritten automatically.

Click the polling indicator in the dashboard footer or press `,` from any live page to change the refresh interval (2–300 seconds). The new timing applies immediately to Services, Containers, Network, Processes, and Storage and is saved for the next launch. Process Explorer uses a minimum 15-second interval because each update is a full process-list snapshot; slower configured intervals are respected. The same value is available under **Actions → Preferences**, alongside automatic/stacked/side-by-side layout, the 30–70% inventory pane split, inspector wrapping, startup splash, signal graphics, the Nerd Font interface, and accent/background overrides. Theme selection uses `t`; Enter saves and Escape cancels.

A minimal settings file is:

```json
{
  "settings_version": 1,
  "theme": "Cathedral",
  "refresh_seconds": 5,
  "layout": "auto",
  "pane_ratio": 46,
  "graph_mode": "blocks",
  "splash": true,
  "wrap_logs": false,
  "nerd_icons": true,
  "systemd_active_only": false
}
```

Missing settings use defaults. Unknown theme names resolve to Cathedral; legacy Deep names resolve to the nearest Observatory palette. Invalid refresh intervals resolve to five seconds. `graph_mode` accepts `blocks`, `braille`, or `ascii`; invalid values fall back to blocks. Press `G` to cycle it without opening Preferences. Five [Observatory themes](../THEMES.md) are supplied. `accent` and `background` accept `#RRGGBB` strings; blank values restore preset colours. Existing overrides survive theme migration. `nerd_icons` defaults to `true`; the first Observatory-era read promotes the former off-by-default value once, after which setting it to `false` under Preferences is retained as the labeled Unicode/ASCII fallback.

`SYSTEMDOC_KUBECTL` (or `--kubectl`) pins the kubectl invocation used for Kubernetes pods in the Containers suite, for example `k0s kubectl` or `kubectl --context kind-dev`; leave it unset to detect the stack automatically.

`i` toggles active-only filtering and saves the systemd choice. Docker's filter is session-local. `--active-only` enables it at startup. Favourites and registered Compose project definitions are also saved. Configure projects through the project menu so file ordering, working directory, profiles, environment files, and Docker endpoint are explicit.

Saved views (`saved_views`) are managed with **V** or the visible **Views** button. Each named entry stores mode, service manager, system/user scope, filters, sort, favourites-only, layout, pane ratio, inspector tab, drawer visibility and, for Docker, the endpoint. Opening a view restores these session choices; log buffers and backend data are not persisted. Invalid view fields are rejected when opening, and Docker endpoint mismatches do not switch the target.

SSH preferences remember host/login choices and identity paths, not passwords or private-key contents. The remote application reads the remote user's settings. See [remote hosts](remote.md).

Systemdoc does not choose your terminal's font or change its global palette. Docker overview and Connections wrap structured text for readability; raw configuration/log wrapping follows the inspector setting.
