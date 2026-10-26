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

// Result summarises a session, printed after the TUI exits.
type Result struct {
	Bytes  int64
	Items  int
	Failed int
}

type status int

const (
	idle status = iota
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
		inv *inventory.Inventory
		err error
	}
	sizedMsg    inventory.Sized
	measuredMsg struct{}
	cleanedMsg  struct {
		key   string
		bytes int64
		err   error
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
	quitWarn bool     // ctrl+c pressed once while removals run

	result Result
}

// New builds a model that scans with opts and removes through cleaner.
func New(opts inventory.Options, cleaner *clean.Cleaner) *Model {
	home, _ := os.UserHomeDir()
	return &Model{
		opts: opts, cleaner: cleaner, home: home,
		expanded: map[string]bool{}, selected: map[string]bool{},
		status: map[string]status{}, errs: map[string]string{},
		width: 80, height: 24,
	}
}

// Result reports what was cleaned once the program has exited.
func (m *Model) Result() Result { return m.result }

func (m *Model) Init() tea.Cmd {
	opts := m.opts
	return func() tea.Msg {
		inv, err := inventory.Load(opts)
		return loadedMsg{inv, err}
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.inv = msg.inv
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
		if msg.err != nil {
			m.status[msg.key] = failed
			m.errs[msg.key] = msg.err.Error()
			m.result.Failed++
		} else {
			m.status[msg.key] = done
			m.result.Bytes += max(msg.bytes, 0)
			m.result.Items++
		}
		if !m.busy() {
			m.quitWarn = false
		}
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		// Quitting mid-removal can leave a half-deleted directory; ask twice.
		if m.busy() && !m.quitWarn {
			m.quitWarn = true
			return nil
		}
		return tea.Quit
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
			return nil // let running removals finish; ctrl+c still quits
		}
		return tea.Quit
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup", "ctrl+u":
		i -= m.bodyHeight()
	case "pgdown", "ctrl+d":
		i += m.bodyHeight()
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
	case "s":
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
	keys = slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return m.status[k] == done || m.status[k] == working })
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
	keys = slices.DeleteFunc(keys, func(k string) bool { s := m.status[k]; return s == done || s == working })
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

func (m *Model) startClean() tea.Cmd {
	var cmds []tea.Cmd
	for _, k := range m.confirm {
		m.status[k] = working
		cmds = append(cmds, m.cleanCmd(k))
	}
	m.confirm = nil
	return tea.Batch(cmds...)
}

// cleanCmd copies everything it needs on the UI goroutine; the returned
// command touches no model state.
func (m *Model) cleanCmd(key string) tea.Cmd {
	c, p, a := m.lookup(key)
	cleaner, bytes := m.cleaner, m.size(key)
	if c != nil {
		cache := *c
		paths := cache.SizePaths
		if len(paths) == 0 {
			paths = []string{cache.Path}
		}
		return func() tea.Msg {
			if bytes < 0 {
				bytes = sizeOf(paths...)
			}
			return cleanedMsg{key, bytes, cleaner.Cache(&cache)}
		}
	}
	project, name, path := p.Path, a.Name, a.Path
	return func() tea.Msg {
		if bytes < 0 { // deleted before measuring finished
			bytes = sizeOf(path)
		}
		return cleanedMsg{key, bytes, cleaner.Artifact(project, name)}
	}
}

func sizeOf(paths ...string) int64 {
	var n int64
	for _, p := range paths {
		n += scan.DirSize(p)
	}
	return n
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

func (m *Model) busy() bool {
	for _, s := range m.status {
		if s == working {
			return true
		}
	}
	return false
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
