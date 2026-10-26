package caches

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsExistingCachesAndFVMPins(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".dartServer", ".gradle/caches", "fvm/versions/3.24.0", "fvm/versions/3.19.6", "fvm/versions/.tmp"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	env := Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	got := map[string]*Cache{}
	for _, c := range Discover(env, map[string][]string{"3.24.0": {"b_app", "a_app"}}) {
		got[c.ID] = c
	}

	for _, id := range []string{"dart-server", "gradle-caches", "fvm:3.24.0", "fvm:3.19.6"} {
		if got[id] == nil {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{"pub-cache", "gradle-wrapper", "fvm:.tmp", "xcode-derived-data"} {
		if got[id] != nil {
			t.Errorf("unexpected %s", id)
		}
	}
	if c := got["fvm:3.24.0"]; c != nil && (len(c.UsedBy) != 2 || c.UsedBy[0] != "a_app") {
		t.Errorf("UsedBy = %v", c.UsedBy)
	}
}

func TestEnvOverrides(t *testing.T) {
	env := Env{Home: "/h", OS: "linux", Getenv: func(k string) string {
		return map[string]string{"PUB_CACHE": "/x/pub", "GRADLE_USER_HOME": "/x/gradle", "FVM_CACHE_PATH": "/x/fvm"}[k]
	}}
	a := Known(env)
	for _, p := range []string{"/x/pub", "/x/gradle/caches", "/x/gradle/wrapper/dists"} {
		if !a.Exact[p] {
			t.Errorf("%s not allowed", p)
		}
	}
	if a.FVMVersions != "/x/fvm/versions" {
		t.Errorf("FVMVersions = %s", a.FVMVersions)
	}
}

func TestBroadEnvPathsAreDropped(t *testing.T) {
	for _, bad := range []string{"/h", "/", "/h/", "/usr"} {
		env := Env{Home: "/h", OS: "darwin", Getenv: func(k string) string {
			return map[string]string{"PUB_CACHE": bad, "GRADLE_USER_HOME": "/", "FVM_CACHE_PATH": "/"}[k]
		}}
		a := Known(env)
		for p := range a.Exact {
			if !Narrow(p, "/h") {
				t.Errorf("PUB_CACHE=%s: broad path %s allowed", bad, p)
			}
		}
		if a.Exact["/h"] || a.Exact["/"] || a.Exact["/usr"] {
			t.Errorf("PUB_CACHE=%s allowed", bad)
		}
	}
	env := Env{Home: "/h", OS: "linux", Getenv: func(k string) string { return map[string]string{"PUB_CACHE": "/x/pub/"}[k] }}
	if !Known(env).Exact["/x/pub"] {
		t.Error("trailing slash not cleaned")
	}
}

func TestNarrow(t *testing.T) {
	for p, want := range map[string]bool{"/": false, "/Users": false, "/Users/me": false, "/Users/me/.pub-cache": true, "/opt/cache": true, "/opt": false, "rel/x": false} {
		if got := Narrow(p, "/Users/me"); got != want {
			t.Errorf("Narrow(%s) = %v, want %v", p, got, want)
		}
	}
}
