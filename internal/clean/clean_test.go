package clean

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
)

// project creates <root>/<name>/pubspec.yaml plus the given artifact dirs.
func project(t *testing.T, root, name string, artifacts ...string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pubspec.yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range artifacts {
		p := filepath.Join(dir, filepath.FromSlash(a))
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "file"), []byte("x"), 0o644)
	}
	return dir
}

// realTemp resolves symlinks like /var -> /private/var on macOS, as main does.
func realTemp(t *testing.T) string {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCheckArtifact(t *testing.T) {
	root := realTemp(t)
	app := project(t, root, "app", "build", "ios/Pods")
	other := realTemp(t)
	outside := project(t, other, "elsewhere", "build")
	noPubspec := filepath.Join(root, "plain")
	os.MkdirAll(filepath.Join(noPubspec, "build"), 0o755)
	os.MkdirAll(filepath.Join(app, "lib"), 0o755)

	// ios/ is a symlink to a directory that has Pods/.
	linked := project(t, root, "linked")
	os.MkdirAll(filepath.Join(other, "realios", "Pods"), 0o755)
	os.Symlink(filepath.Join(other, "realios"), filepath.Join(linked, "ios"))
	// build itself is a symlink.
	linkedBuild := project(t, root, "linkedbuild")
	os.Symlink(filepath.Join(other, "realios"), filepath.Join(linkedBuild, "build"))
	// build/ holds a package directly, or nested SPM checkouts (generated, fine).
	nested := project(t, root, "nested", "build")
	os.WriteFile(filepath.Join(nested, "build", "pubspec.yaml"), nil, 0o644)
	spm := project(t, root, "spm", "build/ios/SourcePackages/plugin/example")
	os.WriteFile(filepath.Join(spm, "build/ios/SourcePackages/plugin/example/pubspec.yaml"), nil, 0o644)
	// build is a file, not a dir.
	fileBuild := project(t, root, "filebuild")
	os.WriteFile(filepath.Join(fileBuild, "build"), nil, 0o644)

	tests := []struct {
		name, project, artifact string
		ok                      bool
	}{
		{"build next to pubspec", app, "build", true},
		{"nested known artifact", app, "ios/Pods", true},
		{"root itself is the project", app, "build", true},
		{"unknown name", app, "lib", false},
		{"path traversal in name", app, "../app/build", false},
		{"project outside root", outside, "build", false},
		{"no pubspec.yaml", noPubspec, "build", false},
		{"missing artifact", app, ".dart_tool", false},
		{"symlinked parent", linked, "ios/Pods", false},
		{"symlinked artifact", linkedBuild, "build", false},
		{"artifact is a file", fileBuild, "build", false},
		{"artifact holds a pubspec.yaml", nested, "build", false},
		{"nested pubspec in build output is fine", spm, "build", true},
		{"relative project path", "app", "build", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := root
			if tt.name == "root itself is the project" {
				r = app
			}
			err := CheckArtifact(r, tt.project, tt.artifact)
			if (err == nil) != tt.ok {
				t.Errorf("CheckArtifact(%s, %s) err=%v, want ok=%v", tt.project, tt.artifact, err, tt.ok)
			}
		})
	}
}

func TestCheckCache(t *testing.T) {
	home := realTemp(t)
	gradle := filepath.Join(home, ".gradle", "caches")
	fvm := filepath.Join(home, "fvm", "versions")
	dists := filepath.Join(home, ".gradle", "wrapper", "dists")
	studio := filepath.Join(home, "Library", "Caches", "Google")
	for _, d := range []string{gradle, filepath.Join(fvm, "3.24.0"), filepath.Join(home, ".gradle", "other"),
		filepath.Join(dists, "gradle-8.3-all"), filepath.Join(studio, "AndroidStudio2024.1"), filepath.Join(studio, "AndroidStudio2025.1"),
		filepath.Join(home, "Library", "Application Support", "Google", "AndroidStudio2024.1")} {
		os.MkdirAll(d, 0o755)
	}
	os.Symlink(filepath.Join(fvm, "3.24.0"), filepath.Join(fvm, "stable"))
	os.Symlink(filepath.Join(dists, "gradle-8.3-all"), filepath.Join(dists, "gradle-8.4-all"))
	os.WriteFile(filepath.Join(dists, "gradle-file"), nil, 0o644)
	allowed := caches.Allowed{Exact: map[string]bool{gradle: true}, Children: []caches.Children{
		{Dir: fvm},
		{Dir: dists, Prefix: "gradle-"},
		{Dir: studio, Prefix: "AndroidStudio", Except: map[string]bool{"AndroidStudio2025.1": true}},
	}}

	tests := []struct {
		path string
		ok   bool
	}{
		{gradle, true},
		{filepath.Join(fvm, "3.24.0"), true},
		{filepath.Join(fvm, "stable"), false},        // symlink
		{fvm, false},                                 // the versions dir itself
		{filepath.Join(fvm, "3.24.0", "bin"), false}, // grandchild
		{filepath.Join(dists, "gradle-8.3-all"), true},
		{filepath.Join(dists, "gradle-8.4-all"), false}, // symlink
		{filepath.Join(dists, "gradle-file"), false},    // not a directory
		{dists, false},
		{filepath.Join(studio, "AndroidStudio2024.1"), true},
		{filepath.Join(studio, "AndroidStudio2025.1"), false}, // newest version
		{studio, false},
		{filepath.Join(home, "Library", "Application Support", "Google", "AndroidStudio2024.1"), false}, // settings, not caches
		{filepath.Join(home, ".gradle", "other"), false},                                                // not in the catalog
		{filepath.Join(home, ".gradle"), false},
		{home, false},
		{"relative/path", false},
	}
	for _, tt := range tests {
		if err := CheckCache(allowed, "", tt.path); (err == nil) != tt.ok {
			t.Errorf("CheckCache(%s) err=%v, want ok=%v", tt.path, err, tt.ok)
		}
	}
}

