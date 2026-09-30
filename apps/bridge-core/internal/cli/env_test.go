package cli

import (
	"os"
	"strings"
	"testing"

	"browser-bridge/internal/app"
)

// The supervisor hands the daemon its hostnames through BRIDGE_*_HOSTNAME, so
// whatever this package resolves them to is what the process actually binds.
// It used to be a second copy of "127.0.0.1", spelled out inline, while
// app.DefaultInboundHost / DefaultBrowserHost / DefaultMCPHost sat next to it
// looking authoritative — and were dead in the path that runs, because
// childEnv always exports the variable and setDefaults therefore never
// reached those fields.
//
// A change to a constant would have moved nothing, with nothing to notice.
// The assertion is that the two agree, so the copy cannot quietly come back.
func TestSupervisorResolvesTheAppPackagesHostDefaults(t *testing.T) {
	// The env vars must be absent so orEnv takes its default branch — that
	// branch is the one that used to hold the literal.
	for _, name := range []string{
		"BRIDGE_WS_HOSTNAME",
		"BRIDGE_LOCAL_HOSTNAME",
		"BRIDGE_MCP_HOSTNAME",
	} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}

	e, err := EnvFromOSEnv()
	if err != nil {
		t.Fatalf("EnvFromOSEnv: %v", err)
	}

	for _, tc := range []struct {
		field string
		got   string
		want  string
	}{
		{"WSHost", e.WSHost, app.DefaultInboundHost},
		{"LocalHost", e.LocalHost, app.DefaultBrowserHost},
		{"MCPHost", e.MCPHost, app.DefaultMCPHost},
	} {
		if tc.got != tc.want {
			t.Errorf("Env.%s = %q, want app's default %q; the supervisor "+
				"decides what the daemon binds, so the two must be one value",
				tc.field, tc.got, tc.want)
		}
	}
}

// The other half: an explicit override still wins, and it is the one that
// reaches the daemon. Without this the test above would pass even if orEnv
// ignored its environment entirely.
func TestSupervisorHostnameOverrideStillReachesTheChild(t *testing.T) {
	t.Setenv("BRIDGE_LOCAL_HOSTNAME", "192.168.1.9")

	e, err := EnvFromOSEnv()
	if err != nil {
		t.Fatalf("EnvFromOSEnv: %v", err)
	}
	if e.LocalHost != "192.168.1.9" {
		t.Fatalf("LocalHost = %q, want the override", e.LocalHost)
	}

	child := strings.Join(e.childEnv(), "\n")
	if !strings.Contains(child, "BRIDGE_LOCAL_HOSTNAME=192.168.1.9") {
		t.Error("childEnv did not export the override; the daemon would bind " +
			"the default while the operator asked for something else")
	}
}

// Confirms the resolution actually lands in the daemon's Config, closing the
// loop from the CLI's literal to the listener. app.ConfigFromEnv reads the
// environment, so it is driven the same way the supervisor drives it.
func TestResolvedHostnameReachesTheDaemonConfig(t *testing.T) {
	t.Setenv("BRIDGE_LOCAL_HOSTNAME", "127.0.0.1")
	t.Setenv("BRIDGE_WS_HOSTNAME", "127.0.0.1")
	t.Setenv("BRIDGE_MCP_HOSTNAME", "127.0.0.1")

	e, err := EnvFromOSEnv()
	if err != nil {
		t.Fatalf("EnvFromOSEnv: %v", err)
	}
	for _, kv := range e.childEnv() {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if setErr := os.Setenv(name, value); setErr != nil {
			t.Fatalf("set %s: %v", name, setErr)
		}
	}
	t.Cleanup(func() { _ = os.Unsetenv("BRIDGE_LOCAL_HOSTNAME") })

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BrowserHostname != e.LocalHost {
		t.Errorf("Config.BrowserHostname = %q, want %q from the supervisor",
			cfg.BrowserHostname, e.LocalHost)
	}
}
