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

@test "install.sh exits BB-E001 when bun missing" {
  cat > "$BB_TEST_TMP/install.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$(sed -n '/^set -euo pipefail$/,/END PREREQ/p' "$INSTALL_SH" | sed '/END PREREQ/d')
check_prereqs
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  bash_path=$(find_modern_bash)
  # Run with a minimal PATH so bun (and other tools) are not found.
  run env -i HOME="$HOME" PATH="/usr/bin:/bin" "$bash_path" "$BB_TEST_TMP/install.sh"
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E001"* ]]
}

@test "install.sh succeeds bun check when bun present" {
  make_fake_bun
  cat > "$BB_TEST_TMP/install.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$(sed -n '/^set -euo pipefail$/,/END PREREQ/p' "$INSTALL_SH" | sed '/END PREREQ/d')
check_prereqs
echo "OK"
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  bash_path=$(find_modern_bash)
  run env -i HOME="$HOME" PATH="$BB_TEST_TMP/bin:/usr/bin:/bin" "$bash_path" "$BB_TEST_TMP/install.sh"
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
# Task 10: clone_source
# ---------------------------------------------------------------------------

setup_clone_fixture() {
  # Make a bare repo we can clone from.
  mkdir -p "$BB_TEST_TMP/origin.git"
  git -C "$BB_TEST_TMP/origin.git" init --bare --quiet
  # Seed a commit and tag v9.9.9 in a working tree, push it.
  local seed="$BB_TEST_TMP/seed"
  mkdir -p "$seed"
  git -C "$seed" init --quiet -b main
  git -C "$seed" -c user.email=t@t -c user.name=t commit --allow-empty -m initial --quiet
  git -C "$seed" tag v9.9.9
  git -C "$seed" remote add origin "$BB_TEST_TMP/origin.git"
  git -C "$seed" push origin main v9.9.9 --quiet
}

@test "clone_source fresh: shallow-clones repo at tag" {
  setup_clone_fixture
  make_fake_bun
  bash_path=$(find_modern_bash)
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_clone.sh"
  cat >> "$BB_TEST_TMP/test_clone.sh" <<'SCRIPT'
clone_source v9.9.9
test -f "$BB_HOME/repo/.git/HEAD"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_GIT_REMOTE="$BB_TEST_TMP/origin.git" \
  run "$bash_path" "$BB_TEST_TMP/test_clone.sh"
  [ "$status" -eq 0 ]
  [[ -d "$BB_TEST_TMP/bb-home/repo" ]]
}

@test "clone_source update: fetches and resets existing repo" {
  setup_clone_fixture
  make_fake_bun
  bash_path=$(find_modern_bash)
  BB_HOME="$BB_TEST_TMP/bb-home"
  mkdir -p "$BB_HOME"
  # First clone at v9.9.9 to simulate an existing install.
  git clone --depth 1 --branch v9.9.9 "$BB_TEST_TMP/origin.git" "$BB_HOME/repo" >/dev/null 2>&1
  # Tag a new commit and push it.
  local seed="$BB_TEST_TMP/seed2"
  git clone "$BB_TEST_TMP/origin.git" "$seed" >/dev/null 2>&1
  git -C "$seed" -c user.email=t@t -c user.name=t commit --allow-empty -m "v9.9.10" --quiet
  git -C "$seed" tag v9.9.10
  git -C "$seed" push origin v9.9.10 --quiet
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_clone.sh"
  cat >> "$BB_TEST_TMP/test_clone.sh" <<'SCRIPT'
clone_source v9.9.10
git -C "$BB_HOME/repo" log --oneline -1
SCRIPT
  BB_HOME="$BB_HOME" \
  BB_GIT_REMOTE="$BB_TEST_TMP/origin.git" \
  run "$bash_path" "$BB_TEST_TMP/test_clone.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"v9.9.10"* ]]
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
    source <(sed -n '/^write_artifacts()/,/^}/p' '$INSTALL_SH')
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
