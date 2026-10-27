// Package ui is the interactive Bubble Tea front end.
package ui

import (
	"cmp"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

type status int

const (
	idle status = iota
	queued
	working
	done
	failed
)

type tab int

const (
	projectsTab tab = iota
	cachesTab
)

type sortBy int

const (
	bySize sortBy = iota
	byAge
	byName
)

func (s sortBy) String() string { return [...]string{"size", "oldest", "name"}[s] }

// row is one visible line: a project, one of its artifacts, or a cache.
type row struct {
	p   *scan.Project
	art int // artifact index, or -1 for the project row itself
	c   *caches.Cache
}

// key identifies a row; leaves (artifacts, caches) are what get selected.
func (r row) key() string {
	switch {
	case r.c != nil:
		return "cache:" + r.c.ID
	case r.art >= 0:
		return r.p.Artifacts[r.art].Path
	default:
		return "project:" + r.p.Path
	}
}

// leaves are the keys of the artifacts/caches a row stands for.
func (r row) leaves() []string {
	if r.p != nil && r.art < 0 {
		keys := make([]string, len(r.p.Artifacts))
		for i, a := range r.p.Artifacts {
			keys[i] = a.Path
		}
		return keys
	}
	return []string{r.key()}
}

type (
	loadedMsg struct {
		inv  *inventory.Inventory
		free int64
		err  error
	}
	sizedMsg    inventory.Sized
	measuredMsg struct{}
	cleanedMsg  struct {
		key  string
		out  clean.Outcome
		free int64
	}
)

// Model is the TUI state. All mutation happens in Update.
type Model struct {
	opts    inventory.Options
	cleaner *clean.Cleaner
	home    string

	inv       *inventory.Inventory
	err       error
	sizes     chan inventory.Sized
	measuring bool
	freeDisk  int64 // on the root's volume, -1 if unknown

	tab       tab
	sort      sortBy
	cursorKey string
	offset    int
	width     int
	height    int

	expanded map[string]bool
	selected map[string]bool
	status   map[string]status
	errs     map[string]string
	confirm  []string // leaf keys awaiting y/n
	queue    []string // confirmed leaf keys, removed one at a time
	legend   bool
	quitWarn bool // asked to quit while a removal runs

	outcomes []clean.Outcome
}

// New builds a model that scans with opts and removes through cleaner.
func New(opts inventory.Options, cleaner *clean.Cleaner) *Model {
	home, _ := os.UserHomeDir()
	return &Model{
		opts: opts, cleaner: cleaner, home: home,
		expanded: map[string]bool{}, selected: map[string]bool{},
		status: map[string]status{}, errs: map[string]string{},
		width: 80, height: 24, freeDisk: -1,
	}
}

// Outcomes reports what was removed, in order, once the program has exited.
func (m *Model) Outcomes() []clean.Outcome { return m.outcomes }

func (m *Model) Init() tea.Cmd {
	opts := m.opts
	return func() tea.Msg {
		inv, err := inventory.Load(opts)
		return loadedMsg{inv, clean.FreeBytes(opts.Root), err}
	}
}

func waitSize(ch chan inventory.Sized) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return measuredMsg{}
		}
		return sizedMsg(s)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.scroll()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.inv, m.freeDisk = msg.inv, msg.free
		m.sizes = make(chan inventory.Sized)
		m.measuring = true
		m.inv.Measure(m.sizes)
		if len(m.inv.Projects) == 0 && len(m.inv.Caches) > 0 {
			m.tab = cachesTab
		}
		// Pin the cursor to an item, not a position, before sizes reorder rows.
		if rows := m.rows(); len(rows) > 0 {
			m.cursorKey = rows[0].key()
		}
		return m, waitSize(m.sizes)
	case sizedMsg:
		m.inv.Apply(inventory.Sized(msg))
		return m, waitSize(m.sizes)
	case measuredMsg:
		m.measuring = false
	case cleanedMsg:
		delete(m.selected, msg.key)
		m.outcomes = append(m.outcomes, msg.out)
		if msg.out.Err != nil {
			m.status[msg.key] = failed
			m.errs[msg.key] = msg.out.Err.Error()
		} else {
			m.status[msg.key] = done
			delete(m.errs, msg.key)
		}
		if msg.free >= 0 {
			m.freeDisk = msg.free
		}
		return m, m.next()
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		// Quitting mid-removal can leave a half-deleted directory: the first
		// ctrl+c cancels what is queued and lets the running item finish.
		if m.busy() && !m.quitWarn {
			m.stop()
			return nil
		}
		return tea.Quit
	}
	if m.legend {
		m.legend = false
		return nil
	}
	if m.confirm != nil {
		if k == "y" || k == "Y" {
			return m.startClean()
		}
		m.confirm = nil
		return nil
	}
	if m.inv == nil {
		if k == "q" || k == "esc" {
			return tea.Quit
		}
		return nil
	}

	rows := m.rows()
	i := m.cursor(rows)
	switch k {
	case "q", "esc":
		if m.busy() {
			m.stop() // let the running removal finish; ctrl+c still quits
			return nil
		}
		return tea.Quit
	case "?":
		m.legend = true
		return nil
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup", "ctrl+u":
		i -= m.listHeight()
	case "pgdown", "ctrl+d":
		i += m.listHeight()
	case "home", "g":
		i = 0
	case "end", "G":
		i = len(rows) - 1
	case "tab", "shift+tab", "1", "2":
		switch k {
		case "1":
			m.tab = projectsTab
		case "2":
			m.tab = cachesTab
		default:
			m.tab = 1 - m.tab
		}
		m.cursorKey, m.offset = "", 0
		return nil
	case "o", "s": // s was the sort key before v0.3
		m.sort = (m.sort + 1) % 3
		return nil
	case "right", "l", "enter":
		if len(rows) > 0 && rows[i].p != nil {
			p := rows[i].p.Path
			m.expanded[p] = k == "right" || k == "l" || !m.expanded[p]
		}
		return nil
	case "left", "h":
		if len(rows) > 0 && rows[i].p != nil {
			delete(m.expanded, rows[i].p.Path)
			m.cursorKey = "project:" + rows[i].p.Path
		}
		return nil
	case "space":
		if len(rows) > 0 {
			m.toggle(rows[i].leaves())
		}
		return nil
	case "a":
		var all []string
		for _, r := range rows {
			if r.art < 0 {
				all = append(all, r.leaves()...)
			}
		}
		m.toggle(all)
		return nil
	case "d", "x", "delete", "backspace":
		m.askDelete(rows, i)
		return nil
	default:
		return nil
	}
	if len(rows) > 0 {
		i = max(0, min(i, len(rows)-1))
		m.cursorKey = rows[i].key()
	}
	return nil
}

