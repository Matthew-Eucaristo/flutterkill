package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/contract"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
)

// The machine contract (docs/contract.md): capabilities, scan and apply.

// exitError carries a contract exit code to main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func usageErr(err error) error { return &exitError{contract.ExitUsage, err} }

// Swapped by tests.
var (
	stdin      io.Reader = os.Stdin
	defaultEnv           = caches.DefaultEnv
)

func newFlags(name, use string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage:\n  %s\n\nFlags:\n", use)
		fs.PrintDefaults()
	}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageErr(err)
	}
	return nil
}

func runCapabilities(args []string, stdout io.Writer) error {
	fs := newFlags("capabilities", "flutterkill capabilities --json")
	asJSON := fs.Bool("json", false, "print as JSON (required)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if !*asJSON || fs.NArg() > 0 {
		return usageErr(errors.New("usage: flutterkill capabilities --json"))
	}
	return contract.Encode(stdout, contract.NewCapabilities(contract.NewTool(buildVersion()), clean.TrashName))
}

// load scans every root; machine-wide caches come from the first one only,
// so they are listed once.
func load(roots []string, global, measure bool) ([]*contract.Entry, int, []string, error) {
	env := defaultEnv()
	home := env.Home
	now := time.Now()
	var entries []*contract.Entry
	var warnings []string
	projects := 0
	seen := map[string]bool{}
	for i, root := range roots {
		inv, err := inventory.Load(inventory.Options{Root: root, Global: global && i == 0, Env: env})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", format.Tilde(root, home), err))
			continue
		}
		if measure {
			inv.MeasureAll()
		}
		for _, e := range contract.Entries(inv, home, now) {
			if !seen[e.ID] { // overlapping roots report an artifact once
				seen[e.ID] = true
				entries = append(entries, e)
			}
		}
		projects += len(inv.Projects)
	}
	if len(warnings) == len(roots) {
		return nil, 0, nil, errors.New(warnings[0])
	}
	return entries, projects, warnings, nil
}

func resolveRoots(args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{""}
	}
	var roots []string
	for _, a := range args {
		r, err := resolveRoot(a)
		if err != nil {
			return nil, usageErr(err)
		}
		roots = append(roots, r)
	}
	return roots, nil
}

func runScan(args []string, stdout io.Writer) error {
	fs := newFlags("scan", "flutterkill scan --json [--no-global] [root...]")
	asJSON := fs.Bool("json", false, "print the scan document (required)")
	noGlobal := fs.Bool("no-global", false, "skip machine-wide caches")
	if err := parse(fs, args); err != nil {
		return err
	}
	if !*asJSON {
		return usageErr(errors.New("scan prints the contract document; pass --json (or run flutterkill --list for a table)"))
	}
	roots, err := resolveRoots(fs.Args())
	if err != nil {
		return err
	}
	entries, projects, warnings, err := load(roots, !*noGlobal, true)
	if err != nil {
		return err
	}
	home := defaultEnv().Home
	var shown []string
	for _, r := range roots {
		shown = append(shown, format.Tilde(r, home))
	}
	return contract.Encode(stdout, contract.NewScanDoc(contract.NewTool(buildVersion()), shown, warnings, projects, entries, time.Now()))
}

func runApply(args []string, stdout io.Writer) error {
	fs := newFlags("apply", "flutterkill apply --plan <file|-> [--dry-run] [--permanent] [--json] [root...]")
	var (
		planPath  = fs.String("plan", "", "plan file, or - for stdin (required)")
		dryRun    = fs.Bool("dry-run", false, "check every item, change nothing")
		permanent = fs.Bool("permanent", false, "delete directories instead of moving them to the "+clean.TrashName)
		asJSON    = fs.Bool("json", false, "print the result document")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *planPath == "" {
		return usageErr(errors.New("apply needs --plan <file|->"))
	}
	if *dryRun && *permanent {
		return usageErr(errors.New("--dry-run and --permanent are mutually exclusive"))
	}
	in := stdin
	if *planPath != "-" {
		f, err := os.Open(*planPath)
		if err != nil {
			return usageErr(err)
		}
		defer f.Close()
		in = f
	}
	plan, err := contract.ReadPlan(in)
	if err != nil {
		return usageErr(err)
	}
	roots, err := resolveRoots(fs.Args())
	if err != nil {
		return err
	}
	entries, _, _, err := load(roots, plan.Global(), false)
	if err != nil {
		return err
	}
	mode := clean.Trash
	switch {
	case *dryRun:
		mode = clean.DryRun
	case *permanent:
		mode = clean.Permanent
	}
	env := defaultEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := &contract.Applier{Mode: mode, Allowed: caches.Known(env)}
	doc := a.Apply(ctx, contract.NewTool(buildVersion()), plan, entries)
	if *asJSON {
		if err := contract.Encode(stdout, doc); err != nil {
			return err
		}
	} else {
		fmt.Fprint(stdout, doc.Text())
	}
	if code := doc.ExitCode(); code != contract.ExitOK {
		return &exitError{code: code}
	}
	return nil
}
