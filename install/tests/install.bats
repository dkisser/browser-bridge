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

@test "print_next_steps mentions PATH, Chrome load, and the visible extension directory" {
  bash_path=$(find_modern_bash)
  run "$bash_path" -c "
    set -euo pipefail
    source <(sed -n '/^print_next_steps()/,/^}/p' '$INSTALL_SH')
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
main --no-skills
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
main --no-skills
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
  [[ -f "$BB_TEST_TMP/bb-home-autostart/launchagent.plist.tmpl" ]]
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
  [[ ! -f "$BB_TEST_TMP/launchctl_calls.txt" ]]
}

@test "install.sh falls back gracefully when auto-start cannot be enabled" {
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
  [[ "$output" == *"Could not enable login auto-start"* ]]
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

@test "build-installer.sh embeds bridge.sh.tmpl and launchagent.plist.tmpl into install.sh" {
  bash "$BB_TEST_ROOT/.github/scripts/build-installer.sh" "$BB_TEST_TMP/self-contained-install.sh"
  [ -x "$BB_TEST_TMP/self-contained-install.sh" ]
  grep -q '^__BB_TEMPLATE_BEGIN__$' "$BB_TEST_TMP/self-contained-install.sh"
  grep -q '^__BB_TEMPLATE_END__$' "$BB_TEST_TMP/self-contained-install.sh"
  grep -q '^__BB_LAUNCHAGENT_BEGIN__$' "$BB_TEST_TMP/self-contained-install.sh"
  grep -q '^__BB_LAUNCHAGENT_END__$' "$BB_TEST_TMP/self-contained-install.sh"
  awk '/^__BB_TEMPLATE_BEGIN__$/{f=1;next}/^__BB_TEMPLATE_END__$/{f=0}f' "$BB_TEST_TMP/self-contained-install.sh" > "$BB_TEST_TMP/extracted.tmpl"
  diff -u "$BB_TEST_ROOT/install/bridge.sh.tmpl" "$BB_TEST_TMP/extracted.tmpl"
  awk '/^__BB_LAUNCHAGENT_BEGIN__$/{f=1;next}/^__BB_LAUNCHAGENT_END__$/{f=0}f' "$BB_TEST_TMP/self-contained-install.sh" > "$BB_TEST_TMP/extracted-launchagent.tmpl"
  diff -u "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_TEST_TMP/extracted-launchagent.tmpl"
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
  [[ -x "$BB_TEST_TMP/bb-home-sc/bin/ws-server" ]]
  [[ -x "$BB_TEST_TMP/bb-home-sc/bin/local-proxy" ]]
  [[ -x "$BB_TEST_TMP/bb-home-sc/bin/bridge-cmd" ]]
}

@test "fetch_bridge_template falls back to versioned tag when no embedded template" {
  bash_path=$(find_modern_bash)
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_fbt.sh"
  cat >> "$BB_TEST_TMP/test_fbt.sh" <<SCRIPT
resolve_version() { echo "v9.9.9"; }
unset BRIDGE_TEMPLATE_PATH LAUNCHAGENT_TEMPLATE_PATH
curl() {
  printf '%s\n' "\$@" > "$BB_TEST_TMP/curl_args.txt"
  touch "\${4:-$BB_TEST_TMP/bridge.sh.tmpl}"
}
fetch_bridge_template "v9.9.9"
cat "$BB_TEST_TMP/curl_args.txt"
SCRIPT
  run "$bash_path" "$BB_TEST_TMP/test_fbt.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"v9.9.9/install/bridge.sh.tmpl"* ]]
}

# ---------------------------------------------------------------------------
# Task 13: skills installation
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

@test "install.sh --with-skills downloads and installs release skills" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Prepare a fake skills tarball.
  local skills_stage="$BB_TEST_TMP/browser-bridge-user"
  mkdir -p "$skills_stage"
  cat > "$skills_stage/SKILL.md" <<'EOF'
---
name: browser-bridge-user
description: test
---
EOF
  ( cd "$BB_TEST_TMP" && tar czf "browser-bridge-skills-v9.9.9.tar.gz" "browser-bridge-user" )
  cp "$BB_TEST_TMP/browser-bridge-skills-v9.9.9.tar.gz" "$BB_TEST_TMP/www/"
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-skills-v9.9.9.tar.gz > browser-bridge-skills-v9.9.9.tar.gz.sha256 )

  mkdir -p "$HOME/.claude/skills"

  start_mock_http 18765
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e_skills.sh"
  cat >> "$BB_TEST_TMP/test_e2e_skills.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18765'
