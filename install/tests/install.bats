#!/usr/bin/env bats
load helpers

@test "install.sh exits BB-E001 when bun missing" {
  cat > "$BB_TEST_TMP/install.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$(sed -n '/^set -euo pipefail$/,/^# END PREREQ/p' "$INSTALL_SH" | sed '/^# END PREREQ$/d')
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  # /opt/homebrew/bin is prepended so bash 5 (Homebrew) is found first.
  # On Linux /bin/bash is already bash 5 so this is a no-op there.
  export PATH="/opt/homebrew/bin:/usr/bin:/bin"
  run bash "$BB_TEST_TMP/install.sh"
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
  run bash "$BB_TEST_TMP/install.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}
