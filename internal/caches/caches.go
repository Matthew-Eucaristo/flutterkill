// Package caches knows the shared, machine-wide caches that Flutter, Dart,
// Gradle, Android Studio, CocoaPods and Xcode leave behind. The list is fixed:
// flutterkill never removes a global path that Known does not allow.
package caches

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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
	UsedBy  []string `json:"used_by,omitempty"` // projects using this FVM or Gradle version

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

// Children allows the direct children of Dir whose names start with Prefix
// (and are longer than it), except hidden names and those in Except.
type Children struct {
	Dir    string
	Prefix string
	Except map[string]bool
}

func (c Children) allows(name string) bool {
	return !strings.HasPrefix(name, ".") && len(name) > len(c.Prefix) && strings.HasPrefix(name, c.Prefix) && !c.Except[name]
}

// Allowed lists the only global paths flutterkill may remove: exact paths,
// plus direct children matched by one of the Children rules.
type Allowed struct {
	Exact    map[string]bool
	Children []Children
}

// Allows reports whether path is an allowed location. It does not look at the
// filesystem; clean.CheckCache adds those checks.
func (a Allowed) Allows(path string) bool {
	path = filepath.Clean(path)
	if a.Exact[path] {
		return true
	}
	dir, name := filepath.Dir(path), filepath.Base(path)
	for _, c := range a.Children {
		if c.Dir != "" && dir == filepath.Clean(c.Dir) && c.allows(name) {
			return true
		}
	}
	return false
}

// Known returns every global cache location for env, existing or not. The
// Android Studio rule reads the disk once to except the newest version.
func Known(env Env) Allowed {
	a := Allowed{Exact: map[string]bool{}}
	for _, c := range catalog(env) {
		a.Exact[c.Path] = true
	}
	for _, g := range groups(env, Pins{}) {
		a.Children = append(a.Children, g.Children)
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
	// Dart keeps its pub cache and analyzer state in %LOCALAPPDATA% on Windows.
	dartHome, pubCache := home, filepath.Join(home, ".pub-cache")
	if env.OS == "windows" {
		dartHome = localAppData(env)
		pubCache = filepath.Join(dartHome, "Pub", "Cache")
	}
	if p := env.Getenv("PUB_CACHE"); p != "" {
		pubCache = p
	}
	dartServer := filepath.Join(dartHome, ".dartServer")
	if p := env.Getenv("ANALYZER_STATE_LOCATION_OVERRIDE"); p != "" {
		dartServer = p
	}
	gradle := gradleHome(env)
	list := []Cache{
		{ID: "pub-cache", Name: "Pub cache", Path: pubCache,
			Note: "Downloaded Dart/Flutter packages. Re-fetched by the next `pub get`; also removes `dart pub global` tools."},
		{ID: "dart-server", Name: "Dart analysis server", Path: dartServer,
			Note: "Analyzer index for your editor. Rebuilt when a project is opened."},
		{ID: "gradle-caches", Name: "Gradle caches", Path: filepath.Join(gradle, "caches"),
			Note: "Android dependencies and build cache. Re-downloaded on the next Android build; run `./gradlew --stop` first."},
		{ID: "gradle-jdks", Name: "Gradle JDK toolchains", Path: filepath.Join(gradle, "jdks"),
			Note: "JDKs Gradle downloaded for toolchains. Re-provisioned by the next build that needs one; run `./gradlew --stop` first."},
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
			Cache{ID: "swiftpm-cache", Name: "Swift Package Manager cache", Path: filepath.Join(home, "Library", "Caches", "org.swift.swiftpm"),
				Note: "Swift packages fetched for iOS/macOS plugins. Re-fetched by the next Xcode build."},
			Cache{ID: "simulator-caches", Name: "Simulator caches", Path: filepath.Join(dev, "CoreSimulator", "Caches"),
				Note: "dyld caches of the iOS simulators. Rebuilt when a simulator boots."},
		)
	}
	return list
}

// localAppData is %LOCALAPPDATA%, or its default under home when unset.
func localAppData(env Env) string {
	if p := env.Getenv("LOCALAPPDATA"); filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(env.Home, "AppData", "Local")
}

func gradleHome(env Env) string {
	if p := env.Getenv("GRADLE_USER_HOME"); p != "" {
		return p
	}
	return filepath.Join(env.Home, ".gradle")
}

// Pins maps a version to the scanned projects that use it.
type Pins struct {
	FVM    map[string][]string // Flutter version pinned with FVM
	Gradle map[string][]string // wrapper distribution, e.g. "gradle-8.3-all"
}

// Discover returns the caches that exist on this machine, sizes unmeasured.
func Discover(env Env, pins Pins) []*Cache {
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
	for _, g := range groups(env, pins) {
		out = append(out, g.discover()...)
	}
	if env.OS == "darwin" {
		if c := unavailableSimulators(); c != nil {
			out = append(out, c)
		}
	}
	return out
}

