//go:build !unix

package scan

import "io/fs"

func diskUsage(info fs.FileInfo) (int64, fileID, bool) {
	return info.Size(), fileID{}, false
}
