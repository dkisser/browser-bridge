#!/usr/bin/env bats
load helpers

# Find a bash >= 4. On macOS without sudo, /bin/bash is bash 3.2; brew installs
# bash 5 at /opt/homebrew/bin/bash or /usr/local/bin/bash. On Linux /bin/bash
# is already modern.
find_modern_bash() {
  for candidate in /opt/homebrew/bin/bash /usr/local/bin/bash /usr/bin/bash /bin/bash; do
    if [[ -x "$candidate" ]] && "$candidate" -c '(( BASH_VERSINFO[0] >= 4 ))'; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

setup_file() {
  # Build the real Go CLI once per file; the fake runtime tarball ships it
  # as bin/bridge (see helpers.make_fake_runtime_tarball). HOME is the fake
  # test HOME here, so restore the real one for go build to reach the shared
  # module/build caches.
  local out
  out="$(mktemp -d "${TMPDIR:-/tmp}/bb-bridge-bin.XXXXXX")"
  (cd "$BB_TEST_ROOT/apps/bridge-core" && HOME="$BB_REAL_HOME" CGO_ENABLED=0 go build -o "$out/bridge" ./cmd/bridge)
  export BB_TEST_BRIDGE_BIN="$out/bridge"
}

setup() {
  # The Go bin/bridge shipped by the fake tarball is compiled for the host OS
  # (runtime.GOOS), so on macOS `bridge service up|down` always walks the
  # launchd path regardless of any fake uname. Intercept launchctl for EVERY
  # test so the suite can never touch the developer's real launchd — a real
  # bootstrap/bootout once killed the production bridge service mid-run.
  make_fake_launchctl
}

@test "install.sh check_prereqs succeeds when all required tools are present" {
  bash_path=$(find_modern_bash)
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_prereq.sh"
  echo 'check_prereqs; echo OK' >> "$BB_TEST_TMP/test_prereq.sh"
  run "$bash_path" "$BB_TEST_TMP/test_prereq.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}

# ---------------------------------------------------------------------------
# Task 9: resolve_version + download_extension
# ---------------------------------------------------------------------------

@test "resolve_version accepts BB_VERSION env override" {
  bash_path=$(find_modern_bash)
  # Source install.sh minus the trailing "main "$@"" call, then call resolve_version.
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rv.sh"
  echo 'resolve_version' >> "$BB_TEST_TMP/test_rv.sh"
  BB_VERSION="v1.2.3" run "$bash_path" "$BB_TEST_TMP/test_rv.sh"
  [ "$status" -eq 0 ]
  [ "$output" = "v1.2.3" ]
}

@test "resolve_version rejects malformed BB_VERSION" {
  bash_path=$(find_modern_bash)
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rv.sh"
  echo 'resolve_version' >> "$BB_TEST_TMP/test_rv.sh"
  BB_VERSION="not-a-version" run "$bash_path" "$BB_TEST_TMP/test_rv.sh"
  [ "$status" -ne 0 ]
  [[ "$output" == *"version"* ]]
}

@test "resolve_version treats BB_VERSION=latest as a query for the latest release" {
  bash_path=$(find_modern_bash)
  # `bridge [service] update` from older releases passes BB_VERSION=latest;
  # it must resolve via the GitHub API like an unset BB_VERSION, not fail
  # BB-E022. Stub curl (API response) and python3 (tag extraction).
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rv_latest.sh"
  cat >> "$BB_TEST_TMP/test_rv_latest.sh" <<'SCRIPT'
curl() { printf '{"tag_name": "v9.9.9"}'; }
python3() { printf '%s' 'v9.9.9'; }
resolve_version
SCRIPT
  BB_VERSION="latest" run "$bash_path" "$BB_TEST_TMP/test_rv_latest.sh"
  [ "$status" -eq 0 ]
  [ "$output" = "v9.9.9" ]
}

@test "download_extension exits BB-E020 on SHA-256 mismatch" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "actual-zip-content" > "$BB_TEST_TMP/stage/fake.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" fake.zip )
  echo "0000000000000000000000000000000000000000000000000000000000000000  browser-bridge-extension-v9.9.9.zip" \
    > "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip.sha256"
  start_mock_http 18745

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_dl.sh"
  cat >> "$BB_TEST_TMP/test_dl.sh" <<'SCRIPT'
ORG='127.0.0.1:18745'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_extension
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_dl.sh"
  stop_mock_http
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E020"* ]]
}

