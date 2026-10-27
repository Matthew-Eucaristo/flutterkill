package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/report"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// Plain ANSI colors so the terminal theme decides the actual shades. The
// palette and glyphs match fell, the reference look of the tool family; only
// the brand colour is flutterkill's own.
var (
	brand   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Padding(0, 1)
	bold    = lipgloss.NewStyle().Bold(true)
	faint   = lipgloss.NewStyle().Faint(true)
	cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	struck  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cursorC = green.Render("▌")
)

// artifactNotes explain what each artifact is and how it comes back.
var artifactNotes = map[string]string{
	"build":                     "Build output. Recreated by the next `flutter build` / `flutter run`.",
	".dart_tool":                "Pub and build_runner state. `flutter pub get` (run automatically by flutter run) restores it.",
	"ios/Pods":                  "Installed CocoaPods. `pod install` (run by flutter build ios) restores it.",
	"ios/.symlinks":             "Plugin symlinks for CocoaPods. Recreated by the next iOS build.",
	"macos/Pods":                "Installed CocoaPods. Restored by the next macOS build.",
	"android/.gradle":           "Project-level Gradle cache. Recreated by the next Android build.",
	"android/app/.cxx":          "Native (CMake/NDK) build intermediates. Recreated by the next Android build.",
	"ios/Flutter/ephemeral":     "Generated Flutter files for Xcode. Recreated by the next iOS build.",
	"macos/Flutter/ephemeral":   "Generated Flutter files and plugin symlinks. Recreated by the next macOS build.",
	"linux/flutter/ephemeral":   "Flutter engine copy and plugin symlinks. Recreated by the next Linux build.",
	"windows/flutter/ephemeral": "Flutter engine copy and plugin symlinks. Recreated by the next Windows build.",
}

const (
	staleAfter  = 180 * 24 * time.Hour
	activeUntil = 30 * 24 * time.Hour
	barWidth    = 14
	barMinWidth = 90 // narrower terminals drop the bar to keep names visible
	detailRows  = 4
	// header, blank, tabs, column header + separator, detail, summary, help
	chromeLines = 4 + 1 + detailRows + 2
)

// listHeight is the rows left for items below the column header.
func (m *Model) listHeight() int { return max(1, m.height-chromeLines) }

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "flutterkill"
	return v
}

func (m *Model) render() string {
	if m.err != nil {
		return red.Render("flutterkill: "+m.err.Error()) + "\n\n" + faint.Render("q quit")
	}
	if m.inv == nil {
		return m.header() + "\n\n  " + faint.Render("Scanning "+format.Tilde(m.opts.Root, m.home)+" …")
	}
	rows := m.rows()
	cur := m.cursor(rows)
	body := m.listHeight()

	var b strings.Builder
	line := func(s string) { b.WriteString(ansi.Truncate(s, m.width, "…") + "\n") }
	line(m.header())
	line("")
	line(m.tabs())
	var lines []string
	switch {
	case m.legend:
		line("")
		lines = m.legendLines()
	case m.confirm != nil:
		line("")
		lines = m.confirmLines(body)
	case len(rows) == 0:
		line("")
		lines = m.emptyLines()
	default:
		line(faint.Render(m.columns()))
		largest := int64(1)
		for _, r := range rows {
			if r.art < 0 {
				largest = max(largest, m.rowBytes(r))
			}
		}
		for i := m.offset; i < min(len(rows), m.offset+body); i++ {
			lines = append(lines, m.row(rows[i], i == cur, largest))
		}
	}
	for i := range body {
		if i < len(lines) {
			line(lines[i])
		} else {
			line("")
		}
	}
	line(faint.Render(strings.Repeat("─", max(0, m.width))))
	var detail []string
	if m.confirm == nil && !m.legend && len(rows) > 0 {
		detail = m.detail(rows[cur])
	}
	for i := range detailRows {
		if i < len(detail) {
			line(detail[i])
		} else {
			line("")
		}
	}
	line(m.summary())
	b.WriteString(ansi.Truncate(m.help(), m.width, "…"))
	return b.String()
}

