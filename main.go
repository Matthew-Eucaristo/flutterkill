// Command flutterkill finds and removes regenerable Flutter/Dart build
// artifacts and global caches. Run `flutterkill -h` for usage.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

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
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "flutterkill:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("flutterkill", flag.ContinueOnError)
	var (
		dryRun    = flags.Bool("dry-run", false, "never remove anything; show what would be freed")
		permanent = flags.Bool("permanent", false, "delete permanently instead of moving to the Trash")
		jsonOut   = flags.Bool("json", false, "print the scan as JSON and exit (never removes)")
		list      = flags.Bool("list", false, "print the scan as a table and exit (default when not a terminal)")
		yes       = flags.Bool("yes", false, "non-interactive: clean every matched project artifact (global caches are never included)")
		noGlobal  = flags.Bool("no-global", false, "skip global caches")
		olderThan = flags.String("older-than", "", "only projects inactive for at least this long, e.g. 90d, 6w, 3mo, 1y")
		showVer   = flags.Bool("version", false, "print version")
	)
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), usage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *showVer {
		fmt.Fprintln(stdout, "flutterkill", buildVersion())
		return nil
	}
	if *dryRun && *permanent {
		return errors.New("--dry-run and --permanent are mutually exclusive")
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one root directory")
	}

	root, err := resolveRoot(flags.Arg(0))
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

	// Pipes and redirects get the plain table, never the TUI.
	interactive := isTerminal(stdout) && isTerminal(os.Stdin)
	switch {
	case *jsonOut || *list || (!interactive && !*yes):
		inv, err := inventory.Load(opts)
		if err != nil {
			return err
		}
		inv.MeasureAll()
		if *jsonOut {
			return report.JSON(stdout, inv)
		}
		report.Text(stdout, inv)
		return nil
	case *yes:
		opts.Global = false
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return cleanAll(ctx, stdout, opts, cleaner)
	}

	m := ui.New(opts, cleaner)
	_, err = tea.NewProgram(m).Run()
	// Print the receipt even when the program was interrupted.
	report.Receipt(stdout, m.Outcomes())
	return err
}

// cleanAll removes every artifact of every matched project, without
// prompting, one at a time. Cancelling (ctrl+c) stops before the next item.
func cleanAll(ctx context.Context, w io.Writer, opts inventory.Options, c *clean.Cleaner) error {
	inv, err := inventory.Load(opts)
	if err != nil {
		return err
	}
	inv.MeasureAll()
	var outs []clean.Outcome
	var left int
	for _, p := range inv.Projects {
		for _, a := range p.Artifacts {
			if ctx.Err() != nil {
				left++
				continue
			}
			o := c.RemoveArtifact(p.Path, a.Name, a.Bytes)
			report.Line(w, o)
			outs = append(outs, o)
		}
	}
	report.Totals(w, outs)
	if left > 0 {
		return fmt.Errorf("interrupted; %d item(s) were not touched", left)
	}
	if t := report.Count(outs); t.Failed > 0 {
		return fmt.Errorf("%d item(s) could not be removed", t.Failed)
	}
	return nil
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

func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
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
