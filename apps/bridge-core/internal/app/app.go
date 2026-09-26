// Package app assembles the bridge-core control plane from its parts,
// mirroring the wiring in src/index.ts. cmd/bridge-core parses the BRIDGE_*
// environment into a Config; the e2e spike test drives Run directly.
package app

import (
	"context"
	"log"
	"time"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/core"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/http"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/ws"
)

// Default ports: WEBSOCKET_PORT / LOCAL_WS_PORT in packages/shared/src/
// constants.ts, plus the MCP default from src/index.ts.
const (
	DefaultInboundPort = 3001
	DefaultBrowserPort = 3002
	DefaultMCPPort     = 3003
	DefaultMCPTimeout  = 10 * time.Second
	DefaultInboundHost = "127.0.0.1"
	DefaultBrowserHost = "127.0.0.1"
	DefaultMCPHost     = "127.0.0.1"
)

type Config struct {
	InboundPort     int
	InboundHostname string
	BrowserPort     int
	BrowserHostname string
	MCPPort         int
	MCPHostname     string
	MCPTimeout      time.Duration
	APIKeys         []string
	Version         string
	// BufferTimeout overrides the 5s command buffer (0 keeps the default).
	// Exists for the e2e spike test.
	BufferTimeout time.Duration
	Logger        *log.Logger
}

func (c *Config) setDefaults() {
	if c.InboundPort == 0 {
		c.InboundPort = DefaultInboundPort
	}
	if c.InboundHostname == "" {
		c.InboundHostname = DefaultInboundHost
	}
	if c.BrowserPort == 0 {
		c.BrowserPort = DefaultBrowserPort
	}
	if c.BrowserHostname == "" {
		c.BrowserHostname = DefaultBrowserHost
	}
	if c.MCPPort == 0 {
		c.MCPPort = DefaultMCPPort
	}
	if c.MCPHostname == "" {
		c.MCPHostname = DefaultMCPHost
	}
	if c.MCPTimeout == 0 {
		c.MCPTimeout = DefaultMCPTimeout
	}
	if c.Logger == nil {
		c.Logger = log.Default()
	}
}

// Run assembles the control plane and blocks until ctx is canceled, then
// shuts the servers down.
func Run(ctx context.Context, cfg Config) error {
	cfg.setDefaults()
	logger := cfg.Logger

	stateOpts := []core.StateOption{}
	if cfg.BufferTimeout != 0 {
		stateOpts = append(stateOpts, core.WithBufferTimeout(cfg.BufferTimeout))
	}
	st, err := core.NewStateManager(stateOpts...)
	if err != nil {
		return err
	}
	logger.Printf("Browser ID: %s", st.BrowserID())

	reg := core.NewRegistry()

	pm := core.NewPairingManager(st.ExtensionTokenHash, st.SetExtensionTokenHash)

	// BrowserServer needs a Router reference; the getter resolves it after
	// the Router is constructed (index.ts uses the same closure trick).
	var rt *core.Router
	browser := ws.NewBrowser(ws.BrowserOptions{
		Port:      cfg.BrowserPort,
		Hostname:  cfg.BrowserHostname,
		GetRouter: func() ws.BrowserRouter { return rt },
		Pairing:   pm,
		Logger:    logger,
	})

	rt = core.NewRouter(st, browser, reg, logger)

	in := ws.NewInbound(ws.InboundOptions{
		Port:     cfg.InboundPort,
		Hostname: cfg.InboundHostname,
		Auth:     authorizer(cfg.APIKeys),
		Router:   rt,
		Registry: reg,
		Logger:   logger,
	})

	mcpSrv := http.NewMCP(http.MCPOptions{
		Port:           cfg.MCPPort,
		Hostname:       cfg.MCPHostname,
		Router:         rt,
		Registry:       reg,
		DefaultTimeout: cfg.MCPTimeout,
		Version:        cfg.Version,
		Logger:         logger,
	})

	// Start order matches index.ts: browser, inbound, MCP.
	if err := browser.Start(ctx); err != nil {
		return err
	}
	if err := in.Start(ctx); err != nil {
		return err
	}
	if err := mcpSrv.Start(ctx); err != nil {
		return err
	}

	logger.Printf("bridge-core ready: inbound=%d, browser=%d, mcp=%d", cfg.InboundPort, cfg.BrowserPort, cfg.MCPPort)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := in.Shutdown(shutdownCtx); err != nil {
		logger.Printf("inbound shutdown: %v", err)
	}
	if err := browser.Shutdown(shutdownCtx); err != nil {
		logger.Printf("browser shutdown: %v", err)
	}
	// The MCP server follows ctx via its own watcher (no WS connections of
	// its own to close).
	return nil
}

// authorizer picks the auth provider the way index.ts does: no BRIDGE_API_KEYS
// → NoopAuthProvider, else ApiKeyAuthProvider over the trimmed key list.
func authorizer(apiKeys []string) ws.Authorizer {
	if len(apiKeys) == 0 {
		return ws.NoopAuthorizer{}
	}
	return ws.NewAPIKeyAuthorizer(apiKeys)
}