func (m *Model) header() string {
	mode := green.Render("● trash")
	switch m.cleaner.Mode {
	case clean.DryRun:
		mode = yellow.Render("● dry run")
	case clean.Permanent:
		mode = red.Render("● permanent")
	}
	// Mode first and the (possibly long) root last, so truncation hides the least.
	parts := []string{brand.Render("flutterkill") + " " + mode}
	if m.inv != nil {
		parts = append(parts, cyan.Render(format.Bytes(m.reclaimable()))+" reclaimable", fmt.Sprintf("%d projects", len(m.inv.Projects)))
		if m.freeDisk >= 0 {
			parts = append(parts, format.Bytes(m.freeDisk)+" free")
		}
		if m.measuring {
			parts = append(parts, faint.Render("measuring…"))
		}
	}
	parts = append(parts, faint.Render(format.Tilde(m.opts.Root, m.home)))
	return " " + strings.Join(parts, faint.Render("  ·  "))
}

func (m *Model) tabs() string {
	pb, cb := m.totals()
	p := fmt.Sprintf("1 Projects %s", format.Bytes(pb))
	c := fmt.Sprintf("2 Global caches %s", format.Bytes(cb))
	if m.tab == projectsTab {
		return "  " + heading.Render(p) + "    " + faint.Render(c)
	}
	return "  " + faint.Render(p) + "    " + heading.Render(c)
}

func (m *Model) showBar() bool { return m.width >= barMinWidth }

// columns is the header over the rows; its widths match row.
func (m *Model) columns() string {
	bar := ""
	if m.showBar() {
		bar = strings.Repeat(" ", barWidth+2)
	}
	if m.tab == cachesTab {
		return fmt.Sprintf("    %s%9s  %-10s %s", bar, "SIZE", "SAFETY", "CACHE")
	}
	return fmt.Sprintf("    %s%9s  %-8s  %-10s %s", bar, "SIZE", "ACTIVE", "SAFETY", "PROJECT  (sort: "+m.sort.String()+")")
}

func (m *Model) row(r row, isCursor bool, largest int64) string {
	cur := " "
	if isCursor {
		cur = cursorC
	}
	prefix := cur + " " + m.check(r.leaves()) + " "
	bytes := m.rowBytes(r)
	bar := ""
	if m.showBar() {
		bar = m.bar(bytes, largest) + "  "
		if r.art >= 0 || r.c != nil {
			if s := m.status[r.key()]; s != idle && s != failed {
				bar = faint.Render(strings.Repeat("·", barWidth)) + "  " // gone or going
			}
		}
	}
	switch {
	case r.c != nil:
		lv := cacheLevel(r.c)
		line := fmt.Sprintf("%9s  %s %s  %s", format.Bytes(bytes), badge(lv), bold.Render(r.c.Name), m.cacheFlags(r.c))
		return prefix + bar + m.dim(r.key(), line)
	case r.art >= 0:
		a := r.p.Artifacts[r.art]
		line := fmt.Sprintf("%9s  %-8s  %-10s     %s", format.Bytes(bytes), "", "", a.Name+"/")
		return prefix + bar + m.dim(a.Path, line)
	}
	p := r.p
	chev := "▸"
	if m.expanded[p.Path] {
		chev = "▾"
	}
	label := faint.Render(chev) + " " + bold.Render(p.Name)
	if rel := format.Rel(m.opts.Root, p.Path); rel != p.Name {
		label += "  " + faint.Render(rel)
	}
	if p.FVMVersion != "" {
		label += "  " + faint.Render("fvm "+p.FVMVersion)
	}
	return prefix + fmt.Sprintf("%s%9s  %s  %s %s", bar, format.Bytes(bytes), m.age(p.LastActivity), badge(safe), label)
}

// rowBytes is what a row can still reclaim; -1 while measuring.
func (m *Model) rowBytes(r row) int64 {
	switch {
	case r.c != nil:
		return r.c.Bytes
	case r.art >= 0:
		return r.p.Artifacts[r.art].Bytes
	case m.measuringProject(r.p):
		return -1
	}
	return m.pending(r.p)
}

