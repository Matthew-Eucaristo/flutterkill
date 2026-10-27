// Package report prints an inventory as plain text or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
)

// JSON writes the inventory with a total.
func JSON(w io.Writer, inv *inventory.Inventory) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		*inventory.Inventory
		TotalBytes int64 `json:"total_bytes"`
	}{inv, inv.Total()})
}

// Text writes a human-readable table, biggest first.
func Text(w io.Writer, inv *inventory.Inventory) {
	home, _ := os.UserHomeDir()
	now := time.Now()
	sort.SliceStable(inv.Projects, func(i, j int) bool { return inv.Projects[i].Bytes() > inv.Projects[j].Bytes() })

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "PROJECTS in %s\n", format.Tilde(inv.Root, home))
	if len(inv.Projects) == 0 {
		fmt.Fprintln(tw, "  (none)")
	}
	for _, p := range inv.Projects {
		var parts []string
		for _, a := range p.Artifacts {
			if a.Bytes == 0 {
				continue
			}
			parts = append(parts, a.Name+" "+format.Bytes(a.Bytes))
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", format.Bytes(p.Bytes()), format.Ago(p.LastActivity, now),
			format.Rel(inv.Root, p.Path), strings.Join(parts, " · "))
	}
	if len(inv.Caches) > 0 {
		fmt.Fprintln(tw, "\nGLOBAL CACHES")
		for _, c := range inv.Caches {
			if c.Bytes == 0 {
				continue
			}
			where := format.Tilde(c.Path, home)
			if len(c.Command) > 0 {
				where = "runs: " + strings.Join(c.Command, " ")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", format.Bytes(c.Bytes), c.Name, where)
		}
	}
	fmt.Fprintf(tw, "\nReclaimable: %s\n", format.Bytes(inv.Total()))
	tw.Flush()
}

// Receipt prints one line per outcome, then the totals.
func Receipt(w io.Writer, outs []clean.Outcome) {
	for _, o := range outs {
		Line(w, o)
	}
	Totals(w, outs)
}

// Line prints what one removal did.
func Line(w io.Writer, o clean.Outcome) {
	home, _ := os.UserHomeDir()
	what := format.Tilde(o.Path, home)
	if o.Name != "" {
		what = o.Name
		if o.Path != "" {
			what += "  " + format.Tilde(o.Path, home)
		}
	}
	switch {
	case o.Err != nil:
		fmt.Fprintf(w, "%-13s %9s  %s\n%-13s %9s  %v\n", "failed", "", what, "", "", o.Err)
	case o.How == clean.Delegated:
		fmt.Fprintf(w, "%-13s %9s  %s  (ran `%s`)\n", o.How, format.Bytes(o.Bytes), what, o.Command)
	default:
		fmt.Fprintf(w, "%-13s %9s  %s\n", o.How, format.Bytes(o.Bytes), what)
	}
}

// Tally sums outcomes by what happened to them.
type Tally struct {
	Items, Failed int
	Bytes         map[clean.How]int64 // estimated size per kind
	Freed         map[clean.How]int64 // measured change in free space per kind
}

// Count tallies outs.
func Count(outs []clean.Outcome) Tally {
	t := Tally{Bytes: map[clean.How]int64{}, Freed: map[clean.How]int64{}}
	for _, o := range outs {
		if o.Err != nil {
			t.Failed++
			continue
		}
		t.Items++
		t.Bytes[o.How] += max(o.Bytes, 0)
		if o.Freed != -1 {
			t.Freed[o.How] += o.Freed
		}
	}
	return t
}

// Totals prints one sentence per kind of removal: moved to the trash,
// deleted, and delegated to the owning tool are never added together.
func Totals(w io.Writer, outs []clean.Outcome) {
	t := Count(outs)
	if b, ok := t.Bytes[clean.Would]; ok {
		fmt.Fprintf(w, "Dry run: %d item(s) would free about %s. Nothing was changed.\n", t.Items, format.Bytes(b))
	}
	if b, ok := t.Bytes[clean.Trashed]; ok {
		fmt.Fprintf(w, "Moved about %s to the %s (free space changed by %s, measured). Empty the %s to reclaim it.\n",
			format.Bytes(b), clean.TrashName, Signed(t.Freed[clean.Trashed]), clean.TrashName)
	}
	if b, ok := t.Bytes[clean.Deleted]; ok {
		fmt.Fprintf(w, "Deleted about %s; free space changed by %s (measured).\n", format.Bytes(b), Signed(t.Freed[clean.Deleted]))
	}
	if b, ok := t.Bytes[clean.Delegated]; ok {
		fmt.Fprintf(w, "Delegated about %s to the tools' own clean-up commands; free space changed by %s (measured).\n",
			format.Bytes(b), Signed(t.Freed[clean.Delegated]))
	}
	if t.Failed > 0 {
		fmt.Fprintf(w, "%d item(s) failed or were refused (see above).\n", t.Failed)
	}
}

// Signed formats a measured change in free space.
func Signed(n int64) string {
	if n < 0 {
		return "-" + format.Bytes(-n)
	}
	return "+" + format.Bytes(n)
}