// group is a directory whose children are listed, and cleaned, one by one.
type group struct {
	Children
	row func(name string) Cache // ID, Name, Note and UsedBy of one child
}

// groups are the per-version caches. Groups whose directory is not Narrow are
// dropped.
func groups(env Env, pins Pins) []group {
	var out []group
	for _, g := range []group{fvmVersions(env, pins.FVM), gradleDists(env, pins.Gradle), oldAndroidStudios(env)} {
		if g.Dir == "" {
			continue
		}
		if g.Dir = filepath.Clean(g.Dir); Narrow(g.Dir, env.Home) {
			out = append(out, g)
		}
	}
	return out
}

func (g group) discover() []*Cache {
	entries, err := os.ReadDir(g.Dir)
	if err != nil {
		return nil
	}
	var out []*Cache
	for _, e := range entries {
		if !e.IsDir() || !g.allows(e.Name()) { // IsDir is false for symlinks
			continue
		}
		c := g.row(e.Name())
		c.Path, c.Bytes = filepath.Join(g.Dir, e.Name()), -1
		out = append(out, &c)
	}
	return out
}

func fvmVersions(env Env, pins map[string][]string) group {
	dir := filepath.Join(env.Home, "fvm", "versions")
	if p := env.Getenv("FVM_CACHE_PATH"); p != "" {
		dir = filepath.Join(p, "versions")
	}
	return group{Children{Dir: dir}, func(name string) Cache {
		users := sorted(pins[name])
		note := "Not pinned by any scanned project."
		if len(users) > 0 {
			note = "Pinned by " + strings.Join(users, ", ") + ". `fvm install` restores it."
		}
		return Cache{ID: "fvm:" + name, Name: "FVM Flutter " + name, Note: note, UsedBy: users}
	}}
}

// gradleDists are the Gradle wrapper's downloads, one directory per
// distribution, named after the zip in distributionUrl (gradle-8.3-all).
func gradleDists(env Env, pins map[string][]string) group {
	dir := filepath.Join(gradleHome(env), "wrapper", "dists")
	return group{Children{Dir: dir, Prefix: "gradle-"}, func(name string) Cache {
		users := sorted(pins[name])
		note := "Not used by any scanned project. Re-downloaded if a project needs it."
		if len(users) > 0 {
			note = "Used by " + strings.Join(users, ", ") + ". Re-downloaded by their next Android build."
		}
		return Cache{ID: "gradle-dist:" + name, Name: "Gradle " + strings.TrimPrefix(name, "gradle-"), Note: note, UsedBy: users}
	}}
}

const studioPrefix = "AndroidStudio"

// oldAndroidStudios are the system caches (indexes, logs, build data) of
// Android Studio versions older than the newest one of their channel, e.g.
// AndroidStudio2024.1 next to AndroidStudio2025.1. Only the system location
// (idea.system.path) is used; settings and plugins live elsewhere (Application
// Support, %APPDATA%, ~/.config, ~/.local/share) and are never touched. The newest
// version per channel and names that do not parse are excepted.
func oldAndroidStudios(env Env) group {
	var dir string
	switch env.OS {
	case "darwin":
		dir = filepath.Join(env.Home, "Library", "Caches", "Google")
	case "windows":
		dir = filepath.Join(localAppData(env), "Google")
	case "linux":
		dir = filepath.Join(env.Home, ".cache", "Google")
		if p := env.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(p) {
			dir = filepath.Join(p, "Google")
		}
	default:
		return group{}
	}
	except := map[string]bool{}
	newest := map[string]string{} // channel -> directory name
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, studioPrefix) {
			continue // a symlink must not stand in for the newest version
		}
		channel, v, ok := studioVersion(name)
		if !ok {
			except[name] = true
			continue
		}
		if cur, seen := newest[channel]; !seen || slices.Compare(v, version(cur)) > 0 {
			newest[channel] = name
		}
	}
	for _, name := range newest {
		except[name] = true
	}
	return group{Children{Dir: dir, Prefix: studioPrefix, Except: except}, func(name string) Cache {
		channel, _, _ := studioVersion(name)
		label := strings.Join(strings.Fields("Android Studio "+channel+" "+strings.TrimPrefix(name, studioPrefix+channel)), " ")
		return Cache{ID: "android-studio:" + name, Name: label + " caches",
			Note: "Caches of an older Android Studio; " + newest[channel] + " is kept. Rebuilt only if that version runs again; settings are untouched."}
	}}
}

// studioVersion splits "AndroidStudioPreview2025.1.2" into channel "Preview"
// and version [2025 1 2]. The stable channel is "".
func studioVersion(name string) (channel string, v []int, ok bool) {
	rest := strings.TrimPrefix(name, studioPrefix)
	i := strings.IndexFunc(rest, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 {
		return "", nil, false
	}
	for _, part := range strings.Split(rest[i:], ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strings.HasPrefix(part, "+") {
			return "", nil, false
		}
		v = append(v, n)
	}
	return rest[:i], v, true
}

func version(name string) []int {
	_, v, _ := studioVersion(name)
	return v
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
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
