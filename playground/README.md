# Systemdoc playground

[Back to Systemdoc](../README.md) · [Docker guide](../docs/usage.md)

Three real containers for exploring the Docker and Compose views: nginx, Redis,
and a worker that emits a heartbeat every three seconds. The occasional warning
is an explicitly labelled log-colour sample. No container restarts automatically.

Install Systemdoc using [the installation guide](../docs/installation.md), or run `make build` first. From the repository root:

```sh
docker compose -f playground/compose.yaml up -d --wait
docker compose -f playground/compose.yaml ps
docker compose -f playground/compose.yaml port web 80
./bin/systemdoc --docker
```

The web server binds to a dynamically assigned **localhost-only** port. Redis
has no published port and stores its ephemeral data in memory. Each container
has CPU, memory and log-size limits. Press `p` in Systemdoc to manage the
`systemdoc-playground` Compose project, or `L` to follow a selected container's logs.
Press `o` for its name, image, health, port bindings, mounts, and runtime settings;
`d` focuses on network and storage attachments. `z` expands the inspector.

Remove only this playground, including its ephemeral volumes:

```sh
docker compose -f playground/compose.yaml down --volumes
```

New Docker group membership takes effect after logging out and back in. To use
it immediately in your current terminal, run `newgrp docker`, then launch
Systemdoc from that shell. Opening another terminal within an existing desktop
session may still inherit the old groups.
