# Contributing

Thanks for helping. flutterkill aims to stay small, safe and easy to audit.

## Development

```sh
go test ./...
go vet ./...
GOOS=linux go vet ./...
GOOS=windows go vet ./...
go run . --dry-run
```

Never test removal against real directories; tests use temp dirs and the TUI has `--dry-run`.

## Ground rules

- All removal goes through `internal/clean`; every path is checked by `guard.go` right before it is removed. Keep it that way.
- A new per-project artifact is one line in `scan.ArtifactDirs`; a new global cache is one entry in `caches.rawCatalog` (or a `group` in `caches.groups` for one row per version, whose `Children` rule the guard then allows). Give its location on macOS, Linux and Windows where it exists, add a test and a README row saying what regenerates it.
- Keep dependencies to Bubble Tea and Lip Gloss unless there is a strong reason.
- No real paths, usernames or project names in code, tests, docs, screenshots or demos. Use placeholders such as `~/dev/my_app`.
- Pull requests are squash-merged, so the PR title becomes the commit and the release-notes entry. Use a conventional prefix: `feat:`, `fix:`, `docs:`, `test:`, `ci:` or `chore:` (the last four are left out of release notes).

## Reporting security issues

See [SECURITY.md](SECURITY.md).