main --with-skills
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e_skills.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -f "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ -L "$HOME/.local/bin/bridge" ]]
  [[ -f "$HOME/.claude/skills/browser-bridge-user/SKILL.md" ]]
}

@test "install.sh does not install skills by default even when local ./skills exists" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  # Create a local ./skills directory as if the repo were present.
  mkdir -p "$BB_TEST_TMP/cwd/skills/browser-bridge-user"
  cat > "$BB_TEST_TMP/cwd/skills/browser-bridge-user/SKILL.md" <<'EOF'
---
name: browser-bridge-user
description: test
---
EOF

  start_mock_http 18772
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_no_skills.sh"
  cat >> "$BB_TEST_TMP/test_no_skills.sh" <<SCRIPT
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18772'
cd '$BB_TEST_TMP/cwd'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home-no-skills" \
  BB_VERSION="v9.9.9" \
  run "$bash_path" "$BB_TEST_TMP/test_no_skills.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home-no-skills/version" ]]
  [[ ! -d "$HOME/.claude/skills/browser-bridge-user" ]]
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
  [[ ! -d "$HOME/.claude/skills/browser-bridge-user" ]]
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

  start_mock_http 18768
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_force.sh"
  cat >> "$BB_TEST_TMP/test_force.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18768'
main --no-skills --force
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_force.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" != *"already installed and up to date"* ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/ws-server" ]]
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
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/run/ws-server.pid" ]]
  [[ -f "$BB_TEST_TMP/bb-home/run/local-proxy.pid" ]]
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
  mkdir -p "$BB_TEST_TMP/bb-home/bin" "$BB_TEST_TMP/bb-home/run"
  cp -R "$BB_TEST_ROOT/install/bridge.sh.tmpl" "$BB_TEST_TMP/bb-home/bin/bridge"
  sed -i.bak 's/{{BRIDGE_VERSION}}/v0.0.1/g' "$BB_TEST_TMP/bb-home/bin/bridge"
  rm "$BB_TEST_TMP/bb-home/bin/bridge.bak"
  chmod +x "$BB_TEST_TMP/bb-home/bin/bridge"
  echo "v0.0.1" > "$BB_TEST_TMP/bb-home/version"
  ( trap "" TERM; sleep 60 ) & echo $! > "$BB_TEST_TMP/bb-home/run/ws-server.pid"
  ( trap "" TERM; sleep 60 ) & echo $! > "$BB_TEST_TMP/bb-home/run/local-proxy.pid"
  local old_ws_pid old_lp_pid
  old_ws_pid=$(cat "$BB_TEST_TMP/bb-home/run/ws-server.pid")
  old_lp_pid=$(cat "$BB_TEST_TMP/bb-home/run/local-proxy.pid")

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
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_upgrade.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  # Old fake services should have been stopped.
  ! kill -0 "$old_ws_pid" 2>/dev/null
  ! kill -0 "$old_lp_pid" 2>/dev/null
  # New services should be running.
  [[ -f "$BB_TEST_TMP/bb-home/run/ws-server.pid" ]]
  [[ -f "$BB_TEST_TMP/bb-home/run/local-proxy.pid" ]]
  local new_ws_pid
  new_ws_pid=$(cat "$BB_TEST_TMP/bb-home/run/ws-server.pid")
  [ "$new_ws_pid" != "$old_ws_pid" ]
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

  # Occupy the ws-server port so bridge up fails.
  python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); import time; time.sleep(60)" &
  PORT_HOLDER_PID=$!
  sleep 0.3

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
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_autostart_fail.sh"
  kill "$PORT_HOLDER_PID" 2>/dev/null || true
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ "$output" == *"could not auto-start"* ]]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
}

