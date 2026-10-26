# flutterkill

npkill-style TUI that reclaims disk space from Flutter/Dart projects and global caches on macOS, Linux and Windows. Go + Bubble Tea v2 + Lip Gloss v2 (+ charmbracelet/x/ansi); keep dependencies to Charm modules, plus `golang.org/x/sys/windows` for the Recycle Bin.

## Layout
- `internal/scan` — find projects (`pubspec.yaml`), artifacts, sizes, last activity. Read-only.
- `internal/caches` — fixed catalog of global caches (`rawCatalog`), per-version groups (`groups`: FVM, Gradle dists, old Android Studio), the `Allowed` rules (exact paths + `Children`) and the allowed clean-up commands. Per-OS locations come from `Env` (Windows: `%LOCALAPPDATA%`).
- `internal/clean` — the only code that removes anything. `guard.go` checks every path right before removal; `trash_{darwin,linux,windows}.go` move to the OS trash.
- `internal/inventory` — load + parallel measuring shared by TUI and `--list`/`--json`.
- `internal/ui` — Bubble Tea model (`model.go`) and rendering (`view.go`).
- `internal/report` — text/JSON output. `main.go` — flags and mode dispatch.
- `packaging/homebrew/flutterkill.rb` — source-build formula template for the tap; `RELEASING.md` — release steps.
- `demo/` — synthetic workspace (`setup.sh`) and VHS tape for the README demo.

## Invariants
- Never remove a path that did not pass `clean.CheckArtifact` / `CheckCache` / `CheckCommand`.
- Artifacts match only by position next to `pubspec.yaml` (`scan.ArtifactDirs`), never by name alone, and are refused if they hold git-tracked files or a top-level `pubspec.yaml`/`.git`.
- Refuse symlinks; never follow them when measuring or deleting.
- Default is Trash; permanent deletion only with `--permanent` or the two built-in commands.
- `--dry-run`, `--list`, `--json` must never change the filesystem.

## Release & privacy
- Public repo. No PII anywhere (code, tests, docs, demos, screenshots, commit messages): no real home paths, usernames, hostnames, project names, emails or timezones. Use placeholders like `~/dev/my_app`; demos only via `demo/setup.sh`.
- Commit with the GitHub noreply identity. Review `git add -n .` before committing; never commit built binaries or `dist/`.
- The tool writes no config, cache or state and makes no network requests; git queries run with `GIT_OPTIONAL_LOCKS=0` and `core.fsmonitor=false`. Any future state must be documented in the README's uninstall section.
- Homebrew: source-build formula in `Matthew-Eucaristo/homebrew-tap` (`Formula/flutterkill.rb`), bumped by hand per RELEASING.md (no tap token in CI). GoReleaser only builds release archives with `GITHUB_TOKEN` (`brews` is deprecated; casks are not used).
- Never create the repo, push, tag, release or edit the tap without the user's go-ahead.

## Verify
`gofmt -l .` (empty), `go vet ./...`, `GOOS=linux go vet ./...`, `GOOS=windows go vet ./...`, `go test -race ./...`; CI runs tests on macOS, Linux and Windows. Test paths must be portable (use `filepath`, temp dirs, the `abs` helper in caches tests). Never test removal against real user directories; use temp dirs or `--dry-run`.
