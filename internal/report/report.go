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
