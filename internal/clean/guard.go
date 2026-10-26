package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// These checks run immediately before every removal, independently of how the
// target was found, so a stale or tampered list cannot widen what gets deleted.

// CheckArtifact allows removing <project>/<name> only if:
//   - name is one of scan.ArtifactDirs,
//   - project is root or lies inside root, and is not the home directory,
//   - project still contains a pubspec.yaml,
//   - neither the artifact nor any directory between root and it is a symlink,
//   - the artifact is a real directory,
//   - the artifact does not look like source: no pubspec.yaml or .git directly
//     inside, and no files in it tracked by git.
func CheckArtifact(root, project, name string) error {
	if !scan.IsArtifactDir(name) {
		return fmt.Errorf("%q is not a known artifact directory", name)
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(project) {
		return errors.New("paths must be absolute")
	}
	root, project = filepath.Clean(root), filepath.Clean(project)
	if project != root && !within(root, project) {
		return fmt.Errorf("%s is outside the scanned root %s", project, root)
	}
	if fi, err := os.Lstat(filepath.Join(project, "pubspec.yaml")); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s has no pubspec.yaml", project)
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == project {
		return errors.New("refusing to treat the home directory as a project")
	}
	target := filepath.Join(project, filepath.FromSlash(name))
	if err := scan.RealDirBelow(root, target); err != nil {
		return err
	}
	if scan.HoldsSourceTopLevel(target) {
		return fmt.Errorf("%s holds a pubspec.yaml or .git; not generated output, remove it by hand", target)
	}
	if f := scan.TrackedFile(target); f != "" {
		return fmt.Errorf("%s contains files tracked by git (%s); not generated output, remove it by hand", target, f)
	}
	return nil
}

// CheckCache allows removing path only if it is one of the fixed global cache
// locations (or a direct child of the FVM versions directory), is not itself
// a symlink, is a real directory, is not home or an ancestor of it, and does
// not contain the scan root.
func CheckCache(allowed caches.Allowed, root, path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	path = filepath.Clean(path)
	if home, err := os.UserHomeDir(); err == nil && !caches.Narrow(path, home) {
		return fmt.Errorf("refusing broad path %s", path)
	}
	if root != "" && (path == filepath.Clean(root) || within(path, root)) {
		return fmt.Errorf("%s contains the scanned root", path)
	}
	known := allowed.Exact[path] ||
		(allowed.FVMVersions != "" && filepath.Dir(path) == filepath.Clean(allowed.FVMVersions) && !strings.HasPrefix(filepath.Base(path), "."))
	if !known {
		return fmt.Errorf("%s is not a known global cache", path)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink %s", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

// CheckCommand allows only the built-in clean-up commands.
func CheckCommand(cmd []string) error {
	for _, c := range caches.Commands {
		if strings.Join(c, "\x00") == strings.Join(cmd, "\x00") {
			return nil
		}
	}
	return fmt.Errorf("unknown command %q", strings.Join(cmd, " "))
}

// within reports whether path is strictly inside dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
