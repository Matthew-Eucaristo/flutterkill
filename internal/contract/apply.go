package contract

import (
	"context"
	"fmt"
	"strings"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
)

// Result is what apply did with one plan item.
type Result struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind,omitempty"`
	Action      string   `json:"action,omitempty"` // trash, delete or command
	Path        string   `json:"path,omitempty"`
	Command     []string `json:"command,omitempty"`
	Status      string   `json:"status"` // done, dry_run, refused or failed
	Estimated   int64    `json:"estimated_bytes"`
	Freed       int64    `json:"freed_bytes_measured"` // -1 unknown
	RestoreHint string   `json:"restore_hint,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
	Error       string   `json:"error,omitempty"`
}

const (
	StatusDone    = "done"
	StatusDryRun  = "dry_run"
	StatusRefused = "refused"
	StatusFailed  = "failed"
)

// ApplyDoc is `flutterkill apply --json`.
type ApplyDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Tool          Tool         `json:"tool"`
	Mode          string       `json:"mode"`
	Results       []Result     `json:"results"`
	Summary       ApplySummary `json:"summary"`
}

type ApplySummary struct {
	Done     int   `json:"done"`
	DryRun   int   `json:"dry_run"`
	Refused  int   `json:"refused"`
	Failed   int   `json:"failed"`
	Trashed  int64 `json:"trashed_bytes"`   // moved to the trash: freed when it is emptied
	ByTool   int64 `json:"command_bytes"`   // removed by official commands
	Deleted  int64 `json:"deleted_bytes"`   // deleted with --permanent
	Estimate int64 `json:"estimated_bytes"` // all of the above, by the scan
	Freed    int64 `json:"freed_bytes_measured"`
}

// ExitCode for an apply.
func (d ApplyDoc) ExitCode() int {
	switch {
	case d.Summary.Failed > 0:
		return ExitPartial
	case d.Summary.Refused > 0:
		return ExitRefused
	}
	return ExitOK
}

// Applier runs plans against a fresh scan.
type Applier struct {
	Mode    clean.Mode
	Allowed caches.Allowed
}

// Apply acts on the plan's items one at a time. Each one is looked up in
// fresh (a scan made just now), refused unless its fingerprint still matches
// and its level allows it, checked by clean's guards, and only then removed;
// clean runs the same guards again right before removal. Items left when ctx
// is cancelled are refused untouched.
func (a *Applier) Apply(ctx context.Context, tool Tool, plan *Plan, fresh []*Entry) ApplyDoc {
	byID := map[string]*Entry{}
	for _, e := range fresh {
		if _, dup := byID[e.ID]; !dup {
			byID[e.ID] = e
		}
	}
	doc := ApplyDoc{SchemaVersion: SchemaVersion, Tool: tool, Mode: a.Mode.String(), Results: []Result{}}
	for _, p := range plan.Items {
		var r Result
		if ctx.Err() != nil {
			r = Result{ID: p.ID, Status: StatusRefused, Freed: -1, Reasons: []string{"interrupted"}, Error: "cancelled before this item; nothing changed"}
		} else {
			r = a.one(p, byID[p.ID])
		}
		doc.add(r)
	}
	return doc
}

func (a *Applier) one(p PlanItem, e *Entry) Result {
	if e == nil {
		return Result{ID: p.ID, Status: StatusRefused, Freed: -1, Reasons: []string{"gone"}, Error: "not found by a fresh scan; it may be gone already"}
	}
	r := Result{ID: p.ID, Kind: e.Kind, Path: e.Path, Command: e.Command, Action: e.Action, Freed: -1}
	if e.Action == "trash" && a.Mode == clean.Permanent {
		r.Action = "delete"
	}
	refuse := func(code, msg string) Result {
		r.Status, r.Reasons, r.Error = StatusRefused, []string{code}, msg
		return r
	}
	switch {
	case e.Fingerprint != p.Fingerprint:
		return refuse("stale", "changed since the scan; scan again")
	case !e.Safety.Level.Allowed():
		return refuse(string(e.Safety.Level), "flutterkill does not act on "+string(e.Safety.Level)+" items")
	}
	c := &clean.Cleaner{Root: e.Root, Mode: a.Mode, Allowed: a.Allowed}
	var o clean.Outcome
	if e.Cache == nil {
		if err := clean.CheckArtifact(e.Root, e.ProjectDir, e.Artifact); err != nil {
			return refuse("guard", err.Error())
		}
		o = c.RemoveArtifact(e.ProjectDir, e.Artifact, -1)
	} else {
		if err := a.check(e); err != nil {
			return refuse("guard", err.Error())
		}
		o = c.RemoveCache(e.Cache, -1)
	}
	r.Estimated, r.Freed = max(o.Bytes, 0), o.Freed
	if o.Err != nil {
		r.Status, r.Error = StatusFailed, o.Err.Error()
		return r
	}
	r.Status = StatusDone
	if a.Mode == clean.DryRun {
		r.Status = StatusDryRun
	}
	r.RestoreHint = a.hint(r.Action, e)
	return r
}

func (a *Applier) check(e *Entry) error {
	if len(e.Cache.Command) > 0 {
		return clean.CheckCommand(e.Cache.Command)
	}
	return clean.CheckCache(a.Allowed, e.Root, e.Cache.Path)
}

func (a *Applier) hint(action string, e *Entry) string {
	if action == "trash" {
		return "restore from the " + clean.TrashName + ", or let " + e.Regenerate + " recreate it"
	}
	return "recreated by " + e.Regenerate
}

func (d *ApplyDoc) add(r Result) {
	d.Results = append(d.Results, r)
	switch r.Status {
	case StatusDone, StatusDryRun:
		if r.Status == StatusDone {
			d.Summary.Done++
		} else {
			d.Summary.DryRun++
		}
		d.Summary.Estimate += r.Estimated
		switch r.Action {
		case "command":
			d.Summary.ByTool += r.Estimated
		case "delete":
			d.Summary.Deleted += r.Estimated
		default:
			d.Summary.Trashed += r.Estimated
		}
	case StatusRefused:
		d.Summary.Refused++
	case StatusFailed:
		d.Summary.Failed++
	}
	if r.Freed > 0 {
		d.Summary.Freed += r.Freed
	}
}

// Text writes a short human-readable account of an apply.
func (d ApplyDoc) Text() string {
	var b strings.Builder
	for _, r := range d.Results {
		what := r.Path
		if what == "" {
			what = strings.Join(r.Command, " ")
		}
		fmt.Fprintf(&b, "%-8s %9s  %s", r.Status, format.Bytes(r.Estimated), what)
		if r.Error != "" {
			fmt.Fprintf(&b, "  (%s)", r.Error)
		}
		b.WriteString("\n")
	}
	s := d.Summary
	fmt.Fprintf(&b, "done %d · dry run %d · refused %d · failed %d · about %s\n", s.Done, s.DryRun, s.Refused, s.Failed, format.Bytes(s.Estimate))
	return b.String()
}
