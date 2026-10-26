//go:build unix

package scan

import (
	"io/fs"
	"syscall"
)

// diskUsage returns the allocated size of a file and, for hard-linked files,
// its identity so it can be counted once.
func diskUsage(info fs.FileInfo) (n int64, id fileID, multi bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), fileID{}, false
	}
	return int64(st.Blocks) * 512, fileID{uint64(st.Dev), uint64(st.Ino)}, !info.IsDir() && st.Nlink > 1
}
