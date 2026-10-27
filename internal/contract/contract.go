// Package contract is flutterkill's stable machine interface (schema_version
// 1), in the same envelope as the other tools of the family (fell, nodekill):
// capabilities, the scan document, plans and apply results, so an umbrella
// such as decruft can show what flutterkill sees and hand removals back to it.
// docs/contract.md describes it. Fields may be added; existing fields and
// codes are never renamed or repurposed within a schema version.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// SchemaVersion of every document flutterkill emits or accepts.
const SchemaVersion = 1

// Tool identifies the emitter.
type Tool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

// NewTool describes flutterkill at version v.
func NewTool(v string) Tool { return Tool{Name: "flutterkill", Version: v, Ecosystem: "flutter"} }

// Level says what may be done with an item.
type Level string

const (
	Safe      Level = "safe"      // regenerable; may be bulk-selected
	Review    Level = "review"    // allowed when chosen explicitly
	Protected Level = "protected" // never acted on
	Live      Level = "live"      // in use right now; never acted on
)

// Allowed reports whether a plan may act on an item at level l.
func (l Level) Allowed() bool { return l == Safe || l == Review }

// Reason codes, part of the contract.
const (
	ReasonGenerated = "generated"
	ReasonGlobal    = "global_cache"
	ReasonUsedBy    = "used_by_projects"
	ReasonDeletes   = "deletes"
)

// ReasonInfo documents a reason code in capabilities.
type ReasonInfo struct {
	Code   string `json:"code"`
	Level  Level  `json:"level"`
	Effect string `json:"effect"`
}

// Reasons lists every code flutterkill emits.
var Reasons = []ReasonInfo{
	{ReasonGenerated, Safe, "Generated output at a fixed position next to a pubspec.yaml, with no git-tracked files; the next build or pub get recreates it."},
	{ReasonGlobal, Review, "Shared by every project on this machine; recreated when needed, but the next build is slower."},
	{ReasonUsedBy, Review, "A scanned project uses this version; it is downloaded again by that project's next build."},
	{ReasonDeletes, Review, "Removed by the owning tool's own command, which deletes instead of using the trash."},
}

type Reason struct {
	Code   string `json:"code"`
	Level  Level  `json:"level"`
	Detail string `json:"detail"`
}

type Safety struct {
	Level   Level    `json:"level"`
	Reasons []Reason `json:"reasons"`
}

// Bytes is a measured size. flutterkill counts allocated blocks like du and
// hard links once; it does not separate blocks shared with APFS clones, so
// exclusive equals allocated.
type Bytes struct {
	Allocated int64 `json:"allocated"`
	Shared    int64 `json:"shared"`
	Exclusive int64 `json:"exclusive"`
	Files     int64 `json:"file_count"`
}

// Time is a timestamp with its source and age.
type Time struct {
	At      time.Time `json:"at"`
	Source  string    `json:"source"`
	AgeDays int       `json:"age_days"`
}

// Project describes the pubspec.yaml directory an artifact belongs to.
type Project struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Flutter    bool   `json:"flutter"`
	FVMVersion string `json:"fvm_version,omitempty"`
	Activity   *Time  `json:"last_activity"`
}

// Item is one removable thing.
type Item struct {
	ID          string   `json:"id"`
	Fingerprint string   `json:"fingerprint"`
	Kind        string   `json:"kind"`
	Category    string   `json:"category"` // project, cache or runtime
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Project     *Project `json:"project"`
	Tool        string   `json:"tool,omitempty"` // owner of a machine-wide item
	Action      string   `json:"action"`         // trash or command
	Command     []string `json:"command,omitempty"`
	Regenerate  string   `json:"regenerate"`
	Explain     string   `json:"explain"`
	Note        string   `json:"note,omitempty"`
	UsedBy      []string `json:"used_by,omitempty"`
	Measured    bool     `json:"measured"`
	Bytes       Bytes    `json:"bytes"`
	Safety      Safety   `json:"safety"`
}

// Entry is an item plus what apply needs to act on it.
type Entry struct {
	Item
	Root       string        // scan root the item was found under
	ProjectDir string        // artifacts: the pubspec.yaml directory
	Artifact   string        // artifacts: name relative to ProjectDir
	Cache      *caches.Cache // machine-wide items
}

type artifactInfo struct{ explain, regenerate string }

