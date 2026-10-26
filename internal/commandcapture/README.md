# Structured command capture

`Capture(ctx, name, args...)` returns stdout, sanitized stderr and an error.
It preserves the beginning of each stream, rejects overflow above 4 MiB,
sets `LC_ALL=C`, and limits execution to eight seconds or the caller's deadline.
This is for structured snapshots; live streams and action output use separate
tail buffers in the dashboard.

Run `go test ./internal/commandcapture`. The package depends only on the standard
library and `internal/textutil`.
