package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ConfigFromEnv reads the BRIDGE_* variables exactly as src/index.ts did
// (moved from cmd/bridge-core when the daemon became `bridge serve`,
// ADR-0013). Zero-value fields fall back to the package defaults in Run.
func ConfigFromEnv() (Config, error) {
	var cfg Config

	// BRIDGE_API_KEYS: comma-separated, trimmed, empties dropped; empty →
	// no auth (NoopAuthProvider).
	if raw := os.Getenv("BRIDGE_API_KEYS"); raw != "" {
		for _, key := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				cfg.APIKeys = append(cfg.APIKeys, trimmed)
			}
		}
	}

	port, err := parsePort("BRIDGE_WS_PORT")
	if err != nil {
		return cfg, err
	}
	cfg.InboundPort = port
	cfg.InboundHostname = os.Getenv("BRIDGE_WS_HOSTNAME")

	localPort, err := parsePort("BRIDGE_LOCAL_PORT")
	if err != nil {
		return cfg, err
	}
	cfg.BrowserPort = localPort
	cfg.BrowserHostname = os.Getenv("BRIDGE_LOCAL_HOSTNAME")

	mcpPort, err := parsePort("BRIDGE_MCP_PORT")
	if err != nil {
		return cfg, err
	}
	cfg.MCPPort = mcpPort
	cfg.MCPHostname = os.Getenv("BRIDGE_MCP_HOSTNAME")

	if raw := os.Getenv("BRIDGE_MCP_TIMEOUT_MS"); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil {
			//nolint:staticcheck // ST1005: message mirrors src/index.ts.
			return cfg, fmt.Errorf("Invalid BRIDGE_MCP_TIMEOUT_MS: %s", raw)
		}
		cfg.MCPTimeout = time.Duration(ms) * time.Millisecond
	}

	return cfg, nil
}

// parsePort mirrors the TS `Number(env)` + NaN guard. index.ts throws on
// NaN for BRIDGE_LOCAL_PORT / BRIDGE_MCP_PORT but not BRIDGE_WS_PORT; Go
// rejects a non-numeric value for all three (the only difference is which
// process error message an invalid BRIDGE_WS_PORT produces).
func parsePort(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		//nolint:staticcheck // ST1005: message mirrors src/index.ts.
		return 0, fmt.Errorf("Invalid %s: %s", name, raw)
	}
	return port, nil
}