@test "download_extension exits BB-E021 on HTTP error" {
  bash_path=$(find_modern_bash)
  # No mock server — port 1 is unbound and refuses connections.
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_dl.sh"
  cat >> "$BB_TEST_TMP/test_dl.sh" <<'SCRIPT'
ORG='127.0.0.1:1'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_extension
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_dl.sh"
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E021"* ]]
}

@test "download_extension succeeds with correct sha256" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "real-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )
  start_mock_http 18746

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_dl.sh"
  cat >> "$BB_TEST_TMP/test_dl.sh" <<'SCRIPT'
ORG='127.0.0.1:18746'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
mkdir -p "$BB_HOME/extension"
download_extension
ls "$BB_HOME/extension"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_dl.sh"
  stop_mock_http
  [ "$status" -eq 0 ]
  [[ "$output" == *"bb.zip"* ]]
}

# ---------------------------------------------------------------------------
# Task 10: download_runtime
# ---------------------------------------------------------------------------

@test "download_runtime exits BB-E029 on SHA-256 mismatch" {
  bash_path=$(find_modern_bash)
  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  mkdir -p "$BB_TEST_TMP/www"
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  echo "0000000000000000000000000000000000000000000000000000000000000000  $tarball_name" \
    > "$BB_TEST_TMP/www/${tarball_name}.sha256"
  start_mock_http 18760

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rt.sh"
  cat >> "$BB_TEST_TMP/test_rt.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18760'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_runtime v9.9.9 arm64 "http://${ORG}"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_rt.sh"
  stop_mock_http
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E029"* ]]
}

@test "download_runtime succeeds with correct sha256 and extracts binaries" {
  bash_path=$(find_modern_bash)
  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  mkdir -p "$BB_TEST_TMP/www"
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"
  start_mock_http 18761

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rt.sh"
  cat >> "$BB_TEST_TMP/test_rt.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18761'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_runtime v9.9.9 arm64 "http://${ORG}"
ls "$BB_HOME/bin"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home2" run "$bash_path" "$BB_TEST_TMP/test_rt.sh"
  stop_mock_http
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge"* ]]
  [[ "$output" != *"bridge-core"* ]]
}

@test "download_runtime removes the leftover bridge-core binary from the two-binary era" {
  bash_path=$(find_modern_bash)
  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  mkdir -p "$BB_TEST_TMP/www"
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"
  start_mock_http 18778

  # Seed the ADR-0012-era leftover the migration block must remove: a
  # standalone bridge-core daemon binary next to an older bridge CLI.
  mkdir -p "$BB_TEST_TMP/bb-home/bin"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$BB_TEST_TMP/bb-home/bin/bridge-core"
  chmod +x "$BB_TEST_TMP/bb-home/bin/bridge-core"

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rt_migrate.sh"
  cat >> "$BB_TEST_TMP/test_rt_migrate.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18778'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_runtime v9.9.9 arm64 "http://${ORG}"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_rt_migrate.sh"
  stop_mock_http
  [ "$status" -eq 0 ]
  [[ ! -e "$BB_TEST_TMP/bb-home/bin/bridge-core" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/bridge" ]]
}

# ---------------------------------------------------------------------------
# Task 11: write_artifacts + print_next_steps
# ---------------------------------------------------------------------------

@test "write_artifacts emits version file and PATH symlink" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_HOME/bin"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$BB_HOME/bin/bridge"
  chmod +x "$BB_HOME/bin/bridge"
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    BB_HOME='$BB_HOME'
    HOME='$HOME'
    info() { :; }
    write_artifacts v9.9.9
    cat '$BB_HOME/version'
    test -L '$HOME/.local/bin/bridge'
    readlink '$HOME/.local/bin/bridge'
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"v9.9.9"* ]]
  [[ "$output" == *"$BB_HOME/bin/bridge"* ]]
}

@test "print_next_steps mentions PATH, Chrome load, and the visible extension directory" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"PATH"* ]]
  [[ "$output" == *"Chrome"* ]]
  [[ "$output" == *"$BB_EXTENSION_DIR/extension/"* ]]
  [[ "$output" != *"bridge up"* ]]
  [[ "$output" == *"Bridge services are already running"* ]]
}

