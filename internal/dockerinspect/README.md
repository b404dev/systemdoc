# Docker inspection

`Overview(raw, connectionsOnly)` turns one container's inspect JSON into a
readable overview or connections report. It excludes environment values and
preserves labels and names as plain text; the UI escapes terminal markup.

Run `go test ./internal/dockerinspect`. The JSON fixture lives in `testdata/`.
Inspector routing and terminal markup integration tests remain in
`internal/dashboard/docker_overview_test.go`.

Dependencies: the standard library and `internal/textutil`. Docker invocation
and workspace state belong to the caller.
