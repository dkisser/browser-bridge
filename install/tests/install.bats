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
$(sed -n '/^set -euo pipefail$/,/^# END PREREQ/p' "$INSTALL_SH" | sed '/^# END PREREQ$/d')
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
$(sed -n '/^set -euo pipefail$/,/^# END PREREQ/p' "$INSTALL_SH" | sed '/^# END PREREQ$/d')
echo "OK"
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  bash_path=$(find_modern_bash)
  run env -i HOME="$HOME" PATH="$BB_TEST_TMP/bin:/usr/bin:/bin" "$bash_path" "$BB_TEST_TMP/install.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}
