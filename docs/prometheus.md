# Metrics suite (Prometheus)

[Documentation index](README.md) · [Configuration](configuration.md)

Press `6` from any suite, or choose **06 METRICS** in the Control Deck, to open the Metrics suite. It reads one Prometheus server over its HTTP API and shows the exporter families it finds as trend panels, lists scrape targets with server health, and, when no server answers, shows how to install one on this host.

## Finding the server

The suite resolves the Prometheus URL in this order:

1. A workload opened with **Actions → Open in Metrics** (a running Prometheus service or container).
2. `--prometheus URL` or `SYSTEMDOC_PROMETHEUS`.
3. `prometheus_url` in `settings.json`, edited under **Actions → Preferences**.
4. Detection from the current inventory: an active `prometheus.service` (or the Homebrew launchd job `sh.brew.prometheus` on macOS, when the user scope is shown) means `http://localhost:9090`; a running container whose image is `prometheus` uses its published 9090 port.
5. Otherwise the conventional `http://localhost:9090`, so a local Prometheus works with no configuration at all; the setup view says so when nothing answers there.

An optional bearer token is read from `SYSTEMDOC_PROMETHEUS_TOKEN`. It is sent as a header, never written to settings, never shown on screen and never forwarded over SSH. A username and password inside the URL are rejected. TLS uses the system certificate roots only.

A server that does not answer is retried with back-off (30 s doubling to ten minutes). `r` probes again immediately. Previous samples stay on screen with the failure in the status line.

## Panels

Panels are built in for these exporter families and appear when the server has their series from the last 15 minutes:

| Family | Detected by | Panels |
| --- | --- | --- |
| node_exporter | `node_cpu_seconds_total` | CPU busy, per-CPU, I/O wait and steal, memory, swap, load against CPU count, filesystem use, disk throughput and busy time, network throughput, uptime, pressure stall, temperatures |
| cAdvisor | `container_cpu_usage_seconds_total` | Per-container CPU, working set, memory against limit, network, age, container count |
| kubelet cAdvisor | the same metric with a `pod` label | The same panels per `namespace/pod`, plus restarts when kube-state-metrics is scraped |
| Proxmox VE (`prometheus-pve-exporter`) | `pve_up` | Node CPU, memory and status; VM and container CPU, memory, network and disk with guest names; storage use; guests running and stopped; HA state |
| Prometheus itself | `prometheus_tsdb_head_series` | Head series, samples appended per second, targets up, slowest scrapes |

Each trend panel shows the latest value, its direction, a chart of the last 30 minutes with an axis naming the window's peak, and a caption naming the window and the step per column. Charts scale to the window's peak with a little headroom, never above the panel's ceiling, so a trend is visible whether the value is 3% or 90%; the colour of the value, not the scale, says how close it is to its ceiling. Charts follow the `G` graphics setting (blocks, braille, ASCII); `b` switches to bar glyphs. `Enter` opens the selected panel full screen with the chart across the whole terminal and every series listed under it with its own trend; `←` and `→` (or `h` and `l`) flick to the previous and next panel without leaving the view, and `Esc` returns to the grid. Panels the server has no data for are hidden and counted in the status line. Values with an honest ceiling (percentages, usage against a limit) take the green/amber/red pressure colours used everywhere else; unbounded values keep the suite's identity colour. A missing sample draws as a gap (`·`), never as zero, and a window that predates the data starts blank. Panels for per-device or per-container series show the busiest series and name how many more there are; the full-screen view lists them all with the exact PromQL.

Every poll re-reads the window from the server with `query_range`, so a chart is always the server's view of time rather than a locally accumulated history. Polling is floored at ten seconds for this reason, and only the panels on screen are queried. Rates use a window of four scrape intervals (1m, 2m or 5m).

When the Prometheus scrapes several hosts, the panels show this machine when its hostname or an address matches an instance, otherwise the first instance; `i` chooses another host and the choice is saved per server and family.

### Pinning your own metrics

`a` lists the server's metric names; pick one and give it a title, an optional label filter (`job="node",mode!="idle"`), a unit, whether to show its rate, and an optional ceiling. The pin is saved under `prometheus_metrics` in `settings.json` and appears as a **PIN** panel. `d` removes the selected pin after review. Built-in panels cannot be removed.

## Targets and health

`t` lists active scrape targets with state, job, instance, last scrape, duration and the last error, down targets first. The title names the Prometheus version, how many targets are up and whether the last configuration reload succeeded; the detail pane shows the scrape URL and labels. `Enter` opens the full record.

## Setup when nothing answers

`s` (shown automatically when no server is configured or reachable) explains where the URL comes from and lists install steps for this host, detected from the package manager (`apt-get`, `dnf`, `pacman`, `zypper`, `brew`), Docker availability and whether the host is a Proxmox VE node:

- Prometheus and node_exporter through the package manager where the packages exist (Debian, Ubuntu, Arch, Fedora, Homebrew), or as containers.
- cAdvisor as a container.
- `prometheus-pve-exporter` on Proxmox: creating a read-only API user and token with `pveum`, installing with `pipx` or as a container, the `pve.yml` file and the scrape job.
- Scrape configuration snippets, `promtool check config` and a configuration reload.

Numbered steps run through the same approval card as every other action; steps that need root run with `sudo` after the usual authorisation and are recorded in **Operations**. Steps marked *show only* need a file or a secret that only you can supply, so they are displayed for you to apply by hand. When an install finishes, the suite probes the server again.

## Keys

| Key | Action |
| --- | --- |
| p / t / s | Panels, targets, setup |
| Arrows, h/j/k/l, Home, End | Select a panel |
| Enter | Open the selected panel full screen, open a target record or run the selected setup step |
| ← → or h l (full screen) | Flick to the previous or next panel |
| a / d | Pin a metric / unpin the selected pin |
| i | Choose which scraped host a family shows |
| b | Toggle bar glyphs |
| r | Refresh now and probe the server again |
| P | Pause polling |
| G | Cycle block, braille and ASCII graphics |
| e | Review and export the page as text |
| , | Change the polling interval |
| Escape | Return to the dashboard |

## Limits

One server at a time. Self-signed certificates are not trusted; place such a server behind a certificate the system trusts. Alerting rules, remote write, recording rules and PromQL editing are not part of the suite. Charts show the server's samples for the last 30 minutes and do not fetch older history.
