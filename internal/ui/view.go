package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// Plain ANSI colors so the terminal theme decides the actual shades.
var (
	accent  = lipgloss.Color("6")
	brand   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(accent).Padding(0, 1)
	bold    = lipgloss.NewStyle().Bold(true)
	faint   = lipgloss.NewStyle().Faint(true)
	cyan    = lipgloss.NewStyle().Foreground(accent)
	green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	struck  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	tabOn   = lipgloss.NewStyle().Bold(true).Foreground(accent).Underline(true)
	cursorC = cyan.Render("▌")
)

// artifactNotes explain what each artifact is and how it comes back.
var artifactNotes = map[string]string{
	"build":            "Build output. Recreated by the next `flutter build` / `flutter run`.",
	".dart_tool":       "Pub and build_runner state. `flutter pub get` (run automatically by flutter run) restores it.",
	"ios/Pods":         "Installed CocoaPods. `pod install` (run by flutter build ios) restores it.",
	"ios/.symlinks":    "Plugin symlinks for CocoaPods. Recreated by the next iOS build.",
	"macos/Pods":       "Installed CocoaPods. Restored by the next macOS build.",
	"android/.gradle":  "Project-level Gradle cache. Recreated by the next Android build.",
	"android/app/.cxx": "Native (CMake/NDK) build intermediates. Recreated by the next Android build.",
}

const (
	staleAfter  = 180 * 24 * time.Hour
	activeUntil = 30 * 24 * time.Hour
	// header, blank, tabs, column header, blank, 3 footer lines
	chromeLines = 8
)

func (m *Model) bodyHeight() int { return max(3, m.height-chromeLines) }

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
	body := m.bodyHeight()
	if cur < m.offset {
		m.offset = cur
	}
	if cur >= m.offset+body {
		m.offset = cur - body + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(rows)-body)))

	var b strings.Builder
	line := func(s string) { b.WriteString(ansi.Truncate(s, m.width, "…") + "\n") }
	line(m.header())
	line("")
	line(m.tabs())
	if m.tab == projectsTab {
		line(faint.Render(fmt.Sprintf("  %9s  %-9s  %s", "SIZE", "ACTIVE", "PROJECT  (sort: "+m.sort.String()+")")))
	} else {
		line(faint.Render(fmt.Sprintf("  %9s  %s", "SIZE", "CACHE")))
	}
	var lines []string
	switch {
	case m.confirm != nil:
		lines = m.confirmLines(body)
	case len(rows) == 0:
		lines = []string{"", "  " + faint.Render(m.emptyText())}
	default:
		for i := m.offset; i < min(len(rows), m.offset+body); i++ {
			lines = append(lines, m.row(rows[i], i == cur))
		}
	}
	for i := range body {
		if i < len(lines) {
			line(lines[i])
		} else {
			line("")
		}
	}
	line("")
	var r row
	if len(rows) > 0 {
		r = rows[cur]
	}
	line(m.detail(r))
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
		parts = append(parts, bold.Render(format.Bytes(m.reclaimable()))+" reclaimable", fmt.Sprintf("%d projects", len(m.inv.Projects)))
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
		return "  " + tabOn.Render(p) + "    " + faint.Render(c)
	}
	return "  " + faint.Render(p) + "    " + tabOn.Render(c)
}

func (m *Model) row(r row, isCursor bool) string {
	cursor := " "
	if isCursor {
		cursor = cursorC
	}
	check := m.check(r.leaves())
	switch {
	case r.c != nil:
		key := r.key()
		name := r.c.Name
		where := format.Tilde(r.c.Path, m.home)
		if len(r.c.Command) > 0 {
			where = "runs `" + strings.Join(r.c.Command, " ") + "`"
		}
		line := fmt.Sprintf("%9s  %s  %s", format.Bytes(r.c.Bytes), name, faint.Render(where))
		return cursor + check + " " + m.dim(key, line)
	case r.art >= 0:
		a := r.p.Artifacts[r.art]
		line := fmt.Sprintf("%9s  %-9s     %s/", format.Bytes(a.Bytes), "", a.Name)
		return cursor + check + " " + m.dim(a.Path, line)
	default:
		p := r.p
		chev := "▸"
		if m.expanded[p.Path] {
			chev = "▾"
		}
		size := format.Bytes(m.pending(p))
		if m.measuringProject(p) {
			size = format.Bytes(-1)
		}
		rel := format.Rel(m.opts.Root, p.Path)
		label := bold.Render(p.Name)
		if rel != p.Name {
			label += "  " + faint.Render(rel)
		}
		if p.FVMVersion != "" {
			label += "  " + faint.Render("fvm "+p.FVMVersion)
		}
		return fmt.Sprintf("%s%s %9s  %s  %s %s", cursor, check, size, m.age(p.LastActivity), faint.Render(chev), label)
	}
}

// dim styles a leaf line by its clean-up status.
func (m *Model) dim(key, line string) string {
	switch m.status[key] {
	case done:
		return struck.Render(ansi.Strip(line))
	case working:
		return faint.Render(ansi.Strip(line))
	case failed:
		return red.Render(ansi.Strip(line))
	}
	return line
}

