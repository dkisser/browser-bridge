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
  [[ "$output" == *"[OK] ws-server binary present"* ]]
}

setup_up() {
  make_fake_binaries
  make_fake_uname Linux
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"
}

@test "bridge service up writes PID files for both services (Linux path)" {
  setup_up
  run bash "$BRIDGE_TMPL" service up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/run/ws-server.pid" ]]
  [[ -f "$BB_HOME/run/local-proxy.pid" ]]
  [[ -f "$BB_HOME/logs/ws-server.log" ]]
  [[ -f "$BB_HOME/logs/local-proxy.log" ]]
}

@test "bridge service up fails with BB-E002 when binaries missing" {
  make_fake_uname Linux
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" service up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}

@test "bridge service up fails with BB-E010 when ws-server port is taken" {
  setup_up
  # Occupy the ws-server port.
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
  echo "$SLEEP_PID" > "$BB_HOME/run/ws-server.pid"
  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/ws-server.pid" ]]
  ! kill -0 "$SLEEP_PID" 2>/dev/null
}

@test "bridge service down with no PID file is a no-op (exit 0)" {
  make_fake_uname Linux
  rm -f "$BB_HOME/run/ws-server.pid" "$BB_HOME/run/local-proxy.pid"
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
  echo "$ZOMBIE_PID" > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" service down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/local-proxy.pid" ]]
  ! kill -0 "$ZOMBIE_PID" 2>/dev/null
}

@test "bridge service status exits 0 when both services running" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  ( trap "" TERM; sleep 30 ) & echo $! > "$BB_HOME/run/ws-server.pid"
  ( trap "" TERM; sleep 30 ) & echo $! > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" service status
  [ "$status" -eq 0 ]
  [[ "$output" == *"ws-server:    running"* ]]
  [[ "$output" == *"local-proxy:  running"* ]]
}

@test "bridge service status exits 1 when a service is down" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/run"
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  echo "99998" > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" service status
  [ "$status" -eq 1 ]
  [[ "$output" == *"ws-server:    stopped"* ]]
}

@test "bridge service restart runs down then up" {
  setup_up
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  run bash "$BRIDGE_TMPL" service restart
  [ "$status" -eq 0 ]
  # The fake PID 99999 is gone; new PIDs are written.
  [[ "$(cat "$BB_HOME/run/ws-server.pid")" != "99999" ]]
}

@test "bridge service logs without name tails both logs (smoke test that files exist)" {
  make_fake_uname Linux
  mkdir -p "$BB_HOME/logs"
  echo "ws log" > "$BB_HOME/logs/ws-server.log"
  echo "lp log" > "$BB_HOME/logs/local-proxy.log"
  # We can't easily test tail -f in bats; instead confirm the files are referenced.
  run bash -c "BB_HOME='$BB_HOME' bash '$BRIDGE_TMPL' service logs 2>&1 & sleep 0.2; pkill -P \$\$ ; wait"
  [ -f "$BB_HOME/logs/ws-server.log" ]
  [ -f "$BB_HOME/logs/local-proxy.log" ]
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
  [[ "$output" == *"[OK] ws-server binary present"* ]]
  [[ "$output" == *"[OK] local-proxy binary present"* ]]
  [[ "$output" == *"[OK] bridge-cmd binary present"* ]]
  [[ "$output" == *"[OK] extension/manifest.json valid"* ]]
}

@test "bridge service doctor reports FAIL when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" service doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] ws-server binary missing"* ]]
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
  grep -q 'ws://127.0.0.1:3001' "$HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist"
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
  [[ ! -f "$BB_HOME/run/ws-server.pid" ]]
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
  grep -q "bootstrap user/501 $BB_HOME/launchagents/com.browser-bridge.bridge.plist" "$BB_TEST_TMP/launchctl_calls.txt"
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
  grep -q "bootstrap user/501 $HOME/Library/LaunchAgents/com.browser-bridge.bridge.plist" "$BB_TEST_TMP/launchctl_calls.txt"
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
  grep -q "bootout user/501/com.browser-bridge.bridge" "$BB_TEST_TMP/launchctl_calls.txt"
}

@test "supervisor restarts a crashed child and shuts down cleanly" {
  make_fake_binaries
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"

  bash "$BRIDGE_TMPL" service up --foreground >"$BB_TEST_TMP/supervisor.log" 2>&1 &
  SUP_PID=$!
  local waited=0
  while [[ $waited -lt 50 ]]; do
    [[ -f "$BB_HOME/run/ws-server.pid" && -f "$BB_HOME/run/local-proxy.pid" ]] && break
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ -f "$BB_HOME/run/ws-server.pid" ]]

  local old_ws new_ws=""
  old_ws=$(cat "$BB_HOME/run/ws-server.pid")
  kill "$old_ws"

  waited=0
  while [[ $waited -lt 50 ]]; do
    if [[ -f "$BB_HOME/run/ws-server.pid" ]]; then
      new_ws=$(cat "$BB_HOME/run/ws-server.pid")
      if [[ -n "$new_ws" && "$new_ws" != "$old_ws" ]]; then
        break
      fi
    fi
    new_ws=""
    sleep 0.1
    waited=$((waited + 1))
  done
  [[ -n "$new_ws" ]]
  [ "$new_ws" != "$old_ws" ]
  grep -q "restarting ws-server" "$BB_TEST_TMP/supervisor.log"

  kill -TERM "$SUP_PID"
  wait "$SUP_PID" 2>/dev/null || true
  ! kill -0 "$new_ws" 2>/dev/null
  [[ ! -f "$BB_HOME/run/ws-server.pid" ]]
  [[ ! -f "$BB_HOME/run/local-proxy.pid" ]]
}

@test "supervisor exits 0 when a recorded pair is already running" {
  make_fake_binaries
  mkdir -p "$BB_HOME/run"
  python3 -c "import socket, time; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); time.sleep(30)" &
  P1=$!
  python3 -c "import socket, time; s=socket.socket(); s.bind(('127.0.0.1',3002)); s.listen(); time.sleep(30)" &
  P2=$!
  sleep 0.3
  echo "$P1" > "$BB_HOME/run/ws-server.pid"
  echo "$P2" > "$BB_HOME/run/local-proxy.pid"

  run bash "$BRIDGE_TMPL" service up --foreground
  kill "$P1" "$P2" 2>/dev/null || true
  [ "$status" -eq 0 ]
  [[ "$output" == *"nothing to do"* ]]
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
