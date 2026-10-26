# Host networking

This package owns socket parsing and collection, socket filters, interface
enumeration, byte counters and rate calculations. It has no terminal UI state.

- `CollectSockets` collects Linux `ss` or macOS/fallback `lsof` records.
- `CollectView` selects sockets, interfaces or byte counters for a view.
- `Rates` compares counters without inventing rates after resets.
- `MatchesSocket`, `Endpoint` and `Binding` support filtering and presentation.
- `ParseSS` exposes parsing with a skipped-row count for partial snapshots.

Run `go test ./internal/hostnet`. Parser and collector tests live here; terminal
focus, rendering and refresh integration tests remain in
`internal/dashboard/network_test.go`.

Dependencies: the standard library, `internal/commandcapture` and
`internal/textutil`. Do not import dashboard or terminal widget packages here.
