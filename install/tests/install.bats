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
  [[ "$output" == *"ws-server"* ]]
  [[ "$output" == *"local-proxy"* ]]
  [[ "$output" == *"bridge-cmd"* ]]
}

# ---------------------------------------------------------------------------
# Task 11: write_artifacts + print_next_steps
# ---------------------------------------------------------------------------

@test "write_artifacts emits bridge script and version file" {
  bash_path=$(find_modern_bash)
  mkdir -p "$BB_HOME"
  cat > "$BB_TEST_TMP/bridge.tmpl" <<'TPL'
#!/usr/bin/env bash
echo bridge-{{BRIDGE_VERSION}}
echo org-{{ORG}}
TPL
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed '\$d' '$INSTALL_SH')
    BB_HOME='$BB_HOME'
    ORG='testorg'
    BRIDGE_TEMPLATE_PATH='$BB_TEST_TMP/bridge.tmpl'
    info() { :; }
    write_artifacts v9.9.9
    cat '$BB_HOME/bin/bridge'
    cat '$BB_HOME/version'
    test -L '$HOME/.local/bin/bridge'
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge-v9.9.9"* ]]
  [[ "$output" == *"v9.9.9"* ]]
}

@test "print_next_steps mentions PATH, Chrome load, and bridge up" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed -n '/^print_next_steps()/,/^}/p' '$INSTALL_SH')
    BB_HOME='$BB_HOME'
    print_next_steps v9.9.9
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"PATH"* ]]
  [[ "$output" == *"Chrome"* ]]
  [[ "$output" == *"bridge up"* ]]
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

  start_mock_http 18762
  bash_path=$(find_modern_bash)

  # Source install.sh (minus trailing 'main "$@"'), override ORG, then call main.
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18762'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -f "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/ws-server" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/local-proxy" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/bridge-cmd" ]]
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

  start_mock_http 18763
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18763'
main
SCRIPT

  # First run — fresh install
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  first_status=$status

  # Second run — upgrade in place
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$first_status" -eq 0 ]
  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ "$(cat "$BB_TEST_TMP/bb-home/version")" == "v9.9.9" ]]
}
