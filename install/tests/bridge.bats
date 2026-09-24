#!/usr/bin/env bats
load helpers

@test "bridge with no args prints short usage and exits 0" {
  run bash "$BRIDGE_TMPL"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage: bridge"* ]]
  [[ "$output" == *"bridge service <command>"* ]]
  [[ "$output" == *"navigate"* ]]
}

@test "bridge with unknown subcommand exits non-zero with BB-E" {
  run bash "$BRIDGE_TMPL" frobnicate
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E"* ]]
}

@test "bridge --version prints template placeholder marker" {
  run bash "$BRIDGE_TMPL" --version
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge {{BRIDGE_VERSION}}"* ]]
}

@test "bridge works when BB_HOME is not set in environment" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  run env -u BB_HOME bash "$BRIDGE_TMPL" service doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] bridge-core binary present"* ]]
}

setup_up() {
  make_fake_binaries
  make_fake_uname Linux
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"
}

@test "bridge service up writes bridge-core pidfile and log (Linux path)" {
  setup_up
  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/run/bridge-core.pid" ]]
  [[ -f "$BB_HOME/logs/bridge-core.log" ]]
}

@test "bridge service up fails with BB-E002 when binaries missing" {
  make_fake_uname Linux
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" service up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}

@test "bridge service up fails with BB-E010 when control plane port is taken" {
  setup_up
  # Occupy the control plane port (3001).
  python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); import time; time.sleep(30)" &
  SOCAT_PID=$!
  sleep 0.3
  run bash "$BRIDGE_TMPL" service up
  kill "$SOCAT_PID" 2>/dev/null || true
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E010"* ]]
}

@test "moved lifecycle commands point at 'bridge service' (BB-E305)" {
  make_fake_uname Linux
  run bash "$BRIDGE_TMPL" up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E305"* ]]
  [[ "$output" == *"bridge service up"* ]]
}

@test "bridge service down stops running service via SIGTERM and removes PID file" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  # Spawn a sleeper that traps SIGTERM.
  sleeper() { trap "exit 0" TERM; sleep 60; }
  sleeper &
  SLEEP_PID=$!
  echo "$SLEEP_PID" > "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/bridge-core.pid" ]]
  ! kill -0 "$SLEEP_PID" 2>/dev/null
}

@test "bridge service down with no PID file is a no-op (exit 0)" {
  make_fake_uname Linux
  rm -f "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ "$output" == *"already stopped"* ]]
}

@test "bridge service down SIGKILLs after 3s if service ignores SIGTERM" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  # Spawn a sleeper that ignores SIGTERM.
  ( trap "" TERM; sleep 60 ) &
  ZOMBIE_PID=$!
  echo "$ZOMBIE_PID" > "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/bridge-core.pid" ]]
  ! kill -0 "$ZOMBIE_PID" 2>/dev/null
}

@test "bridge service status exits 0 when bridge-core is running" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  ( trap "" TERM; sleep 30 ) & echo $! > "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service status
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge-core:  running"* ]]
}

@test "bridge service status exits 1 when bridge-core is down" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  echo "99999" > "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service status
  [ "$status" -eq 1 ]
  [[ "$output" == *"bridge-core:  stopped"* ]]
}

@test "bridge service restart runs down then up" {
  setup_up
  echo "99999" > "$BB_HOME/run/bridge-core.pid"
  run bash "$BRIDGE_TMPL" service restart
  [ "$status" -eq 0 ]
  # The fake PID 99999 is gone; new PIDs are written.
  [[ "$(cat "$BB_HOME/run/bridge-core.pid")" != "99999" ]]
}

@test "bridge service logs tails bridge-core.log (smoke test that file exists)" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/logs"
  echo "fake log line" > "$BB_HOME/logs/bridge-core.log"
  # We can't easily test tail -f in bats; instead confirm the file is referenced.
  run bash -c "BB_HOME='$BB_HOME' bash '$BRIDGE_TMPL' service logs 2>&1 & sleep 0.2; pkill -P \$\$ ; wait"
  [ -f "$BB_HOME/logs/bridge-core.log" ]
}

@test "bridge service prints service help when called without a subcommand" {
  run bash "$BRIDGE_TMPL" service
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage: bridge service"* ]]
  [[ "$output" == *"enable"* ]]
  [[ "$output" == *"disable"* ]]
}

@test "bridge --help shows service section then browser commands" {
  make_fake_binaries
  run bash "$BRIDGE_TMPL" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Service commands:"* ]]
  [[ "$output" == *"Browser commands:"* ]]
  [[ "$output" == *"fake-bridge-cmd: --help"* ]]
}

