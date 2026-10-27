# Prometheus API client and panel catalog

`New(raw, token)` validates a Prometheus base URL and returns a client whose
requests are bounded: eight seconds per request, 4 MiB per body, no redirects,
system TLS roots. `Ready`, `BuildInfo`, `RuntimeInfo`, `Query`, `QueryRange`,
`MetricNames`, `LabelValues` and `Targets` decode `/api/v1` envelopes into
plain values. Label values and server messages pass through `textutil.Clean`;
the UI still escapes terminal markup. NaN and infinite samples are reported as
unknown. A bearer token is sent as a header and never appears in an error or
in `String()`. A username or password inside the URL is rejected.

`Catalog` lists the built-in exporter families (node_exporter, cAdvisor,
kubelet, Proxmox VE exporter, Prometheus itself) with their PromQL templates;
`Expand` fills the `$sel`, `$win`, `$node` and `$top` placeholders.
`InstallSteps` returns per-platform install guidance with runnable steps
marked separately from steps that only show a command or a file.

Run `go test ./internal/promapi`. Fixtures live in `testdata/`. The package
depends only on the standard library and `internal/textutil`; it must not
import `dashboard`, tview or tcell.
