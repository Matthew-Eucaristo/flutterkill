// Package clean is the only code in flutterkill that removes anything. Every
// removal passes a guard check (guard.go) first.
package clean

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
)

// TrashName is what the OS calls its trash, for messages.
var TrashName = "Trash"

// Mode selects what "remove" means.
type Mode int

const (
	Trash     Mode = iota // move to the OS trash (default, recoverable)
	Permanent             // os.RemoveAll
	DryRun                // check only, change nothing
)

func (m Mode) String() string {
	return [...]string{"trash", "permanent", "dry-run"}[m]
}

// Cleaner removes artifacts under Root and known global caches.
type Cleaner struct {
	Root    string
	Mode    Mode
	Allowed caches.Allowed
}

// Artifact removes <project>/<name> after CheckArtifact.
func (c *Cleaner) Artifact(project, name string) error {
	if err := CheckArtifact(c.Root, project, name); err != nil {
		return err
	}
	return c.remove(filepath.Join(project, filepath.FromSlash(name)))
}

// Cache cleans a global cache, by its built-in command or by removing its path.
func (c *Cleaner) Cache(cc *caches.Cache) error {
	if len(cc.Command) > 0 {
		if err := CheckCommand(cc.Command); err != nil {
			return err
		}
		if c.Mode == DryRun {
			return nil
		}
		out, err := exec.Command(cc.Command[0], cc.Command[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %v: %s", strings.Join(cc.Command, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := CheckCache(c.Allowed, c.Root, cc.Path); err != nil {
		return err
	}
	return c.remove(cc.Path)
}

// Commands run by a cache are permanent regardless of Mode; the UI says so.
func (c *Cleaner) remove(path string) error {
	switch c.Mode {
	case DryRun:
		return nil
	case Permanent:
		return os.RemoveAll(path)
	}
	if err := moveToTrash(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s still exists after moving to trash", path)
	}
	return nil
}
