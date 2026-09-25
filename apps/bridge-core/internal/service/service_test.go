package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---- fakes ---------------------------------------------------------------

type fakeProc struct{ state, command string }

// fakeRunner implements Runner in memory: launchd state is a set of loaded
// labels, processes a pid table. No real launchctl, ps, or signal is ever
// touched.
type fakeRunner struct {
	mu           sync.Mutex
	loaded       map[string]bool
	uid          int
	procs        map[int]fakeProc
	kills        []string
	bootstraps   []string
	bootouts     []string
	bootstrapErr error
	bootoutErr   error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{loaded: map[string]bool{}, uid: 501, procs: map[int]fakeProc{}}
}

func (f *fakeRunner) LaunchctlLoaded(_ context.Context, label string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loaded[label], nil
}

func (f *fakeRunner) LaunchctlBootstrap(_ context.Context, domain, plist string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bootstraps = append(f.bootstraps, domain+" "+plist)
	if f.bootstrapErr != nil {
		return f.bootstrapErr
	}
	f.loaded[LaunchAgentLabel] = true
	return nil
}

func (f *fakeRunner) LaunchctlBootout(_ context.Context, domain, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bootouts = append(f.bootouts, domain+"/"+label)
	if f.bootoutErr != nil {
		return f.bootoutErr
	}
	delete(f.loaded, label)
	return nil
}

func (f *fakeRunner) PS(_ context.Context, pid int) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.procs[pid]
	if !ok {
		return "", "", fmt.Errorf("no such process %d", pid)
	}
	return p.state, p.command, nil
}

func (f *fakeRunner) Kill(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills = append(f.kills, fmt.Sprintf("%d:%d", pid, sig))
	if _, ok := f.procs[pid]; !ok {
		return errors.New("no such process")
	}
	if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
		delete(f.procs, pid)
	}
	return nil
}

func (f *fakeRunner) UID() int { return f.uid }

// ---- helpers -------------------------------------------------------------

// freePort returns a loopback port that is currently free.
func freePort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()
	addr, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected addr type %T", lis.Addr())
	}
	return addr.Port
}

// holdPort occupies a loopback port until the test ends.
func holdPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	addr, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected addr type %T", lis.Addr())
	}
	return addr.Port
}

// testEnv builds an Env rooted in temp dirs with fast poll timings. Ports
// default to free ones; override fields before calling the verb under test.
func testEnv(t *testing.T, goos string) *Env {
	t.Helper()
	bbHome := t.TempDir()
	home := t.TempDir()
	return &Env{
		BBHome:           bbHome,
		ExtensionDir:     filepath.Join(home, "Browser-Bridge"),
		HomeDir:          home,
		GOOS:             goos,
		WSPort:           freePort(t),
		LocalPort:        freePort(t),
		MCPPort:          freePort(t),
		WSHost:           "127.0.0.1",
		LocalHost:        "127.0.0.1",
		MCPHost:          "127.0.0.1",
		UpdateOrg:        "dkisser",
		UpdateRepo:       "browser-bridge",
		pollInterval:     10 * time.Millisecond,
		bindWaitAttempts: 500,
		termWaitAttempts: 100,
	}
}

