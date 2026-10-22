//go:build linux || darwin

package octoapi

import "syscall"

// diskSpace is the free and the total size of the file system of dir, in bytes.
func diskSpace(dir string) (free, total uint64) {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0, 0
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize)
}
