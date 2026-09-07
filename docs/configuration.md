# Configuration

[Documentation index](README.md)

Settings live at `$XDG_CONFIG_HOME/systemdoc/settings.json`, normally `~/.config/systemdoc/settings.json`. Saves use an atomic replacement and owner-only file permissions. Invalid JSON is reported and not overwritten automatically.

Open **Actions → Preferences** to change refresh interval (2–300 seconds), automatic/stacked/side-by-side layout, inspector wrapping, startup splash, optional Nerd Font icons, and accent/background overrides. Theme selection uses `t`; Enter saves and Escape cancels.

A minimal settings file is:

```json
{
  "theme": "Deep Navy",
  "refresh_seconds": 5,
  "layout": "auto",
  "splash": true,
  "wrap_logs": false,
  "nerd_icons": false,
  "systemd_active_only": false
}
```

Missing settings use defaults. Unknown/retired theme names resolve to Deep Navy; invalid refresh intervals resolve to five seconds. Ten [Deep themes](../THEMES.md) are supplied. `accent` and `background` accept `#RRGGBB` strings; blank values restore preset colours. Existing overrides survive theme migration.

`i` toggles active-only filtering and saves the systemd choice. Docker's filter is session-local. `--active-only` enables it at startup. Favourites and registered Compose project definitions are also saved. Configure projects through the project menu so file ordering, working directory, profiles, environment files, and Docker endpoint are explicit.

Saved views (`saved_views`) are managed with **V**. Each named entry stores mode, system/user scope, filters, sort, favourites-only, layout, inspector tab, drawer visibility and, for Docker, the endpoint. Opening a view restores these session choices; log buffers and backend data are not persisted. Invalid view fields are rejected when opening, and Docker endpoint mismatches do not switch the target.

SSH preferences remember host/login choices and identity paths, not passwords or private-key contents. The remote application reads the remote user's settings. See [remote hosts](remote.md).

Systemdoc does not choose your terminal's font or change its global palette. Docker overview and Connections wrap structured text for readability; raw configuration/log wrapping follows the inspector setting.
