package scan

import (
	"io/fs"
	"path/filepath"
)

// DirSize returns the disk space used by everything under path, like `du -s`.
// Symlinks are counted as links and never followed; hard-linked files are
// counted once.
func DirSize(path string) int64 {
	var total int64
	seen := map[fileID]bool{}
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		n, id, multi := diskUsage(info)
		if multi {
			if seen[id] {
				return nil
			}
			seen[id] = true
		}
		total += n
		return nil
	})
	return total
}

type fileID struct{ dev, ino uint64 }
