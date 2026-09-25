package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// spawnRunner answers PS/Kill with the real implementation (tests spawn real,
// disposable child processes) but fails the test on any launchctl touch —
// the developer machine runs a production LaunchAgent that must never be
// affected by the test suite.
type spawnRunner struct {
	t    *testing.T
	exec ExecRunner
}

func (s spawnRunner) LaunchctlLoaded(context.Context, string) (bool, error) {
	s.t.Error("launchctl list called in a spawn test")
	return false, errors.New("launchctl disabled in this test")
}

func (s spawnRunner) LaunchctlBootstrap(context.Context, string, string) error {
	s.t.Error("launchctl bootstrap called in a spawn test")
	return errors.New("launchctl disabled in this test")
}

func (s spawnRunner) LaunchctlBootout(context.Context, string, string) error {
	s.t.Error("launchctl bootout called in a spawn test")
	return errors.New("launchctl disabled in this test")
}

func (s spawnRunner) PS(ctx context.Context, pid int) (string, string, error) {
	return s.exec.PS(ctx, pid)
}

func (s spawnRunner) Kill(pid int, sig syscall.Signal) error {
	return s.exec.Kill(pid, sig)
}

func (s spawnRunner) UID() int { return 0 }

// fakeCoreBindsAll is a bridge-core stand-in that binds all three ports from
// the child env and then idles, like the real daemon.
const fakeCoreBindsAll = `#!/usr/bin/env python3
import os, socket, time
socks = []
for port in (int(os.environ["BRIDGE_WS_PORT"]), int(os.environ["BRIDGE_LOCAL_PORT"]), int(os.environ["BRIDGE_MCP_PORT"])):
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("127.0.0.1", port))
    s.listen()
    socks.append(s)
while True:
    time.sleep(60)
`

// fakeCoreBindsThenDies binds the control-plane port, records the spawn, and
// exits quickly — the crash-looping child for the restart-budget test.
const fakeCoreBindsThenDies = `#!/usr/bin/env python3
import os, socket, time
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", int(os.environ["BRIDGE_WS_PORT"])))
s.listen()
with open(os.environ["BB_HOME"] + "/spawns", "a") as f:
    f.write("x\n")
time.sleep(0.15)
`

// fakeCoreNeverBinds idles without binding any port.
const fakeCoreNeverBinds = `#!/usr/bin/env python3
import time
while True:
    time.sleep(60)
`

func TestStartServiceLinuxLifecycle(t *testing.T) {
	e := testEnv(t, "linux")
	writeFakeCore(t, e, fakeCoreBindsAll)
	r := spawnRunner{t: t}
	var out bytes.Buffer

	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up: %v", err)
	}
	pid, ok := readPid(e.PidFile())
	if !ok {
		t.Fatal("pidfile missing after up")
	}
	if !pidAlive(context.Background(), r, pid) {
		t.Fatal("bridge-core child not alive after up")
	}
	if !portInUse(e.WSPort) {
		t.Fatal("control-plane port not bound after up")
	}

	// Idempotent: a second up reports the running child and spawns nothing.
	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up again: %v", err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("bridge-core already running (pid %d)", pid)) {
		t.Errorf("out = %q", out.String())
	}

	if err := Down(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if pidAlive(context.Background(), r, pid) {
		t.Error("bridge-core child still alive after down")
	}
	if _, err := os.Stat(e.PidFile()); !os.IsNotExist(err) {
		t.Error("pidfile still present after down")
	}
	if !strings.Contains(out.String(), "bridge-core stopped") {
		t.Errorf("out = %q", out.String())
	}
}

func TestStartServicePortConflict(t *testing.T) {
	e := testEnv(t, "linux")
	writeFakeCore(t, e, fakeCoreBindsAll)
	e.LocalPort = holdPort(t) // a non-WS port conflict must also block startup
	r := spawnRunner{t: t}
	var out bytes.Buffer
	err := Up(context.Background(), e, r, false, &out)
	if !IsCode(err, "BB-E010") {
		t.Fatalf("err = %v, want BB-E010", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("port %d is already in use", e.LocalPort)) {
		t.Errorf("err = %v", err)
	}
}

func TestStartServiceBindTimeout(t *testing.T) {
	e := testEnv(t, "linux")
	e.bindWaitAttempts = 30 // 300ms with the test poll interval
	writeFakeCore(t, e, fakeCoreNeverBinds)
	r := spawnRunner{t: t}
	var out bytes.Buffer
	err := Up(context.Background(), e, r, false, &out)
	if !IsCode(err, "BB-E011") {
		t.Fatalf("err = %v, want BB-E011", err)
	}
	if !strings.Contains(err.Error(), "failed to bind port") {
		t.Errorf("err = %v", err)
	}
	if _, statErr := os.Stat(e.PidFile()); !os.IsNotExist(statErr) {
		t.Error("pidfile still present after failed start")
	}
}

func TestSuperviseLifecycle(t *testing.T) {
	e := testEnv(t, "darwin") // supervise is the same code on both platforms
	writeFakeCore(t, e, fakeCoreBindsAll)
	r := spawnRunner{t: t}

	ctx, cancel := context.WithCancel(context.Background())
	var out safeBuffer
	done := make(chan error, 1)
	go func() { done <- e.Supervise(ctx, r, out.Printf) }()

	// Wait for the child to come up: pidfile + port bound.
	childPID := waitForChild(t, e)
	waitFor(t, func() bool {
		return strings.Contains(out.String(), fmt.Sprintf("supervisor: bridge-core=%d", childPID))
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Supervise: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("supervise did not return after cancel")
	}
	if pidAlive(context.Background(), r, childPID) {
		t.Error("bridge-core child still alive after shutdown")
	}
	for _, p := range []string{e.PidFile(), e.SupervisorPidFile()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present after shutdown", p)
		}
	}
	if !strings.Contains(out.String(), "supervisor: shutting down") {
		t.Errorf("out = %q", out.String())
	}
}

func TestSuperviseSingleInstance(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, fakeCoreBindsAll)
	r := newFakeRunner()
	r.procs[4242] = fakeProc{state: "Ss", command: e.BBHome + "/bin/bridge service up --foreground"}
	writeSupervisorPidFile(t, e, 4242)
	var out bytes.Buffer
	err := e.Supervise(context.Background(), r, logf(&out))
	if !IsCode(err, "BB-E307") {
		t.Fatalf("err = %v, want BB-E307", err)
	}
}

