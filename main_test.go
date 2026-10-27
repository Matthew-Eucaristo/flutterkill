package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
)

func project(t *testing.T) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(root, "app", "build"), 0o755)
	os.WriteFile(filepath.Join(root, "app", "pubspec.yaml"), []byte("name: app\n"), 0o644)
	os.WriteFile(filepath.Join(root, "app", "build", "blob"), make([]byte, 4096), 0o644)
	return root
}

func exists(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "app", "build", "blob")); err != nil {
		t.Errorf("artifact was touched: %v", err)
	}
}

// A buffer is not a terminal, like a pipe or a redirect: the default is the
// plain table, never the TUI, and nothing is removed.
func TestNonTerminalOutputIsAPlainList(t *testing.T) {
	root := project(t)
	var out bytes.Buffer
	if err := run([]string{"--no-global", root}, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "PROJECTS in") || !strings.Contains(s, "app") || strings.Contains(s, "\x1b") {
		t.Errorf("output:\n%q", s)
	}
	exists(t, root)
}

func TestJSONIsReadOnly(t *testing.T) {
	root := project(t)
	var out bytes.Buffer
	if err := run([]string{"--json", "--no-global", root}, &out); err != nil {
		t.Fatal(err)
	}
	var doc struct{ Projects []struct{ Name string } }
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || len(doc.Projects) != 1 {
		t.Errorf("json = %s (%v)", out.String(), err)
	}
	exists(t, root)
}

func TestYesDryRunPrintsAReceiptAndKeepsFiles(t *testing.T) {
	root := project(t)
	var out bytes.Buffer
	if err := run([]string{"--yes", "--dry-run", root}, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "would remove") || !strings.Contains(s, "Nothing was changed") {
		t.Errorf("output:\n%s", s)
	}
	exists(t, root)
}

func TestYesPermanentRemovesAndMeasures(t *testing.T) {
	root := project(t)
	var out bytes.Buffer
	if err := run([]string{"--yes", "--permanent", root}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "app", "build")); !os.IsNotExist(err) {
		t.Errorf("build still exists: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "deleted") || !strings.Contains(s, "(measured)") {
		t.Errorf("output:\n%s", s)
	}
}

func TestBadFlags(t *testing.T) {
	for _, args := range [][]string{{"--dry-run", "--permanent"}, {"a", "b"}, {"--older-than", "soon"}, {"--nope"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

func TestYesStopsWhenCancelled(t *testing.T) {
	root := project(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ctrl+c before the first item
	c := &clean.Cleaner{Root: root, Mode: clean.Permanent}
	err := cleanAll(ctx, &bytes.Buffer{}, inventory.Options{Root: root}, c)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("err = %v, want interrupted", err)
	}
	exists(t, root)
}
