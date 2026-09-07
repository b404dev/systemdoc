# Changelog

Changes are recorded here before being assigned a release tag.

## Unreleased

### Added

- Named saved views with filters, sorting, scope, layout, inspector tab and drawer state; Docker endpoint checks.
- Retained log search with regex, inclusive time bounds, merged context and next/previous matches.
- Read-only systemd timer browser with next/last triggers and timer/activated-unit inspection.
- Editable troubleshooting snapshot reports with optional logs/configuration and private, non-overwriting export.
- Visual roadmap and an interactive browser concept for compact workspace, timer timeline and troubleshooting layouts.

### Fixes

- Background inventory refresh no longer resets focus from the inspector, log drawer, search field, or an active overlay.
- Slow inspector responses preserve the latest reading position instead of restoring the scroll offset from the start of the request.

### Interface

- Five additional moody themes: Deep Obsidian, Forest, Aubergine, Copper and Midnight, with subdued accents and consistent state colours.

- Replaced the three-row live/activity bar with a muted bottom-right refresh spinner; sample timestamps stay fixed between refreshes and activity remains available with `v`.

- Five original Deep themes: Navy, Violet, Teal, Ember, and Rose; retired presets fall back to Deep Navy.
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
