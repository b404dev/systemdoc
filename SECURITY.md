# Security

## Reporting a vulnerability

Use the repository's **Security → Report a vulnerability** form when private vulnerability reporting is enabled. Do not include exploit details, credentials, or sensitive configuration in a public issue. If private reporting is unavailable, open an issue asking the maintainer to provide a private contact without describing the vulnerability.

Include affected version, platform, reproduction steps, expected impact, and a minimal sanitized example. No response-time guarantee or supported-version policy has been established before the first public release.

## Permissions and local data

Run Systemdoc as a regular user. Native systemd, journal, Docker, and SSH permissions determine access. Systemdoc does not change socket permissions or group membership. Management actions can change real services and containers after target review.

Settings and exports use owner-only permissions. Project definitions contain source paths; those sources, command arguments, labels, inspect JSON, and logs can contain secrets. The Docker overview omits environment values, but it is not a general-purpose redaction layer. Review exports and AI context before sharing.

AI assistance sends the text you review to an external CLI/provider. Authentication belongs to that CLI. SSH uses native host-key and authentication handling; only public keys are installed by key setup. Temporary uploads are private and cleaned up on exit when the connection permits.

## Downloads

The installer requires HTTPS and verifies SHA-256 against the manifest from the same release tag before replacing an executable. This detects corruption and mismatched assets; it is not an independent publisher signature. Download-and-inspect instructions are available in [installation](docs/installation.md).

Maintainers should enable private vulnerability reporting before publication and review release assets before making a draft public.
