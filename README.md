# flutterkill

Reclaim disk space from Flutter and Dart projects — like [npkill](https://github.com/voidcosmos/npkill), but it knows Flutter.

![flutterkill: four Flutter projects sorted by last activity, two artifacts of a two-year-old project moved to the Trash, then the machine-wide caches with the FVM and Gradle versions each project pins](demo/demo.gif)

A Flutter workspace grows quietly: every project keeps `build/`, `.dart_tool/`, CocoaPods and Gradle state, and the machine collects a pub cache, Gradle downloads, old FVM SDKs, old Android Studio caches and Xcode DerivedData. It works on macOS, Linux and Windows, for Android, iOS, macOS, Linux, Windows and web targets alike. `flutterkill` finds all of it, shows how big it is and which projects you haven't touched in months, and moves what you pick to the Trash.

- **Flutter-aware.** Finds projects by `pubspec.yaml` and only reclaims directories at their known place next to it — a folder called `build` anywhere else is never touched.
- **Global caches too.** Pub cache, Dart analysis server, Gradle caches, distributions and JDKs, FVM SDK versions and Gradle versions (with which projects use them), caches of old Android Studio versions, Xcode DerivedData, iOS DeviceSupport, CocoaPods and SwiftPM caches, simulator caches, unavailable simulators.
- **Staleness at a glance.** Last activity per project — the latest commit or uncommitted change (gitignored build output doesn't count), or file times outside git — so the project you shipped a year ago stands out.
- **Recoverable by default.** Items go to the Trash (Recycle Bin on Windows); `--permanent` is opt-in.
- **Small and auditable.** One static binary, dependencies only from Charm ([Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), x/ansi) plus `golang.org/x/sys` for the Windows Recycle Bin; all removal code lives in [`internal/clean`](internal/clean).

## Install

**Homebrew** (macOS, Linux):

```sh
brew install matthew-eucaristo/tap/flutterkill
```

**Go** (1.26+, any OS including Windows):

```sh
go install github.com/Matthew-Eucaristo/flutterkill@latest
```

Prebuilt archives for macOS, Linux and Windows (amd64, arm64) are on the [releases page](https://github.com/Matthew-Eucaristo/flutterkill/releases). On Windows, unzip `flutterkill.exe` into a folder on your `PATH` and run it in Windows Terminal or PowerShell.

## Usage

```sh
flutterkill                  # scan ~/dev (or the current dir if ~/dev does not exist)
flutterkill ~/code           # scan another root
flutterkill --dry-run        # try everything, remove nothing
flutterkill --older-than 6mo # only projects untouched for 6 months
flutterkill --list           # print a table and exit
flutterkill --json           # machine-readable scan, never removes
flutterkill --older-than 1y --yes --dry-run   # preview a non-interactive clean
flutterkill --older-than 1y --yes             # trash artifacts of year-old projects
```

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k`, `PgUp` `PgDn` / `ctrl+u` `ctrl+d`, `g` `G` / `Home` `End` | Move |
| `space` | Select / unselect (a project row selects all its artifacts) |
| `a` | Select / unselect everything in the tab |
| `→` / `l` / `enter`, `←` / `h` | Expand / collapse a project |
| `tab` / `shift+tab`, `1`, `2` | Switch between Projects and Global caches |
| `o` | Sort by size, oldest activity, or name (`s` also works) |
| `d` / `x` / `delete` / `backspace` | Remove the selection (or the row under the cursor) after a `y/N` prompt |
| `?` | Legend: safety labels, marks and bars |
| `q` / `esc` | Quit and print the receipt. While a removal runs, it first cancels the queue |
| `ctrl+c` | Same as `q`; press again to quit at once |

Each row shows a size bar, its size, last activity and a safety label: **● safe** (a project's build output, recreated by its next build) or **▲ caution** (a machine-wide cache: every project shares it, a scanned project may pin it, such as an FVM SDK, and some are cleaned by the owner's own command, which cannot be undone). These are the same levels as `scan --json` (`safe` / `review`), so decruft shows the same judgement. The panel under the list says where the item is, how it comes back and any error. Marks: `○` not selected, `◐` partly, `●` selected, `◌` queued or running, `✓` done, `✗` failed. Every state has its own glyph, so it reads the same with `NO_COLOR`.

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Never remove anything; the UI shows "would free" |
| `--permanent` | Delete instead of moving to the Trash |
| `--older-than DUR` | Only projects inactive at least `DUR` (`90d`, `6w`, `3mo`, `1y`, or a Go duration) |
| `--no-global` | Skip global caches |
| `--list` | Print a table and exit (also the default when stdin or stdout is not a terminal) |
| `--json` | Print JSON and exit |
| `--yes` | Non-interactive: clean every artifact of every matched project. Never touches global caches |
| `--version` | Print the version |

## What it cleans

**Per project** (directories relative to `pubspec.yaml`):

| Directory | Comes back with |
| --- | --- |
| `build/` | next `flutter build` / `flutter run` |
| `.dart_tool/` | `flutter pub get` (flutter run does it for you) |
| `ios/Pods/`, `ios/.symlinks/`, `macos/Pods/` | `pod install` (run by the next iOS/macOS build) |
| `android/.gradle/`, `android/app/.cxx/` | next Android build |
| `ios/Flutter/ephemeral/`, `macos/Flutter/ephemeral/`, `linux/flutter/ephemeral/`, `windows/flutter/ephemeral/` | next build for that platform (these are what `flutter clean` removes there) |

**Global caches** (shown only if present):

| Cache | Location | How it is cleaned |
| --- | --- | --- |
| Pub cache | `$PUB_CACHE`, `~/.pub-cache`, or `%LOCALAPPDATA%\Pub\Cache` on Windows | `dart pub cache clean --force` (also removes `dart pub global` tools); Trash if `dart` is not on `PATH` |
| Dart analysis server | `~/.dartServer` (`%LOCALAPPDATA%\.dartServer` on Windows, or `$ANALYZER_STATE_LOCATION_OVERRIDE`) | Trash; rebuilt when your editor opens a project |
| Gradle caches | `$GRADLE_USER_HOME/caches` or `~/.gradle/caches` | Trash — run `./gradlew --stop` first; dependencies are re-downloaded on the next Android build |
| Gradle distributions | `$GRADLE_USER_HOME/wrapper/dists` or `~/.gradle/wrapper/dists`, one row per version | Trash; the wrapper re-downloads the version a project needs on its next build. The UI says which scanned projects use each version (`android/gradle/wrapper/gradle-wrapper.properties`) |
| Gradle JDK toolchains | `$GRADLE_USER_HOME/jdks` or `~/.gradle/jdks` | Trash — run `./gradlew --stop` first; Gradle re-provisions a JDK when a build needs one |
| FVM Flutter SDKs | `$FVM_CACHE_PATH/versions` or `~/fvm/versions`, one row per version | Trash; `fvm install` restores one. The UI says which scanned projects pin each version (`.fvmrc`, inherited from parent folders) |
| Old Android Studio caches | `~/Library/Caches/Google/AndroidStudio<version>` (macOS), `$XDG_CACHE_HOME/Google` or `~/.cache/Google` (Linux), `%LOCALAPPDATA%\Google` (Windows), one row per version | Trash; the newest version of each channel (stable, Preview) is always kept. Only caches of versions you no longer run, which they would rebuild if started again; settings and plugins (`Application Support`, `~/.config`, `%APPDATA%`) are never touched |
| Xcode DerivedData | `~/Library/Developer/Xcode/DerivedData` | Trash (macOS); rebuilt by the next iOS/macOS build |
| iOS DeviceSupport | `~/Library/Developer/Xcode/iOS DeviceSupport` | Trash (macOS); re-copied when a device is connected |
| CocoaPods cache | `~/Library/Caches/CocoaPods` | Trash (macOS); re-fetched by the next `pod install` |
| Swift Package Manager cache | `~/Library/Caches/org.swift.swiftpm` | Trash (macOS); re-fetched when Xcode resolves the packages of SwiftPM-enabled plugins |
| Simulator caches | `~/Library/Developer/CoreSimulator/Caches` | Trash (macOS); rebuilt when a simulator boots |
| Unavailable simulators | CoreSimulator devices with no runtime | `xcrun simctl delete unavailable` (macOS) |

The Android SDK, Flutter SDK checkouts and emulator images are deliberately not on the list: they are installs, not caches.

## Safety model

Every removal goes through a guard in [`internal/clean/guard.go`](internal/clean/guard.go) immediately before it happens, independently of how the item was found:

1. **Project artifacts** are removed only if the name is one of the fixed artifact paths above, the project is inside the scanned root and is not your home directory, it still has a `pubspec.yaml`, and the artifact is a real directory.
2. **Never real work.** An artifact is refused if it holds a `pubspec.yaml` or `.git` directly, or contains any file tracked by git — generated output is never committed. (Deeper `pubspec.yaml` files are allowed: Flutter's Swift Package Manager integration copies plugin sources into `build/ios/SourcePackages`.)
3. **Symlinks are refused.** No path component between the scanned root and the artifact may be a symlink, and nothing is followed while measuring or deleting. The root itself is resolved once at startup.
4. **Global caches** are removed only if the path is in the fixed catalog in [`internal/caches`](internal/caches) (or is a direct child of one of its per-version directories: FVM versions, Gradle wrapper distributions, or Android Studio caches other than the newest version), is not a symlink, and is specific: `/`, top-level directories, your home directory or anything containing it or the scan root are refused, even when `PUB_CACHE`, `GRADLE_USER_HOME` or `FVM_CACHE_PATH` point there.
5. **Commands** are limited to `dart pub cache clean --force` and `xcrun simctl delete unavailable`; these are permanent and the confirmation marks them in red.
6. **You see what goes.** The confirmation lists the items it will remove (with a count when the list is longer than the screen). A delete only reaches items visible in the current tab; selections in the other tab are left alone.
7. **Trash by default**: `/usr/bin/trash` on macOS 15+ (Finder on older versions), the freedesktop.org home trash on Linux, the Recycle Bin on Windows. On Linux, moves across filesystems are refused rather than copied; on Windows, drives without a Recycle Bin (network, removable) and paths of 260+ characters are refused, and Windows asks before deleting anything too big to recycle; use `--permanent` there. Removals run one at a time; `ctrl+c` or `q` cancels what is still queued, lets the running item finish, and a second `ctrl+c` quits at once. `--yes` stops before the next item on `ctrl+c`.
8. `--dry-run` runs every check and changes nothing. `--json` and `--list` never remove anything.

Moving to the Trash does not free space until you empty it. On exit flutterkill prints a receipt: one line per item, then moved to the Trash, deleted, and delegated to a tool's own command as separate totals, each with the change in free disk space measured before and after, not assumed.

## Privacy and uninstall

flutterkill writes no config, cache, log or state files and makes no network requests. The only things it changes are the items you choose to remove (moved to the Trash, or deleted with `--permanent`); on Linux the first move creates the standard `~/.local/share/Trash` folders if they are missing. It runs read-only `git` queries in your projects with optional locks and fsmonitor disabled, so it does not even refresh their index.

To remove it completely:

```sh
brew uninstall flutterkill
brew autoremove                # removes Go if it was installed only to build flutterkill
brew cleanup --prune=all       # optional: clears Homebrew's whole download and build cache, including flutterkill's
brew untap matthew-eucaristo/tap   # only if nothing else is installed from this tap
```

Installed with `go install`? Delete the binary: `rm "$(go env GOPATH)/bin/flutterkill"` (on Windows, `flutterkill.exe` in the same folder). From a release archive: delete the binary you unpacked.

Anything you moved to the Trash with flutterkill stays there until you empty it or restore it.

## How it compares

| | flutterkill | [npkill](https://github.com/voidcosmos/npkill) | [kondo](https://github.com/tbillington/kondo) | `flutter clean` loops ([fclean](https://pub.dev/packages/fclean) & co.) |
| --- | --- | --- | --- | --- |
| Flutter/Dart projects | ✓ by `pubspec.yaml` | — (Node and other profiles, matched by dir name) | ✓ among many languages | ✓ |
| iOS Pods, Gradle, `.cxx` per project | ✓ | — | partly | — (`flutter clean` leaves Pods) |
| Global caches (pub, Gradle, FVM, Xcode) | ✓ | — | — | — |
| Last-activity / staleness | ✓ git (incl. uncommitted) or mtime | ✓ mtime | ✓ mtime | — |
| Default action | Trash | delete | delete | delete |
| Interactive TUI | ✓ | ✓ | ✓ | — |

If you work across many ecosystems, kondo or npkill may suit you better; flutterkill is for people whose disk is mostly Flutter.

## JSON

```sh
flutterkill --json | jq '.projects[] | select(.last_activity < "2026-01-01") | .path'
```

```json
{
  "root": "/Users/me/dev",
  "projects": [
    {
      "name": "my_app",
      "path": "/Users/me/dev/my_app",
      "flutter": true,
      "fvm_version": "3.24.5",
      "last_activity": "2025-11-02T10:14:03Z",
      "activity_source": "git",
      "artifacts": [{ "name": "build", "path": "/Users/me/dev/my_app/build", "bytes": 412000000 }]
    }
  ],
  "caches": [{ "id": "gradle-caches", "name": "Gradle caches", "path": "/Users/me/.gradle/caches", "bytes": 6600000000, "note": "…" }],
  "total_bytes": 7012000000
}
```

Sizes are disk usage in bytes (like `du`), shown with decimal units (1 GB = 10⁹ bytes) to match Finder.

## Machine contract

For tools that build on flutterkill, such as [decruft](https://github.com/Matthew-Eucaristo/decruft), flutterkill speaks the same versioned contract as [fell](https://github.com/Matthew-Eucaristo/fell) and [nodekill](https://github.com/Matthew-Eucaristo/nodekill):

```sh
flutterkill capabilities --json                       # what this build can do
flutterkill scan --json [--no-global] [root...]       # items with ids, fingerprints and safety reasons (read-only)
flutterkill apply --plan - [--dry-run] [--permanent] [--json] [root...]   # act on ids + fingerprints
```

A caller never deletes paths itself: it sends back `{id, fingerprint}` pairs, and flutterkill scans again, refuses anything that changed, and runs its own guards before removing. See [docs/contract.md](docs/contract.md).

## Development

```sh
go test ./...
go vet ./...
go run . --dry-run
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md). Release notes: [GitHub Releases](https://github.com/Matthew-Eucaristo/flutterkill/releases). The GIF at the top is recorded locally (not in CI) with [VHS](https://github.com/charmbracelet/vhs): `vhs demo/demo.tape`. It runs against the synthetic projects and caches from `demo/setup.sh` in a temporary `HOME` (a small disk image on macOS, so its Trash and free space are the image's own), never against your machine.

Layout: `internal/scan` (find + measure, read-only), `internal/caches` (global cache catalog), `internal/clean` (guard + removal — the only code that deletes), `internal/inventory` (parallel measuring), `internal/ui` (Bubble Tea), `internal/report` (text/JSON), `internal/contract` (the machine contract: scan document, plans, apply).

Adding an artifact directory is one line in `scan.ArtifactDirs`; adding a global cache is one entry in `caches.rawCatalog`. Please include a test and a note on how the directory is regenerated.

## License

[MIT](LICENSE)