// bar shows size relative to the largest row with eighth-block resolution.
// Everything flutterkill lists is rebuildable, so it is cyan, as in fell.
func (m *Model) bar(n, largest int64) string {
	if n < 0 {
		return faint.Render(strings.Repeat("·", barWidth))
	}
	cells := fine(n, largest, barWidth)
	return cyan.Render(cells) + faint.Render(strings.Repeat("·", barWidth-ansi.StringWidth(cells)))
}

// eighths are partial block glyphs, so small differences still show.
var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// fine draws n/total of width cells with eighth-block resolution.
func fine(n, total int64, width int) string {
	if total <= 0 || n <= 0 {
		return ""
	}
	units := int(n * int64(width) * 8 / total)
	units = max(1, min(units, width*8))
	return strings.Repeat("█", units/8) + eighths[units%8]
}

// level is how careful to be with an item. The names and glyphs are fell's.
type level int

const (
	safe    level = iota // rebuilt or re-downloaded on demand
	caution              // allowed, but a project uses it or it cannot be undone
)

func (l level) String() string { return [...]string{"safe", "caution"}[l] }

func cacheLevel(c *caches.Cache) level {
	if len(c.UsedBy) > 0 || len(c.Command) > 0 {
		return caution
	}
	return safe
}

func levelStyle(l level) lipgloss.Style {
	if l == caution {
		return yellow
	}
	return green
}

func badge(l level) string {
	icon := [...]string{"●", "▲"}[l]
	return levelStyle(l).Render(fmt.Sprintf("%s %-8s", icon, l))
}

// cacheFlags say why a cache needs care, or where it lives.
func (m *Model) cacheFlags(c *caches.Cache) string {
	var parts []string
	if len(c.Command) > 0 {
		parts = append(parts, red.Render("no undo"), faint.Render("runs `"+strings.Join(c.Command, " ")+"`"))
	} else {
		parts = append(parts, faint.Render(format.Tilde(c.Path, m.home)))
	}
	if n := len(c.UsedBy); n > 0 {
		parts = append(parts, yellow.Render(fmt.Sprintf("used by %d project(s)", n)))
	}
	return strings.Join(parts, " ")
}

// dim styles a leaf line by its clean-up status.
func (m *Model) dim(key, line string) string {
	switch m.status[key] {
	case done:
		plain := ansi.Strip(line)
		text := strings.TrimLeft(plain, " ")
		return plain[:len(plain)-len(text)] + struck.Render(text)
	case working, queued:
		return faint.Render(ansi.Strip(line))
	case failed:
		return red.Render(ansi.Strip(line))
	}
	return line
}

// check is the selection mark; every state has its own glyph so it reads
// without colour (NO_COLOR).
func (m *Model) check(keys []string) string {
	sel, fin, bad := 0, 0, 0
	for _, k := range keys {
		switch {
		case m.status[k] == done:
			fin++
		case m.status[k] == working || m.status[k] == queued:
			return faint.Render("◌")
		case m.selected[k]:
			sel++
		case m.status[k] == failed:
			bad++
		}
	}
	switch {
	case len(keys) > 0 && fin == len(keys):
		return green.Render("✓")
	case sel > 0 && sel+fin == len(keys):
		return cyan.Render("●")
	case sel > 0:
		return cyan.Render("◐")
	case bad > 0:
		return red.Render("✗")
	}
	return faint.Render("○")
}

func (m *Model) age(t time.Time) string {
	s := fmt.Sprintf("%-8s", format.Ago(t, time.Now()))
	switch d := time.Since(t); {
	case t.IsZero():
		return faint.Render(s)
	case d < activeUntil:
		return green.Render(s)
	case d >= staleAfter:
		return yellow.Render(s)
	}
	return s
}