@test "print_next_steps emits real newlines for autostart/skills notes (not literal backslash-n)" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    AUTOSTART=true SKILLS_STATUS=installed SKILLS_INSTALLED_DIRS='$HOME/.agents/skills
$HOME/.claude/skills' print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" != *"\\n"* ]]
  [[ "$output" == *"Login auto-start is enabled"* ]]
  [[ "$output" == *"Installed the browser-bridge skill into:"* ]]
  [[ "$output" == *"    $HOME/.agents/skills"* ]]
  [[ "$output" == *"    $HOME/.claude/skills"* ]]
  [[ "$output" == *"(available the next time you start your agent)."* ]]
  # Skills note must be on its own line, not glued to the previous one.
  [[ "$output" == *$'\n  Installed the browser-bridge skill into:'* ]]
  [[ "$output" == *$'\n  Login auto-start is enabled; bridge services will start automatically when you log in.\n'* ]]
}

@test "print_next_steps reports failed skills install when SKILLS_STATUS=failed" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    AUTOSTART=false SKILLS_STATUS=failed print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"Skills installation was attempted but failed"* ]]
}

@test "print_next_steps reports skipped-opt-out when SKILLS_STATUS=skipped-opt-out" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    AUTOSTART=false SKILLS_STATUS=skipped-opt-out print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"Skills: skipped (--no-skills)."* ]]
}

@test "print_next_steps reports skipped-no-target when SKILLS_STATUS=skipped-no-target" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    AUTOSTART=false SKILLS_STATUS=skipped-no-target print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"Skills: skipped — neither ~/.agents nor ~/.claude exists."* ]]
}

@test "print_next_steps omits the skills note when no skills were installed" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    # Extract print_next_steps's definition into this subshell. Matches
    # the function header up to the FIRST line that is just '}' — robust
    # only because the function body has no nested '{', '}' (no case
    # branches with braces, no ${var/pat/replace} substitutions, no
    # blocks). If a future contributor adds any of those, this sed stops
    # at the first internal '}' and the extracted source is truncated;
    # switch to brace-counting extraction (e.g. awk) when that happens.
    source <(sed -n '/^print_next_steps() {/,/^}$/p' "$INSTALL_SH")
    BB_HOME='$BB_HOME'
    BB_EXTENSION_DIR='$BB_EXTENSION_DIR'
    print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" != *"Installed the browser-bridge skill"* ]]
}

# ---------------------------------------------------------------------------
# Task 12: end-to-end install against mock release server
# ---------------------------------------------------------------------------

@test "install.sh end-to-end against mock release server" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Linux
  start_mock_http 18762
  bash_path=$(find_modern_bash)

  # Source install.sh (minus trailing 'main "$@"'), override ORG, then call main.
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18762'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ ! -e "$BB_TEST_TMP/bb-home/bin/bridge-core" ]]
  [[ -L "$HOME/.local/bin/bridge" ]]
}

@test "install.sh idempotent: second run upgrades in place" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Linux
  start_mock_http 18763
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18763'
main --no-skills
SCRIPT

  # First run — fresh install
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  first_status=$status

  # Second run — upgrade in place
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$first_status" -eq 0 ]
  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ "$(cat "$BB_TEST_TMP/bb-home/version")" == "v9.9.9" ]]
}

@test "install.sh enables auto-start by default on macOS" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  start_mock_http 18774
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart_default.sh"
  cat >> "$BB_TEST_TMP/test_autostart_default.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18774'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-autostart" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_default.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  [[ "$output" == *"Login auto-start enabled"* ]]
  grep -q 'bootstrap' "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "install.sh --no-autostart does not enable auto-start" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  start_mock_http 18775
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart_off.sh"
  cat >> "$BB_TEST_TMP/test_autostart_off.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18775'
main --no-skills --no-autostart
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-no-autostart" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_off.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  # Services still start now, bootstrapped from the staging plist (not login auto-start).
  grep -q "launchagents" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "install.sh succeeds when service start fails due to launchctl error" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Darwin
  make_fake_id 501
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"

  start_mock_http 18777
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart_fail.sh"
  cat >> "$BB_TEST_TMP/test_autostart_fail.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18777'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-autostart-fail" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_fail.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"could not auto-start"* ]]
  [[ -f "$BB_TEST_TMP/bb-home-autostart-fail/version" ]]
}