@test "bridge service doctor reports OK when install is healthy" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  run bash "$BRIDGE_TMPL" service doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] bridge-core binary present"* ]]
  [[ "$output" == *"[OK] bridge-core binary present"* ]]
  [[ "$output" == *"[OK] bridge-cmd binary present"* ]]
  [[ "$output" == *"[OK] extension/manifest.json valid"* ]]
}

@test "bridge service doctor reports FAIL when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" service doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] bridge-core binary missing"* ]]
}

@test "bridge service version prints installed and latest release" {
  mkdir -p "$BB_HOME"
  echo "v1.2.3" > "$BB_HOME/version"
  run bash "$BRIDGE_TMPL" service version
  [ "$status" -eq 0 ]
  [[ "$output" == *"installed: v1.2.3"* ]]
}

@test "bridge service uninstall without --yes prompts and aborts on 'n'" {
  mkdir -p "$BB_HOME"
  echo "n" | run bash "$BRIDGE_TMPL" service uninstall
  [[ -d "$BB_HOME" ]]
}

@test "bridge service uninstall --yes removes BB_HOME" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" service uninstall --yes
  [ "$status" -eq 0 ]
  [[ ! -d "$BB_HOME" ]]
}

@test "bridge service doctor reports OK when extension is only in BB_EXTENSION_DIR" {
  make_fake_binaries
  mkdir -p "$BB_EXTENSION_DIR/extension"
  echo '{"manifest_version":3}' > "$BB_EXTENSION_DIR/extension/manifest.json"
  run bash "$BRIDGE_TMPL" service doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] extension/manifest.json valid"* ]]
}

@test "bridge service uninstall --yes removes BB_HOME and BB_EXTENSION_DIR" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/bin"
  mkdir -p "$BB_EXTENSION_DIR/extension"
  echo '{"manifest_version":3}' > "$BB_EXTENSION_DIR/extension/manifest.json"
  run bash "$BRIDGE_TMPL" service uninstall --yes
  [ "$status" -eq 0 ]
  [[ ! -d "$BB_HOME" ]]
  [[ ! -d "$BB_EXTENSION_DIR" ]]
}

@test "bridge service update fetches the self-contained release installer" {
  local tmpl="$BB_TEST_TMP/bridge-substituted.sh"
  sed -e "s|{{ORG}}|dkisser|g" -e "s|{{REPO}}|browser-bridge|g" "$BRIDGE_TMPL" > "$tmpl"

  cat > "$BB_TEST_TMP/update_test.sh" <<EOF
BB_HOME=/nonexistent
source '$tmpl'
curl() { printf '%s\n' "curl-url: \${*: -1}" >> '$BB_TEST_TMP/update.log'; printf '#!/usr/bin/env bash\necho installed\n'; }
bash() { printf '%s\n' "bash-args: \$*" >> '$BB_TEST_TMP/update.log'; }
cmd_service_update 'v9.9.9'
EOF

  run bash "$BB_TEST_TMP/update_test.sh"
  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/update.log" ]]
  grep -q 'curl-url: https://github.com/dkisser/browser-bridge/releases/download/v9.9.9/install.sh' "$BB_TEST_TMP/update.log"
}

@test "bridge service update latest fetches the latest release installer" {
  local tmpl="$BB_TEST_TMP/bridge-substituted-latest.sh"
  sed -e "s|{{ORG}}|dkisser|g" -e "s|{{REPO}}|browser-bridge|g" "$BRIDGE_TMPL" > "$tmpl"

  cat > "$BB_TEST_TMP/update_latest_test.sh" <<EOF
BB_HOME=/nonexistent
source '$tmpl'
curl() { printf '%s\n' "curl-url: \${*: -1}" >> '$BB_TEST_TMP/update-latest.log'; printf '#!/usr/bin/env bash\necho installed\n'; }
bash() { printf '%s\n' "bash-args: \$*" >> '$BB_TEST_TMP/update-latest.log'; }
cmd_service_update
EOF

  run bash "$BB_TEST_TMP/update_latest_test.sh"
  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/update-latest.log" ]]
  grep -q 'curl-url: https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh' "$BB_TEST_TMP/update-latest.log"
}

