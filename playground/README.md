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

## Single-node k0s stack

`playground/k0s.sh` runs a real k0s control plane in one privileged Docker
container, with the API published on `127.0.0.1:6443` only. No root is needed.
The image is pinned to a current k0s release because the `latest` tag on Docker
Hub is years old; set `K0S_IMAGE` to try another version.
It writes an admin kubeconfig to `~/.kube/systemdoc-k0s.yaml`, links it as
`~/.kube/config` when nothing is there yet, installs a matching `kubectl` into
`~/.local/bin` if none is found, and applies `playground/k8s-workloads.yaml`:

| Workload | What Systemdoc shows |
| --- | --- |
| `shop/web` Deployment + NodePort Service | two running pods, probes, requests and limits, a ConfigMap mount, a matching Service under `d` |
| `shop/api` Deployment | running but **not ready** (a sidecar readiness probe never passes) |
| `shop/crashy` Pod | **restarting** · `CrashLoopBackOff` |
| `shop/nopull` Pod | **failed** · `ImagePullBackOff` |
| `shop/migrate` Job | **succeeded** |

```sh
playground/k0s.sh up
./bin/systemdoc --docker
playground/k0s.sh status
playground/k0s.sh down
```

The masthead reads `k0s <version> · single node`, the `systemdoc-k0s` container
sits in the same list as the pods, and k0s's built-in metrics-server fills pod CPU
and memory after about a minute. Filter with `project:shop`, press `o`, `d`, `l`,
`c` and `r` on a pod, and use Actions for a reviewed rollout restart or delete.
`down` removes the container, its volume and the kubeconfig; a kubectl the
script installed is left in `~/.local/bin`.

To test a host-native install instead (k0s as a systemd service under root),
follow [the k0s quick start](https://docs.k0sproject.io/stable/install/), then
either export a readable kubeconfig with `sudo k0s kubeconfig admin > ~/.kube/config`
or run `systemdoc --docker --kubectl "sudo k0s kubectl"`.

New Docker group membership takes effect after logging out and back in. To use
it immediately in your current terminal, run `newgrp docker`, then launch
Systemdoc from that shell. Opening another terminal within an existing desktop
session may still inherit the old groups.
