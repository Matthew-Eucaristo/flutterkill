#!/usr/bin/env bash
# Builds a synthetic workspace for the demo recording: fake HOME, fake Flutter
# projects and caches, fixed git identity and prompt. Nothing from the real
# machine (paths, username, projects, simulators) appears in the recording.
# Usage: source demo/setup.sh   (from the repository root)
# Remove: hdiutil detach /tmp/flutterkill-demo/home (macOS); rm -rf /tmp/flutterkill-demo
set -e

DEMO="$(cd /tmp && pwd -P)/flutterkill-demo"
[ -d "$DEMO/home" ] && command -v hdiutil >/dev/null && hdiutil detach -quiet "$DEMO/home" 2>/dev/null
rm -rf "$DEMO"
mkdir -p "$DEMO/bin" "$DEMO/home"
go build -trimpath -o "$DEMO/bin/flutterkill" .
# On macOS the fake HOME is a small disk image: "free" in the header shows the
# image's space, and anything moved to the Trash lands in the image's own
# .Trashes, which goes away with it.
if command -v hdiutil >/dev/null; then
  hdiutil create -quiet -size 2g -fs APFS -volname demo -type SPARSE "$DEMO/home.sparseimage"
  hdiutil attach -quiet -nobrowse -mountpoint "$DEMO/home" "$DEMO/home.sparseimage"
fi
mkdir -p "$DEMO/home/dev"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

blob() { mkdir -p "$1" && dd if=/dev/zero of="$1/blob" bs=1048576 count="$2" status=none; }
stamp_ago() { local t=$(($(date +%s) - $1 * 86400)); date -d "@$t" +%Y%m%d%H%M 2>/dev/null || date -r "$t" +%Y%m%d%H%M; }
days_ago() { date -u -d "@$(($(date +%s) - $1 * 86400))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-"$1"d +%Y-%m-%dT%H:%M:%SZ; }

project() { # name path age_days fvm_version artifact:MB...
  local name=$1 dir="$DEMO/home/dev/$2" age=$3 fvm=$4; shift 4
  mkdir -p "$dir/lib" "$dir/android/gradle/wrapper"
  echo "{\"flutter\":\"$fvm\"}" > "$dir/.fvmrc"
  printf 'distributionUrl=https\\://services.gradle.org/distributions/%s.zip\n' "${GRADLE:-gradle-8.3-all}" \
    > "$dir/android/gradle/wrapper/gradle-wrapper.properties"
  printf 'name: %s\ndependencies:\n  flutter:\n    sdk: flutter\n' "$name" > "$dir/pubspec.yaml"
  echo 'void main() {}' > "$dir/lib/main.dart"
  printf 'build/\n.dart_tool/\nios/Pods/\nandroid/.gradle/\n' > "$dir/.gitignore"
  for a in "$@"; do blob "$dir/${a%%:*}" "${a##*:}"; done
  local when; when=$(days_ago "$age")
  git -C "$dir" init -q
  git -C "$dir" add .
  GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com \
    GIT_AUTHOR_DATE=$when GIT_COMMITTER_DATE=$when git -C "$dir" commit -qm init
  # Ages also come from file times: DEVELOPER_DIR below disables the macOS git shim.
  find "$dir" -not -path '*/.git/*' -exec touch -t "$(stamp_ago "$age")" {} +
}

# Four projects: a big one untouched for a year, an older archived one on an
# older SDK, a side project and the app worked on today.
project shop_app clients/shop_app 430 3.24.5 build:60 .dart_tool:40 ios/Pods:30 android/.gradle:10
GRADLE=gradle-7.6-all project legacy_app archive/legacy_app 800 3.19.6 build:28 ios/Pods:18 .dart_tool:9
project weather side/weather 120 3.24.5 build:20 .dart_tool:15
project my_app my_app 0 3.24.5 build:12 .dart_tool:8
# Machine-wide caches: pinned and unpinned SDK and Gradle versions side by side.
blob "$DEMO/home/.gradle/caches" 75
blob "$DEMO/home/.pub-cache/hosted" 45
blob "$DEMO/home/fvm/versions/3.24.5" 30
blob "$DEMO/home/fvm/versions/3.19.6" 25
blob "$DEMO/home/fvm/versions/3.13.9" 22
blob "$DEMO/home/.gradle/wrapper/dists/gradle-8.3-all/demo" 16
blob "$DEMO/home/.gradle/wrapper/dists/gradle-7.6-all/demo" 14
blob "$DEMO/home/.gradle/wrapper/dists/gradle-7.4-all/demo" 12

# DEVELOPER_DIR=/nonexistent keeps `xcrun simctl` from listing this machine's simulators.
export HOME="$DEMO/home" PATH="$DEMO/bin:/usr/bin:/bin" PS1='$ ' DEVELOPER_DIR=/nonexistent
unset PUB_CACHE GRADLE_USER_HOME FVM_CACHE_PATH XDG_DATA_HOME
cd "$HOME"
set +e
