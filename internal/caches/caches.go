// Package caches knows the shared, machine-wide caches that Flutter, Dart,
// Gradle, CocoaPods and Xcode leave behind. The list is fixed: flutterkill
// never removes a global path that is not produced by Known.
package caches

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Cache is one global cache. It is cleaned either by moving Path away or, when
// Command is set, by running that command (the tool's own clean-up).
type Cache struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Path    string   `json:"path,omitempty"`
	Bytes   int64    `json:"bytes"` // -1 until measured
	Command []string `json:"command,omitempty"`
	Note    string   `json:"note"`
	UsedBy  []string `json:"used_by,omitempty"` // projects pinning an FVM version

	// SizePaths are measured instead of Path (e.g. simulator device dirs).
	SizePaths []string `json:"-"`
}

// Env holds what discovery depends on, so tests can fake it.
type Env struct {
	Home   string
	Getenv func(string) string
	OS     string
}

// DefaultEnv describes the current user and machine.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, Getenv: os.Getenv, OS: runtime.GOOS}
}

// Allowed lists the only global paths flutterkill may remove: exact paths,
// plus direct children of FVMVersions.
type Allowed struct {
	Exact       map[string]bool
	FVMVersions string
}

// Known returns every global cache location for env, existing or not.
func Known(env Env) Allowed {
	a := Allowed{Exact: map[string]bool{}, FVMVersions: fvmVersionsDir(env)}
	for _, c := range catalog(env) {
		a.Exact[c.Path] = true
	}
	return a
}

// Commands are the only external clean-up commands flutterkill runs.
var Commands = [][]string{
	{"dart", "pub", "cache", "clean", "--force"},
	{"xcrun", "simctl", "delete", "unavailable"},
}

// Narrow reports whether path is specific enough to be a cache: not the
// filesystem root, a top-level directory, home, or an ancestor of home.
func Narrow(path, home string) bool {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || filepath.Dir(path) == path || filepath.Dir(filepath.Dir(path)) == filepath.Dir(path) {
		return false
	}
	if home != "" {
		rel, err := filepath.Rel(path, filepath.Clean(home))
		if err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
			return false // path is home or contains it
		}
	}
	return true
}

// catalog is the fixed list of path-based caches. Paths derived from
// environment variables are cleaned, and entries that are not Narrow dropped.
func catalog(env Env) []Cache {
	var out []Cache
	for _, c := range rawCatalog(env) {
		if c.Path = filepath.Clean(c.Path); Narrow(c.Path, env.Home) {
			out = append(out, c)
		}
	}
	return out
}

func rawCatalog(env Env) []Cache {
	home := env.Home
	pubCache := env.Getenv("PUB_CACHE")
	if pubCache == "" {
		pubCache = filepath.Join(home, ".pub-cache")
	}
	gradle := env.Getenv("GRADLE_USER_HOME")
	if gradle == "" {
		gradle = filepath.Join(home, ".gradle")
	}
	list := []Cache{
		{ID: "pub-cache", Name: "Pub cache", Path: pubCache,
			Note: "Downloaded Dart/Flutter packages. Re-fetched by the next `pub get`; also removes `dart pub global` tools."},
		{ID: "dart-server", Name: "Dart analysis server", Path: filepath.Join(home, ".dartServer"),
			Note: "Analyzer index for your editor. Rebuilt when a project is opened."},
		{ID: "gradle-caches", Name: "Gradle caches", Path: filepath.Join(gradle, "caches"),
			Note: "Android dependencies and build cache. Re-downloaded on the next Android build; run `./gradlew --stop` first."},
		{ID: "gradle-wrapper", Name: "Gradle distributions", Path: filepath.Join(gradle, "wrapper", "dists"),
			Note: "Every Gradle version any project ever used. The one a project needs is re-downloaded on its next build."},
	}
	if env.OS == "darwin" {
		dev := filepath.Join(home, "Library", "Developer")
		list = append(list,
			Cache{ID: "xcode-derived-data", Name: "Xcode DerivedData", Path: filepath.Join(dev, "Xcode", "DerivedData"),
				Note: "Xcode build intermediates. Rebuilt on the next iOS/macOS build."},
			Cache{ID: "xcode-device-support", Name: "iOS DeviceSupport", Path: filepath.Join(dev, "Xcode", "iOS DeviceSupport"),
				Note: "Debug symbols copied from physical devices. Re-copied when a device is connected again."},
			Cache{ID: "cocoapods-cache", Name: "CocoaPods cache", Path: filepath.Join(home, "Library", "Caches", "CocoaPods"),
				Note: "Downloaded pods. Re-fetched by the next `pod install`."},
		)
	}
	return list
}

// Discover returns the caches that exist on this machine, sizes unmeasured.
// pins maps FVM versions to the projects pinning them.
func Discover(env Env, pins map[string][]string) []*Cache {
	var out []*Cache
	for _, c := range catalog(env) {
		if !isDir(c.Path) {
			continue
		}
		c := c
		c.Bytes = -1
		if c.ID == "pub-cache" {
			if _, err := exec.LookPath("dart"); err == nil {
				c.Command = Commands[0]
			}
		}
		out = append(out, &c)
	}
	out = append(out, fvmVersions(env, pins)...)
	if env.OS == "darwin" {
		if c := unavailableSimulators(); c != nil {
			out = append(out, c)
		}
	}
	return out
}

// fvmVersionsDir is "" when the configured location is not Narrow.
func fvmVersionsDir(env Env) string {
	dir := filepath.Join(env.Home, "fvm", "versions")
	if p := env.Getenv("FVM_CACHE_PATH"); p != "" {
		dir = filepath.Join(p, "versions")
	}
	if !Narrow(dir, env.Home) {
		return ""
	}
	return dir
}

func fvmVersions(env Env, pins map[string][]string) []*Cache {
	dir := fvmVersionsDir(env)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*Cache
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") { // IsDir is false for symlinks
			continue
		}
		users := pins[e.Name()]
		sort.Strings(users)
		note := "Not pinned by any scanned project."
		if len(users) > 0 {
			note = "Pinned by " + strings.Join(users, ", ") + ". `fvm install` restores it."
		}
		out = append(out, &Cache{
			ID: "fvm:" + e.Name(), Name: "FVM Flutter " + e.Name(), Path: filepath.Join(dir, e.Name()),
			Bytes: -1, Note: note, UsedBy: users,
		})
	}
	return out
}

// unavailableSimulators reports simulators whose runtime is no longer
// installed; `xcrun simctl delete unavailable` removes them.
func unavailableSimulators() *Cache {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "unavailable", "-j").Output()
	if err != nil {
		return nil
	}
	var list struct {
		Devices map[string][]struct {
			DataPath string `json:"dataPath"`
		} `json:"devices"`
	}
	if json.Unmarshal(out, &list) != nil {
		return nil
	}
	var paths []string
	for _, devs := range list.Devices {
		for _, d := range devs {
			if d.DataPath != "" {
				paths = append(paths, filepath.Dir(d.DataPath))
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return &Cache{
		ID: "simulators-unavailable", Name: "Unavailable simulators", Bytes: -1,
		Command: Commands[1], SizePaths: paths,
		Note: "Simulators whose iOS runtime is gone. They can no longer boot.",
	}
}

func isDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}