var artifacts = map[string]artifactInfo{
	"build":                     {"Flutter build output for every platform.", "the next flutter build or flutter run"},
	".dart_tool":                {"Dart tool state: package config, build_runner and analyzer caches.", "flutter pub get, which the next build runs"},
	"ios/Pods":                  {"CocoaPods dependencies of the iOS app.", "pod install, which the next iOS build runs"},
	"ios/.symlinks":             {"Links to plugin sources that Flutter makes for CocoaPods.", "flutter pub get, which the next build runs"},
	"macos/Pods":                {"CocoaPods dependencies of the macOS app.", "pod install, which the next macOS build runs"},
	"android/.gradle":           {"Gradle's per-project state and caches.", "the next Android build"},
	"android/app/.cxx":          {"Native (CMake/NDK) build intermediates.", "the next Android build"},
	"ios/Flutter/ephemeral":     {"Generated iOS glue that flutter clean removes.", "flutter pub get, which the next build runs"},
	"macos/Flutter/ephemeral":   {"Generated macOS glue that flutter clean removes.", "flutter pub get, which the next build runs"},
	"linux/flutter/ephemeral":   {"Generated Linux glue that flutter clean removes.", "flutter pub get, which the next build runs"},
	"windows/flutter/ephemeral": {"Generated Windows glue that flutter clean removes.", "flutter pub get, which the next build runs"},
}

// cacheKind is a cache's kind: its catalog ID, without the version of a
// per-version group (fvm:3.24.0 → fvm).
func cacheKind(id string) string {
	k, _, _ := strings.Cut(id, ":")
	return k
}

type cacheInfo struct{ owner, category, regenerate string }

var cacheKinds = map[string]cacheInfo{
	"pub-cache":              {"Dart", "cache", "the next pub get"},
	"dart-server":            {"Dart", "cache", "opening a project in your editor"},
	"gradle-caches":          {"Gradle", "cache", "the next Android build"},
	"gradle-jdks":            {"Gradle", "cache", "the next build that needs that JDK"},
	"gradle-dist":            {"Gradle", "runtime", "the next Android build that uses this version"},
	"android-studio":         {"Android Studio", "cache", "running that Android Studio version again"},
	"fvm":                    {"FVM", "runtime", "fvm install"},
	"xcode-derived-data":     {"Xcode", "cache", "the next iOS or macOS build"},
	"xcode-device-support":   {"Xcode", "cache", "connecting that device again"},
	"cocoapods-cache":        {"CocoaPods", "cache", "the next pod install"},
	"swiftpm-cache":          {"Swift Package Manager", "cache", "the next Xcode build"},
	"simulator-caches":       {"Xcode", "cache", "booting a simulator"},
	"simulators-unavailable": {"Xcode", "cache", "nothing: these simulators can no longer boot"},
}

// Entries turns an inventory into contract items. Unmeasured sizes (-1) are
// reported with measured false.
func Entries(inv *inventory.Inventory, home string, now time.Time) []*Entry {
	var out []*Entry
	for _, p := range inv.Projects {
		for _, a := range p.Artifacts {
			out = append(out, artifactEntry(inv.Root, p, a, home, now))
		}
	}
	for _, c := range inv.Caches {
		out = append(out, cacheEntry(inv.Root, c, home))
	}
	return out
}

func artifactEntry(root string, p *scan.Project, a scan.Artifact, home string, now time.Time) *Entry {
	info := artifacts[a.Name]
	e := &Entry{Root: root, ProjectDir: p.Path, Artifact: a.Name, Item: Item{
		ID: id("art", a.Path), Kind: a.Name, Category: "project", Name: a.Name, Path: format.Tilde(a.Path, home),
		Action: "trash", Explain: info.explain, Regenerate: info.regenerate,
		Project: &Project{Path: format.Tilde(p.Path, home), Name: p.Name, Flutter: p.Flutter, FVMVersion: p.FVMVersion,
			Activity: stamp(p.LastActivity, p.ActivitySource, now)},
		Safety: Safety{Level: Safe, Reasons: []Reason{{ReasonGenerated, Safe, "regenerated by " + info.regenerate}}},
	}}
	e.measure(a.Bytes)
	e.Fingerprint = fingerprint(e.Item, a.Path, nil)
	return e
}

