package clean

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// How says what a removal did, so moved, deleted and delegated space are
// reported separately.
type How string

const (
	Trashed   How = "trashed"      // moved to the OS trash; recoverable
	Deleted   How = "deleted"      // removed with --permanent
	Delegated How = "delegated"    // removed by the owning tool's own command
	Would     How = "would remove" // dry run
)

// Outcome is what one removal did.
type Outcome struct {
	Name    string // what the user saw: a cache name, or "" for artifacts
	Path    string // the artifact or cache path ("" for some commands)
	Command string // the clean-up command that ran, for Delegated
	How     How
	Bytes   int64 // size measured before removal
	Freed   int64 // change in free disk space, measured; -1 if unknown
	Err     error
}

// RemoveArtifact removes <project>/<name> and reports the outcome. size is
// the measured size, or -1 to measure it now.
func (c *Cleaner) RemoveArtifact(project, name string, size int64) Outcome {
	path := filepath.Join(project, filepath.FromSlash(name))
	if size < 0 {
		size = scan.DirSize(path)
	}
	o := Outcome{Path: path, How: c.how(), Bytes: size}
	return c.measure(o, project, func() error { return c.Artifact(project, name) })
}

// RemoveCache cleans a global cache and reports the outcome. size is the
// measured size, or -1 to measure it now.
func (c *Cleaner) RemoveCache(cc *caches.Cache, size int64) Outcome {
	paths := cc.SizePaths
	if len(paths) == 0 {
		paths = []string{cc.Path}
	}
	if size < 0 {
		size = 0
		for _, p := range paths {
			size += scan.DirSize(p)
		}
	}
	o := Outcome{Name: cc.Name, Path: cc.Path, How: c.how(), Bytes: size}
	if len(cc.Command) > 0 && c.Mode != DryRun {
		o.How, o.Command = Delegated, strings.Join(cc.Command, " ")
	}
	// Measure on the volume that holds the cache; it is gone afterwards.
	volume := filepath.Dir(paths[0])
	if paths[0] == "" {
		volume, _ = os.UserHomeDir()
	}
	return c.measure(o, volume, func() error { return c.Cache(cc) })
}

func (c *Cleaner) how() How {
	return map[Mode]How{Trash: Trashed, Permanent: Deleted, DryRun: Would}[c.Mode]
}

// measure runs remove and records the change in free space on volume.
func (c *Cleaner) measure(o Outcome, volume string, remove func() error) Outcome {
	o.Freed = -1
	before := int64(-1)
	if c.Mode != DryRun {
		before = FreeBytes(volume)
	}
	o.Err = remove()
	if before >= 0 && o.Err == nil {
		if after := FreeBytes(volume); after >= 0 {
			o.Freed = after - before
		}
	}
	return o
}
