package caches

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDiscoverFindsExistingCachesAndFVMPins(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".dartServer", ".gradle/caches/modules-2", ".gradle/jdks", "fvm/versions/3.24.0", "fvm/versions/3.19.6", "fvm/versions/.tmp"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	env := Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	got := map[string]*Cache{}
	for _, c := range Discover(env, Pins{FVM: map[string][]string{"3.24.0": {"b_app", "a_app"}}}) {
		got[c.ID] = c
	}

	for _, id := range []string{"dart-server", "gradle-caches", "gradle-jdks", "fvm:3.24.0", "fvm:3.19.6"} {
		if got[id] == nil {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{"pub-cache", "fvm:.tmp", "xcode-derived-data"} {
		if got[id] != nil {
			t.Errorf("unexpected %s", id)
		}
	}
	if c := got["fvm:3.24.0"]; c != nil && (len(c.UsedBy) != 2 || c.UsedBy[0] != "a_app") {
		t.Errorf("UsedBy = %v", c.UsedBy)
	}
}

// abs makes a slash path absolute on this OS: "/x" is C:\x on Windows.
func abs(p string) string {
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") {
		return "C:" + filepath.FromSlash(p)
	}
	return p
}

func TestWindowsLocations(t *testing.T) {
	home := abs("/Users/me")
	local := filepath.Join(home, "AppData", "Local")
	for name, getenv := range map[string]func(string) string{
		"default":      func(string) string { return "" },
		"LOCALAPPDATA": func(k string) string { return map[string]string{"LOCALAPPDATA": local}[k] },
	} {
		a := Known(Env{Home: home, OS: "windows", Getenv: getenv})
		for _, p := range []string{
			filepath.Join(local, "Pub", "Cache"),
			filepath.Join(local, ".dartServer"),
			filepath.Join(home, ".gradle", "caches", "modules-2"),
			filepath.Join(home, ".gradle", "caches", "8.14.5"),
			filepath.Join(home, "fvm", "versions", "3.24.0"),
			filepath.Join(local, "Google", "AndroidStudio2024.1"),
		} {
			if !a.Allows(p) {
				t.Errorf("%s: %s not allowed", name, p)
			}
		}
		for _, p := range []string{
			filepath.Join(home, ".pub-cache"),
			filepath.Join(home, "AppData", "Roaming", "Google", "AndroidStudio2024.1"), // settings
			filepath.Join(home, "Library", "Caches", "CocoaPods"),
		} {
			if a.Allows(p) {
				t.Errorf("%s: %s allowed", name, p)
			}
		}
	}
	env := Env{Home: home, OS: "windows", Getenv: func(k string) string {
		return map[string]string{"PUB_CACHE": abs("/x/pub"), "ANALYZER_STATE_LOCATION_OVERRIDE": abs("/x/analyzer")}[k]
	}}
	if a := Known(env); !a.Exact[abs("/x/pub")] || !a.Exact[abs("/x/analyzer")] {
		t.Errorf("overrides ignored: %v", a.Exact)
	}
}

func TestEnvOverrides(t *testing.T) {
	env := Env{Home: abs("/h"), OS: "linux", Getenv: func(k string) string {
		return map[string]string{"PUB_CACHE": abs("/x/pub"), "GRADLE_USER_HOME": abs("/x/gradle"), "FVM_CACHE_PATH": abs("/x/fvm")}[k]
	}}
	a := Known(env)
	for _, p := range []string{abs("/x/pub"), abs("/x/gradle/caches/modules-2"), abs("/x/gradle/caches/build-cache-1"), abs("/x/gradle/caches/9.4.1"), abs("/x/gradle/jdks"), abs("/x/gradle/wrapper/dists/gradle-8.3-all"), abs("/x/fvm/versions/3.24.0")} {
		if !a.Allows(p) {
			t.Errorf("%s not allowed", p)
		}
	}
	if a.Allows(abs("/x/gradle/wrapper/dists")) || a.Allows(abs("/x/fvm/versions")) || a.Allows(abs("/x/gradle/caches")) {
		t.Error("a group directory itself is allowed")
	}
}

func TestBroadEnvPathsAreDropped(t *testing.T) {
	for _, bad := range []string{abs("/h"), abs("/"), abs("/h/"), abs("/usr")} {
		env := Env{Home: abs("/h"), OS: "darwin", Getenv: func(k string) string {
			return map[string]string{"PUB_CACHE": bad, "GRADLE_USER_HOME": abs("/"), "FVM_CACHE_PATH": abs("/")}[k]
		}}
		a := Known(env)
		for p := range a.Exact {
			if !Narrow(p, abs("/h")) {
				t.Errorf("PUB_CACHE=%s: broad path %s allowed", bad, p)
			}
		}
		if a.Exact[abs("/h")] || a.Exact[abs("/")] || a.Exact[abs("/usr")] {
			t.Errorf("PUB_CACHE=%s allowed", bad)
		}
		for _, c := range a.Children {
			if !Narrow(c.Dir, abs("/h")) {
				t.Errorf("broad child rule %s", c.Dir)
			}
		}
	}
	env := Env{Home: abs("/h"), OS: "linux", Getenv: func(k string) string { return map[string]string{"PUB_CACHE": abs("/x/pub/")}[k] }}
	if !Known(env).Exact[abs("/x/pub")] {
		t.Error("trailing slash not cleaned")
	}
}

func TestNarrow(t *testing.T) {
	for p, want := range map[string]bool{abs("/"): false, abs("/Users"): false, abs("/Users/me"): false, abs("/Users/me/.pub-cache"): true, abs("/opt/cache"): true, abs("/opt"): false, "rel/x": false} {
		if got := Narrow(p, abs("/Users/me")); got != want {
			t.Errorf("Narrow(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestGradleDistsPerVersion(t *testing.T) {
	home := t.TempDir()
	dists := filepath.Join(home, ".gradle", "wrapper", "dists")
	for _, d := range []string{"gradle-8.3-all", "gradle-7.6.3-bin", ".tmp"} {
		os.MkdirAll(filepath.Join(dists, d), 0o755)
	}
	os.WriteFile(filepath.Join(dists, "CACHEDIR.TAG"), nil, 0o644)
	env := Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	got := map[string]*Cache{}
	for _, c := range Discover(env, Pins{Gradle: map[string][]string{"gradle-8.3-all": {"b_app", "a_app"}}}) {
		got[c.ID] = c
	}
	if c := got["gradle-dist:gradle-8.3-all"]; c == nil || len(c.UsedBy) != 2 || c.UsedBy[0] != "a_app" || c.Path != filepath.Join(dists, "gradle-8.3-all") {
		t.Errorf("gradle-8.3-all = %+v", c)
	}
	if c := got["gradle-dist:gradle-7.6.3-bin"]; c == nil || len(c.UsedBy) != 0 {
		t.Errorf("gradle-7.6.3-bin = %+v", c)
	}
	if len(got) != 2 {
		t.Errorf("got %d caches, want the two distributions", len(got))
	}

	a := Known(env)
	for p, want := range map[string]bool{
		filepath.Join(dists, "gradle-8.3-all"):         true,
		filepath.Join(dists, "gradle-9.0-bin"):         true, // not on disk yet; the guard checks existence
		dists:                                          false,
		filepath.Join(dists, ".tmp"):                   false,
		filepath.Join(dists, "gradle-"):                false,
		filepath.Join(dists, "other"):                  false,
		filepath.Join(dists, "gradle-8.3-all", "hash"): false,
	} {
		if a.Allows(p) != want {
			t.Errorf("Allows(%s) = %v, want %v", p, !want, want)
		}
	}
}

func TestOldAndroidStudioCaches(t *testing.T) {
	for _, tt := range []struct{ os, dir string }{
		{"darwin", "Library/Caches/Google"},
		{"linux", ".cache/Google"},
	} {
		home := t.TempDir()
		dir := filepath.Join(home, filepath.FromSlash(tt.dir))
		for _, d := range []string{"AndroidStudio2024.2", "AndroidStudio2025.1.2", "AndroidStudio2025.1.10", "AndroidStudioPreview2025.2", "AndroidStudioPreview2024.3", "AndroidStudioWeird", "Chrome"} {
			os.MkdirAll(filepath.Join(dir, d), 0o755)
		}
		os.Symlink(filepath.Join(dir, "AndroidStudio2024.2"), filepath.Join(dir, "AndroidStudio2023.1"))
		env := Env{Home: home, OS: tt.os, Getenv: func(string) string { return "" }}

		got := map[string]*Cache{}
		for _, c := range Discover(env, Pins{}) {
			got[c.ID] = c
		}
		var ids []string
		for id := range got {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		want := []string{"android-studio:AndroidStudio2024.2", "android-studio:AndroidStudio2025.1.2", "android-studio:AndroidStudioPreview2024.3"}
		if !slices.Equal(ids, want) {
			t.Errorf("%s: ids = %v, want %v", tt.os, ids, want)
		}
		if c := got["android-studio:AndroidStudioPreview2024.3"]; c != nil && c.Name != "Android Studio Preview 2024.3 caches" {
			t.Errorf("name = %q", c.Name)
		}

		a := Known(env)
		for name, ok := range map[string]bool{
			"AndroidStudio2024.2":        true,
			"AndroidStudio2025.1.2":      true,
			"AndroidStudioPreview2024.3": true,
			"AndroidStudio2025.1.10":     false, // newest stable
			"AndroidStudioPreview2025.2": false, // newest preview
			"AndroidStudioWeird":         false, // version does not parse
			"AndroidStudio":              false,
			"Chrome":                     false,
		} {
			if a.Allows(filepath.Join(dir, name)) != ok {
				t.Errorf("%s: Allows(%s) = %v, want %v", tt.os, name, !ok, ok)
			}
		}
		if a.Allows(dir) || a.Allows(filepath.Join(dir, "AndroidStudio2024.2", "caches")) {
			t.Errorf("%s: Google dir or a grandchild allowed", tt.os)
		}
	}
}

func TestAndroidStudioSymlinkIsNotTheNewest(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "Library", "Caches", "Google")
	os.MkdirAll(filepath.Join(dir, "AndroidStudio2025.1"), 0o755)
	os.Symlink(filepath.Join(dir, "AndroidStudio2025.1"), filepath.Join(dir, "AndroidStudio2099.1"))
	a := Known(Env{Home: home, OS: "darwin", Getenv: func(string) string { return "" }})
	if a.Allows(filepath.Join(dir, "AndroidStudio2025.1")) {
		t.Error("newest real version allowed because a symlink claims a newer one")
	}
}

func TestAndroidStudioOnlyUsesCachesLocation(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"Library/Application Support/Google/AndroidStudio2024.1", "Library/Application Support/Google/AndroidStudio2025.1"} {
		os.MkdirAll(filepath.Join(home, filepath.FromSlash(d)), 0o755)
	}
	env := Env{Home: home, OS: "darwin", Getenv: func(string) string { return "" }}
	for _, c := range Discover(env, Pins{}) {
		if strings.HasPrefix(c.ID, "android-studio:") {
			t.Errorf("config dir listed: %s", c.Path)
		}
	}
	if Known(env).Allows(filepath.Join(home, "Library", "Application Support", "Google", "AndroidStudio2024.1")) {
		t.Error("Application Support dir allowed")
	}
	xdg := Env{Home: abs("/h"), OS: "linux", Getenv: func(k string) string { return map[string]string{"XDG_CACHE_HOME": abs("/x/cache")}[k] }}
	if g := oldAndroidStudios(xdg); g.Dir != abs("/x/cache/Google") {
		t.Errorf("XDG_CACHE_HOME ignored: %s", g.Dir)
	}
}

func TestGradleVersionNames(t *testing.T) {
	for name, want := range map[string]bool{
		"8.3": true, "8.14.5": true, "9.4.1": true, "9.0-rc-1": true, "8.0-milestone-2": true,
		"modules-2": false, "build-cache-1": false, "jars-9": false, "journal-1": false, "transforms-4": false,
		"8": false, "8.": false, ".8.3": false, "8.3.1.2": false, "8.3-foo": false, "v8.3": false, "8.3 ": false, "": false,
	} {
		if got := IsGradleVersion(name); got != want {
			t.Errorf("IsGradleVersion(%q) = %v", name, got)
		}
	}
	for name, want := range map[string]string{"gradle-8.3-all": "8.3", "gradle-9.0-rc-1-bin": "9.0-rc-1", "gradle-8.3": "", "gradle-x-all": ""} {
		if got, _ := distVersion(name); got != want {
			t.Errorf("distVersion(%q) = %q", name, got)
		}
	}
}

// ~/.gradle/caches is split: one row per version directory, plus exact rows
// for the dependency and build caches. The rest is never listed or allowed.
func TestGradleCachesAreSplitByVersion(t *testing.T) {
	home := t.TempDir()
	gradle := filepath.Join(home, ".gradle")
	for _, d := range []string{"caches/9.4.1/transforms", "caches/8.14.5", "caches/9.0-rc-1", "caches/modules-2", "caches/build-cache-1",
		"caches/jars-9", "caches/journal-1", "caches/foo", "wrapper/dists/gradle-8.14.5-bin", "wrapper/dists/gradle-9.3.1-all",
		"wrapper/dists/gradle-9.4.1-all", "wrapper/dists/gradle-weird", "daemon/9.4.1", "daemon/9.3.1"} {
		os.MkdirAll(filepath.Join(gradle, filepath.FromSlash(d)), 0o755)
	}
	// A 9.4.1 daemon logged just now; the 9.3.1 one is a day old.
	os.WriteFile(filepath.Join(gradle, "daemon", "9.4.1", "daemon-1.out.log"), []byte("x"), 0o644)
	old := filepath.Join(gradle, "daemon", "9.3.1", "daemon-2.out.log")
	os.WriteFile(old, []byte("x"), 0o644)
	day := time.Now().Add(-24 * time.Hour)
	os.Chtimes(old, day, day)

	env := Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	pins := Pins{Gradle: map[string][]string{"gradle-8.14.5-all": {"b_app", "a_app"}, "gradle-8.14.5-bin": {"a_app", "native"}}}
	got := map[string]*Cache{}
	for _, c := range Discover(env, pins) {
		got[c.ID] = c
	}
	type want struct {
		path           string
		users          []string
		unused, daemon bool
	}
	for id, w := range map[string]want{
		"gradle-caches":                 {"caches/modules-2", nil, false, false},
		"gradle-build-cache":            {"caches/build-cache-1", nil, false, false},
		"gradle-version:8.14.5":         {"caches/8.14.5", []string{"a_app", "b_app", "native"}, false, false},
		"gradle-version:9.4.1":          {"caches/9.4.1", nil, true, true},
		"gradle-version:9.0-rc-1":       {"caches/9.0-rc-1", nil, true, false},
		"gradle-dist:gradle-8.14.5-bin": {"wrapper/dists/gradle-8.14.5-bin", []string{"a_app", "native"}, false, false},
		"gradle-dist:gradle-9.3.1-all":  {"wrapper/dists/gradle-9.3.1-all", nil, true, false},
		"gradle-dist:gradle-9.4.1-all":  {"wrapper/dists/gradle-9.4.1-all", nil, true, true},
		"gradle-dist:gradle-weird":      {"wrapper/dists/gradle-weird", nil, false, false}, // no version: stays review
	} {
		c := got[id]
		if c == nil {
			t.Errorf("missing %s", id)
			continue
		}
		if c.Path != filepath.Join(gradle, filepath.FromSlash(w.path)) || !slices.Equal(c.UsedBy, w.users) || c.Unused != w.unused || c.DaemonRecent != w.daemon {
			t.Errorf("%s = %+v, want %+v", id, *c, w)
		}
	}
	for id := range got {
		if strings.Contains(id, "jars") || strings.Contains(id, "journal") || strings.Contains(id, "foo") {
			t.Errorf("unexpected %s", id)
		}
	}
	if got["gradle-version:9.4.1"].Name != "Gradle 9.4.1 caches" {
		t.Errorf("name %q", got["gradle-version:9.4.1"].Name)
	}

	a := Known(env)
	caches := filepath.Join(gradle, "caches")
	for p, ok := range map[string]bool{
		filepath.Join(caches, "9.4.1"):                  true,
		filepath.Join(caches, "9.0-rc-1"):               true,
		filepath.Join(caches, "modules-2"):              true,
		filepath.Join(caches, "build-cache-1"):          true,
		caches:                                          false,
		filepath.Join(caches, "foo"):                    false,
		filepath.Join(caches, "jars-9"):                 false,
		filepath.Join(caches, "journal-1"):              false,
		filepath.Join(caches, "9.4.1", "transforms"):    false, // grandchild
		filepath.Join(caches, "modules-2", "files-2.1"): false,
		filepath.Join(gradle, "daemon", "9.4.1"):        false,
	} {
		if a.Allows(p) != ok {
			t.Errorf("Allows(%s) = %v, want %v", p, !ok, ok)
		}
	}
}
