//go:build unix

package clean

import "golang.org/x/sys/unix"

// FreeBytes is the space available to the user on path's filesystem, or -1
// when it cannot be read.
func FreeBytes(path string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
