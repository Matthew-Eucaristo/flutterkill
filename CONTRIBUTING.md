# Contributing

Thanks for helping. flutterkill aims to stay small, safe and easy to audit.

## Development

```sh
gofmt -l .                 # must print nothing
go vet ./...
GOOS=linux go vet ./...
GOOS=windows go vet ./...
go test -race ./...
go run . --dry-run
```

Never test removal against real directories; tests use temp dirs and the TUI has `--dry-run`.

If you change what the TUI shows, re-record the README demo with [VHS](https://github.com/charmbracelet/vhs): `vhs demo/demo.tape` from the repository root. It builds the tool and runs it against synthetic projects in a temporary `HOME` (see `demo/setup.sh`); commit the regenerated `demo/demo.gif`. CI never renders it.

## Ground rules

- All removal goes through `internal/clean`; every path is checked by `guard.go` right before it is removed. Keep it that way.
- A new per-project artifact is one line in `scan.ArtifactDirs`; a new global cache is one entry in `caches.rawCatalog` (or a `group` in `caches.groups` for one row per version, whose `Children` rule the guard then allows). Give its location on macOS, Linux and Windows where it exists, add a test and a README row saying what regenerates it.
- Keep dependencies to Bubble Tea and Lip Gloss unless there is a strong reason.
- No real paths, usernames or project names in code, tests, docs, screenshots or demos. Use placeholders such as `~/dev/my_app`.
- Pull requests are squash-merged, so the PR title becomes the commit and the release-notes entry. Use a conventional prefix: `feat:`, `fix:`, `docs:`, `test:`, `ci:` or `chore:` (the last four are left out of release notes).

## Reporting security issues

See [SECURITY.md](SECURITY.md).
