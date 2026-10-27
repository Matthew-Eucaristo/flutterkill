//go:build !unix && !windows

package clean

// FreeBytes is unknown on this OS.
func FreeBytes(string) int64 { return -1 }
