package dashboard

import (
	"context"
	"time"

	"systemdoc/internal/hostnet"
)

// These adapters are used only by tests. Production callers use the staged
// inventory, tolerant parsers, filtered logs, and generic command streamer.

func inventory(ctx context.Context, mode int, user bool) ([]workload, error) {
	items, err := listWorkloads(ctx, mode, user)
	if err != nil {
		return nil, err
	}
	items, _ = enrichInventory(ctx, mode, user, items)
	return items, nil
}

// parseProcesses keeps the historical signature: it fails only when the
// snapshot had rows and none of them parsed.
func parseProcesses(raw string) ([]hostProcess, error) {
	rows, _, err := parseProcessesTolerant(raw)
	return rows, err
}

// parseMounts keeps the historical signature: a missing header, or a table
// whose every row is unreadable, is an error; a single odd row is skipped.
func parseMounts(raw, platform string, inodeOnly bool) ([]hostMount, error) {
	rows, _, err := parseMountsTolerant(raw, platform, inodeOnly)
	return rows, err
}

// parseDeletedFiles keeps the historical signature: output that is not lsof
// field format, or whose every record is unreadable, is an error.
func parseDeletedFiles(raw string) ([]deletedFile, error) {
	rows, _, err := parseDeletedFilesTolerant(raw)
	return rows, err
}

func resetKubeState() {
	kube.mu.Lock()
	defer kube.mu.Unlock()
	kube.stack, kube.err, kube.dockerErr, kube.probed, kube.failures = nil, "", "", time.Time{}, 0
}

// followLogs batches bounded CLI output; callers choose how to display it.
func followLogs(ctx context.Context, mode int, user bool, item workload, update func(string, string)) {
	followLogsWith(ctx, mode, user, item, logFilter{}, update)
}

// parseSS keeps the historical signature: it fails only when the output had
// rows and none of them was a socket.
func parseSS(raw string) ([]hostnet.Socket, error) {
	rows, _, err := hostnet.ParseSS(raw)
	return rows, err
}

// streamSysdig runs a live probe and publishes the bounded buffer every 400 ms
// until the context is cancelled or sysdig exits.
func streamSysdig(ctx context.Context, runner sysdigRunner, args []string, update func(text, ending string)) {
	streamCommand(ctx, sysdigCommand(ctx, runner, args), "sysdig", update)
}

// Host utilisation sits beside the workload counts in the masthead so the
// machine's own load is visible without opening a panel. Missing readings stay
// missing rather than rendering as zero.
// hostReadoutFor omits the readout when the masthead has no room for it, so a
// narrow terminal shows a complete identity row rather than a clipped one.
func hostReadoutFor(p palette, usage hostUtilisation, width int) string {
	if width > 0 && width < 100 {
		return ""
	}
	return "   " + hostReadout(p, usage, width)
}

func resetPrometheusState() { resetPrometheus() }
