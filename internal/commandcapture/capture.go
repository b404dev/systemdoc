// Package commandcapture captures bounded structured CLI output with cancellation.
package commandcapture

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"systemdoc/internal/textutil"
)

// Preserve the beginning of structured output, and fail visibly on overflow.
// A tail buffer could drop a process header and misattribute subsequent sockets.
type prefixBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *prefixBuffer) Write(p []byte) (int, error) {
	n := len(p)
	space := 4*1024*1024 - b.Len()
	if n > space {
		b.overflow = true
		p = p[:space]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
// Capture returns stdout, sanitized stderr and the command error. Each stream
// is capped at 4 MiB; overflow invalidates the entire structured snapshot.
// Commands use the C locale and an eight-second timeout bounded by ctx.
func Capture(ctx context.Context, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var output, diagnostic prefixBuffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdout = &output
	cmd.Stderr = &diagnostic
	// lsof forks a helper child; if it keeps the pipe open after the parent is
	// killed on timeout, Wait must still return or the page's busy flag never
	// clears and it stops refreshing for the rest of the session.
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if output.overflow || diagnostic.overflow {
		return "", "", fmt.Errorf("%s output exceeded 4 MiB; snapshot was not used", name)
	}
	if ctx.Err() != nil {
		return "", "", fmt.Errorf("%s: %w", name, ctx.Err())
	}
	return output.String(), textutil.Clean(diagnostic.String()), err
}
