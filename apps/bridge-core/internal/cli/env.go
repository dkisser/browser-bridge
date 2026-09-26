package cli

// The service lifecycle implementation (env.go, commands.go, service.go,
// supervise.go, logs.go, plist.go, runner.go) is the Go port of
// install/bridge.sh.tmpl. Semantics, messages, and exit codes follow the
// bash implementation; the macOS supervisor design is documented in
// docs/adr/0005 (launchd KeepAlive + foreground supervisor).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Default ports: packages/shared/src/constants.ts (WEBSOCKET_PORT,
// LOCAL_WS_PORT, MCP Streamable HTTP).
const (
	defaultWSPort    = 3001
	defaultLocalPort = 3002
	defaultMCPPort   = 3003

	// LaunchAgentLabel is the launchd job label (unchanged from the bash
	// implementation so existing installs are recognized).
	LaunchAgentLabel = "com.browser-bridge.bridge"

	serviceName = "bridge-core"
)

// Env carries the service-management configuration resolved from the process
// environment. Tests construct it directly; GOOS and the poll timings are
// fields so darwin-only behavior and slow timeouts are exercisable anywhere.
type Env struct {
	BBHome       string // $BB_HOME, default ~/.browser-bridge
	ExtensionDir string // $BB_EXTENSION_DIR, default ~/Browser-Bridge
	HomeDir      string
	GOOS         string // defaults to runtime.GOOS

	WSPort    int
	LocalPort int
	MCPPort   int
	WSHost    string
	LocalHost string
	MCPHost   string

	APIKeys string // BRIDGE_API_KEYS, passed through to bridge-core

	// UpdateOrg/UpdateRepo build the install.sh URL for `service update`
	// (ORG/REPO env; the host:port form switches to plain http for mocks).
	UpdateOrg  string
	UpdateRepo string

	// Poll timing for process liveness and port-bind waits. Defaults match
	// the bash implementation (100ms ticks, 5s bind budget, 3s SIGTERM
	// grace); tests shrink them.
	pollInterval     time.Duration
	bindWaitAttempts int
	termWaitAttempts int
}

// EnvFromOSEnv resolves the environment exactly like the bash script header.
func EnvFromOSEnv() (*Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	e := &Env{
		BBHome:           orEnv("BB_HOME", filepath.Join(home, ".browser-bridge")),
		ExtensionDir:     orEnv("BB_EXTENSION_DIR", filepath.Join(home, "Browser-Bridge")),
		HomeDir:          home,
		GOOS:             runtime.GOOS,
		WSHost:           orEnv("BRIDGE_WS_HOSTNAME", "127.0.0.1"),
		LocalHost:        orEnv("BRIDGE_LOCAL_HOSTNAME", "127.0.0.1"),
		MCPHost:          orEnv("BRIDGE_MCP_HOSTNAME", "127.0.0.1"),
		APIKeys:          os.Getenv("BRIDGE_API_KEYS"),
		UpdateOrg:        orEnv("ORG", "dkisser"),
		UpdateRepo:       orEnv("REPO", "browser-bridge"),
		pollInterval:     100 * time.Millisecond,
		bindWaitAttempts: 50,
		termWaitAttempts: 30,
	}
	for _, p := range []struct {
		name string
		dst  *int
		def  int
	}{
		{"BRIDGE_WS_PORT", &e.WSPort, defaultWSPort},
		{"BRIDGE_LOCAL_PORT", &e.LocalPort, defaultLocalPort},
		{"BRIDGE_MCP_PORT", &e.MCPPort, defaultMCPPort},
	} {
		raw := os.Getenv(p.name)
		if raw == "" {
			*p.dst = p.def
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", p.name, raw, err)
		}
		*p.dst = v
	}
	return e, nil
}

func orEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---- Well-known paths (all under BBHome except the login LaunchAgent) ----

// CoreBin is the supervised daemon.
func (e *Env) CoreBin() string { return filepath.Join(e.BBHome, "bin", "bridge-core") }

// BridgeBin is the CLI binary itself (as installed under BBHome).
func (e *Env) BridgeBin() string { return filepath.Join(e.BBHome, "bin", "bridge") }

// LogDir holds bridge-core.log and launchagent.log.
func (e *Env) LogDir() string { return filepath.Join(e.BBHome, "logs") }

// RunDir holds the pidfiles.
func (e *Env) RunDir() string { return filepath.Join(e.BBHome, "run") }

// LogFile is the bridge-core stdout/stderr log.
func (e *Env) LogFile() string { return filepath.Join(e.LogDir(), serviceName+".log") }

// PidFile records the bridge-core pid.
func (e *Env) PidFile() string { return filepath.Join(e.RunDir(), serviceName+".pid") }

// SupervisorPidFile records the foreground supervisor pid.
func (e *Env) SupervisorPidFile() string { return filepath.Join(e.RunDir(), "supervisor.pid") }

// LaunchAgentPlist is the login auto-start plist (macOS).
func (e *Env) LaunchAgentPlist() string {
	return filepath.Join(e.HomeDir, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

// StagingPlist is the plist `service up` bootstraps from when login
// auto-start is disabled.
func (e *Env) StagingPlist() string {
	return filepath.Join(e.BBHome, "launchagents", LaunchAgentLabel+".plist")
}

// Enabled reports whether login auto-start is on (plist present in
// ~/Library/LaunchAgents).
func (e *Env) Enabled() bool {
	_, err := os.Stat(e.LaunchAgentPlist())
	return err == nil
}

// childEnv is the environment bridge-core is spawned with: the inherited
// environment plus the resolved BB_HOME/ports/hostnames, so StateManager
// picks the same prefix install.sh used.
func (e *Env) childEnv() []string {
	return append(os.Environ(),
		"BB_HOME="+e.BBHome,
		"BRIDGE_WS_PORT="+strconv.Itoa(e.WSPort),
		"BRIDGE_WS_HOSTNAME="+e.WSHost,
		"BRIDGE_LOCAL_PORT="+strconv.Itoa(e.LocalPort),
		"BRIDGE_LOCAL_HOSTNAME="+e.LocalHost,
		"BRIDGE_MCP_PORT="+strconv.Itoa(e.MCPPort),
		"BRIDGE_MCP_HOSTNAME="+e.MCPHost,
		"BRIDGE_API_KEYS="+e.APIKeys,
	)
}
