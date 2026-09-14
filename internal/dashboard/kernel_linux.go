//go:build linux

package dashboard

import "syscall"

// kernelRelease reports the running kernel's release string, or "" when the
// uname syscall fails. sysdig chooses its driver from this value.
func kernelRelease() string {
	var utsname syscall.Utsname
	if syscall.Uname(&utsname) != nil {
		return ""
	}
	var release []byte
	for _, c := range utsname.Release {
		if c == 0 {
			break
		}
		release = append(release, byte(c))
	}
	return string(release)
}