func cacheEntry(root string, c *caches.Cache, home string) *Entry {
	kind := cacheKind(c.ID)
	info := cacheKinds[kind]
	if info.category == "" {
		info.category = "cache"
	}
	e := &Entry{Root: root, Cache: c, Item: Item{
		ID: id("glb", c.ID), Kind: kind, Category: info.category, Name: c.Name, Tool: info.owner,
		Action: "trash", Explain: c.Note, Regenerate: info.regenerate, UsedBy: c.UsedBy,
		Safety: Safety{Level: Review, Reasons: []Reason{{ReasonGlobal, Review, "shared by every project on this machine"}}},
	}}
	if c.Path != "" {
		e.Path = format.Tilde(c.Path, home)
	}
	if len(c.UsedBy) > 0 {
		e.Safety.Reasons = append(e.Safety.Reasons, Reason{ReasonUsedBy, Review, "used by " + strings.Join(c.UsedBy, ", ")})
	}
	if len(c.Command) > 0 {
		e.Action, e.Command = "command", c.Command
		e.Safety.Reasons = append(e.Safety.Reasons, Reason{ReasonDeletes, Review, "`" + strings.Join(c.Command, " ") + "` deletes; it does not use the trash"})
	}
	e.measure(c.Bytes)
	e.Fingerprint = fingerprint(e.Item, c.Path, c.SizePaths)
	return e
}

func (e *Entry) measure(n int64) {
	if n >= 0 {
		e.Measured, e.Bytes = true, Bytes{Allocated: n, Exclusive: n}
	}
}

func id(prefix, key string) string {
	h := sha256.Sum256([]byte(key))
	return prefix + "-" + hex.EncodeToString(h[:6])
}

// fingerprint hashes the safety-relevant state: kind, path, the directory's
// identity, level, reasons and command, plus the folders a command removes
// (so a simulator that became unavailable after the scan changes it). Sizes
// and times are left out, so a build writing into the folder keeps the plan.
func fingerprint(it Item, path string, extra []string) string {
	var codes []string
	for _, r := range it.Safety.Reasons {
		codes = append(codes, r.Code)
	}
	slices.Sort(codes)
	extra = slices.Sorted(slices.Values(extra))
	dev, ino := scan.Identity(path)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s", it.ID, it.Kind, strings.ToLower(filepath.Clean(path)),
		dev, ino, it.Safety.Level, strings.Join(codes, ","), it.Action, strings.Join(it.Command, "\x01"), strings.Join(extra, "\x01"))
	return "fp-" + hex.EncodeToString(h.Sum(nil)[:8])
}

func stamp(t time.Time, src string, now time.Time) *Time {
	if t.IsZero() {
		return nil
	}
	return &Time{At: t.UTC(), Source: src, AgeDays: int(now.Sub(t).Hours() / 24)}
}

// Summary totals a scan.
type Summary struct {
	Items      int            `json:"items"`
	Projects   int            `json:"projects"`
	Bytes      Bytes          `json:"bytes"`
	SafeBytes  Bytes          `json:"safe_bytes"`
	ByLevel    map[string]int `json:"by_safety_level"`
	ByCategory map[string]int `json:"by_category"`
}

// ScanDoc is `flutterkill scan --json`.
type ScanDoc struct {
	SchemaVersion int       `json:"schema_version"`
	Tool          Tool      `json:"tool"`
	GeneratedAt   time.Time `json:"generated_at"`
	Roots         []string  `json:"roots"`
	Warnings      []string  `json:"warnings"`
	SizeNote      string    `json:"size_note"`
	Summary       Summary   `json:"summary"`
	Items         []Item    `json:"items"`
}

// SizeNote explains how bytes are counted.
const SizeNote = "allocated blocks like du; hard links counted once; blocks shared with APFS clones are not separated, so exclusive = allocated; freed space is measured after each action"

// NewScanDoc builds the scan document.
func NewScanDoc(tool Tool, roots, warnings []string, projects int, entries []*Entry, now time.Time) ScanDoc {
	doc := ScanDoc{SchemaVersion: SchemaVersion, Tool: tool, GeneratedAt: now.UTC(), Roots: nonNil(roots),
		Warnings: nonNil(warnings), SizeNote: SizeNote, Items: []Item{}}
	for _, e := range entries {
		doc.Items = append(doc.Items, e.Item)
	}
	doc.Summary = Summarize(doc.Items)
	doc.Summary.Projects = projects
	return doc
}

