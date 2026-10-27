package ui

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
	"github.com/Matthew-Eucaristo/flutterkill/internal/report"
)

// loaded returns a model that has scanned root and measured everything.
func loaded(t *testing.T, root string, mode clean.Mode) *Model {
	t.Helper()
	m := New(inventory.Options{Root: root}, &clean.Cleaner{Root: root, Mode: mode, Allowed: caches.Allowed{}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	_, cmd := m.Update(m.Init()())
	for cmd != nil {
		_, cmd = m.Update(cmd())
	}
	return m
}

// press sends keys and runs the resulting commands to completion, as the
// runtime would.
func press(m *Model, keys ...string) {
	for _, k := range keys {
		drain(m, m.key(k))
		m.scroll()
	}
}

func drain(m *Model, cmd tea.Cmd) {
	for cmd != nil {
		switch msg := cmd().(type) {
		case nil, tea.QuitMsg:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				drain(m, c)
			}
			return
		default:
			_, cmd = m.Update(msg)
		}
	}
}

func tally(m *Model) report.Tally { return report.Count(m.Outcomes()) }

func fixture(t *testing.T) string {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, p := range []string{"alpha/build", "alpha/.dart_tool", "beta/ios/Pods"} {
		os.MkdirAll(filepath.Join(root, p), 0o755)
		os.WriteFile(filepath.Join(root, p, "blob"), make([]byte, 8192), 0o644)
	}
	for _, p := range []string{"alpha", "beta"} {
		os.WriteFile(filepath.Join(root, p, "pubspec.yaml"), []byte("name: "+p+"\n"), 0o644)
	}
	return root
}

func TestRenderListsProjects(t *testing.T) {
	m := loaded(t, fixture(t), clean.Trash)
	out := ansi.Strip(m.render())
	for _, want := range []string{"flutterkill", "2 projects", "alpha", "beta", "Projects", "Global caches", "trash"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestSelectAllDryRunKeepsFiles(t *testing.T) {
	root := fixture(t)
	m := loaded(t, root, clean.DryRun)
	press(m, "a", "d")
	if len(m.confirm) != 3 {
		t.Fatalf("confirm = %v, want 3 artifacts", m.confirm)
	}
	press(m, "y")
	if r := tally(m); r.Items != 3 || r.Failed != 0 || r.Bytes[clean.Would] <= 0 {
		t.Errorf("result = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "build", "blob")); err != nil {
		t.Errorf("dry run removed files: %v", err)
	}
	if !strings.Contains(ansi.Strip(m.render()), "Would free") {
		t.Error("summary does not say it was a dry run")
	}
}

func TestDeleteCurrentProjectWithoutSelection(t *testing.T) {
	root := fixture(t)
	m := loaded(t, root, clean.Permanent)
	press(m, "o", "o") // sort by name: alpha first
	press(m, "d", "y")
	if _, err := os.Stat(filepath.Join(root, "alpha", "build")); !os.IsNotExist(err) {
		t.Errorf("alpha/build still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "beta", "ios", "Pods")); err != nil {
		t.Errorf("beta was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "pubspec.yaml")); err != nil {
		t.Errorf("pubspec.yaml removed: %v", err)
	}
}

func TestDeclineConfirmation(t *testing.T) {
	root := fixture(t)
	m := loaded(t, root, clean.Permanent)
	press(m, "a", "d", "n")
	if m.confirm != nil || len(m.Outcomes()) != 0 {
		t.Errorf("declined delete still ran: %+v", m.Outcomes())
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", "build")); err != nil {
		t.Error(err)
	}
}

func TestExpandAndSelectSingleArtifact(t *testing.T) {
	m := loaded(t, fixture(t), clean.DryRun)
	press(m, "o", "o", "l", "down", "space")
	if len(m.selected) != 1 {
		t.Errorf("selected = %v, want one artifact", m.selected)
	}
}

func TestDeleteOnlyReachesTheVisibleTab(t *testing.T) {
	root := fixture(t)
	m := loaded(t, root, clean.DryRun)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	os.MkdirAll(cacheDir, 0o755)
	m.inv.Caches = append(m.inv.Caches, &caches.Cache{ID: "fake", Name: "Fake cache", Path: cacheDir, Bytes: 5000})
	m.cleaner.Allowed = caches.Allowed{Exact: map[string]bool{cacheDir: true}}

	press(m, "2", "a", "1", "o", "o", "d") // select the cache, go back, delete current project
	for _, k := range m.confirm {
		if strings.HasPrefix(k, "cache:") {
			t.Fatalf("delete from Projects tab included a cache: %v", m.confirm)
		}
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, filepath.Join("alpha", "build")) || !strings.Contains(out, "would remove") {
		t.Errorf("confirmation does not list targets:\n%s", out)
	}
}

func TestDeleteBeforeMeasuringCountsBytes(t *testing.T) {
	root := fixture(t)
	m := New(inventory.Options{Root: root}, &clean.Cleaner{Root: root, Mode: clean.DryRun})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(m.Init()()) // loaded, sizes still pending
	press(m, "a", "d", "y")
	if r := tally(m); r.Items != 3 || r.Bytes[clean.Would] <= 0 {
		t.Errorf("result = %+v, want 3 items with bytes", r)
	}
}

func TestCtrlCDuringRemovalAsksTwice(t *testing.T) {
	m := loaded(t, fixture(t), clean.DryRun)
	m.status["x"] = working
	if cmd := m.key("ctrl+c"); cmd != nil || !m.quitWarn {
		t.Fatal("first ctrl+c quit while busy")
	}
	if cmd := m.key("ctrl+c"); cmd == nil {
		t.Fatal("second ctrl+c did not quit")
	}
}

func TestEmptyResults(t *testing.T) {
	m := loaded(t, t.TempDir(), clean.Permanent)
	press(m, "a", "space", "l", "h", "down", "G", "o", "d", "y", "tab", "a", "d", "y", "?", "x")
	if m.confirm != nil || len(m.Outcomes()) != 0 || m.busy() {
		t.Errorf("empty scan started something: confirm=%v outcomes=%v", m.confirm, m.Outcomes())
	}
	press(m, "1")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "No Dart/Flutter projects") {
		t.Errorf("no empty-state text:\n%s", out)
	}
}

func TestFailedRemovalIsShownAndTheQueueContinues(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := fixture(t)
	locked := filepath.Join(root, "alpha", "build", "locked")
	os.MkdirAll(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o644)
	os.Chmod(locked, 0o500) // its file cannot be unlinked
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	m := loaded(t, root, clean.Permanent)
	press(m, "o", "o", "a", "d", "y")
	if r := tally(m); r.Failed != 1 || r.Items != 2 {
		t.Fatalf("tally = %+v, want 1 failed and 2 removed", r)
	}
	if _, err := os.Stat(filepath.Join(root, "beta", "ios", "Pods")); !os.IsNotExist(err) {
		t.Errorf("the queue stopped at the failure: %v", err)
	}
	press(m, "l", "down") // alpha is first by name; its build/ failed
	out := ansi.Strip(m.render())
	for _, want := range []string{"✗", "permission denied", "1 refused/failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestNarrowAndShortTerminals(t *testing.T) {
	m := loaded(t, fixture(t), clean.DryRun)
	for _, size := range [][2]int{{1, 1}, {20, 5}, {40, 10}, {60, 12}, {200, 6}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, l := range strings.Split(m.render(), "\n") {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Errorf("%dx%d: line is %d cells wide: %q", size[0], size[1], w, ansi.Strip(l))
			}
		}
	}
	// One visible row: the cursor row must be the one shown.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	press(m, "o", "o", "G")
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "beta") || strings.Contains(out, "█") {
		t.Errorf("want only the cursor row (beta) and no bar at 60 columns:\n%s", out)
	}
}

func TestNoColorKeepsEveryStateReadable(t *testing.T) {
	// Bubble Tea downsamples output with the profile colorprofile picks from
	// the environment; NO_COLOR must mean no colours, attributes only.
	p := colorprofile.Env([]string{"TERM=xterm-256color", "NO_COLOR=1"})
	if p != colorprofile.Ascii {
		t.Fatalf("NO_COLOR profile = %v, want Ascii", p)
	}
	root := fixture(t)
	m := loaded(t, root, clean.DryRun)
	cacheDir := filepath.Join(t.TempDir(), "fvm")
	os.MkdirAll(cacheDir, 0o755)
	m.inv.Caches = append(m.inv.Caches, &caches.Cache{ID: "fvm:1", Name: "FVM Flutter 1", Path: cacheDir, Bytes: 5000, UsedBy: []string{"alpha"}})
	press(m, "o", "o", "l", "down", "d", "y", "down", "space") // alpha/build done, .dart_tool selected
	m.status[filepath.Join(root, "beta", "ios", "Pods")] = failed

	var buf strings.Builder
	w := &colorprofile.Writer{Forward: &buf, Profile: p}
	io.WriteString(w, m.render())
	press(m, "2")
	io.WriteString(w, m.render())
	out := buf.String()
	for _, sgr := range regexp.MustCompile(`\x1b\[([0-9;:]*)m`).FindAllStringSubmatch(out, -1) {
		for _, n := range strings.FieldsFunc(sgr[1], func(r rune) bool { return r == ';' || r == ':' }) {
			if c, _ := strconv.Atoi(n); (c >= 30 && c <= 49) || (c >= 90 && c <= 107) {
				t.Fatalf("colour code %q under NO_COLOR", sgr[0])
			}
		}
	}
	for _, want := range []string{"✓", "●", "○", "✗", "safe", "caution", "dry run"} {
		if !strings.Contains(ansi.Strip(out), want) {
			t.Errorf("without colour, %q is missing:\n%s", want, ansi.Strip(out))
		}
	}
}

func TestCancelMidRunStopsTheQueue(t *testing.T) {
	m := loaded(t, fixture(t), clean.DryRun)
	press(m, "a", "d")
	running := m.key("y") // first removal started, not finished
	if len(m.queue) != 2 || !m.working() {
		t.Fatalf("queue = %v, want 2 waiting behind 1 running", m.queue)
	}
	if cmd := m.key("ctrl+c"); cmd != nil {
		t.Fatal("first ctrl+c quit while a removal runs")
	}
	if len(m.queue) != 0 || len(m.selected) != 3 {
		t.Errorf("queue = %v, selected = %d; want queue cleared and the selection kept", m.queue, len(m.selected))
	}
	if !strings.Contains(ansi.Strip(m.render()), "Stopping after the current item") {
		t.Error("no stop notice")
	}
	drain(m, running)
	if len(m.Outcomes()) != 1 || m.busy() {
		t.Errorf("outcomes = %d, busy = %v; want only the running item done", len(m.Outcomes()), m.busy())
	}
	if cmd := m.key("ctrl+c"); cmd == nil {
		t.Error("ctrl+c did not quit once idle")
	}
}

func TestQuitKeyWhileBusyStopsInsteadOfQuitting(t *testing.T) {
	m := loaded(t, fixture(t), clean.DryRun)
	press(m, "a", "d")
	running := m.key("y")
	if cmd := m.key("q"); cmd != nil || !m.quitWarn || len(m.queue) != 0 {
		t.Fatal("q quit or kept the queue while a removal runs")
	}
	drain(m, running)
	if cmd := m.key("q"); cmd == nil {
		t.Error("q did not quit once idle")
	}
}