// detail is what the row under the cursor is, how it comes back, and any error.
func (m *Model) detail(r row) []string {
	pad := "  "
	var out []string
	switch {
	case r.c != nil:
		c := r.c
		lv := cacheLevel(c)
		out = append(out, pad+bold.Render(c.Name)+"  "+levelStyle(lv).Render(lv.String()))
		if len(c.Command) > 0 {
			out = append(out, pad+red.Render("Runs `"+strings.Join(c.Command, " ")+"`, the tool's own clean-up. Not recoverable from the "+clean.TrashName+"."))
		} else {
			out = append(out, pad+faint.Render(format.Tilde(c.Path, m.home)))
		}
		out = append(out, m.errLines(m.errs[r.key()])...)
		out = append(out, pad+c.Note)
	case r.art >= 0:
		a := r.p.Artifacts[r.art]
		out = append(out, pad+bold.Render(r.p.Name+"/"+a.Name)+"  "+green.Render("safe"), pad+faint.Render(format.Tilde(a.Path, m.home)))
		out = append(out, m.errLines(m.errs[a.Path])...)
		out = append(out, pad+artifactNotes[a.Name])
	default:
		p := r.p
		src := map[string]string{"git": "last commit", "mtime": "last file change", "": "activity unknown"}[p.ActivitySource]
		info := src + " " + format.Ago(p.LastActivity, time.Now())
		if p.FVMVersion != "" {
			info = "fvm " + p.FVMVersion + " · " + info
		}
		out = append(out, pad+bold.Render(p.Name)+"  "+faint.Render(info), pad+faint.Render(format.Tilde(p.Path, m.home)))
		for _, a := range p.Artifacts {
			if e := m.errs[a.Path]; e != "" {
				out = append(out, m.errLines(a.Name+": "+e)...)
				break
			}
		}
		var arts []string
		for _, a := range p.Artifacts {
			if m.status[a.Path] != done {
				arts = append(arts, a.Name+" "+format.Bytes(a.Bytes))
			}
		}
		if len(arts) > 0 {
			out = append(out, pad+cyan.Render("rebuildable: ")+strings.Join(arts, ", "))
		}
	}
	return out
}

// errLines wraps an error over up to two detail rows; errors often hold
// long paths and matter more than the note below them.
func (m *Model) errLines(e string) []string {
	if e == "" {
		return nil
	}
	if m.home != "" {
		e = strings.ReplaceAll(e, m.home+string(filepath.Separator), "~"+string(filepath.Separator))
	}
	lines := strings.Split(ansi.Hardwrap("✗ "+e, max(20, m.width-4), true), "\n")
	if len(lines) > 2 {
		lines = lines[:2]
	}
	for i, l := range lines {
		lines[i] = "  " + red.Render(l)
	}
	return lines
}

// summary shows this tab's selection and what has been done so far, with
// moved, deleted and delegated space kept apart.
func (m *Model) summary() string {
	sel := m.selectedHere(m.rows())
	parts := []string{fmt.Sprintf("%d selected (%s)", len(sel), format.Bytes(m.sum(sel)))}
	t := report.Count(m.outcomes)
	if n, ok := t.Bytes[clean.Would]; ok {
		parts = append(parts, green.Render("Would free: "+format.Bytes(n)))
	}
	if n, ok := t.Bytes[clean.Trashed]; ok {
		parts = append(parts, green.Render("Moved to "+clean.TrashName+": "+format.Bytes(n)),
			faint.Render("free space "+report.Signed(t.Freed[clean.Trashed])+" (measured; empty the "+clean.TrashName+" to reclaim)"))
	}
	if n, ok := t.Bytes[clean.Deleted]; ok {
		parts = append(parts, green.Render("Deleted: "+format.Bytes(n)), faint.Render("free space "+report.Signed(t.Freed[clean.Deleted])+" (measured)"))
	}
	if n, ok := t.Bytes[clean.Delegated]; ok {
		parts = append(parts, green.Render("Delegated: "+format.Bytes(n)), faint.Render("free space "+report.Signed(t.Freed[clean.Delegated])+" (measured)"))
	}
	if t.Failed > 0 {
		parts = append(parts, red.Render(fmt.Sprintf("%d refused/failed", t.Failed)))
	}
	if len(m.queue) > 0 {
		parts = append(parts, faint.Render(fmt.Sprintf("%d queued", len(m.queue))))
	}
	return "  " + strings.Join(parts, faint.Render("  ·  "))
}