// Summarize totals items.
func Summarize(items []Item) Summary {
	s := Summary{Items: len(items), ByLevel: map[string]int{}, ByCategory: map[string]int{}}
	add := func(a *Bytes, b Bytes) {
		a.Allocated += b.Allocated
		a.Shared += b.Shared
		a.Exclusive += b.Exclusive
		a.Files += b.Files
	}
	for _, it := range items {
		add(&s.Bytes, it.Bytes)
		if it.Safety.Level == Safe {
			add(&s.SafeBytes, it.Bytes)
		}
		s.ByLevel[string(it.Safety.Level)]++
		s.ByCategory[it.Category]++
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Encode writes v as indented JSON.
func Encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Plan is what a caller asks `flutterkill apply` to do. The action follows
// from the item: its official command, or a move to the trash.
type Plan struct {
	SchemaVersion int        `json:"schema_version"`
	Items         []PlanItem `json:"items"`
}

// PlanItem selects one item, pinned to its scan fingerprint.
type PlanItem struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

// ReadPlan parses and validates a plan. Unknown fields are rejected so a
// plan cannot smuggle options; consent comes only from command-line flags.
func ReadPlan(r io.Reader) (*Plan, error) {
	var p Plan
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if p.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("plan: schema_version %d is not supported (want %d)", p.SchemaVersion, SchemaVersion)
	}
	if len(p.Items) == 0 {
		return nil, errors.New("plan: no items")
	}
	for i, it := range p.Items {
		if it.ID == "" || it.Fingerprint == "" {
			return nil, fmt.Errorf("plan: item %d needs id and fingerprint", i)
		}
	}
	return &p, nil
}

// Global reports whether the plan names a machine-wide item, so apply only
// discovers caches when it needs them.
func (p *Plan) Global() bool {
	for _, it := range p.Items {
		if strings.HasPrefix(it.ID, "glb-") {
			return true
		}
	}
	return false
}

// Exit codes, part of the contract.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitRefused = 3 // at least one item refused, none failed
	ExitPartial = 4 // at least one item failed
)

// Capabilities is `flutterkill capabilities --json`.
type Capabilities struct {
	SchemaVersion int               `json:"schema_version"`
	Tool          Tool              `json:"tool"`
	Commands      map[string]string `json:"commands"`
	Actions       []ActionInfo      `json:"actions"`
	Categories    []string          `json:"categories"`
	Levels        []Level           `json:"safety_levels"`
	Reasons       []ReasonInfo      `json:"reason_codes"`
	Artifacts     []KindInfo        `json:"artifact_kinds"`
	Caches        []KindInfo        `json:"cache_kinds"`
	ExitCodes     map[string]int    `json:"exit_codes"`
	Trash         string            `json:"trash"`
}

type ActionInfo struct {
	Name        string `json:"name"`
	Reversible  bool   `json:"reversible"`
	Description string `json:"description"`
}

type KindInfo struct {
	Kind       string `json:"kind"`
	Level      Level  `json:"level"`
	Owner      string `json:"owner,omitempty"`
	Regenerate string `json:"regenerate"`
}

// NewCapabilities describes what this build can do.
func NewCapabilities(tool Tool, trash string) Capabilities {
	c := Capabilities{
		SchemaVersion: SchemaVersion, Tool: tool, Trash: trash,
		Commands: map[string]string{
			"scan":         "flutterkill scan --json [--no-global] [root...]",
			"apply":        "flutterkill apply --plan <file|-> [--dry-run] [--permanent] [--json] [root...]",
			"capabilities": "flutterkill capabilities --json",
		},
		Actions: []ActionInfo{
			{"trash", true, "Move a directory to the " + trash + "."},
			{"delete", false, "With --permanent: delete the directory instead."},
			{"command", false, "Run the owning tool's own clean-up (" + commandList() + "); it deletes by its own design."},
		},
		Categories: []string{"project", "cache", "runtime"},
		Levels:     []Level{Safe, Review, Protected, Live},
		Reasons:    Reasons,
		ExitCodes:  map[string]int{"ok": ExitOK, "error": ExitError, "usage": ExitUsage, "refused": ExitRefused, "partial": ExitPartial},
	}
	for _, a := range scan.ArtifactDirs {
		c.Artifacts = append(c.Artifacts, KindInfo{Kind: a, Level: Safe, Regenerate: artifacts[a].regenerate})
	}
	for _, k := range slices.Sorted(maps.Keys(cacheKinds)) {
		info := cacheKinds[k]
		c.Caches = append(c.Caches, KindInfo{Kind: k, Level: Review, Owner: info.owner, Regenerate: info.regenerate})
	}
	return c
}

func commandList() string {
	var s []string
	for _, c := range caches.Commands {
		s = append(s, strings.Join(c, " "))
	}
	return strings.Join(s, ", ")
}