@test "install.sh does not attempt auto-start on Linux" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Linux
  make_fake_launchctl

  start_mock_http 18776
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart_linux.sh"
  cat >> "$BB_TEST_TMP/test_autostart_linux.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18776'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-linux" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_linux.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  [[ "$output" != *"Login auto-start enabled"* ]]
}

@test "build-installer.sh copies install.sh verbatim (templates embedded in Go since ADR-0012)" {
  bash "$BB_TEST_ROOT/.github/scripts/build-installer.sh" "$BB_TEST_TMP/self-contained-install.sh"
  [ -x "$BB_TEST_TMP/self-contained-install.sh" ]
  diff -u "$BB_TEST_ROOT/install/install.sh" "$BB_TEST_TMP/self-contained-install.sh"
  ! grep -q '^__BB_TEMPLATE_BEGIN__$' "$BB_TEST_TMP/self-contained-install.sh"
}

@test "self-contained install.sh installs without fetching template from main" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  bash "$BB_TEST_ROOT/.github/scripts/build-installer.sh" "$BB_TEST_TMP/self-contained-install.sh"

  make_fake_uname Linux
  start_mock_http 18764
  bash_path=$(find_modern_bash)

  BB_HOME="$BB_TEST_TMP/bb-home-sc" \
  BB_VERSION="v9.9.9" \
  BB_INSTALL_ARCH=arm64 \
  BB_NO_SKILLS=true \
  ORG='127.0.0.1:18764' \
  REPO='browser-bridge' \
  run "$bash_path" "$BB_TEST_TMP/self-contained-install.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home-sc/version" ]]
  [[ "$(cat "$BB_TEST_TMP/bb-home-sc/version")" == "v9.9.9" ]]
  [[ -x "$BB_TEST_TMP/bb-home-sc/bin/bridge" ]]
  [[ ! -e "$BB_TEST_TMP/bb-home-sc/bin/bridge-core" ]]
}


# ---------------------------------------------------------------------------
# Task 13: skills installation (default-on since ADR-0015)
# ---------------------------------------------------------------------------

@test "install_skills installs a single skill directory" {
  bash_path=$(find_modern_bash)
  local src dest
  src=$(mktemp -d)
  dest=$(mktemp -d)
  mkdir -p "$src/single-test-skill"
  cat > "$src/single-test-skill/SKILL.md" <<'EOF'
---
name: single-test-skill
description: test
---
EOF

  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    install_skills '$src/single-test-skill' '$dest'
  "
  [ "$status" -eq 0 ]
  [[ -f "$dest/single-test-skill/SKILL.md" ]]
  rm -rf "$src" "$dest"
}

@test "install_skills installs multiple skills from a collection directory" {
  bash_path=$(find_modern_bash)
  local src dest
  src=$(mktemp -d)
  dest=$(mktemp -d)
  mkdir -p "$src/skill-a" "$src/skill-b"
  echo "name: skill-a" > "$src/skill-a/SKILL.md"
  echo "name: skill-b" > "$src/skill-b/SKILL.md"

  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    install_skills '$src' '$dest'
  "
  [ "$status" -eq 0 ]
  [[ -f "$dest/skill-a/SKILL.md" ]]
  [[ -f "$dest/skill-b/SKILL.md" ]]
  rm -rf "$src" "$dest"
}

@test "install_skills fails when source has no valid skills" {
  bash_path=$(find_modern_bash)
  local src dest
  src=$(mktemp -d)
  dest=$(mktemp -d)

  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    install_skills '$src' '$dest'
  "
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E202"* ]]
  rm -rf "$src" "$dest"
}

@test "install_skills replaces an existing skill directory so no stale files survive" {
  bash_path=$(find_modern_bash)
  local src dest
  src=$(mktemp -d)
  dest=$(mktemp -d)
  mkdir -p "$src/browser-bridge"
  echo "name: browser-bridge" > "$src/browser-bridge/SKILL.md"
  # Pre-existing install from an older version, with a file the new skill no
  # longer ships.
  mkdir -p "$dest/browser-bridge"
  echo "old" > "$dest/browser-bridge/SKILL.md"
  echo "stale" > "$dest/browser-bridge/stale.txt"

  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    install_skills '$src/browser-bridge' '$dest'
  "
  [ "$status" -eq 0 ]
  [[ -f "$dest/browser-bridge/SKILL.md" ]]
  [[ "$(cat "$dest/browser-bridge/SKILL.md")" == "name: browser-bridge" ]]
  [[ ! -e "$dest/browser-bridge/stale.txt" ]]
  rm -rf "$src" "$dest"
}