// The guard and Known agree on a real layout: older Android Studio caches are
// removable, the newest is not.
func TestCheckCacheWithKnownAndroidStudio(t *testing.T) {
	home := realTemp(t)
	studio := filepath.Join(home, "Library", "Caches", "Google")
	for _, v := range []string{"AndroidStudio2024.3", "AndroidStudio2025.1.1", "AndroidStudio2025.1.3"} {
		os.MkdirAll(filepath.Join(studio, v), 0o755)
	}
	allowed := caches.Known(caches.Env{Home: home, OS: "darwin", Getenv: func(string) string { return "" }})
	for v, ok := range map[string]bool{"AndroidStudio2024.3": true, "AndroidStudio2025.1.1": true, "AndroidStudio2025.1.3": false} {
		if err := CheckCache(allowed, "", filepath.Join(studio, v)); (err == nil) != ok {
			t.Errorf("CheckCache(%s) err=%v, want ok=%v", v, err, ok)
		}
	}
}

func TestKnownCatalogHasNoBroadPaths(t *testing.T) {
	home := "/home/u"
	env := caches.Env{Home: home, OS: "darwin", Getenv: func(string) string { return "" }}
	for p := range caches.Known(env).Exact {
		if p == home || p == "/" || filepath.Dir(p) == "/" || p == filepath.Join(home, "Library") {
			t.Errorf("catalog contains a broad path: %s", p)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	if err := CheckCommand([]string{"dart", "pub", "cache", "clean", "--force"}); err != nil {
		t.Error(err)
	}
	for _, c := range [][]string{{"rm", "-rf", "/"}, {"dart", "pub", "cache", "clean"}, {"xcrun", "simctl", "erase", "all"}} {
		if CheckCommand(c) == nil {
			t.Errorf("command %v allowed", c)
		}
	}
}

func TestDryRunRemovesNothing(t *testing.T) {
	root := realTemp(t)
	app := project(t, root, "app", "build")
	c := &Cleaner{Root: root, Mode: DryRun}
	if err := c.Artifact(app, "build"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(app, "build", "file")); err != nil {
		t.Errorf("dry run removed the artifact: %v", err)
	}
	// Dry run still enforces the guard.
	if err := c.Artifact(app, "lib"); err == nil {
		t.Error("dry run skipped the guard")
	}
}

func TestPermanentRemovesOnlyTheArtifact(t *testing.T) {
	root := realTemp(t)
	app := project(t, root, "app", "build", ".dart_tool")
	c := &Cleaner{Root: root, Mode: Permanent}
	if err := c.Artifact(app, "build"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(app, "build")); !os.IsNotExist(err) {
		t.Errorf("build still exists: %v", err)
	}
	for _, keep := range []string{"pubspec.yaml", ".dart_tool/file"} {
		if _, err := os.Stat(filepath.Join(app, keep)); err != nil {
			t.Errorf("%s was removed: %v", keep, err)
		}
	}
}

func TestPermanentDoesNotFollowSymlinksInside(t *testing.T) {
	root := realTemp(t)
	app := project(t, root, "app", "build")
	precious := filepath.Join(realTemp(t), "precious")
	os.WriteFile(precious, []byte("keep"), 0o644)
	os.Symlink(precious, filepath.Join(app, "build", "link"))

	c := &Cleaner{Root: root, Mode: Permanent}
	if err := c.Artifact(app, "build"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Errorf("symlink target was removed: %v", err)
	}
}

func TestHomeIsNeverAProject(t *testing.T) {
	root := realTemp(t)
	home := project(t, root, "home", "build")
	t.Setenv("HOME", home)
	if err := CheckArtifact(root, home, "build"); err == nil {
		t.Error("~/build allowed because of a stray ~/pubspec.yaml")
	}
}

func TestCheckCacheRefusesBroadOrRootContainingPaths(t *testing.T) {
	root := realTemp(t)
	dev := filepath.Join(root, "dev")
	os.MkdirAll(dev, 0o755)
	allowed := caches.Allowed{Exact: map[string]bool{root: true, "/": true}}
	if CheckCache(allowed, dev, root) == nil {
		t.Error("cache containing the scan root allowed")
	}
	if CheckCache(allowed, "", "/") == nil {
		t.Error("/ allowed")
	}
}

func TestGitTrackedArtifactIsRefused(t *testing.T) {
	root := realTemp(t)
	app := project(t, root, "app", "build/tools")
	os.WriteFile(filepath.Join(app, "build", "tools", "gen.dart"), []byte("x"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	if err := CheckArtifact(root, app, "build"); err == nil {
		t.Error("git-tracked build/ allowed")
	}
}
