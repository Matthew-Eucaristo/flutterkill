package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

func TestEmptyInventory(t *testing.T) {
	inv := &inventory.Inventory{Root: "/r", Projects: []*scan.Project{}, Caches: []*caches.Cache{}}
	var txt bytes.Buffer
	Text(&txt, inv)
	if !strings.Contains(txt.String(), "(none)") || !strings.Contains(txt.String(), "Reclaimable: 0 B") {
		t.Errorf("text:\n%s", txt.String())
	}
	var js bytes.Buffer
	if err := JSON(&js, inv); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if p, ok := doc["projects"].([]any); !ok || len(p) != 0 {
		t.Errorf("projects = %v, want []", doc["projects"])
	}
}

func TestReceiptKeepsKindsApart(t *testing.T) {
	outs := []clean.Outcome{
		{Path: "/r/a/build", How: clean.Trashed, Bytes: 2000, Freed: 0},
		{Name: "Pub cache", Path: "/h/.pub-cache", How: clean.Delegated, Command: "dart pub cache clean --force", Bytes: 5000, Freed: 4000},
		{Path: "/r/b/build", How: clean.Trashed, Bytes: 1000, Err: errors.New("permission denied")},
	}
	var b bytes.Buffer
	Receipt(&b, outs)
	out := b.String()
	for _, want := range []string{
		"trashed", "delegated", "(ran `dart pub cache clean --force`)", "failed", "permission denied",
		"Moved about 2.0 kB to the " + clean.TrashName + " (free space changed by +0 B, measured)",
		"Delegated about 5.0 kB", "changed by +4.0 kB",
		"1 item(s) failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("receipt missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Deleted") || strings.Contains(out, "Dry run") {
		t.Errorf("receipt mentions kinds that did not happen:\n%s", out)
	}
	var none bytes.Buffer
	Receipt(&none, nil)
	if none.Len() != 0 {
		t.Errorf("empty receipt printed %q", none.String())
	}
}
