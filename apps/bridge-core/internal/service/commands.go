package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// logf returns a printf-style logger writing to w (bash's info()).
func logf(w io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		fmt.Fprintf(w, format+"\n", args...)
	}
}

// Up is `bridge service up`. Without --foreground it is the launchd path on
// macOS and the pidfile path elsewhere; --foreground runs the supervisor.
func Up(ctx context.Context, e *Env, r Runner, foreground bool, w io.Writer) error {
	log := logf(w)
	if !executable(e.CoreBin()) {
		return errf("BB-E002", "install not run. Execute the install script first.")
	}
	if foreground {
		return e.Supervise(ctx, r, log)
	}
	if e.GOOS != "darwin" {
		return e.startService(ctx, r, log)
	}

	// launchctl list failing means "not loaded" in the bash pipeline (grep
	// simply finds no match); keep that reading instead of surfacing it.
	loaded, _ := r.LaunchctlLoaded(ctx, LaunchAgentLabel)
	if loaded {
		if spid, ok := readPid(e.SupervisorPidFile()); ok && pidIs(ctx, r, spid, "service up --foreground") {
			log("bridge service up: already running (supervised by launchd, label %s)", LaunchAgentLabel)
			return nil
		}
		_ = r.LaunchctlBootout(ctx, "gui/"+strconv.Itoa(r.UID()), LaunchAgentLabel)
		log("bridge service up: replaced stale LaunchAgent job (label %s)", LaunchAgentLabel)
	}

	state, num := e.classify(ctx, r)
	if state == "foreign" {
		return errf("BB-E010", "port %d is occupied by something other than a supervised bridge instance (check 'bridge service status'). Free the port and retry.", num)
	}
	if err := e.writeLaunchAgentPlists(); err != nil {
		return err
	}
	plist := e.StagingPlist()
	if e.Enabled() {
		plist = e.LaunchAgentPlist()
	}
	if err := r.LaunchctlBootstrap(ctx, "gui/"+strconv.Itoa(r.UID()), plist); err != nil {
		return errf("BB-E302", "failed to load LaunchAgent with launchctl bootstrap")
	}
	log("bridge service up: supervisor started (label %s)", LaunchAgentLabel)
	return nil
}

// Down is `bridge service down`.
func Down(ctx context.Context, e *Env, r Runner, w io.Writer) error {
	log := logf(w)
	if e.GOOS != "darwin" {
		return e.stopService(ctx, r, log)
	}
	loaded, err := r.LaunchctlLoaded(ctx, LaunchAgentLabel)
	if err != nil {
		return errf("BB-E304", "%v", err)
	}
	if loaded {
		if err := r.LaunchctlBootout(ctx, "gui/"+strconv.Itoa(r.UID()), LaunchAgentLabel); err != nil {
			return errf("BB-E304", "failed to stop the supervisor (launchctl bootout failed)")
		}
		log("supervisor stopped")
		// The supervisor's shutdown stops the child; give it a moment before
		// the sweep below so we don't double-signal (bash: sleep 1).
		e.waitForSupervisorExit(ctx, r)
	} else {
		log("supervisor not loaded")
	}
	if _, ok := readPid(e.PidFile()); ok {
		if err := e.stopService(ctx, r, log); err != nil {
			return err
		}
	}
	_ = os.Remove(e.SupervisorPidFile())
	return nil
}

// waitForSupervisorExit polls briefly for the supervisor to exit after a
// bootout, bounded at ~2s (the bash version slept a fixed 1s).
func (e *Env) waitForSupervisorExit(ctx context.Context, r Runner) {
	spid, ok := readPid(e.SupervisorPidFile())
	if !ok {
		return
	}
	for range 20 {
		if !pidAlive(ctx, r, spid) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.pollInterval):
		}
	}
}

// Restart is `bridge service restart` = down + up.
func Restart(ctx context.Context, e *Env, r Runner, w io.Writer) error {
	if err := Down(ctx, e, r, w); err != nil {
		return err
	}
	return Up(ctx, e, r, false, w)
}

