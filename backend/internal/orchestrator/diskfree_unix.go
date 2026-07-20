//go:build !windows

package orchestrator

import "golang.org/x/sys/unix"

// diskFree returns free and total bytes for the filesystem holding path.
func diskFree(path string) (free, total uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	// Bavail, not Bfree: the reserved blocks are not usable by our service user.
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), nil
}