@test "bridge autostart on (deprecated) writes plist, loads agent, and warns" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" autostart on
  [ "$status" -eq 0 ]
  [[ "$output" == *"deprecated"* ]]
  [[ -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  grep -q 'com.browser-bridge.bridge' "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  grep -q "${BB_HOME}" "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  # Bridge-core merge: control plane URL is now BRIDGE_WS_PORT env var,
  # not a literal ws:// URL in the plist.
  grep -q '<key>BRIDGE_WS_PORT</key>' "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  grep -q '<true/>' "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  grep -q 'bootstrap' "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "bridge autostart on reports error when launchctl fails" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_id 501
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"

  run bash "$BRIDGE_TMPL" autostart on
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E302"* ]]
}

@test "bridge autostart off (deprecated) removes plist and unloads agent" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$HOME/Library/LaunchAgents"
  echo '<plist></plist>' > "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" autostart off
  [ "$status" -eq 0 ]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
}

@test "bridge autostart status reports enabled when plist is present" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$HOME/Library/LaunchAgents"
  cp "$BB_HOME/launchagent.plist.tmpl" "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  make_fake_uname Darwin

  run bash "$BRIDGE_TMPL" autostart status
  [[ "$output" == *"enabled"* ]]
}

@test "bridge autostart status reports disabled when plist is absent" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin

  run bash "$BRIDGE_TMPL" autostart status
  [ "$status" -ne 0 ]
  [[ "$output" == *"disabled"* ]]
}

@test "bridge autostart fails on Linux" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Linux

  run bash "$BRIDGE_TMPL" autostart on
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E300"* ]]
}

@test "bridge service uninstall --yes removes LaunchAgent plist on macOS" {
  mkdir -p "$BB_HOME/bin"
  mkdir -p "$BB_EXTENSION_DIR/extension"
  echo '{"manifest_version":3}' > "$BB_EXTENSION_DIR/extension/manifest.json"
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$HOME/Library/LaunchAgents"
  echo '<plist></plist>' > "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service uninstall --yes
  [ "$status" -eq 0 ]
  [[ ! -d "$BB_HOME" ]]
  [[ ! -d "$BB_EXTENSION_DIR" ]]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
}

@test "bridge service enable writes plist without starting services" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin

  run bash "$BRIDGE_TMPL" service enable
  [ "$status" -eq 0 ]
  [[ -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  [[ ! -f "$BB_HOME/run/bridge-core.pid" ]]
}

@test "bridge service disable removes LaunchAgents and staging plists" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$HOME/Library/LaunchAgents"
  cp "$BB_HOME/launchagent.plist.tmpl" "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/launchagents/com.browser-bridge.bridge.plist" ]]

  run bash "$BRIDGE_TMPL" service disable
  [ "$status" -eq 0 ]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  [[ ! -f "$BB_HOME/launchagents/com.browser-bridge.bridge.plist" ]]
}

@test "bridge service up bootstraps from staging plist when auto-start is disabled" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/launchagents/com.browser-bridge.bridge.plist" ]]
  [[ ! -f "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" ]]
  grep -q "bootstrap gui/501 $BB_HOME/launchagents/com.browser-bridge.bridge.plist" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "bridge service up bootstraps from LaunchAgents plist when auto-start is enabled" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$HOME/Library/LaunchAgents"
  cp "$BB_HOME/launchagent.plist.tmpl" "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  grep -q "bootstrap gui/501 $HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" "$BB_TEST_TMP/launchctl_calls.txt"
}

# launchctl fake that reports the label as loaded (as a pre-supervision or
# finished job would be after an upgrade).
make_fake_launchctl_loaded() {
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$BB_TEST_TMP/launchctl_calls.txt"
if [[ "\$1" == "list" ]]; then
  echo "- 0 com.browser-bridge.bridge"
fi
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

@test "bridge service up reports already running when the label is loaded and a supervisor is alive" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_launchctl_loaded
  make_fake_id 501

  bash "$BRIDGE_TMPL" service up --foreground >"$BB_TEST_TMP/sup.log" 2>&1 &
  SUP_PID=$!
  local waited=0
  while [[ $waited -lt 50 ]]; do
    [[ -f "$BB_HOME/run/supervisor.pid" ]] && break
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ -f "$BB_HOME/run/supervisor.pid" ]]

  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ "$output" == *"already running"* ]]
  [[ ! -f "$BB_TEST_TMP/launchctl_calls.txt" ]] || ! grep -q bootout "$BB_TEST_TMP/launchctl_calls.txt"

  kill -TERM "$SUP_PID"
  wait "$SUP_PID" 2>/dev/null || true
}