func (m *Model) help() string {
	switch {
	case m.legend:
		return faint.Render("  any key closes")
	case m.confirm != nil:
		return yellow.Render("  Proceed?") + "  " + bold.Render("y") + faint.Render(" yes   any other key cancels")
	case m.quitWarn && m.busy():
		return yellow.Render("  Stopping after the current item. Press ctrl+c again to quit now, or wait.")
	case m.tab == cachesTab:
		return faint.Render("  ↑↓ move  space select  a all  d delete  tab projects  ? legend  q quit")
	}
	return faint.Render("  ↑↓ move  space select  a all  →← expand  d delete  tab caches  o sort  ? legend  q quit")
}

// confirmLines lists exactly what the pending delete will remove.
func (m *Model) confirmLines(height int) []string {
	verb := map[clean.Mode]string{clean.Trash: "Move to the " + clean.TrashName + ":", clean.Permanent: "PERMANENTLY delete:", clean.DryRun: "Dry run: would remove"}[m.cleaner.Mode]
	title := fmt.Sprintf("  %s %d item(s), about %s", verb, len(m.confirm), format.Bytes(m.sum(m.confirm)))
	if m.cleaner.Mode == clean.DryRun {
		title += " (nothing will change)"
	}
	if m.cleaner.Mode == clean.Permanent {
		title = red.Render(title)
	} else {
		title = yellow.Render(title)
	}
	lines := []string{title, ""}
	for i, k := range m.confirm {
		if len(lines) >= height-1 && i < len(m.confirm)-1 {
			lines = append(lines, faint.Render(fmt.Sprintf("    … and %d more", len(m.confirm)-i)))
			break
		}
		c, _, a := m.lookup(k)
		switch {
		case c != nil && len(c.Command) > 0 && m.cleaner.Mode != clean.DryRun:
			lines = append(lines, fmt.Sprintf("    %9s  %s  %s", format.Bytes(c.Bytes), bold.Render(c.Name),
				red.Render("runs `"+strings.Join(c.Command, " ")+"`, not recoverable")))
		case c != nil:
			lines = append(lines, fmt.Sprintf("    %9s  %s  %s", format.Bytes(c.Bytes), bold.Render(c.Name), faint.Render(format.Tilde(c.Path, m.home))))
		case a != nil:
			lines = append(lines, fmt.Sprintf("    %9s  %s", format.Bytes(a.Bytes), format.Rel(m.opts.Root, a.Path)))
		}
	}
	return lines
}

func (m *Model) emptyLines() []string {
	if m.tab == cachesTab {
		return []string{"  " + faint.Render("No global caches found.")}
	}
	return []string{"  " + faint.Render("No Dart/Flutter projects with build artifacts under "+format.Tilde(m.opts.Root, m.home)+"."),
		"  " + faint.Render("flutterkill looks for pubspec.yaml and lists build/, .dart_tool/, Pods and Gradle state next to it.")}
}

func (m *Model) legendLines() []string {
	return []string{
		"  " + bold.Render("Safety"),
		"    " + badge(safe) + "  rebuilt or re-downloaded on demand; moves to the " + clean.TrashName,
		"    " + badge(caution) + "  allowed, but a scanned project uses it, or the tool's own clean-up command runs (no undo)",
		"",
		"  " + bold.Render("Marks") + "  ○ not selected  ◐ partly  ● selected  ◌ queued or running  ✓ done  ✗ failed (see the detail)",
		"  " + bold.Render("Bar") + "    " + cyan.Render("███") + " size, relative to the largest item in this tab",
		"",
		"  Only directories at a known place next to a pubspec.yaml are listed; symlinks and git-tracked files are refused.",
		"  Removals run one at a time. Freed space is measured from the disk, not assumed.",
		"  Moving to the " + clean.TrashName + " frees nothing until you empty it.",
	}
}

func (m *Model) reclaimable() int64 {
	p, c := m.totals()
	return p + c
}

// totals is what projects and caches can still reclaim.
func (m *Model) totals() (projects, caches int64) {
	for _, p := range m.inv.Projects {
		projects += m.pending(p)
	}
	for _, c := range m.inv.Caches {
		if m.status["cache:"+c.ID] != done {
			caches += max(c.Bytes, 0)
		}
	}
	return projects, caches
}

func (m *Model) measuringProject(p *scan.Project) bool {
	for _, a := range p.Artifacts {
		if a.Bytes < 0 {
			return true
		}
	}
	return false
}