@test "parse_install_args handles --skills-dir" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    parse_install_args --skills-dir /tmp/agent-skills
    [[ \"\$SKILLS_TARGET_DIR\" == '/tmp/agent-skills' ]]
    echo OK
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}

@test "parse_install_args accepts --with-skills as a deprecated no-op" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    parse_install_args --with-skills
    [[ \"\$NO_SKILLS\" == 'false' ]]
    [[ \"\$SAW_DEPRECATED_SKILLS_OPT\" == '1' ]]
    echo OK
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"--with-skills"* ]]
  [[ "$output" == *"are deprecated"* ]]
  [[ "$output" == *"OK"* ]]
}

@test "parse_install_args accepts BB_WITH_SKILLS=true as a deprecated no-op" {
  bash_path=$(find_modern_bash)
  BB_WITH_SKILLS=true run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    parse_install_args
    [[ \"\$NO_SKILLS\" == 'false' ]]
    [[ \"\$SAW_DEPRECATED_SKILLS_OPT\" == '1' ]]
    echo OK
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"BB_WITH_SKILLS"* ]]
  [[ "$output" == *"are deprecated"* ]]
  [[ "$output" == *"OK"* ]]
}

@test "parse_install_args suppresses deprecation note when --no-skills is also set" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    parse_install_args --with-skills --no-skills
    [[ \"\$NO_SKILLS\" == 'true' ]]
    [[ \"\$SAW_DEPRECATED_SKILLS_OPT\" == '1' ]]
    echo OK
  "
  [ "$status" -eq 0 ]
  # Note suppressed — "use --no-skills to opt out" while --no-skills has just
  # been set would read as "we ignored your opt-out".
  [[ "$output" != *"are deprecated"* ]]
  [[ "$output" == *"OK"* ]]
}

@test "install.sh installs skills by default into both ~/.agents/skills and ~/.claude/skills" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Fake skills release asset: top-level browser-bridge/ dir, like
  # build-skills-tarball.sh produces.
  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"

  # A local ./skills checkout must NOT be used as the skills source — the
  # installer installs from the release tarball.
  mkdir -p "$BB_TEST_TMP/cwd/skills/browser-bridge"
  echo "LOCAL-CHECKOUT-MARKER" > "$BB_TEST_TMP/cwd/skills/browser-bridge/SKILL.md"

  # Both agent config roots exist → both receive the skill.
  mkdir -p "$HOME/.agents" "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18780
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e_skills.sh"
  cat >> "$BB_TEST_TMP/test_e2e_skills.sh" <<SCRIPT
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18780'
cd '$BB_TEST_TMP/cwd'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e_skills.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -f "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ -L "$HOME/.local/bin/bridge" ]]
  [[ -f "$HOME/.agents/skills/browser-bridge/SKILL.md" ]]
  [[ -f "$HOME/.claude/skills/browser-bridge/SKILL.md" ]]
  # Content comes from the release tarball, not the local checkout.
  ! grep -q "LOCAL-CHECKOUT-MARKER" "$HOME/.agents/skills/browser-bridge/SKILL.md"
  [[ "$output" == *"Installed the browser-bridge skill into:"* ]]
}

@test "install.sh installs skills only into the default targets whose parent exists" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"

  # Only ~/.agents exists — ~/.claude must not be created for the user.
  mkdir -p "$HOME/.agents"

  make_fake_uname Linux
  start_mock_http 18781
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_single_parent.sh"
  cat >> "$BB_TEST_TMP/test_single_parent.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18781'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-single-parent" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_single_parent.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$HOME/.agents/skills/browser-bridge/SKILL.md" ]]
  [[ ! -e "$HOME/.claude" ]]
  [[ "$output" == *"Installed the browser-bridge skill into:"* ]]
  [[ "$output" != *".claude/skills"* ]]
}

@test "install.sh skips skills with a note when neither ~/.agents nor ~/.claude exists" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Deliberately no skills tarball in the mock release: with no eligible
  # target the installer must skip before ever trying to download it.

  make_fake_uname Linux
  start_mock_http 18782
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_no_parents.sh"
  cat >> "$BB_TEST_TMP/test_no_parents.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18782'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-no-parents" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_no_parents.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"skipping skills installation"* ]]
  [[ "$output" != *"Installed the browser-bridge skill"* ]]
  [[ -f "$BB_TEST_TMP/bb-home-no-parents/version" ]]
  [[ ! -e "$HOME/.agents" ]]
  [[ ! -e "$HOME/.claude" ]]
}

@test "install.sh --no-skills skips skills even with --with-skills" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Serve the skills tarball and offer both default targets, so --no-skills
  # (not a missing target) is what skips the install.
  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"
  mkdir -p "$HOME/.agents" "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18773
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_no_skills_override.sh"
  cat >> "$BB_TEST_TMP/test_no_skills_override.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18773'
main --with-skills --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-no-skills-override" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_no_skills_override.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ ! -e "$HOME/.agents/skills" ]]
  [[ ! -e "$HOME/.claude/skills" ]]
  [[ "$output" != *"Installed the browser-bridge skill"* ]]
}

@test "install.sh --with-skills is accepted as a no-op and still installs skills (default on)" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"

  mkdir -p "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18783
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_with_skills_noop.sh"
  cat >> "$BB_TEST_TMP/test_with_skills_noop.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18783'
main --with-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-with-skills" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_with_skills_noop.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"--with-skills"* ]]
  [[ "$output" == *"are deprecated"* ]]
  [[ -f "$HOME/.claude/skills/browser-bridge/SKILL.md" ]]
}

