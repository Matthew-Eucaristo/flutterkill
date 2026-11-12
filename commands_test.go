package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/contract"
)

func scanDoc(t *testing.T, args ...string) contract.ScanDoc {
	t.Helper()
	var out bytes.Buffer
	if err := run(append([]string{"scan", "--json"}, args...), &out); err != nil {
		t.Fatal(err)
	}
	var doc contract.ScanDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return doc
}

func plan(items ...contract.Item) string {
	p := contract.Plan{SchemaVersion: contract.SchemaVersion}
	for _, it := range items {
		p.Items = append(p.Items, contract.PlanItem{ID: it.ID, Fingerprint: it.Fingerprint})
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// apply runs `apply --json` with the plan on stdin and returns the document
// and the contract exit code.
func apply(t *testing.T, planJSON string, args ...string) (contract.ApplyDoc, int) {
	t.Helper()
	stdin = strings.NewReader(planJSON)
	t.Cleanup(func() { stdin = os.Stdin })
	var out bytes.Buffer
	err := run(append([]string{"apply", "--plan", "-", "--json"}, args...), &out)
	code := 0
	var ee *exitError
	switch {
	case errors.As(err, &ee):
		code = ee.code
	case err != nil:
		t.Fatal(err)
	}
	var doc contract.ApplyDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return doc, code
}

func TestCapabilities(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"capabilities", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	var c contract.Capabilities
	if err := json.Unmarshal(out.Bytes(), &c); err != nil || c.SchemaVersion != 1 || c.Tool.Name != "flutterkill" || len(c.Artifacts) == 0 || len(c.Reasons) == 0 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestScanJSONListsArtifactsReadOnly(t *testing.T) {
	root := project(t)
	doc := scanDoc(t, "--no-global", root)
	if doc.SchemaVersion != 1 || len(doc.Items) != 1 || doc.Summary.Projects != 1 {
		t.Fatalf("%+v", doc)
	}
	it := doc.Items[0]
	if !strings.HasPrefix(it.ID, "art-") || !strings.HasPrefix(it.Fingerprint, "fp-") || it.Kind != "build" || it.Action != "trash" ||
		it.Safety.Level != contract.Safe || !it.Measured || it.Bytes.Allocated <= 0 || it.Project == nil || it.Project.Name != "app" {
		t.Fatalf("%+v", it)
	}
	if again := scanDoc(t, "--no-global", root); again.Items[0].Fingerprint != it.Fingerprint {
		t.Fatal("fingerprint is not stable across scans")
	}
	exists(t, root)
}

func TestApplyDryRunChangesNothing(t *testing.T) {
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	doc, code := apply(t, plan(it), "--dry-run", root)
	if code != 0 || doc.Summary.DryRun != 1 || doc.Results[0].Status != contract.StatusDryRun {
		t.Fatalf("code %d %+v", code, doc)
	}
	exists(t, root)
}

func TestApplyPermanentRemoves(t *testing.T) {
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	doc, code := apply(t, plan(it), "--permanent", root)
	if code != 0 || doc.Results[0].Status != contract.StatusDone || doc.Results[0].Action != "delete" || doc.Summary.Deleted <= 0 {
		t.Fatalf("code %d %+v", code, doc)
	}
	if _, err := os.Stat(filepath.Join(root, "app", "build")); !os.IsNotExist(err) {
		t.Fatalf("build still exists: %v", err)
	}
}

func TestApplyRefusesStaleAndUnknownItems(t *testing.T) {
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	stale := it
	stale.Fingerprint = "fp-0000000000000000"
	gone := contract.Item{ID: "art-000000000000", Fingerprint: "fp-1"}
	doc, code := apply(t, plan(stale, gone), "--permanent", root)
	if code != contract.ExitRefused || doc.Summary.Refused != 2 || doc.Results[0].Reasons[0] != "stale" || doc.Results[1].Reasons[0] != "gone" {
		t.Fatalf("code %d %+v", code, doc)
	}
	exists(t, root)
}

// A folder swapped for a symlink after the scan is not the folder the user
// saw: the fresh scan does not report it, so the plan is refused.
func TestApplyRefusesASymlinkSwappedInAfterTheScan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	build := filepath.Join(root, "app", "build")
	elsewhere := filepath.Join(root, "precious")
	if err := os.Rename(build, elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, build); err != nil {
		t.Fatal(err)
	}
	doc, code := apply(t, plan(it), "--permanent", root)
	if code != contract.ExitRefused || doc.Results[0].Status != contract.StatusRefused {
		t.Fatalf("code %d %+v", code, doc)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "blob")); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

// Files committed to git after the scan are real work, not build output.
func TestApplyRefusesAnArtifactThatBecameTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	app := filepath.Join(root, "app")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-f", "build/blob"}} {
		if out, err := exec.Command("git", append([]string{"-C", app}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	doc, code := apply(t, plan(it), "--permanent", root)
	if code != contract.ExitRefused || doc.Results[0].Status != contract.StatusRefused {
		t.Fatalf("code %d %+v", code, doc)
	}
	exists(t, root)
}

func TestGlobalCachesAreReviewAndPinned(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	server := filepath.Join(home, ".dartServer")
	os.MkdirAll(server, 0o755)
	os.WriteFile(filepath.Join(server, "index"), make([]byte, 4096), 0o644)
	defaultEnv = func() caches.Env {
		return caches.Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }} // linux: no xcrun lookups
	}
	t.Cleanup(func() { defaultEnv = caches.DefaultEnv })
	root := project(t)

	var cache contract.Item
	for _, it := range scanDoc(t, root).Items {
		if it.Kind == "dart-server" {
			cache = it
		}
	}
	if cache.ID == "" || !strings.HasPrefix(cache.ID, "glb-") || cache.Safety.Level != contract.Review || cache.Tool != "Dart" {
		t.Fatalf("%+v", cache)
	}
	doc, code := apply(t, plan(cache), "--permanent", root)
	if code != 0 || doc.Results[0].Status != contract.StatusDone {
		t.Fatalf("code %d %+v", code, doc)
	}
	if _, err := os.Stat(server); !os.IsNotExist(err) {
		t.Fatalf("cache still exists: %v", err)
	}
}

// A per-version Gradle item nothing scanned uses is safe; a used one, or one
// whose daemon ran recently, stays review. A plan made while a version was
// unused is stale once a project starts using it.
func TestGradleVersionLevels(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	gradle := filepath.Join(home, ".gradle")
	for _, d := range []string{"caches/8.3", "caches/9.4.1", "caches/9.3.1", "caches/modules-2", "caches/foo",
		"wrapper/dists/gradle-8.3-all", "wrapper/dists/gradle-9.3.1-all", "daemon/9.4.1"} {
		os.MkdirAll(filepath.Join(gradle, filepath.FromSlash(d)), 0o755)
	}
	os.WriteFile(filepath.Join(gradle, "daemon", "9.4.1", "registry.bin"), []byte("x"), 0o644)
	defaultEnv = func() caches.Env {
		return caches.Env{Home: home, OS: "linux", Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { defaultEnv = caches.DefaultEnv })
	root := project(t)
	wrapper := func(dir, dist string) {
		os.MkdirAll(filepath.Join(root, dir, "gradle", "wrapper"), 0o755)
		os.WriteFile(filepath.Join(root, dir, "gradle", "wrapper", "gradle-wrapper.properties"),
			[]byte("distributionUrl=https\\://services.gradle.org/distributions/"+dist+".zip\n"), 0o644)
	}
	wrapper("app/android", "gradle-8.3-all") // the Flutter project

	byName := func() map[string]contract.Item {
		m := map[string]contract.Item{}
		for _, it := range scanDoc(t, root).Items {
			m[it.Name] = it
		}
		return m
	}
	items := byName()
	for name, want := range map[string]struct {
		kind  string
		level contract.Level
		code  string
	}{
		"Gradle 8.3 caches":       {"gradle-version", contract.Review, contract.ReasonUsedBy},
		"Gradle 8.3-all":          {"gradle-dist", contract.Review, contract.ReasonUsedBy},
		"Gradle 9.3.1 caches":     {"gradle-version", contract.Safe, contract.ReasonUnused},
		"Gradle 9.3.1-all":        {"gradle-dist", contract.Safe, contract.ReasonUnused},
		"Gradle 9.4.1 caches":     {"gradle-version", contract.Review, contract.ReasonDaemon},
		"Gradle dependency cache": {"gradle-caches", contract.Review, contract.ReasonGlobal},
	} {
		it, ok := items[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		var codes []string
		for _, r := range it.Safety.Reasons {
			codes = append(codes, r.Code)
		}
		if it.Kind != want.kind || it.Safety.Level != want.level || !slices.Contains(codes, want.code) {
			t.Errorf("%s: kind %s level %s reasons %v, want %+v", name, it.Kind, it.Safety.Level, codes, want)
		}
	}
	for name := range items {
		if strings.Contains(name, "foo") || name == "Gradle caches" {
			t.Errorf("unexpected item %s", name)
		}
	}

	// A native Android project under the root starts using 9.3.1.
	unused := items["Gradle 9.3.1 caches"]
	wrapper("native_app", "gradle-9.3.1-bin")
	if it := byName()["Gradle 9.3.1 caches"]; it.Safety.Level != contract.Review || it.Fingerprint == unused.Fingerprint {
		t.Fatalf("after the pin: %+v", it.Safety)
	}
	doc, code := apply(t, plan(unused), "--permanent", root)
	if code != contract.ExitRefused || doc.Results[0].Reasons[0] != "stale" {
		t.Fatalf("code %d %+v", code, doc)
	}
	if _, err := os.Stat(filepath.Join(gradle, "caches", "9.3.1")); err != nil {
		t.Fatalf("version cache touched: %v", err)
	}
	// The -all distribution is still unused (native_app downloads -bin).
	if doc, code := apply(t, plan(byName()["Gradle 9.3.1-all"]), "--dry-run", root); code != 0 || doc.Results[0].Status != contract.StatusDryRun {
		t.Fatalf("code %d %+v", code, doc)
	}
}

func TestContractUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		plan string
	}{
		{[]string{"scan"}, ""},
		{[]string{"capabilities"}, ""},
		{[]string{"apply"}, ""},
		{[]string{"apply", "--plan", "-"}, `{"schema_version":1,"items":[{"id":"a","fingerprint":"b","force":true}]}`},
		{[]string{"apply", "--plan", "-"}, `{"schema_version":2,"items":[{"id":"a","fingerprint":"b"}]}`},
		{[]string{"apply", "--plan", "-"}, `{"schema_version":1,"items":[]}`},
		{[]string{"apply", "--plan", "-", "--dry-run", "--permanent"}, `{"schema_version":1,"items":[{"id":"a","fingerprint":"b"}]}`},
	} {
		stdin = strings.NewReader(tc.plan)
		err := run(tc.args, &bytes.Buffer{})
		var ee *exitError
		if !errors.As(err, &ee) || ee.code != contract.ExitUsage {
			t.Errorf("%v %s: err = %v, want usage", tc.args, tc.plan, err)
		}
	}
	stdin = os.Stdin
}

func TestApplyTextKeepsKindsApart(t *testing.T) {
	root := project(t)
	it := scanDoc(t, "--no-global", root).Items[0]
	stdin = strings.NewReader(plan(it))
	t.Cleanup(func() { stdin = os.Stdin })
	var out bytes.Buffer
	if err := run([]string{"apply", "--plan", "-", "--permanent", root}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "deleted; free space grew by") || !strings.Contains(s, "(measured)") || strings.Contains(s, "moved to the") {
		t.Errorf("text receipt:\n%s", s)
	}
}
