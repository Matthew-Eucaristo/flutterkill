package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// mk creates dirs (trailing "/") and files under root.
func mk(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if p[len(p)-1] == '/' {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func names(p *Project) []string {
	var out []string
	for _, a := range p.Artifacts {
		out = append(out, a.Name)
	}
	return out
}

func find(t *testing.T, root string) map[string]*Project {
	t.Helper()
	ps, err := FindProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]*Project{}
	for _, p := range ps {
		rel, _ := filepath.Rel(root, p.Path)
		m[filepath.ToSlash(rel)] = p
	}
	return m
}

func TestFindProjectsDetectsArtifacts(t *testing.T) {
	root := t.TempDir()
	mk(t, root,
		"app/pubspec.yaml", "app/build/x", "app/.dart_tool/x", "app/ios/Pods/x", "app/ios/.symlinks/",
		"app/macos/Pods/", "app/android/.gradle/x", "app/android/app/.cxx/x", "app/lib/main.dart",
		"app/ios/Flutter/ephemeral/x", "app/macos/Flutter/ephemeral/.symlinks/", "app/linux/flutter/ephemeral/x",
		"app/windows/flutter/ephemeral/x", "app/windows/flutter/CMakeLists.txt",
	)
	os.WriteFile(filepath.Join(root, "app/pubspec.yaml"), []byte("name: my_app\ndependencies:\n  flutter:\n    sdk: flutter\n"), 0o644)

	p := find(t, root)["app"]
	if p == nil {
		t.Fatal("project not found")
	}
	if p.Name != "my_app" || !p.Flutter {
		t.Errorf("name=%q flutter=%v", p.Name, p.Flutter)
	}
	if got := names(p); !slices.Equal(got, ArtifactDirs) {
		t.Errorf("artifacts = %v, want %v", got, ArtifactDirs)
	}
}

func TestBuildOnlyCountsNextToPubspec(t *testing.T) {
	root := t.TempDir()
	mk(t, root,
		"app/pubspec.yaml", "app/lib/build/keep.dart", // lib/build is source, not an artifact
		"notdart/build/x", // no pubspec: never an artifact
		"app/.dart_tool/x",
	)
	ps := find(t, root)
	if _, ok := ps["notdart"]; ok {
		t.Error("directory without pubspec.yaml reported as a project")
	}
	if got := names(ps["app"]); !slices.Equal(got, []string{".dart_tool"}) {
		t.Errorf("artifacts = %v, want only .dart_tool", got)
	}
}

func TestProjectsWithoutArtifactsAreReturnedEmpty(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "clean/pubspec.yaml", "clean/lib/main.dart")
	p := find(t, root)["clean"]
	if p == nil || len(p.Artifacts) != 0 {
		t.Errorf("got %+v, want project with no artifacts", p)
	}
}

func TestSymlinkedArtifactIsIgnored(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "app/pubspec.yaml", "elsewhere/important/x", "elsewhere/Pods/x")
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "app/build")); err != nil {
		t.Fatal(err)
	}
	// A symlinked parent (app/ios -> elsewhere) must not expose ios/Pods either.
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "app/ios")); err != nil {
		t.Fatal(err)
	}
	if got := names(find(t, root)["app"]); len(got) != 0 {
		t.Errorf("symlinked artifacts reported: %v", got)
	}
}

func TestArtifactHoldingSourceIsIgnored(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "mono/pubspec.yaml", "mono/build/pubspec.yaml", "mono/.dart_tool/.git/HEAD",
		"spm/pubspec.yaml", "spm/build/ios/SourcePackages/plugin/example/pubspec.yaml") // SPM copies plugin sources into build/
	ps := find(t, root)
	if got := names(ps["mono"]); len(got) != 0 {
		t.Errorf("artifacts with source reported: %v", got)
	}
	if got := names(ps["spm"]); len(got) != 1 {
		t.Errorf("build/ with nested SPM packages not reported: %v", got)
	}
}

func TestFlutterSDKIsSkipped(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "sdk/bin/flutter", "sdk/packages/flutter_tools/pubspec.yaml", "sdk/packages/flutter_tools/.dart_tool/x")
	if ps := find(t, root); len(ps) != 0 {
		t.Errorf("SDK packages reported: %v", ps)
	}
}

func TestNestedProjectsAndSkippedTrees(t *testing.T) {
	root := t.TempDir()
	mk(t, root,
		"pkg/pubspec.yaml", "pkg/build/x",
		"pkg/example/pubspec.yaml", "pkg/example/.dart_tool/x", // nested example app
		"pkg/build/inner/pubspec.yaml", "pkg/build/inner/build/x", // inside an artifact: not scanned
		"web/node_modules/dep/pubspec.yaml", "web/node_modules/dep/build/x",
		".hidden/app/pubspec.yaml", ".hidden/app/build/x",
		"flutter/bin/internal/engine.version", "flutter/packages/flutter/pubspec.yaml", "flutter/packages/flutter/.dart_tool/x",
	)
	ps := find(t, root)
	var got []string
	for k, p := range ps {
		if len(p.Artifacts) == 0 {
			continue
		}
		got = append(got, k)
	}
	slices.Sort(got)
	if want := []string{"pkg", "pkg/example"}; !slices.Equal(got, want) {
		t.Errorf("projects = %v, want %v", got, want)
	}
}