func (m *Model) check(keys []string) string {
	sel, fin := 0, 0
	for _, k := range keys {
		switch {
		case m.status[k] == done:
			fin++
		case m.status[k] == working:
			return faint.Render("◌")
		case m.selected[k]:
			sel++
		}
	}
	switch {
	case fin == len(keys):
		return green.Render("✓")
	case sel > 0 && sel+fin == len(keys):
		return cyan.Render("●")
	case sel > 0:
		return cyan.Render("◐")
	}
	return faint.Render("○")
}

func (m *Model) age(t time.Time) string {
	s := fmt.Sprintf("%-9s", format.Ago(t, time.Now()))
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

// detail describes the row under the cursor.
func (m *Model) detail(r row) string {
	pad := "  "
	switch {
	case r.c != nil:
		if e := m.errs[r.key()]; e != "" {
			return pad + red.Render(e)
		}
		return pad + faint.Render(r.c.Note)
	case r.p != nil && r.art >= 0:
		a := r.p.Artifacts[r.art]
		if e := m.errs[a.Path]; e != "" {
			return pad + red.Render(e)
		}
		return pad + faint.Render(format.Tilde(a.Path, m.home)+"  "+artifactNotes[a.Name])
	case r.p != nil:
		for _, a := range r.p.Artifacts {
			if e := m.errs[a.Path]; e != "" {
				return pad + red.Render(a.Name+": "+e)
			}
		}
		var names []string
		for _, a := range r.p.Artifacts {
			names = append(names, a.Name)
		}
		src := map[string]string{"git": "last commit", "mtime": "last file change", "": "activity unknown"}[r.p.ActivitySource]
		return pad + faint.Render(fmt.Sprintf("%s  ·  %s %s  ·  %s",
			format.Tilde(r.p.Path, m.home), src, format.Ago(r.p.LastActivity, time.Now()), strings.Join(names, ", ")))
	}
	return ""
}

// summary shows this tab's selection and what has been cleaned so far.
func (m *Model) summary() string {
	sel := m.selectedHere(m.rows())
	parts := []string{fmt.Sprintf("%d selected (%s)", len(sel), format.Bytes(m.sum(sel)))}
	if m.result.Items > 0 {
		verb := map[clean.Mode]string{clean.Trash: "Moved to Trash", clean.Permanent: "Freed", clean.DryRun: "Would free"}[m.cleaner.Mode]
		parts = append(parts, green.Render(fmt.Sprintf("%s: %s", verb, format.Bytes(m.result.Bytes))))
	}
	if m.result.Failed > 0 {
		parts = append(parts, red.Render(fmt.Sprintf("%d failed", m.result.Failed)))
	}
	return "  " + strings.Join(parts, faint.Render("  ·  "))
}

func (m *Model) help() string {
	if m.confirm != nil {
		return yellow.Render("  Proceed?") + "  " + bold.Render("y") + faint.Render(" yes   any other key cancels")
	}
	if m.quitWarn {
		return yellow.Render("  Removals still running. Press ctrl+c again to quit anyway, or wait.")
	}
	return faint.Render("  ↑↓ move  space select  a all  →← expand  tab switch  s sort  d delete  q quit")
}

// confirmLines lists exactly what the pending delete will remove.
func (m *Model) confirmLines(height int) []string {
	verb := map[clean.Mode]string{clean.Trash: "Move to Trash", clean.Permanent: "PERMANENTLY delete", clean.DryRun: "Dry run: would remove"}[m.cleaner.Mode]
	title := fmt.Sprintf("  %s %d item(s), %s:", verb, len(m.confirm), format.Bytes(m.sum(m.confirm)))
	if m.cleaner.Mode == clean.Permanent {
		title = red.Render(title)
	} else {
		title = yellow.Render(title)
	}
	lines := []string{"", title, ""}
	for i, k := range m.confirm {
		if len(lines) >= height-1 && i < len(m.confirm)-1 {
			lines = append(lines, faint.Render(fmt.Sprintf("    … and %d more", len(m.confirm)-i)))
			break
		}
		c, _, a := m.lookup(k)
		switch {
		case c != nil && len(c.Command) > 0 && m.cleaner.Mode != clean.DryRun:
			lines = append(lines, fmt.Sprintf("    %9s  %s  %s", format.Bytes(c.Bytes), c.Name,
				red.Render("runs `"+strings.Join(c.Command, " ")+"`, not recoverable")))
		case c != nil:
			lines = append(lines, fmt.Sprintf("    %9s  %s  %s", format.Bytes(c.Bytes), c.Name, faint.Render(format.Tilde(c.Path, m.home))))
		case a != nil:
			lines = append(lines, fmt.Sprintf("    %9s  %s", format.Bytes(a.Bytes), format.Rel(m.opts.Root, a.Path)))
		}
	}
	return lines
}

func (m *Model) emptyText() string {
	if m.tab == cachesTab {
		return "No global caches found."
	}
	return "No Dart/Flutter projects with build artifacts under " + format.Tilde(m.opts.Root, m.home) + "."
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