@test "bridge service up replaces a stale loaded LaunchAgent job and bootstraps" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_launchctl_loaded
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ "$output" == *"replaced stale LaunchAgent job"* ]]
  grep -q "bootout gui/501/com.browser-bridge.bridge" "$BB_TEST_TMP/launchctl_calls.txt"
  grep -q "bootstrap gui/501" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "bridge service up adopts a running pidfile-owned bridge-core instead of failing BB-E010" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  mkdir -p "$BB_HOME/run"
  "$BB_HOME/bin/bridge-core" &
  WS_PID=$!
  sleep 0.5
  echo "$WS_PID" > "$BB_HOME/run/bridge-core.pid"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service up
  kill "$WS_PID" 2>/dev/null || true
  [ "$status" -eq 0 ]
  [[ "$output" != *"BB-E010"* ]]
  grep -q "bootstrap gui/501" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "bridge service down bootouts the supervisor by label when loaded" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_id 501
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$BB_TEST_TMP/launchctl_calls.txt"
if [[ "\$1" == "list" ]]; then
  echo "PID Status Label"
  echo "4242 0 com.browser-bridge.bridge"
fi
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"

  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  grep -q "bootout gui/501/com.browser-bridge.bridge" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "bridge service down falls back to the user domain for jobs parked there" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_id 501
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$BB_TEST_TMP/launchctl_calls.txt"
if [[ "\$1" == "list" ]]; then
  echo "PID Status Label"
  echo "4242 0 com.browser-bridge.bridge"
