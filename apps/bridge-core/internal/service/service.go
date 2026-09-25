package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CodedError carries the BB-E### codes the bash implementation used, so the
// user-facing text stays greppable across the rewrite.
type CodedError struct {
	Code string
	Msg  string
}

func (e *CodedError) Error() string { return e.Code + ": " + e.Msg }

// errf formats "BB-E000: ..." errors (bash's `die "BB-E000: ..."`).
func errf(code, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// IsCode reports whether err is a CodedError with the given code.
func IsCode(err error, code string) bool {
	var ce *CodedError
	return errors.As(err, &ce) && ce.Code == code
}

// portInUse mirrors port_in_use: a TCP connect with a 1s timeout. The bash
// probes always targeted 127.0.0.1 (not the per-service hostname).
func portInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// servicePortInUse checks all three bridge-core ports (it binds all three
// and exits if any single bind fails). Returns the held port, 0 when free.
func (e *Env) servicePortInUse() int {
	for _, p := range []int{e.WSPort, e.LocalPort, e.MCPPort} {
		if portInUse(p) {
			return p
		}
	}
	return 0
}

// readPid reads a pidfile; ok=false when absent or unparseable.
func readPid(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// pidAlive mirrors pid_alive: ps state empty → kill -0 fallback; a zombie
// counts as dead (our own children are reaped via cmd.Wait).
func pidAlive(ctx context.Context, r Runner, pid int) bool {
	state, _, err := r.PS(ctx, pid)
	if err != nil {
		return r.Kill(pid, 0) == nil
	}
	return !strings.HasPrefix(state, "Z")
}

// pidIs reports whether pid's command line contains want (bash pid_is
// matched comm+args as one string; PS's command column covers both).
func pidIs(ctx context.Context, r Runner, pid int, want string) bool {
	_, command, err := r.PS(ctx, pid)
	return err == nil && command != "" && strings.Contains(command, want)
}

// serviceState returns the live bridge-core pid, 0 when stopped.
func (e *Env) serviceState(ctx context.Context, r Runner) int {
	pid, ok := readPid(e.PidFile())
	if !ok || !pidAlive(ctx, r, pid) {
		return 0
	}
	return pid
}

// classify is classify_service: "ours <pid>" (pidfile live and really
// bridge-core), "free", or "foreign <port>" (held without a trustworthy
// pidfile). A stale/unverifiable pidfile is removed.
func (e *Env) classify(ctx context.Context, r Runner) (state string, num int) {
	pidFile := e.PidFile()
	pid, ok := readPid(pidFile)
	if ok && pidAlive(ctx, r, pid) {
		if pidIs(ctx, r, pid, serviceName) {
			return "ours", pid
		}
		if held := e.servicePortInUse(); held != 0 {
			return "foreign", held
		}
	}
	_ = os.Remove(pidFile)
	if held := e.servicePortInUse(); held != 0 {
		return "foreign", held
	}
	return "free", 0
}

// startService is the unsupervised (Linux / fallback) start_service.
func (e *Env) startService(ctx context.Context, r Runner, logf func(string, ...any)) error {
	if pid := e.serviceState(ctx, r); pid != 0 {
		logf("%s already running (pid %d)", serviceName, pid)
		return nil
	}
	if held := e.servicePortInUse(); held != 0 {
		return errf("BB-E010", "%s port %d is already in use", serviceName, held)
	}
	child, err := e.spawnCore()
	if err != nil {
		return err
	}
	defer func() { _ = child.logFile.Close() }()

	if err := e.waitForBind(ctx, child); err != nil {
		_ = r.Kill(child.pid, syscall.SIGTERM)
		_ = os.Remove(e.PidFile())
		return errf("BB-E011", "%s failed to bind port %d within 5s (see %s)", serviceName, e.WSPort, e.LogFile())
	}
	return nil
}

// spawnedCore is a freshly started bridge-core child.
type spawnedCore struct {
	cmd     *exec.Cmd
	pid     int
	logFile *os.File
	wait    chan error // receives cmd.Wait() exactly once
}

// spawnCore starts bridge-core in the background with its log and pidfile,
// shared by start_service and supervisor_spawn.
func (e *Env) spawnCore() (*spawnedCore, error) {
	if err := os.MkdirAll(e.LogDir(), 0o755); err != nil {
		return nil, errf("BB-E011", "create log dir: %v", err)
	}
	if err := os.MkdirAll(e.RunDir(), 0o755); err != nil {
		return nil, errf("BB-E011", "create run dir: %v", err)
	}
	logFile, err := os.OpenFile(e.LogFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, errf("BB-E011", "open log file: %v", err)
	}
	cmd := exec.Command(e.CoreBin()) //nolint:gosec // path is $BB_HOME/bin/bridge-core, same as bash
	cmd.Env = e.childEnv()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, errf("BB-E011", "start %s: %v", e.CoreBin(), err)
	}
	child := &spawnedCore{cmd: cmd, pid: cmd.Process.Pid, logFile: logFile, wait: make(chan error, 1)}
	go func() { child.wait <- cmd.Wait() }()
	if err := os.WriteFile(e.PidFile(), []byte(strconv.Itoa(child.pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_ = logFile.Close()
		return nil, errf("BB-E011", "write pid file: %v", err)
	}
	return child, nil
}

// errChildExitedBeforeBind marks the waitForBind outcome "the child died
// before binding" so the supervisor can pick the bash BB-E011 wording.
var errChildExitedBeforeBind = errors.New("child exited before binding")

// waitForBind polls the control-plane port until bridge-core has bound it
// (the liveness proxy from the bash script: 3002 binds first, so 3001
// answering means the child got that far). A child that dies first is
// reported as errChildExitedBeforeBind — start_service polls the full 5s in
// bash, but failing fast changes only the wait, not the message.
func (e *Env) waitForBind(ctx context.Context, child *spawnedCore) error {
	for range e.bindWaitAttempts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-child.wait:
			return fmt.Errorf("%w: %v", errChildExitedBeforeBind, err)
		default:
		}
		if portInUse(e.WSPort) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.pollInterval):
		}
	}
	return fmt.Errorf("timeout waiting for port %d", e.WSPort)
}

// stopService is the bash stop_service: TERM, short grace, then KILL.
func (e *Env) stopService(ctx context.Context, r Runner, logf func(string, ...any)) error {
	pidFile := e.PidFile()
	pid, ok := readPid(pidFile)
	if !ok {
		logf("%s already stopped", serviceName)
		return nil
	}
	if !pidAlive(ctx, r, pid) {
		logf("%s: pid %d not running, cleaning up", serviceName, pid)
		_ = os.Remove(pidFile)
		return nil
	}
	_ = r.Kill(pid, syscall.SIGTERM)
	for range e.termWaitAttempts {
		if !pidAlive(ctx, r, pid) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.pollInterval):
		}
	}
	if pidAlive(ctx, r, pid) {
		_ = r.Kill(pid, syscall.SIGKILL)
		logf("%s: sent SIGKILL after timeout", serviceName)
	}
	_ = os.Remove(pidFile)
	logf("%s stopped", serviceName)
	return nil
}
