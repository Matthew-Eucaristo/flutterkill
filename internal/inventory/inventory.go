// Package inventory ties a project scan and the global caches together and
// measures them in parallel. Used by both the TUI and the plain-text/JSON output.
package inventory

import (
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/scan"
)

// Options control what Load collects.
type Options struct {
	Root      string
	Global    bool          // include global caches
	OlderThan time.Duration // keep only projects inactive at least this long (0 = all)
	Env       caches.Env
}

type Inventory struct {
	Root     string          `json:"root"`
	Projects []*scan.Project `json:"projects"`
	Caches   []*caches.Cache `json:"caches"`
}

// Load finds projects (with their last activity) and existing global caches.
// Sizes are left at -1; call Measure.
func Load(o Options) (*Inventory, error) {
	projects, err := scan.FindProjects(o.Root)
	if err != nil {
		return nil, err
	}
	parallel(len(projects), func(i int) {
		p := projects[i]
		// Committed files are not regenerable output; the guard would refuse them.
		p.Artifacts = slices.DeleteFunc(p.Artifacts, func(a scan.Artifact) bool { return scan.TrackedFile(a.Path) != "" })
		if len(p.Artifacts) > 0 {
			p.LastActivity, p.ActivitySource = scan.LastActivity(p.Path)
		}
	})

	inv := &Inventory{Root: o.Root, Caches: []*caches.Cache{}}
	cutoff := time.Now().Add(-o.OlderThan)
	pins := map[string][]string{} // from every project, cleaned ones included
	for _, p := range projects {
		if p.FVMVersion != "" {
			pins[p.FVMVersion] = append(pins[p.FVMVersion], p.Name)
		}
		if len(p.Artifacts) > 0 && (o.OlderThan == 0 || p.LastActivity.Before(cutoff)) {
			inv.Projects = append(inv.Projects, p)
		}
	}
	if inv.Projects == nil {
		inv.Projects = []*scan.Project{}
	}
	if o.Global {
		inv.Caches = caches.Discover(o.Env, pins)
	}
	return inv, nil
}

// Sized reports one finished measurement. Key is an artifact path or a cache ID.
type Sized struct {
	Key   string
	Bytes int64
}

// Measure sizes every artifact and cache in parallel and sends one Sized per
// item, then closes out. It does not touch inv; apply results with Apply.
func (inv *Inventory) Measure(out chan<- Sized) {
	type job struct {
		key   string
		paths []string
	}
	var jobs []job
	for _, p := range inv.Projects {
		for _, a := range p.Artifacts {
			jobs = append(jobs, job{a.Path, []string{a.Path}})
		}
	}
	for _, c := range inv.Caches {
		paths := c.SizePaths
		if len(paths) == 0 {
			paths = []string{c.Path}
		}
		jobs = append(jobs, job{c.ID, paths})
	}
	go func() {
		parallel(len(jobs), func(i int) {
			var n int64
			for _, p := range jobs[i].paths {
				n += scan.DirSize(p)
			}
			out <- Sized{jobs[i].key, n}
		})
		close(out)
	}()
}

// Apply records a measurement.
func (inv *Inventory) Apply(s Sized) {
	for _, p := range inv.Projects {
		for i := range p.Artifacts {
			if p.Artifacts[i].Path == s.Key {
				p.Artifacts[i].Bytes = s.Bytes
				return
			}
		}
	}
	for _, c := range inv.Caches {
		if c.ID == s.Key {
			c.Bytes = s.Bytes
			return
		}
	}
}

// MeasureAll measures synchronously; for non-interactive output.
func (inv *Inventory) MeasureAll() {
	ch := make(chan Sized)
	inv.Measure(ch)
	for s := range ch {
		inv.Apply(s)
	}
}

// Total is the reclaimable size of everything measured so far.
func (inv *Inventory) Total() int64 {
	var n int64
	for _, p := range inv.Projects {
		n += p.Bytes()
	}
	for _, c := range inv.Caches {
		if c.Bytes > 0 {
			n += c.Bytes
		}
	}
	return n
}

// parallel runs fn(0..n-1) on a bounded pool and waits.
func parallel(n int, fn func(int)) {
	sem := make(chan struct{}, max(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fn(i)
		}()
	}
	wg.Wait()
}