func TestRootItselfCanBeAProject(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "pubspec.yaml", "build/x")
	if ps := find(t, root); ps["."] == nil {
		t.Errorf("root project not found: %v", ps)
	}
}

func TestFVMVersion(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "a/pubspec.yaml", "a/build/", "b/pubspec.yaml", "b/build/")
	os.WriteFile(filepath.Join(root, "a/.fvmrc"), []byte(`{"flutter":"3.24.5"}`), 0o644)
	mk(t, root, "b/.fvm/")
	os.WriteFile(filepath.Join(root, "b/.fvm/fvm_config.json"), []byte(`{"flutterSdkVersion":"stable"}`), 0o644)
	mk(t, root, "a/packages/core/pubspec.yaml") // inherits a/.fvmrc
	ps := find(t, root)
	if ps["a"].FVMVersion != "3.24.5" || ps["b"].FVMVersion != "stable" || ps["a/packages/core"].FVMVersion != "3.24.5" {
		t.Errorf("fvm versions: a=%q b=%q core=%q", ps["a"].FVMVersion, ps["b"].FVMVersion, ps["a/packages/core"].FVMVersion)
	}
}

func TestDirSizeCountsHardLinksOnceAndSkipsSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, 64*1024)
	os.WriteFile(filepath.Join(root, "a"), big, 0o644)
	os.Link(filepath.Join(root, "a"), filepath.Join(root, "b"))
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "huge"), make([]byte, 1<<20), 0o644)
	os.Symlink(outside, filepath.Join(root, "link"))

	n := DirSize(root)
	if runtime.GOOS == "windows" {
		n -= 64 * 1024 // hard links are not deduplicated there; only the symlink matters
	}
	if n < 64*1024 || n >= 2*64*1024 {
		t.Errorf("DirSize = %d, want one copy of 64 KiB", n)
	}
}

func TestLastActivityFallsBackToMtime(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "lib/main.dart", "build/new")
	old := time.Now().Add(-400 * 24 * time.Hour)
	os.Chtimes(filepath.Join(root, "lib/main.dart"), old, old)
	os.Chtimes(filepath.Join(root, "lib"), old, old)

	got, src := LastActivity(root)
	if src != "mtime" {
		t.Fatalf("source = %q, want mtime (no git repo)", src)
	}
	if got.Sub(old).Abs() > time.Second {
		t.Errorf("activity = %v, want %v (build/ must be ignored)", got, old)
	}
}

func TestLastActivityUsesUncommittedChanges(t *testing.T) {
	root := t.TempDir()
	mk(t, root, "app/pubspec.yaml", "app/build/x")
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0o644)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2024-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2024-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	app := filepath.Join(root, "app")

	got, src := LastActivity(app)
	if src != "git" || got.Year() != 2024 {
		t.Fatalf("clean repo: %v %q, want the 2024 commit", got, src)
	}
	// Ignored build output is not activity...
	os.WriteFile(filepath.Join(app, "build", "y"), []byte("y"), 0o644)
	if got, _ = LastActivity(app); got.Year() != 2024 {
		t.Errorf("ignored file counted: %v", got)
	}
	// ...but an uncommitted source file is.
	mk(t, root, "app/lib/new.dart")
	if got, _ = LastActivity(app); time.Since(got) > time.Minute {
		t.Errorf("uncommitted change ignored: %v", got)
	}
}

func TestGradleDist(t *testing.T) {
	root := t.TempDir()
	for dir, props := range map[string]string{
		"a": "#Gradle\ndistributionBase=GRADLE_USER_HOME\ndistributionUrl=https\\://services.gradle.org/distributions/gradle-8.3-all.zip\n",
		"b": "distributionUrl = https\\://mirror.example.com/gradle/gradle-7.6.3-bin.zip\n",
		"c": "distributionUrl=https\\://example.com/not-gradle.zip\n",
	} {
		mk(t, root, dir+"/pubspec.yaml")
		mk(t, root, dir+"/android/gradle/wrapper/")
		os.WriteFile(filepath.Join(root, dir, "android/gradle/wrapper/gradle-wrapper.properties"), []byte(props), 0o644)
	}
	mk(t, root, "d/pubspec.yaml")
	ps := find(t, root)
	if ps["a"].GradleDist != "gradle-8.3-all" || ps["b"].GradleDist != "gradle-7.6.3-bin" || ps["c"].GradleDist != "" || ps["d"].GradleDist != "" {
		t.Errorf("gradle dists: a=%q b=%q c=%q d=%q", ps["a"].GradleDist, ps["b"].GradleDist, ps["c"].GradleDist, ps["d"].GradleDist)
	}
}