// toggle selects every pending leaf in keys, or clears them if all were set.
func (m *Model) toggle(keys []string) {
	keys = slices.DeleteFunc(slices.Clone(keys), m.locked)
	all := len(keys) > 0
	for _, k := range keys {
		all = all && m.selected[k]
	}
	for _, k := range keys {
		if all {
			delete(m.selected, k)
		} else {
			m.selected[k] = true
		}
	}
}

// askDelete targets this tab's visible selection, or the row under the
// cursor if nothing is selected. The confirmation lists every target.
func (m *Model) askDelete(rows []row, i int) {
	keys := m.selectedHere(rows)
	if len(keys) == 0 && len(rows) > 0 {
		keys = rows[i].leaves()
	}
	keys = slices.DeleteFunc(keys, m.locked)
	if len(keys) > 0 {
		slices.Sort(keys)
		m.confirm = keys
	}
}

// selectedHere is the selection limited to leaves shown in this tab, so a
// delete never reaches items the user cannot see.
func (m *Model) selectedHere(rows []row) []string {
	var keys []string
	for _, r := range rows {
		if r.art >= 0 {
			continue // covered by its project row
		}
		for _, k := range r.leaves() {
			if m.selected[k] {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// locked reports whether a leaf can no longer be selected or removed.
func (m *Model) locked(key string) bool {
	switch m.status[key] {
	case queued, working, done:
		return true
	}
	return false
}

// startClean queues the confirmed keys. They run one at a time, so the free
// space measured around each removal belongs to that removal.
func (m *Model) startClean() tea.Cmd {
	for _, k := range m.confirm {
		m.status[k] = queued
		delete(m.errs, k)
	}
	m.queue = append(m.queue, m.confirm...)
	m.confirm = nil
	if m.working() {
		return nil
	}
	return m.next()
}

// next starts the first queued removal. The returned command copies what it
// needs on the UI goroutine and touches no model state.
func (m *Model) next() tea.Cmd {
	if len(m.queue) == 0 {
		m.quitWarn = false
		return nil
	}
	key := m.queue[0]
	m.queue = m.queue[1:]
	m.status[key] = working
	c, p, a := m.lookup(key)
	cleaner, bytes, root := m.cleaner, m.size(key), m.opts.Root
	if c != nil {
		cache := *c
		return func() tea.Msg {
			return cleanedMsg{key, cleaner.RemoveCache(&cache, bytes), clean.FreeBytes(root)}
		}
	}
	project, name := p.Path, a.Name
	return func() tea.Msg {
		return cleanedMsg{key, cleaner.RemoveArtifact(project, name, bytes), clean.FreeBytes(root)}
	}
}

// stop cancels queued removals; the running one finishes.
func (m *Model) stop() {
	for _, k := range m.queue {
		m.status[k] = idle // still selected, so d resumes
	}
	m.queue = nil
	m.quitWarn = true
}

// lookup finds the cache or project artifact behind a leaf key.
func (m *Model) lookup(key string) (*caches.Cache, *scan.Project, *scan.Artifact) {
	if id, ok := strings.CutPrefix(key, "cache:"); ok {
		for _, c := range m.inv.Caches {
			if c.ID == id {
				return c, nil, nil
			}
		}
	}
	for _, p := range m.inv.Projects {
		for i := range p.Artifacts {
			if p.Artifacts[i].Path == key {
				return nil, p, &p.Artifacts[i]
			}
		}
	}
	return nil, nil, nil
}

func (m *Model) busy() bool { return m.working() || len(m.queue) > 0 }

func (m *Model) working() bool {
	for _, s := range m.status {
		if s == working {
			return true
		}
	}
	return false
}

// scroll keeps the cursor row inside the visible list.
func (m *Model) scroll() {
	rows := m.rows()
	cur, body := m.cursor(rows), m.listHeight()
	if cur < m.offset {
		m.offset = cur
	}
	if cur >= m.offset+body {
		m.offset = cur - body + 1
	}
	m.offset = max(0, min(m.offset, len(rows)-body))
}

// rows lists what the current tab shows, in the current sort order.
func (m *Model) rows() []row {
	var rows []row
	if m.inv == nil {
		return nil
	}
	if m.tab == cachesTab {
		cs := slices.Clone(m.inv.Caches)
		slices.SortStableFunc(cs, func(a, b *caches.Cache) int { return cmp.Compare(b.Bytes, a.Bytes) })
		for _, c := range cs {
			if c.Bytes == 0 && m.status["cache:"+c.ID] == idle {
				continue // measured and empty: nothing to reclaim
			}
			rows = append(rows, row{c: c, art: -1})
		}
		return rows
	}
	ps := slices.Clone(m.inv.Projects)
	slices.SortStableFunc(ps, func(a, b *scan.Project) int {
		switch m.sort {
		case byAge:
			return a.LastActivity.Compare(b.LastActivity)
		case byName:
			return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		}
		return cmp.Compare(m.pending(b), m.pending(a))
	})
	for _, p := range ps {
		rows = append(rows, row{p: p, art: -1})
		if m.expanded[p.Path] {
			for i := range p.Artifacts {
				rows = append(rows, row{p: p, art: i})
			}
		}
	}
	return rows
}

// cursor returns the index of the row under the cursor, following its key
// across re-sorts.
func (m *Model) cursor(rows []row) int {
	for i, r := range rows {
		if r.key() == m.cursorKey {
			return i
		}
	}
	return 0
}

// size of a leaf key; -1 while measuring.
func (m *Model) size(key string) int64 {
	c, _, a := m.lookup(key)
	if c != nil {
		return c.Bytes
	}
	if a != nil {
		return a.Bytes
	}
	return 0
}

// pending is what a project can still reclaim (cleaned artifacts excluded).
func (m *Model) pending(p *scan.Project) int64 {
	var n int64
	for _, a := range p.Artifacts {
		if m.status[a.Path] != done && a.Bytes > 0 {
			n += a.Bytes
		}
	}
	return n
}

func (m *Model) sum(keys []string) int64 {
	var n int64
	for _, k := range keys {
		n += max(m.size(k), 0)
	}
	return n
}