@test "install.sh replaces a pre-existing modified browser-bridge skill on upgrade" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"

  # Older install with local modifications and a stale file that the new
  # skill version no longer ships — both must be gone after the upgrade.
  mkdir -p "$HOME/.claude/skills/browser-bridge"
  echo "USER-MODIFIED-OLD-VERSION" > "$HOME/.claude/skills/browser-bridge/SKILL.md"
  echo "stale" > "$HOME/.claude/skills/browser-bridge/stale-file.txt"

  make_fake_uname Linux
  start_mock_http 18784
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_replace_skills.sh"
  cat >> "$BB_TEST_TMP/test_replace_skills.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18784'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-replace" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_replace_skills.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$HOME/.claude/skills/browser-bridge/SKILL.md" ]]
  [[ "$(cat "$HOME/.claude/skills/browser-bridge/SKILL.md")" == *"name: browser-bridge"* ]]
  [[ ! -e "$HOME/.claude/skills/browser-bridge/stale-file.txt" ]]
}

@test "install.sh --skills-dir installs into the explicit target only" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  local skills_tarball
  skills_tarball=$(make_fake_skills_tarball v9.9.9)
  cp "$skills_tarball" "$BB_TEST_TMP/www/"
  cp "${skills_tarball}.sha256" "$BB_TEST_TMP/www/"

  # Both default parents exist but must be ignored in favor of --skills-dir.
  mkdir -p "$HOME/.agents" "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18785
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_skills_dir.sh"
  cat >> "$BB_TEST_TMP/test_skills_dir.sh" <<SCRIPT
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18785'
main --skills-dir '$BB_TEST_TMP/custom-skills'
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-skills-dir" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_skills_dir.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/custom-skills/browser-bridge/SKILL.md" ]]
  [[ ! -e "$HOME/.agents/skills" ]]
  [[ ! -e "$HOME/.claude/skills" ]]
  # With multi-target rendering, paths move to their own indented line —
  # the old "into: <path>" substring no longer holds. Each rendered target
  # appears as "    <path>" following the "into:" headline.
  [[ "$output" == *"Installed the browser-bridge skill into:"* ]]
  [[ "$output" == *"    $BB_TEST_TMP/custom-skills"* ]]
}

@test "install.sh degrades to a warning when the skills download fails" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Deliberately no skills tarball in the mock release (e.g. a pinned
  # pre-ADR-0015 version): the overall install must still succeed.
  mkdir -p "$HOME/.agents" "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18786
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_skills_download_fail.sh"
  cat >> "$BB_TEST_TMP/test_skills_download_fail.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18786'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-skills-fail" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_skills_download_fail.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"Warning: skills installation into"* ]]
  [[ "$output" != *"Installed the browser-bridge skill"* ]]
  [[ -f "$BB_TEST_TMP/bb-home-skills-fail/version" ]]
  [[ -x "$BB_TEST_TMP/bb-home-skills-fail/bin/bridge" ]]
}