func TestSuperviseStaleSupervisorPidRemoved(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, fakeCoreBindsAll)
	r := spawnRunner{t: t}
	// A dead supervisor pidfile must not block a new supervisor.
	writeSupervisorPidFile(t, e, 999999)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out safeBuffer
	go func() { done <- e.Supervise(ctx, r, out.Printf) }()
	waitForChild(t, e)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Supervise: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("supervise did not return after cancel")
	}
}

func TestSuperviseForeignPort(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, fakeCoreBindsAll)
	e.MCPPort = holdPort(t)
	r := spawnRunner{t: t}
	var out bytes.Buffer
	err := e.Supervise(context.Background(), r, logf(&out))
	if !IsCode(err, "BB-E010") {
		t.Fatalf("err = %v, want BB-E010", err)
	}
	if !strings.Contains(err.Error(), "no trustworthy bridge pidfile") {
		t.Errorf("err = %v", err)
	}
	// The supervisor must not have started (no pidfile written on the way out).
	if _, statErr := os.Stat(e.SupervisorPidFile()); !os.IsNotExist(statErr) {
		t.Error("supervisor pidfile present after refusal")
	}
}

func TestSuperviseRestartBudget(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, fakeCoreBindsThenDies)
	r := spawnRunner{t: t}
	var out safeBuffer
	err := e.Supervise(context.Background(), r, out.Printf)
	if err == nil || !strings.Contains(err.Error(), "keeps dying; giving up so launchd can restart the group") {
		t.Fatalf("err = %v, want the give-up message", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(e.BBHome, "spawns"))
	if readErr != nil {
		t.Fatalf("spawns: %v", readErr)
	}
	spawns := len(strings.Split(strings.TrimSpace(string(raw)), "\n"))
	if spawns != maxRestarts+1 {
		t.Errorf("spawns = %d, want %d (initial + %d restarts)", spawns, maxRestarts+1, maxRestarts)
	}
	if !strings.Contains(out.String(), "supervisor: restarting bridge-core (attempt 1)") {
		t.Errorf("out = %q", out.String())
	}
}

func TestSuperviseExitedBeforeBinding(t *testing.T) {
	e := testEnv(t, "darwin")
	// Exits immediately without binding: the BB-E011 "exited before binding"
	// variant, distinct from the bind timeout.
	writeFakeCore(t, e, "#!/usr/bin/env bash\nexit 1\n")
	e.bindWaitAttempts = 500
	r := spawnRunner{t: t}
	var out bytes.Buffer
	err := e.Supervise(context.Background(), r, logf(&out))
	if !IsCode(err, "BB-E011") {
		t.Fatalf("err = %v, want BB-E011", err)
	}
	if !strings.Contains(err.Error(), "exited before binding port") {
		t.Errorf("err = %v", err)
	}
}

func TestLogsTailAndFollow(t *testing.T) {
	e := testEnv(t, "linux")
	if err := os.MkdirAll(e.LogDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	var initial strings.Builder
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&initial, "line %d\n", i)
	}
	if err := os.WriteFile(e.LogFile(), []byte(initial.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var out safeBuffer
	done := make(chan error, 1)
	go func() { done <- Logs(ctx, e, &out) }()

	// The initial tail shows only the last 10 lines.
	waitFor(t, func() bool { return strings.Contains(out.String(), "line 15") })
	if strings.Contains(out.String(), "line 5\n") {
		t.Errorf("initial tail printed more than 10 lines:\n%s", out.String())
	}

	// Follow: appended lines appear.
	f, err := os.OpenFile(e.LogFile(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("follow me\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	waitFor(t, func() bool { return strings.Contains(out.String(), "follow me") })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Logs: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logs did not return after cancel")
	}

	// Every line carries the timestamp prefix.
	for line := range strings.Lines(out.String()) {
		if len(line) < 20 || line[4] != '-' || line[7] != '-' || line[13] != ':' {
			t.Errorf("line missing timestamp prefix: %q", line)
		}
	}
}

func TestLogsMissingFile(t *testing.T) {
	e := testEnv(t, "linux")
	var out bytes.Buffer
	if err := Logs(context.Background(), e, &out); err == nil {
		t.Fatal("want an error for a missing log file")
	}
}

// ---- helpers ----------------------------------------------------------------

// safeBuffer is a bytes.Buffer safe for the supervise goroutine + test reader.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Printf adapts the buffer to the logf signature.
func (b *safeBuffer) Printf(format string, args ...any) {
	fmt.Fprintf(b, format+"\n", args...)
}

// waitForChild waits until the supervisor has written the child pidfile and
// returns the pid.
func waitForChild(t *testing.T, e *Env) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := readPid(e.PidFile()); ok {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for bridge-core pidfile")
	return 0
}

// waitFor polls cond until it holds, failing after 10s.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never held within 10s")
}
