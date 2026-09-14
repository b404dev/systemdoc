//go:build !linux

package dashboard

// kernelRelease is Linux-only: sysdig's driver selection depends on the Linux
// kernel release, and there is no equivalent on other platforms.
func kernelRelease() string {
	return ""
}
