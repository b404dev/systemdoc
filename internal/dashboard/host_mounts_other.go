//go:build !linux

package dashboard

import "errors"

// Other platforms keep the df path; macOS df -Pki is reliable and reports
// inodes in the same table.
const nativeMountsSupported = false

func statfsMount(string) (mountUsage, error) {
	return mountUsage{}, errors.New("native mount statistics are Linux-only")
}