// writeFakeCore installs an executable at $BB_HOME/bin/bridge-core.
func writeFakeCore(t *testing.T, e *Env, script string) {
	t.Helper()
	path := e.CoreBin()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writePidFile(t *testing.T, e *Env, pid int) {
	t.Helper()
	if err := os.MkdirAll(e.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.PidFile(), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSupervisorPidFile(t *testing.T, e *Env, pid int) {
	t.Helper()
	if err := os.MkdirAll(e.RunDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.SupervisorPidFile(), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---- up (darwin, launchd path) --------------------------------------------

func TestUpMissingBinary(t *testing.T) {
	e := testEnv(t, "darwin")
	r := newFakeRunner()
	var out bytes.Buffer
	err := Up(context.Background(), e, r, false, &out)
	if !IsCode(err, "BB-E002") {
		t.Fatalf("err = %v, want BB-E002", err)
	}
}

func TestUpDarwinBootstrapsStagingPlistWhenAutoStartDisabled(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(r.bootstraps) != 1 {
		t.Fatalf("bootstraps = %v, want 1 call", r.bootstraps)
	}
	want := "gui/501 " + e.StagingPlist()
	if r.bootstraps[0] != want {
		t.Errorf("bootstrap = %q, want %q", r.bootstraps[0], want)
	}
	if !strings.Contains(out.String(), "bridge service up: supervisor started (label com.browser-bridge.bridge)") {
		t.Errorf("out = %q", out.String())
	}
	plist, err := os.ReadFile(e.StagingPlist())
	if err != nil {
		t.Fatalf("staging plist: %v", err)
	}
	if !strings.Contains(string(plist), e.BBHome+"/bin/bridge") {
		t.Errorf("staging plist missing rendered BB_HOME:\n%s", plist)
	}
	if !strings.Contains(string(plist), fmt.Sprintf("<string>%d</string>", e.WSPort)) {
		t.Errorf("staging plist missing rendered WS port:\n%s", plist)
	}
	if _, err := os.Stat(e.LaunchAgentPlist()); !os.IsNotExist(err) {
		t.Errorf("login plist should not exist when auto-start is disabled")
	}
}

func TestUpDarwinBootstrapsLoginPlistWhenEnabled(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	// Pretend `service enable` ran earlier.
	if err := os.MkdirAll(filepath.Dir(e.LaunchAgentPlist()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.LaunchAgentPlist(), []byte("STALE-PLIST-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up: %v", err)
	}
	want := "gui/501 " + e.LaunchAgentPlist()
	if len(r.bootstraps) != 1 || r.bootstraps[0] != want {
		t.Errorf("bootstraps = %v, want [%q]", r.bootstraps, want)
	}
	plist, err := os.ReadFile(e.LaunchAgentPlist())
	if err != nil {
		t.Fatalf("login plist: %v", err)
	}
	if strings.Contains(string(plist), "STALE-PLIST-CONTENT") || !strings.Contains(string(plist), `<plist version="1.0">`) {
		t.Errorf("login plist was not re-rendered:\n%s", plist)
	}
}

func TestUpDarwinAlreadyRunning(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	r := newFakeRunner()
	r.loaded[LaunchAgentLabel] = true
	r.procs[4242] = fakeProc{state: "Ss", command: e.BBHome + "/bin/bridge service up --foreground"}
	writeSupervisorPidFile(t, e, 4242)
	var out bytes.Buffer
	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !strings.Contains(out.String(), "already running (supervised by launchd, label com.browser-bridge.bridge)") {
		t.Errorf("out = %q", out.String())
	}
	if len(r.bootstraps) != 0 || len(r.bootouts) != 0 {
		t.Errorf("bootstraps=%v bootouts=%v, want none", r.bootstraps, r.bootouts)
	}
}

func TestUpDarwinReplacesStaleJob(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	r := newFakeRunner()
	r.loaded[LaunchAgentLabel] = true // loaded, but no supervisor pidfile
	var out bytes.Buffer
	if err := Up(context.Background(), e, r, false, &out); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(r.bootouts) != 1 || r.bootouts[0] != "gui/501/"+LaunchAgentLabel {
		t.Errorf("bootouts = %v", r.bootouts)
	}
	if !strings.Contains(out.String(), "replaced stale LaunchAgent job") {
		t.Errorf("out = %q", out.String())
	}
	if len(r.bootstraps) != 1 {
		t.Errorf("bootstraps = %v, want 1", r.bootstraps)
	}
}

func TestUpDarwinForeignPortRefused(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	e.WSPort = holdPort(t) // something else occupies the control plane
	r := newFakeRunner()
	var out bytes.Buffer
	err := Up(context.Background(), e, r, false, &out)
	if !IsCode(err, "BB-E010") {
		t.Fatalf("err = %v, want BB-E010", err)
	}
	if !strings.Contains(err.Error(), "is occupied by something other than a supervised bridge instance") {
		t.Errorf("err = %v", err)
	}
	if len(r.bootstraps) != 0 {
		t.Errorf("bootstraps = %v, want none", r.bootstraps)
	}
}

func TestUpDarwinBootstrapFailure(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	r := newFakeRunner()
	r.bootstrapErr = errors.New("simulated launchd error")
	var out bytes.Buffer
	err := Up(context.Background(), e, r, false, &out)
	if !IsCode(err, "BB-E302") {
		t.Fatalf("err = %v, want BB-E302", err)
	}
}

// ---- down -----------------------------------------------------------------

func TestDownLinuxAlreadyStopped(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Down(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if out.String() != "bridge-core already stopped\n" {
		t.Errorf("out = %q", out.String())
	}
}

func TestDownLinuxStopsService(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	r.procs[777] = fakeProc{state: "S", command: e.BBHome + "/bin/bridge-core"}
	writePidFile(t, e, 777)
	var out bytes.Buffer
	if err := Down(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if _, ok := r.procs[777]; ok {
		t.Errorf("pid 777 still alive after down")
	}
	if len(r.kills) == 0 || !strings.HasPrefix(r.kills[0], "777:") {
		t.Errorf("kills = %v", r.kills)
	}
	if _, err := os.Stat(e.PidFile()); !os.IsNotExist(err) {
		t.Errorf("pidfile still present")
	}
	if !strings.Contains(out.String(), "bridge-core stopped") {
		t.Errorf("out = %q", out.String())
	}
}

func TestDownDarwinBootoutThenSweep(t *testing.T) {
	e := testEnv(t, "darwin")
	r := newFakeRunner()
	r.loaded[LaunchAgentLabel] = true
	r.procs[4242] = fakeProc{state: "Ss", command: "bridge service up --foreground"}
	r.procs[777] = fakeProc{state: "S", command: e.BBHome + "/bin/bridge-core"}
	writeSupervisorPidFile(t, e, 4242)
	writePidFile(t, e, 777)
	var out bytes.Buffer
	if err := Down(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if len(r.bootouts) != 1 {
		t.Errorf("bootouts = %v, want 1", r.bootouts)
	}
	if !strings.Contains(out.String(), "supervisor stopped") {
		t.Errorf("out = %q", out.String())
	}
	// bootout kills the supervisor in the fake? No — Down only bootouts; the
	// supervisor is expected to stop its child. Mark supervisor dead so the
	// sweep proceeds... the fake bootout does not kill pids, so the sweep
	// below relies on the pidfile stop.
	if _, err := os.Stat(e.SupervisorPidFile()); !os.IsNotExist(err) {
		t.Errorf("supervisor pidfile still present")
	}
}

func TestDownDarwinBootoutFailure(t *testing.T) {
	e := testEnv(t, "darwin")
	r := newFakeRunner()
	r.loaded[LaunchAgentLabel] = true
	r.bootoutErr = errors.New("simulated bootout failure")
	var out bytes.Buffer
	err := Down(context.Background(), e, r, &out)
	if !IsCode(err, "BB-E304") {
		t.Fatalf("err = %v, want BB-E304", err)
	}
}

func TestDownStalePidFile(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner() // pid 999 not in procs → dead
	writePidFile(t, e, 999)
	var out bytes.Buffer
	if err := Down(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if !strings.Contains(out.String(), "pid 999 not running, cleaning up") {
		t.Errorf("out = %q", out.String())
	}
	if _, err := os.Stat(e.PidFile()); !os.IsNotExist(err) {
		t.Errorf("pidfile still present")
	}
}

// ---- status ---------------------------------------------------------------

func TestStatus(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		running     bool
		enabled     bool
		loaded      bool
		wantRunning bool
		wantLines   []string
	}{
		{
			name: "darwin running enabled loaded", goos: "darwin",
			running: true, enabled: true, loaded: true, wantRunning: true,
			wantLines: []string{"bridge-core:  running  (pid 777)", "auto-start:  enabled, agent loaded (label com.browser-bridge.bridge)"},
		},
		{
			name: "darwin stopped enabled not loaded", goos: "darwin",
			running: false, enabled: true, loaded: false, wantRunning: false,
			wantLines: []string{"bridge-core:  stopped", "auto-start:  enabled (starts at login; not running now)"},
		},
		{
			name: "darwin stopped disabled", goos: "darwin",
			running: false, enabled: false, loaded: false, wantRunning: false,
			wantLines: []string{"bridge-core:  stopped", "auto-start:  disabled"},
		},
		{
			name: "linux running", goos: "linux",
			running: true, wantRunning: true,
			wantLines: []string{"bridge-core:  running  (pid 777)", "auto-start:  unsupported on this platform (macOS only)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEnv(t, tt.goos)
			r := newFakeRunner()
			if tt.running {
				r.procs[777] = fakeProc{state: "S", command: e.BBHome + "/bin/bridge-core"}
				writePidFile(t, e, 777)
			}
			if tt.enabled {
				if err := os.MkdirAll(filepath.Dir(e.LaunchAgentPlist()), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(e.LaunchAgentPlist(), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			r.loaded[LaunchAgentLabel] = tt.loaded
			var out bytes.Buffer
			running, err := Status(context.Background(), e, r, &out)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if running != tt.wantRunning {
				t.Errorf("running = %v, want %v", running, tt.wantRunning)
			}
			lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			if len(lines) != len(tt.wantLines) {
				t.Fatalf("lines = %v, want %v", lines, tt.wantLines)
			}
			for i := range lines {
				if lines[i] != tt.wantLines[i] {
					t.Errorf("line %d = %q, want %q", i, lines[i], tt.wantLines[i])
				}
			}
		})
	}
}

// ---- enable / disable -------------------------------------------------------

func TestEnableDarwin(t *testing.T) {
	e := testEnv(t, "darwin")
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Enable(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err := os.Stat(e.LaunchAgentPlist()); err != nil {
		t.Fatalf("login plist: %v", err)
	}
	if !strings.Contains(out.String(), "Login auto-start enabled.") {
		t.Errorf("out = %q", out.String())
	}
	if !strings.Contains(out.String(), "Services are not running yet ('bridge service up' starts them now).") {
		t.Errorf("out = %q", out.String())
	}
}

func TestEnableLinuxUnsupported(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	var out bytes.Buffer
	err := Enable(context.Background(), e, r, &out)
	if !IsCode(err, "BB-E300") {
		t.Fatalf("err = %v, want BB-E300", err)
	}
}

func TestDisableDarwin(t *testing.T) {
	e := testEnv(t, "darwin")
	r := newFakeRunner()
	for _, p := range []string{e.LaunchAgentPlist(), e.StagingPlist()} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := Disable(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	for _, p := range []string{e.LaunchAgentPlist(), e.StagingPlist()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present", p)
		}
	}
	if !strings.Contains(out.String(), "Login auto-start disabled. Running services are not stopped ('bridge service down' stops them).") {
		t.Errorf("out = %q", out.String())
	}
}

func TestDisableLinux(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Disable(context.Background(), e, r, &out); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if out.String() != "Login auto-start: not supported on this platform (macOS only).\n" {
		t.Errorf("out = %q", out.String())
	}
}

// ---- classify ---------------------------------------------------------------

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, e *Env, r *fakeRunner)
		wantState string
	}{
		{
			name: "ours: pidfile live and bridge-core",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				r.procs[777] = fakeProc{state: "S", command: e.BBHome + "/bin/bridge-core"}
				writePidFile(t, e, 777)
			},
			wantState: "ours",
		},
		{
			name: "foreign: pidfile live but not ours and port held",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				r.procs[778] = fakeProc{state: "S", command: "python3 -m http.server"}
				writePidFile(t, e, 778)
				e.WSPort = holdPort(t)
			},
			wantState: "foreign",
		},
		{
			name: "free: pidfile live but not ours and ports free (stale removed)",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				r.procs[779] = fakeProc{state: "S", command: "sleep 60"}
				writePidFile(t, e, 779)
			},
			wantState: "free",
		},
		{
			name: "free: dead pidfile removed",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				writePidFile(t, e, 999)
			},
			wantState: "free",
		},
		{
			name: "foreign: port held with no pidfile",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				e.MCPPort = holdPort(t)
			},
			wantState: "foreign",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEnv(t, "linux")
			r := newFakeRunner()
			tt.setup(t, e, r)
			state, _ := e.classify(context.Background(), r)
			if state != tt.wantState {
				t.Errorf("state = %q, want %q", state, tt.wantState)
			}
		})
	}
}

// ---- doctor -----------------------------------------------------------------

func TestDoctor(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, e *Env, r *fakeRunner)
		wantOK   bool
		wantLine []string
	}{
		{
			name: "all green",
			setup: func(t *testing.T, e *Env, r *fakeRunner) {
				writeFakeCore(t, e, "#!/usr/bin/env bash\n")
				if err := os.WriteFile(e.BridgeBin(), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(e.BBHome, "extension"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.BBHome, "extension", "manifest.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
					t.Fatal(err)
				}
				r.procs[777] = fakeProc{state: "S", command: e.BBHome + "/bin/bridge-core"}
				writePidFile(t, e, 777)
			},
			wantOK: true,
			wantLine: []string{
				"[OK] bridge-core binary present",
				"[OK] bridge binary present",
				"[OK] extension/manifest.json valid",
				"[OK] bridge-core running",
			},
		},
		{
			name:   "empty install fails",
			setup:  func(t *testing.T, e *Env, r *fakeRunner) {},
			wantOK: false,
			wantLine: []string{
				"[FAIL] bridge-core binary missing",
				"[FAIL] bridge binary missing",
				"[FAIL] extension/manifest.json missing or invalid",
				"[WARN] bridge-core not running (run 'bridge service up')",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := testEnv(t, "linux")
			r := newFakeRunner()
			tt.setup(t, e, r)
			var out bytes.Buffer
			ok, err := Doctor(context.Background(), e, r, &out)
			if err != nil {
				t.Fatalf("Doctor: %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (out %q)", ok, tt.wantOK, out.String())
			}
			for _, line := range tt.wantLine {
				if !strings.Contains(out.String(), line) {
					t.Errorf("out missing %q:\n%s", line, out.String())
				}
			}
		})
	}
}

// ---- version ------------------------------------------------------------------

func TestVersionOutput(t *testing.T) {
	e := testEnv(t, "linux")
	if err := os.WriteFile(filepath.Join(e.BBHome, "version"), []byte("v1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LATEST_VERSION", "v9.9.9")
	var out bytes.Buffer
	if err := Version(e, &out); err != nil {
		t.Fatalf("Version: %v", err)
	}
	if out.String() != "installed: v1.2.3\nlatest:    v9.9.9\n" {
		t.Errorf("out = %q", out.String())
	}
}

func TestVersionUnknown(t *testing.T) {
	e := testEnv(t, "linux")
	t.Setenv("LATEST_VERSION", "")
	var out bytes.Buffer
	if err := Version(e, &out); err != nil {
		t.Fatalf("Version: %v", err)
	}
	if out.String() != "installed: unknown\nlatest:    unknown\n" {
		t.Errorf("out = %q", out.String())
	}
}

// ---- update ---------------------------------------------------------------------

func TestUpdateScriptURL(t *testing.T) {
	tests := []struct {
		org, target, want string
	}{
		{"dkisser", "latest", "https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh"},
		{"dkisser", "v1.2.3", "https://github.com/dkisser/browser-bridge/releases/download/v1.2.3/install.sh"},
		{"127.0.0.1:8123", "v1.2.3", "http://127.0.0.1:8123/install.sh"},
	}
	for _, tt := range tests {
		if got := UpdateScriptURL(tt.org, "browser-bridge", tt.target); got != tt.want {
			t.Errorf("UpdateScriptURL(%q, %q) = %q, want %q", tt.org, tt.target, got, tt.want)
		}
	}
}

func TestUpdateRunsInstallerWithPinnedVersion(t *testing.T) {
	e := testEnv(t, "linux")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "#!/usr/bin/env bash\nprintf '%s' \"$BB_VERSION\" > \"$BB_HOME/update.marker\"\n")
	}))
	t.Cleanup(srv.Close)
	url := srv.URL + "/install.sh"

	var out bytes.Buffer
	if err := Update(context.Background(), e, newFakeRunner(), "v9.9.9", url, &out); err != nil {
		t.Fatalf("Update: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(e.BBHome, "update.marker"))
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if string(raw) != "v9.9.9" {
		t.Errorf("marker = %q, want v9.9.9", raw)
	}
	if !strings.Contains(out.String(), "Updating to v9.9.9") {
		t.Errorf("out = %q", out.String())
	}
}

func TestUpdateHTTPFailure(t *testing.T) {
	e := testEnv(t, "linux")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	err := Update(context.Background(), e, newFakeRunner(), "v9.9.9", srv.URL+"/install.sh", &out)
	if !IsCode(err, "BB-E103") {
		t.Fatalf("err = %v, want BB-E103", err)
	}
}

// ---- uninstall -----------------------------------------------------------------

func TestUninstallAbortsWithoutYes(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	var out bytes.Buffer
	if err := Uninstall(context.Background(), e, r, false, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Errorf("out = %q", out.String())
	}
	if _, err := os.Stat(e.BBHome); err != nil {
		t.Errorf("BB_HOME removed despite abort: %v", err)
	}
}

func TestUninstallYes(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	// Plant an extension dir and a dangling ~/.local/bin/bridge symlink.
	if err := os.MkdirAll(e.ExtensionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(e.HomeDir, ".local", "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "bridge")
	if err := os.Symlink(e.BridgeBin(), link); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Uninstall(context.Background(), e, r, true, strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	for _, p := range []string{e.BBHome, e.ExtensionDir, link} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present", p)
		}
	}
	for _, want := range []string{"Removed " + e.BBHome, "Removed " + e.ExtensionDir, "Removed stale symlink " + link} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out missing %q:\n%s", want, out.String())
		}
	}
}

// ---- autostart (deprecated alias) -------------------------------------------

func TestAutostartInvalidAction(t *testing.T) {
	e := testEnv(t, "linux")
	r := newFakeRunner()
	var out, warn bytes.Buffer
	err := Autostart(context.Background(), e, r, "bogus", &out, &warn)
	if !IsCode(err, "BB-E303") {
		t.Fatalf("err = %v, want BB-E303", err)
	}
	if !strings.Contains(warn.String(), "Deprecation warning: 'bridge autostart' is deprecated") {
		t.Errorf("warn = %q", warn.String())
	}
}

func TestAutostartOn(t *testing.T) {
	e := testEnv(t, "darwin")
	writeFakeCore(t, e, "#!/usr/bin/env bash\nsleep 60\n")
	r := newFakeRunner()
	var out, warn bytes.Buffer
	if err := Autostart(context.Background(), e, r, "on", &out, &warn); err != nil {
		t.Fatalf("Autostart on: %v", err)
	}
	if _, err := os.Stat(e.LaunchAgentPlist()); err != nil {
		t.Errorf("login plist missing: %v", err)
	}
	if len(r.bootstraps) != 1 {
		t.Errorf("bootstraps = %v, want 1 (service up)", r.bootstraps)
	}
}

// ---- plist rendering ---------------------------------------------------------

func TestRenderLaunchAgentPlistEscapes(t *testing.T) {
	e := testEnv(t, "darwin")
	e.BBHome = "/tmp/bb & <home>"
	rendered := e.renderLaunchAgentPlist()
	if strings.Contains(rendered, "/tmp/bb & <home>") {
		t.Errorf("BB_HOME not XML-escaped:\n%s", rendered)
	}
	if !strings.Contains(rendered, "/tmp/bb &amp; &lt;home&gt;/bin/bridge") {
		t.Errorf("escaped BB_HOME missing:\n%s", rendered)
	}
}