// Status is `bridge service status`: prints service state and (macOS) the
// auto-start state. running=false means the command should exit 1.
func Status(ctx context.Context, e *Env, r Runner, w io.Writer) (running bool, err error) {
	log := logf(w)
	pid := e.serviceState(ctx, r)
	if pid != 0 {
		log("%-13s running  (pid %d)", serviceName+":", pid)
	} else {
		log("%-13s stopped", serviceName+":")
	}
	if e.GOOS != "darwin" {
		log("auto-start:  unsupported on this platform (macOS only)")
		return pid != 0, nil
	}
	// launchctl list failing reads as "not loaded" (bash pipeline semantics).
	loaded, _ := r.LaunchctlLoaded(ctx, LaunchAgentLabel)
	switch {
	case e.Enabled() && loaded:
		log("auto-start:  enabled, agent loaded (label %s)", LaunchAgentLabel)
	case e.Enabled():
		log("auto-start:  enabled (starts at login; not running now)")
	default:
		log("auto-start:  disabled")
	}
	return pid != 0, nil
}

// Enable is `bridge service enable` (macOS login auto-start).
func Enable(ctx context.Context, e *Env, r Runner, w io.Writer) error {
	log := logf(w)
	if e.GOOS != "darwin" {
		return errf("BB-E300", "login auto-start is only supported on macOS")
	}
	if err := os.MkdirAll(filepath.Dir(e.LaunchAgentPlist()), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(e.LaunchAgentPlist(), []byte(e.renderLaunchAgentPlist()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", e.LaunchAgentPlist(), err)
	}
	log("Login auto-start enabled.")
	// launchctl list failing reads as "not loaded" (bash pipeline semantics).
	loaded, _ := r.LaunchctlLoaded(ctx, LaunchAgentLabel)
	if !loaded {
		log("Services are not running yet ('bridge service up' starts them now).")
	}
	return nil
}

// Disable is `bridge service disable`.
func Disable(ctx context.Context, e *Env, r Runner, w io.Writer) error {
	log := logf(w)
	if e.GOOS != "darwin" {
		log("Login auto-start: not supported on this platform (macOS only).")
		return nil
	}
	_ = os.Remove(e.LaunchAgentPlist())
	_ = os.Remove(e.StagingPlist())
	log("Login auto-start disabled. Running services are not stopped ('bridge service down' stops them).")
	return nil
}

// Autostart is the deprecated `bridge autostart on|off|status` alias. The
// deprecation warning goes to stderr (bash printed it there); the caller
// wires warn to cmd.ErrOrStderr().
func Autostart(ctx context.Context, e *Env, r Runner, action string, w, warn io.Writer) error {
	fmt.Fprintln(warn, "Deprecation warning: 'bridge autostart' is deprecated; use 'bridge service enable|disable' (state: 'bridge service status').")
	switch action {
	case "on":
		if err := Enable(ctx, e, r, w); err != nil {
			return err
		}
		return Up(ctx, e, r, false, w)
	case "off":
		if err := Down(ctx, e, r, w); err != nil {
			return err
		}
		return Disable(ctx, e, r, w)
	case "status":
		running, err := Status(ctx, e, r, w)
		if err != nil {
			return err
		}
		if !running {
			return ErrSilent
		}
		return nil
	default:
		return errf("BB-E303", "usage: bridge autostart on|off|status (deprecated)")
	}
}

// ErrSilent marks "exit 1 without further output" (status/doctor findings
// are the output). The cli package maps it to a silent non-zero exit.
var ErrSilent = errors.New("silent failure")

// Update is `bridge service update [version]`: fetch install.sh from the
// release and run it with BB_VERSION pinned, then restart when installed.
func Update(ctx context.Context, e *Env, r Runner, target, scriptURL string, w io.Writer) error {
	log := logf(w)
	log("Updating to %s", target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return errf("BB-E103", "update failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errf("BB-E103", "update failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errf("BB-E103", "update failed")
	}
	script, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errf("BB-E103", "update failed")
	}

	cmd := exec.CommandContext(ctx, "bash")
	cmd.Stdin = strings.NewReader(string(script))
	// The bash router did not export BB_HOME here, so a custom-prefix install
	// updated into ~/.browser-bridge. Pinning BB_HOME is strictly closer to
	// the intent: update the install this bridge belongs to.
	cmd.Env = append(os.Environ(), "BB_VERSION="+target, "BB_HOME="+e.BBHome)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errf("BB-E103", "update failed")
	}
	if st, err := os.Stat(e.BBHome); err == nil && st.IsDir() {
		// bash: `... && cmd_service_restart || true` — a failed restart does
		// not fail the update.
		_ = Restart(ctx, e, r, w)
	}
	return nil
}

// UpdateScriptURL builds the install.sh URL for a target version. The
// host:port org form switches to plain http so tests (and mirrors) can point
// at a mock server, the same trick install.sh uses.
func UpdateScriptURL(org, repo, target string) string {
	if isMockHost(org) {
		return "http://" + org + "/install.sh"
	}
	if target == "latest" {
		return fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/install.sh", org, repo)
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/install.sh", org, repo, target)
}

func isMockHost(org string) bool {
	host, port, err := net.SplitHostPort(org)
	return err == nil && host != "" && port != ""
}

// Doctor is `bridge service doctor`. ok=false means the command should exit 1.
func Doctor(ctx context.Context, e *Env, r Runner, w io.Writer) (ok bool, err error) {
	log := logf(w)
	rc := true
	if executable(e.CoreBin()) {
		log("[OK] bridge-core binary present")
	} else {
		log("[FAIL] bridge-core binary missing")
		rc = false
	}
	if executable(e.BridgeBin()) {
		log("[OK] bridge binary present")
	} else {
		log("[FAIL] bridge binary missing")
		rc = false
	}
	manifest := filepath.Join(e.BBHome, "extension", "manifest.json")
	alt := filepath.Join(e.ExtensionDir, "extension", "manifest.json")
	switch {
	case validJSONFile(manifest):
		log("[OK] extension/manifest.json valid")
	case validJSONFile(alt):
		log("[OK] extension/manifest.json valid (at %s)", alt)
	default:
		log("[FAIL] extension/manifest.json missing or invalid")
		rc = false
	}
	if e.serviceState(ctx, r) != 0 {
		log("[OK] bridge-core running")
	} else {
		log("[WARN] bridge-core not running (run 'bridge service up')")
	}
	return rc, nil
}

// validJSONFile is the bash python3 json.load check.
func validJSONFile(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var v any
	return json.Unmarshal(raw, &v) == nil
}

// Version is `bridge service version`: installed from $BB_HOME/version,
// latest from the LATEST_VERSION env (the bash version never hit the network).
func Version(e *Env, w io.Writer) error {
	installed := "unknown"
	if raw, err := os.ReadFile(filepath.Join(e.BBHome, "version")); err == nil {
		installed = strings.TrimSpace(string(raw))
	}
	latest := os.Getenv("LATEST_VERSION")
	if latest == "" {
		latest = "unknown"
	}
	fmt.Fprintf(w, "installed: %s\nlatest:    %s\n", installed, latest)
	return nil
}

// Uninstall is `bridge service uninstall`. When assumeYes is false it
// prompts on w and reads the answer from in.
func Uninstall(ctx context.Context, e *Env, r Runner, assumeYes bool, in io.Reader, w io.Writer) error {
	log := logf(w)
	if !assumeYes {
		fmt.Fprintf(w, "About to rm -rf %s. Continue? [y/N] ", e.BBHome)
		var ans string
		if _, err := fmt.Fscanln(in, &ans); err != nil {
			ans = ""
		}
		if ans != "y" && ans != "Y" {
			log("aborted")
			return nil
		}
	}
	// bash: service_disable; cmd_service_down || true — best effort.
	_ = Disable(ctx, e, r, w)
	_ = Down(ctx, e, r, w)
	if err := os.RemoveAll(e.BBHome); err != nil {
		return fmt.Errorf("remove %s: %w", e.BBHome, err)
	}
	log("Removed %s", e.BBHome)
	if st, err := os.Stat(e.ExtensionDir); err == nil && st.IsDir() {
		if err := os.RemoveAll(e.ExtensionDir); err != nil {
			return fmt.Errorf("remove %s: %w", e.ExtensionDir, err)
		}
		log("Removed %s", e.ExtensionDir)
	}
	link := filepath.Join(e.HomeDir, ".local", "bin", "bridge")
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if _, err := os.Stat(link); os.IsNotExist(err) {
			// Dangling symlink (target removed with BBHome).
			_ = os.Remove(link)
			log("Removed stale symlink %s", link)
		}
	}
	return nil
}
