package clean

import "golang.org/x/sys/windows"

// FreeBytes is the space available to the user on path's volume, or -1 when
// it cannot be read.
func FreeBytes(path string) int64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -1
	}
	var avail uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, nil, nil); err != nil {
		return -1
	}
	return int64(avail)
}