@test "install.sh dies BB-E211 when skills tarball is missing its top-level wrapper directory" {
  # Build a tarball that ships SKILL.md flat, with no wrapper directory
  # — the case install_skills would silently install under the mktemp
  # basename if download_skills didn't catch it.
  local stage bad_tarball sha
  stage=$(mktemp -d)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage2"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage2/bb.zip"
  ( cd "$BB_TEST_TMP/stage2" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  mkdir -p "$stage"
  cat > "$stage/SKILL.md" <<'EOF'
---
name: flat-no-wrapper
description: test
---
EOF
  bad_tarball="$BB_TEST_TMP/www/browser-bridge-skills-v9.9.9.tar.gz"
  # Ship SKILL.md at the tarball root with no wrapper directory — the
  # exact case download_skills is now responsible for rejecting.
  ( cd "$stage" && tar czf "$bad_tarball" SKILL.md )
  shasum -a 256 "$bad_tarball" | awk '{print $1"  browser-bridge-skills-v9.9.9.tar.gz"}' > "$bad_tarball.sha256"

  mkdir -p "$HOME/.claude"

  make_fake_uname Linux
  start_mock_http 18787
  bash_path=$(find_modern_bash)

  # Point TMPDIR at a controlled location so we can assert no mktemp
  # leakage after download_skills's BB-E211 die(). Default $TMPDIR leaks
  # mktemp dirs invisibly across CI runs.
  local controlled_tmpdir="$BB_TEST_TMP/flat-tmpdir"
  mkdir -p "$controlled_tmpdir"

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_flat_skills.sh"
  cat >> "$BB_TEST_TMP/test_flat_skills.sh" <<SCRIPT
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18787'
TMPDIR='$controlled_tmpdir'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-flat-skills" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_flat_skills.sh"
  stop_mock_http

  # download_skills is best-effort and never fails the install (ADR-0015) —
  # the BB-E211 die inside download_skills is converted to a warning by
  # main(), so the install still succeeds. What we assert is that the
  # warning fires (so the user sees the malformed-tarball signal) and the
  # skills were NOT installed under a mktemp dir or under the flat file's
  # name.
  [ "$status" -eq 0 ]
  [[ "$output" == *"Warning:"* ]]
  [[ ! -e "$HOME/.claude/skills/flat-no-wrapper/SKILL.md" ]]
  [[ ! -e "$HOME/.claude/skills/extract/SKILL.md" ]]

  # download_skills sets mktemp -d under $TMPDIR and traps EXIT (not
  # RETURN — die() bypasses RETURN) to rm -rf the dir. With the wrong
  # trap shape, every BB-E211 invocation leaks a tmp.XXX directory; with
  # the EXIT trap, the dir is gone before main() resumes.
  local leaked
  leaked=$(find "$controlled_tmpdir" -maxdepth 1 -mindepth 1 -name 'tmp.*' -type d 2>/dev/null | wc -l | tr -d ' ')
  [ "$leaked" -eq 0 ]

  rm -rf "$stage" "$controlled_tmpdir"
}

# ---------------------------------------------------------------------------
# Extension symlink, version skip, and auto-start behavior
# ---------------------------------------------------------------------------

@test "download_extension exposes extension as a symlink in BB_EXTENSION_DIR" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "real-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )
  start_mock_http 18766

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_symlink.sh"
  cat >> "$BB_TEST_TMP/test_symlink.sh" <<'SCRIPT'
ORG='127.0.0.1:18766'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
mkdir -p "$BB_HOME/extension"
download_extension
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_EXTENSION_DIR="$BB_TEST_TMP/browser-bridge-visible" \
  run "$bash_path" "$BB_TEST_TMP/test_symlink.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -L "$BB_TEST_TMP/browser-bridge-visible/extension" ]]
  [[ "$(readlink "$BB_TEST_TMP/browser-bridge-visible/extension")" == "$BB_TEST_TMP/bb-home/extension" ]]
}

@test "download_extension leaves existing correct symlink untouched" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "real-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )
  start_mock_http 18767

  mkdir -p "$BB_TEST_TMP/bb-home/extension"
  mkdir -p "$BB_TEST_TMP/browser-bridge-visible"
  ln -s "$BB_TEST_TMP/bb-home/extension" "$BB_TEST_TMP/browser-bridge-visible/extension"
  local original_inode
  original_inode=$(stat -c '%i' "$BB_TEST_TMP/browser-bridge-visible/extension" 2>/dev/null || stat -f '%i' "$BB_TEST_TMP/browser-bridge-visible/extension")

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_symlink2.sh"
  cat >> "$BB_TEST_TMP/test_symlink2.sh" <<'SCRIPT'
