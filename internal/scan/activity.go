package scan

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LastActivity returns when the project was last worked on. In a git repo
// that is the later of the latest commit touching it and the newest
// uncommitted change (gitignored files, like build output, do not count).
// Outside git it is the newest modification time of its non-artifact files.
func LastActivity(dir string) (time.Time, string) {
	if t, ok := lastCommit(dir); ok {
		if d := newestUncommitted(dir); d.After(t) {
			t = d
		}
		return t, "git"
	}
	if t := newestMtime(dir); !t.IsZero() {
		return t, "mtime"
	}
	return time.Time{}, ""
}

// gitCmd runs read-only git queries. GIT_OPTIONAL_LOCKS=0 stops `git status`
// from refreshing the index, so scanning never writes into the user's repos,
// and core.fsmonitor=false keeps a repo's config from launching a hook.
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

func lastCommit(dir string) (time.Time, bool) {
	out, err := gitCmd(dir, "log", "-1", "--format=%ct", "--", ".").Output()
	if err != nil {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}, false // not a repo, or no commits touch this dir
	}
	return time.Unix(sec, 0), true
}

// newestUncommitted is the newest mtime among modified and untracked files.
func newestUncommitted(dir string) time.Time {
	out, err := gitCmd(dir, "status", "--porcelain", "-z", "--untracked-files=normal", "--", ".").Output()
	if err != nil {
		return time.Time{}
	}
	top, err := gitCmd(dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return time.Time{}
	}
	repo := strings.TrimSpace(string(top))
	var newest time.Time
	entries := strings.Split(string(out), "\x00")
	for i, e := range entries {
		if i >= 500 {
			break
		}
		if len(e) < 4 || e[2] != ' ' {
			continue // rename sources carry no status prefix
		}
		if fi, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(e[3:]))); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// newestMtime caps the walk so a huge untracked tree cannot stall a scan.
func newestMtime(dir string) time.Time {
	const maxEntries = 20000
	var newest time.Time
	seen := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if seen++; seen > maxEntries {
			return fs.SkipAll
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || IsArtifactDir(filepath.ToSlash(rel)) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "pubspec.lock" { // rewritten by every `pub get`, not real activity
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
