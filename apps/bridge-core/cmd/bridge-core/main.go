// Command bridge-core is the Browser Bridge control plane: it routes
// commands from the inbound adapters (CLI, MCP) to the browser connection.
// Go rewrite of the deleted Bun implementation — see
// docs/adr/0012-control-plane-and-cli-in-go.md.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/app"
)

// version is the binary version reported by the MCP server (the TS build
// read it from package.json). Overridable at link time:
// -ldflags "-X main.version=1.2.3".
var version = "0.3.2"

func main() {
	cfg, err := configFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg.Version = version
	cfg.Logger = log.Default()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start bridge-core: %v\n", err)
		os.Exit(1)
	}
}

// configFromEnv reads the BRIDGE_* variables exactly as src/index.ts does.
func configFromEnv() (app.Config, error) {
	var cfg app.Config

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
		return 0, fmt.Errorf("Invalid %s: %s", name, raw)
	}
	return port, nil
}