ORG='127.0.0.1:18767'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_extension
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_EXTENSION_DIR="$BB_TEST_TMP/browser-bridge-visible" \
  run "$bash_path" "$BB_TEST_TMP/test_symlink2.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  local new_inode
  new_inode=$(stat -c '%i' "$BB_TEST_TMP/browser-bridge-visible/extension" 2>/dev/null || stat -f '%i' "$BB_TEST_TMP/browser-bridge-visible/extension")
  [ "$original_inode" -eq "$new_inode" ]
}

@test "install.sh skips install when installed version matches target version" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/bb-home"
  echo "v9.9.9" > "$BB_TEST_TMP/bb-home/version"

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_skip.sh"
  cat >> "$BB_TEST_TMP/test_skip.sh" <<'SCRIPT'
resolve_version() { echo 'v9.9.9'; }
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_skip.sh"

  [ "$status" -eq 0 ]
  [[ "$output" == *"already installed and up to date"* ]]
}

@test "install.sh --force bypasses version skip" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  mkdir -p "$BB_TEST_TMP/bb-home"
  echo "v9.9.9" > "$BB_TEST_TMP/bb-home/version"

  make_fake_uname Linux
  start_mock_http 18768
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_force.sh"
  cat >> "$BB_TEST_TMP/test_force.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18768'
main --no-skills --force
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_force.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" != *"already installed and up to date"* ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/bridge" ]]
}

@test "install.sh auto-starts bridge services after install" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  make_fake_uname Linux
  start_mock_http 18769
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart.sh"
  cat >> "$BB_TEST_TMP/test_autostart.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18769'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  # launchd (fake launchctl) starts the supervisor asynchronously; the
  # supervisor then spawns the daemon (`bridge serve`) and writes its
  # pidfile (run/bridge-core.pid — the service keeps the old name).
  wait_for_file "$BB_TEST_TMP/bb-home/run/bridge-core.pid"
  [[ -f "$BB_TEST_TMP/bb-home/run/supervisor.pid" ]]
}

@test "install.sh stops existing bridge before update and starts again after" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Pre-populate an old install with a different version and running services.
  # The "old" bin/bridge is the current Go binary — install.sh only needs
  # `service status`/`service down` from it.
  mkdir -p "$BB_TEST_TMP/bb-home/bin" "$BB_TEST_TMP/bb-home/run"
  cp "$BB_TEST_BRIDGE_BIN" "$BB_TEST_TMP/bb-home/bin/bridge"
  chmod +x "$BB_TEST_TMP/bb-home/bin/bridge"
  echo "v0.0.1" > "$BB_TEST_TMP/bb-home/version"
  ( trap "" TERM; sleep 60 ) & echo $! > "$BB_TEST_TMP/bb-home/run/bridge-core.pid"
  local old_pid
  old_pid=$(cat "$BB_TEST_TMP/bb-home/run/bridge-core.pid")

  make_fake_uname Linux
  start_mock_http 18770
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_upgrade.sh"
  cat >> "$BB_TEST_TMP/test_upgrade.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18770'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_upgrade.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  # Old fake daemon should have been stopped.
  ! kill -0 "$old_pid" 2>/dev/null
  # New daemon (`bridge serve`) should be running (started asynchronously
  # via launchd).
  wait_for_file "$BB_TEST_TMP/bb-home/run/bridge-core.pid"
  local new_pid
  new_pid=$(cat "$BB_TEST_TMP/bb-home/run/bridge-core.pid")
  [ "$new_pid" != "$old_pid" ]
}

@test "install.sh succeeds when auto-start fails due to port conflict" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Occupy the TEST control plane port (never the production 3001) so bridge up fails.
  python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',${BRIDGE_WS_PORT})); s.listen(); import time; time.sleep(60)" &
  PORT_HOLDER_PID=$!
  sleep 0.3

  make_fake_uname Linux
  start_mock_http 18771
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_autostart_fail.sh"
  cat >> "$BB_TEST_TMP/test_autostart_fail.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18771'
main --no-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_fail.sh"
  kill "$PORT_HOLDER_PID" 2>/dev/null || true
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"could not auto-start"* ]]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
}

