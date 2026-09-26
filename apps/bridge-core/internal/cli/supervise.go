package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"
)

// maxRestarts is the supervisor's crash budget: more than this many
// consecutive short-lived daemon starts and the supervisor exits 1 so
// launchd (KeepAlive + ThrottleInterval) backs off and retries the group.
const maxRestarts = 5

// Supervise is `bridge service up --foreground`: one foreground process
// holding the daemon (`bridge serve`, spawned by spawnCore) so launchd can
// supervise it as a single KeepAlive'd job (ADR-0005). Returns nil on a
// clean (signal-driven) shutdown.
func (e *Env) Supervise(ctx context.Context, r Runner, logf func(string, ...any)) error {
	if !executable(e.BridgeBin()) {
		return errf("BB-E002", "install not run. Execute the install script first.")
	}
	if err := os.MkdirAll(e.LogDir(), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := os.MkdirAll(e.RunDir(), 0o755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	// Single-supervisor invariant: at most one supervisor may watch this
	// RunDir. A stale pidfile (pid gone or reused by something else) is
	// dropped.
	if spid, ok := readPid(e.SupervisorPidFile()); ok {
		if pidIs(ctx, r, spid, "service up --foreground") {
			return errf("BB-E307", "a supervisor is already running (pid %d); not starting a second one", spid)
		}
		_ = os.Remove(e.SupervisorPidFile())
	}

	state, num := e.classify(ctx, r)
	if state == "foreign" {
		return errf("BB-E010", "port %d is held by a process with no trustworthy bridge pidfile; not taking over (launchd will retry)", num)
	}

	if err := os.WriteFile(e.SupervisorPidFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return fmt.Errorf("write supervisor pid file: %w", err)
	}
	defer func() { _ = os.Remove(e.SupervisorPidFile()) }()

	var child *spawnedCore
	if state == "ours" {
		logf("supervisor: adopting %s (pid %d)", serviceName, num)
	} else {
		var err error
		child, err = e.supervisorSpawn(ctx, r, logf)
		if err != nil {
			if ctx.Err() != nil {
				e.supervisorShutdown(r, logf)
				return nil
			}
			return err
		}
	}
	if pid, ok := readPid(e.PidFile()); ok {
		logf("supervisor: %s=%d", serviceName, pid)
	}

	return e.supervisorWatch(ctx, r, logf, child)
}

// supervisorSpawn is supervisor_spawn: pre-flight port check, spawn, and a
// bind wait that also notices the child dying before it binds.
func (e *Env) supervisorSpawn(ctx context.Context, r Runner, logf func(string, ...any)) (*spawnedCore, error) {
	if pid := e.serviceState(ctx, r); pid != 0 {
		logf("supervisor: %s already running (pid %d)", serviceName, pid)
		return nil, nil
	}
	if held := e.servicePortInUse(); held != 0 {
		return nil, errf("BB-E010", "%s port %d is already in use", serviceName, held)
	}
	child, err := e.spawnCore()
	if err != nil {
		return nil, err
	}
	err = e.waitForBind(ctx, child)
	if err == nil {
		return child, nil
	}
	_ = child.logFile.Close()
	_ = os.Remove(e.PidFile())
	if ctx.Err() != nil {
		_ = r.Kill(child.pid, syscall.SIGTERM)
		return nil, ctx.Err()
	}
	if errors.Is(err, errChildExitedBeforeBind) {
		return nil, errf("BB-E011", "%s exited before binding port %d (see %s)", serviceName, e.WSPort, e.LogFile())
	}
	_ = r.Kill(child.pid, syscall.SIGTERM)
	return nil, errf("BB-E011", "%s failed to bind port %d within 5s (see %s)", serviceName, e.WSPort, e.LogFile())
}

// supervisorWatch is supervisor_watch. A child that survives at least one
// bash tick (1s) resets the restart budget; consecutive fast deaths exhaust
// it. An adopted child (started by an earlier run, no exec handle) is
// watched with a 1s liveness poll exactly like the bash loop.
func (e *Env) supervisorWatch(ctx context.Context, r Runner, logf func(string, ...any), child *spawnedCore) error {
	restarts := 0
	startedAt := time.Now()
	for {
		if child != nil {
			select {
			case <-ctx.Done():
				e.supervisorShutdown(r, logf)
				return nil
			case <-child.wait:
				_ = child.logFile.Close()
				if time.Since(startedAt) >= time.Second {
					restarts = 0
				}
			}
		} else {
			select {
			case <-ctx.Done():
				e.supervisorShutdown(r, logf)
				return nil
			case <-time.After(time.Second):
			}
			pid, ok := readPid(e.PidFile())
			if ok && pidAlive(ctx, r, pid) {
				restarts = 0
				continue
			}
		}

		restarts++
		if restarts > maxRestarts {
			return fmt.Errorf("supervisor: %s keeps dying; giving up so launchd can restart the group", serviceName)
		}
		logf("supervisor: restarting %s (attempt %d)", serviceName, restarts)
		_ = os.Remove(e.PidFile())
		var err error
		child, err = e.supervisorSpawn(ctx, r, logf)
		if err != nil {
			if ctx.Err() != nil {
				e.supervisorShutdown(r, logf)
				return nil
			}
			return err
		}
		startedAt = time.Now()
	}
}

// supervisorShutdown is the bash TERM/INT trap: drop the supervisor pidfile
// and stop the daemon. Runs on a detached context — the caller's context is
// already canceled by the signal.
func (e *Env) supervisorShutdown(r Runner, logf func(string, ...any)) {
	logf("supervisor: shutting down")
	_ = os.Remove(e.SupervisorPidFile())
	// Fresh context: the command context is already canceled, and stopService
	// still needs its SIGTERM grace polling to work.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = e.stopService(ctx, r, logf)
}

// executable mirrors the bash [[ -x ... ]] check.
func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
