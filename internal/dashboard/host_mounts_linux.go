//go:build linux

package dashboard

import "syscall"

// nativeMountsSupported selects /proc/self/mountinfo plus statfs(2) over df.
// GNU df exits 1 when any one mount (typically a FUSE/gvfs mount another user
// owns) returns EPERM, which blanked the whole Storage view, and a stale NFS
// mount stalls it for the full command timeout.
const nativeMountsSupported = true

// statfsMount reads one mount point. Statfs_t field types differ between
// Linux and Darwin, so the conversion lives behind the build tag.
func statfsMount(path string) (mountUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return mountUsage{}, err
	}
	// df sizes blocks by f_frsize; f_bsize is the preferred I/O size and only
	// stands in when a filesystem reports no fragment size.
	block := st.Frsize
	if block <= 0 {
		block = st.Bsize
	}
	if block <= 0 {
		block = 1
	}
	return mountUsage{BlockSize: uint64(block), Blocks: st.Blocks, Bfree: st.Bfree, Bavail: st.Bavail, Files: st.Files, Ffree: st.Ffree}, nil
}