elif [[ "\$1" == "bootout" && "\$2" == gui/* ]]; then
  echo "Boot-out failed: 3: No such process" >&2
  exit 3
fi
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"

  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  grep -q "bootout gui/501/com.browser-bridge.bridge" "$BB_TEST_TMP/launchctl_calls.txt"
  grep -q "bootout user/501/com.browser-bridge.bridge" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "supervisor restarts a crashed bridge-core and shuts down cleanly" {
  make_fake_binaries
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"

  bash "$BRIDGE_TMPL" service up --foreground >"$BB_TEST_TMP/supervisor.log" 2>&1 &
  SUP_PID=$!
  # Gate on the supervisor's own startup-complete line, not on the pidfile or
  # the bound port. supervisor_spawn writes the pidfile and bridge-core binds
  # the port *before* the handshake finishes; supervisor_spawn then confirms
  # the bind with port_in_use (a fresh python3 probe per poll) and only prints
  # this line once it has returned. Killing bridge-core inside that window
  # makes the handshake see a dead child, so the supervisor dies BB-E011
  # instead of entering its watch loop and restarting — the restart under test
  # then never happens.
  local waited=0
  while [[ $waited -lt 50 ]]; do
    grep -q "supervisor: bridge-core=" "$BB_TEST_TMP/supervisor.log" && break
    sleep 0.1
    waited=$((waited + 1))
  done
  grep -q "supervisor: bridge-core=" "$BB_TEST_TMP/supervisor.log"
  [[ -f "$BB_HOME/run/bridge-core.pid" ]]

  local old_pid new_pid=""
  old_pid=$(cat "$BB_HOME/run/bridge-core.pid")
  kill "$old_pid"

  # supervisor_watch does `rm -f pidfile` then supervisor_spawn does
  # `echo "$pid" > pidfile`. The window between the two is real, so a
  # bare `cat` here can lose the race and abort the test with ENOENT.
  # Read defensively and let the loop retry until the pidfile holds a
  # pid that differs from the one we killed.
  waited=0
  while [[ $waited -lt 50 ]]; do
    new_pid=$(cat "$BB_HOME/run/bridge-core.pid" 2>/dev/null || true)
    if [[ -n "$new_pid" && "$new_pid" != "$old_pid" ]]; then
      break
    fi
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ -n "$new_pid" ]]
  [ "$new_pid" != "$old_pid" ]
  grep -q "restarting bridge-core" "$BB_TEST_TMP/supervisor.log"

  kill -TERM "$SUP_PID"
  wait "$SUP_PID" 2>/dev/null || true
  ! kill -0 "$new_pid" 2>/dev/null
  [[ ! -f "$BB_HOME/run/bridge-core.pid" ]]
}

@test "supervisor adopts an orphaned bridge-core and keeps watching it" {
  make_fake_binaries
  mkdir -p "$BB_HOME/run"
  # Orphan: bridge-core running with pidfile, but the supervisor that
  # started it is gone (SIGKILLed). A stale supervisor.pid with a dead pid
  # simulates the relaunch-after-SIGKILL state.
  "$BB_HOME/bin/bridge-core" & echo $! > "$BB_HOME/run/bridge-core.pid"
  local orphan_pid
  orphan_pid=$(cat "$BB_HOME/run/bridge-core.pid")
  sleep 0.05 & local dead_pid=$!
  wait "$dead_pid" 2>/dev/null || true
  echo "$dead_pid" > "$BB_HOME/run/supervisor.pid"

  bash "$BRIDGE_TMPL" service up --foreground >"$BB_TEST_TMP/supervisor.log" 2>&1 &
  SUP_PID=$!
  local waited=0
  while [[ $waited -lt 50 ]]; do
    grep -q "adopting bridge-core" "$BB_TEST_TMP/supervisor.log" && break
    sleep 0.1
    waited=$((waited + 1))
  done
  # Adoption, not takeover: same child keeps running, supervisor watches.
  grep -q "adopting bridge-core" "$BB_TEST_TMP/supervisor.log"
  [[ "$(cat "$BB_HOME/run/bridge-core.pid")" == "$orphan_pid" ]]

  # The adoption is real supervision: kill the adopted child, it gets restarted.
  kill "$orphan_pid"
  waited=0
  while [[ $waited -lt 50 ]]; do
    if [[ -f "$BB_HOME/run/bridge-core.pid" && "$(cat "$BB_HOME/run/bridge-core.pid")" != "$orphan_pid" ]]; then
      break
    fi
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ "$(cat "$BB_HOME/run/bridge-core.pid")" != "$orphan_pid" ]]
  grep -q "restarting bridge-core" "$BB_TEST_TMP/supervisor.log"

  kill -TERM "$SUP_PID"
  wait "$SUP_PID" 2>/dev/null || true
  ! kill -0 "$orphan_pid" 2>/dev/null
  [[ ! -f "$BB_HOME/run/supervisor.pid" ]]
}

@test "supervisor refuses to start while another live supervisor holds the pidfile (BB-E307)" {
  make_fake_binaries
  mkdir -p "$BB_HOME/run"
  bash "$BRIDGE_TMPL" service up --foreground >"$BB_TEST_TMP/sup1.log" 2>&1 &
  SUP_PID=$!
  local waited=0
  while [[ $waited -lt 50 ]]; do
    [[ -f "$BB_HOME/run/supervisor.pid" ]] && break
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ -f "$BB_HOME/run/supervisor.pid" ]]

  run bash "$BRIDGE_TMPL" service up --foreground
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E307"* ]]

  kill -TERM "$SUP_PID"
  wait "$SUP_PID" 2>/dev/null || true
}

@test "supervisor refuses misleading pidfiles pointing at foreign processes" {
  make_fake_binaries
  mkdir -p "$BB_HOME/run"
  python3 -c "import socket, time; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); time.sleep(30)" &
  P1=$!
  sleep 0.3
  echo "$P1" > "$BB_HOME/run/bridge-core.pid"

  run bash "$BRIDGE_TMPL" service up --foreground
  kill "$P1" 2>/dev/null || true
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E010"* ]]
}

@test "supervisor refuses to take over foreign listeners without bridge pidfiles" {
  make_fake_binaries
  python3 -c "import socket, time; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); time.sleep(30)" &
  P1=$!
  python3 -c "import socket, time; s=socket.socket(); s.bind(('127.0.0.1',3002)); s.listen(); time.sleep(30)" &
  P2=$!
  sleep 0.3

  run bash "$BRIDGE_TMPL" service up --foreground
  kill "$P1" "$P2" 2>/dev/null || true
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E010"* ]]
}

@test "supervisor_tick pauses at least one second (no busy-loop regression)" {
  local tmpl="$BB_TEST_TMP/bridge-substituted.sh"
  sed -e "s|{{ORG}}|dkisser|g" -e "s|{{REPO}}|browser-bridge|g" "$BRIDGE_TMPL" > "$tmpl"

  cat > "$BB_TEST_TMP/tick_test.sh" <<EOF
source '$tmpl'
start=\$(date +%s)
supervisor_tick
end=\$(date +%s)
echo "elapsed=\$((end - start))"
EOF

  run bash "$BB_TEST_TMP/tick_test.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"elapsed=1"* || "$output" == *"elapsed=2"* ]]
}

@test "bridge service down on idle macOS prints no per-service noise" {
  make_fake_binaries
  cp "$BB_TEST_ROOT/install/launchagent.plist.tmpl" "$BB_HOME/launchagent.plist.tmpl"
  make_fake_uname Darwin
  make_fake_launchctl
  make_fake_id 501

  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ "$output" == *"supervisor not loaded"* ]]
  [[ "$output" != *"already stopped"* ]]
}
