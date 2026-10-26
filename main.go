// Command flutterkill finds and removes regenerable Flutter/Dart build
// artifacts and global caches. Run `flutterkill -h` for usage.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/flutterkill/internal/caches"
	"github.com/Matthew-Eucaristo/flutterkill/internal/clean"
	"github.com/Matthew-Eucaristo/flutterkill/internal/format"
	"github.com/Matthew-Eucaristo/flutterkill/internal/inventory"
	"github.com/Matthew-Eucaristo/flutterkill/internal/report"
	"github.com/Matthew-Eucaristo/flutterkill/internal/ui"
)

var version = "" // set by -ldflags at release time

const usage = `flutterkill - reclaim disk space from Flutter/Dart projects and caches.

Usage:
  flutterkill [flags] [root]      root defaults to ~/dev, else the current directory

Interactive by default. Removed items go to the Trash unless --permanent.

Flags:
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "flutterkill:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dryRun    = flag.Bool("dry-run", false, "never remove anything; show what would be freed")
		permanent = flag.Bool("permanent", false, "delete permanently instead of moving to the Trash")
		jsonOut   = flag.Bool("json", false, "print the scan as JSON and exit (never removes)")
		list      = flag.Bool("list", false, "print the scan as a table and exit (default when not a terminal)")
		yes       = flag.Bool("yes", false, "non-interactive: clean every matched project artifact (global caches are never included)")
		noGlobal  = flag.Bool("no-global", false, "skip global caches")
		olderThan = flag.String("older-than", "", "only projects inactive for at least this long, e.g. 90d, 6w, 3mo, 1y")
		showVer   = flag.Bool("version", false, "print version")
	)
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVer {
		fmt.Println("flutterkill", buildVersion())
		return nil
	}
	if *dryRun && *permanent {
		return errors.New("--dry-run and --permanent are mutually exclusive")
	}
	if flag.NArg() > 1 {
		return errors.New("expected at most one root directory")
	}

	root, err := resolveRoot(flag.Arg(0))
	if err != nil {
		return err
	}
	opts := inventory.Options{Root: root, Global: !*noGlobal, Env: caches.DefaultEnv()}
	if *olderThan != "" {
		if opts.OlderThan, err = format.ParseAge(*olderThan); err != nil {
			return err
		}
	}
	mode := clean.Trash
	switch {
	case *dryRun:
		mode = clean.DryRun
	case *permanent:
		mode = clean.Permanent
	}
	cleaner := &clean.Cleaner{Root: root, Mode: mode, Allowed: caches.Known(opts.Env)}

	interactive := isTerminal(os.Stdout) && isTerminal(os.Stdin)
	switch {
	case *jsonOut || *list || (!interactive && !*yes):
		inv, err := inventory.Load(opts)
		if err != nil {
			return err
		}
		inv.MeasureAll()
		if *jsonOut {
			return report.JSON(os.Stdout, inv)
		}
		report.Text(os.Stdout, inv)
		return nil
	case *yes:
		opts.Global = false
		return cleanAll(opts, cleaner)
	}

	m := ui.New(opts, cleaner)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	printResult(m.Result(), mode)
	return nil
}

// cleanAll removes every artifact of every matched project, without prompting.
func cleanAll(opts inventory.Options, c *clean.Cleaner) error {
	inv, err := inventory.Load(opts)
	if err != nil {
		return err
	}
	inv.MeasureAll()
	home, _ := os.UserHomeDir()
	verb := map[clean.Mode]string{clean.Trash: "trashed", clean.Permanent: "deleted", clean.DryRun: "would remove"}[c.Mode]
	var res ui.Result
	for _, p := range inv.Projects {
		for _, a := range p.Artifacts {
			if err := c.Artifact(p.Path, a.Name); err != nil {
				fmt.Fprintf(os.Stderr, "skipped  %s: %v\n", format.Tilde(a.Path, home), err)
				res.Failed++
				continue
			}
			fmt.Printf("%-12s %9s  %s\n", verb, format.Bytes(a.Bytes), format.Tilde(a.Path, home))
			res.Bytes += a.Bytes
			res.Items++
		}
	}
	printResult(res, c.Mode)
	if res.Failed > 0 {
		return fmt.Errorf("%d item(s) could not be removed", res.Failed)
	}
	return nil
}

func printResult(r ui.Result, mode clean.Mode) {
	if r.Items == 0 && r.Failed == 0 {
		return
	}
	switch mode {
	case clean.DryRun:
		fmt.Printf("Dry run: would free %s from %d item(s). Nothing was removed.\n", format.Bytes(r.Bytes), r.Items)
	case clean.Permanent:
		fmt.Printf("Freed %s from %d item(s).\n", format.Bytes(r.Bytes), r.Items)
	default:
		fmt.Printf("Moved %s (%d item(s)) to the Trash. Empty the Trash to reclaim the space.\n", format.Bytes(r.Bytes), r.Items)
	}
}

// resolveRoot picks the scan root and resolves symlinks, so the path-safety
// checks compare real paths.
func resolveRoot(arg string) (string, error) {
	if arg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		arg = filepath.Join(home, "dev")
		if fi, err := os.Stat(arg); err != nil || !fi.IsDir() {
			arg = "."
		}
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", arg)
	}
	return real, nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}
