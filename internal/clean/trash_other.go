//go:build !darwin && !linux

package clean

import "errors"

func moveToTrash(string) error {
	return errors.New("moving to trash is not supported on this OS; use --permanent")
}
