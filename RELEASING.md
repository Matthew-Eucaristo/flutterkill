# Releasing

Releases are cut by pushing a `v*` tag. The `release` workflow runs the tests and publishes release archives for macOS, Linux and Windows with GoReleaser, using only the built-in `GITHUB_TOKEN` (no secrets to maintain). Release notes come from the commits since the previous tag; `docs:`, `test:`, `ci:` and `chore:` commits are left out.

The Homebrew formula lives in [Matthew-Eucaristo/homebrew-tap](https://github.com/Matthew-Eucaristo/homebrew-tap) as `Formula/flutterkill.rb` and is bumped by hand, which takes a minute and needs no token.

## One-time setup

- Enable **Settings → Code security → Private vulnerability reporting** (referenced by SECURITY.md).
- Record the demo GIF from the synthetic workspace (`brew install vhs`, then `vhs demo/demo.tape`), check that it shows no real paths, commit `demo/demo.gif` and embed it at the top of the README.

## Each release

```sh
V=0.1.0
git tag v$V && git push origin v$V
SHA=$(curl -sL https://github.com/Matthew-Eucaristo/flutterkill/archive/refs/tags/v$V.tar.gz | shasum -a 256 | cut -d' ' -f1)
```

In a clone of the tap, create or update `Formula/flutterkill.rb` from `packaging/homebrew/flutterkill.rb` with the new version in `url` and `$SHA` in `sha256`, commit `flutterkill $V`, and push. Then check it:

```sh
brew update
brew upgrade matthew-eucaristo/tap/flutterkill || brew install matthew-eucaristo/tap/flutterkill
brew test flutterkill
brew audit --strict --online matthew-eucaristo/tap/flutterkill
```
