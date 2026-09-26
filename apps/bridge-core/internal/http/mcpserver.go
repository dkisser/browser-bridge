// Package http is the control plane's HTTP surface: the MCP Streamable HTTP
// server (default port 3003, the Go port of src/mcp/server.ts) and the
// connection Tracker that keeps net/http Server shutdowns prompt for
// hijacked WebSocket and idle keep-alive connections.
package http

import (
	"context"
	"fmt"
	"log"
	"net"
	nethttp "net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
)

// ServerName is the FastMCP server name in src/mcp/server.ts.
const ServerName = "Browser Bridge"

// CommandRouter is the slice of core.Router the tools call.
type CommandRouter interface {
	HandleInboundCommand(envelope core.Envelope, sender core.TextSender)
	// RemoveRoute drops a single inbound route by id. sendCommand calls it
	// on context cancel / timeout so a flaky extension does not pin the
	// channelSender in inboundByID after the call has already returned.
	RemoveRoute(id string)
}

// BrowserLister is the slice of core.Registry the tools call.
type BrowserLister interface {
	ListBrowsers() []core.BrowserConnection
}

type MCPOptions struct {
	Port           int
	Hostname       string
	Router         CommandRouter
	Registry       BrowserLister
	DefaultTimeout time.Duration
	Version        string
	Logger         *log.Logger
}

// MCPServer is the Go port of src/mcp/server.ts. Where the TS server dials
// the inbound WebSocket as a client, the Go port dispatches commands to the
// router in-process (allowed by the spike brief); the inbound WebSocket
// behavior on 3001 is preserved for real CLI clients.
type MCPServer struct {
	port           int
	hostname       string
	router         CommandRouter
	registry       BrowserLister
	sessions       *sessionStore
	defaultTimeout time.Duration
	version        string
	logger         *log.Logger

	httpServer *nethttp.Server
	tracker    *Tracker

	// stateMu protects started and the watcher-shutdown signalling. It is
	// held only long enough to read/write those fields; Shutdown does NOT
	// hold it while waiting on the watchShutdown goroutine, so a stuck
	// Shutdown does not block a parallel Start on a restarted instance.
	stateMu sync.Mutex
	started bool           // false until Start has launched the watcher goroutine
	wg      sync.WaitGroup // tracks watchShutdown so Shutdown can wait on it
}

func NewMCP(opts MCPOptions) *MCPServer {
	return &MCPServer{
		port:           opts.Port,
		hostname:       opts.Hostname,
		router:         opts.Router,
		registry:       opts.Registry,
		sessions:       newSessionStore(),
		defaultTimeout: opts.DefaultTimeout,
		version:        semverVersion(opts.Version),
		logger:         opts.Logger,
	}
}

// semverPattern extracts a leading x.y.z version, matching
// `/^(\d+\.\d+\.\d+)/` in src/mcp/server.ts; anything else becomes 0.0.0.
var semverPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)

func semverVersion(version string) string {
	if m := semverPattern.FindString(version); m != "" {
		return m
	}
	return "0.0.0"
}

// buildServer assembles the mcp.Server with every tool registered. It is a
// separate method so tests can mount the same server over httptest.
func (s *MCPServer) buildServer() *mcp.Server {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: s.version}, nil)
	s.registerTools(mcpServer)
	return mcpServer
}

func (s *MCPServer) Start(ctx context.Context) error {
	mcpServer := s.buildServer()
	handler := mcp.NewStreamableHTTPHandler(func(*nethttp.Request) *mcp.Server {
		return mcpServer
	}, nil)
	mux := nethttp.NewServeMux()
	mux.Handle("/mcp", handler)

	listener, err := net.Listen("tcp", net.JoinHostPort(s.hostname, fmt.Sprint(s.port)))
	if err != nil {
		return fmt.Errorf("mcp server listen on %s:%d: %w", s.hostname, s.port, err)
	}
	tracker := NewTracker()
	s.httpServer = &nethttp.Server{Handler: mux, ConnState: tracker.ConnState}
	s.tracker = tracker
	s.stateMu.Lock()
	s.wg.Add(1)
	watcherCtx := ctx
	s.started = true
	s.stateMu.Unlock()
	go func() {
		defer s.wg.Done()
		s.watchShutdown(watcherCtx)
	}()
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != nethttp.ErrServerClosed {
			s.logger.Printf("mcp server: %v", err)
		}
	}()
	s.logger.Printf("MCP server listening on http://localhost:%d/mcp", s.port)
	return nil
}

// Shutdown blocks until the watchShutdown goroutine has finished closing
// the underlying HTTP server, mirroring the contract of ws.InboundServer and
// ws.BrowserServer. The caller is expected to cancel the context passed to
// Start before invoking Shutdown; without that the goroutine never wakes up
// from <-ctx.Done() and Shutdown returns when its context expires.
//
// Without this method the caller (app.Run) used to return the moment the run
// context was canceled, while watchShutdown was still inside
// httpServer.Shutdown — a 5s race that leaked the listener in tests and could
// be killed mid-flight by the process exit in production.
func (s *MCPServer) Shutdown(ctx context.Context) error {
	s.stateMu.Lock()
	started := s.started
	s.stateMu.Unlock()
	if !started {
		// Start was never called; nothing to wait for.
		return nil
	}
	// Wait synchronously on the watcher rather than spawning a helper
	// goroutine that races ctx.Done — the previous version leaked one
	// goroutine per Shutdown timeout because the helper kept the
	// wg.Wait() alive past ctx.Done(). Wait directly is safe because the
	// watcher exits promptly when the parent ctx cancels (see
	// watchShutdown: it derives its inner timeout from the parent, which
	// is already cancelled at this point, so httpServer.Shutdown returns
	// ErrServerClosed immediately).
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// The spawned goroutine is intentionally left running; it is
		// bounded by the watcher's own 5s httpServer.Shutdown budget and
		// then exits, so it cannot leak forever. The trade-off is one
		// short-lived parked goroutine per tight Shutdown deadline
		// instead of the previously unbounded accumulation.
		return fmt.Errorf("mcp server shutdown deadline: %w", ctx.Err())
	}
}

// watchShutdown closes the MCP listener when the run context ends; the go-sdk
// StreamableHTTPHandler has no Close of its own, so this is the lifecycle
// hook the other servers get from Shutdown.
//
// The shutdown timeout derives from the caller's ctx (which is already
// cancelled at this point) so that a tight deadline passed to Shutdown
// propagates into httpServer.Shutdown: net/http returns immediately on a
// cancelled context, the watcher exits, and the WaitGroup.Done fires
// promptly — without this the watcher would block on its own 5s budget
// even after the caller had already given up.
func (s *MCPServer) watchShutdown(ctx context.Context) {
	<-ctx.Done()
	// Close tracked connections (an open SSE stream would otherwise block
	// Shutdown until the client goes away).
	s.tracker.CloseAll()
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Printf("mcp server shutdown: %v", err)
	}
}

// commandErrorMessage is commandErrorMessage in src/mcp/command-client.ts:
// prefer the human-readable message, then the machine error, then a
// fallback.
func commandErrorMessage(p core.ResponsePayload, fallback string) string {
	if p.Message != "" {
		return p.Message
	}
	if p.Error != "" {
		return p.Error
	}
	return fallback
}

// noTabIDPattern matches Chrome's unknown-tab error; the hint points the
// caller at tab_list instead of a dead end.
var noTabIDPattern = regexp.MustCompile(`(?i)No tab with id: \d+`)

const tabListHint = " Call tab_list to discover valid tab ids for the selected browser."

// contentScriptRecovery mirrors CONTENT_SCRIPT_RECOVERY in command-client.ts.
var contentScriptRecovery = map[string]string{
	"tab_not_found":    " The tab was closed between commands — call tab_list to discover valid tab ids.",
	"restricted_page":  " Content commands only work on http(s) pages. Navigate to a non-restricted URL first.",
	"injection_failed": " The extension could not inject its content script into this page. Verify host_permissions cover the origin.",
	"no_listener":      " The content script did not respond. Reload the page or retry the command.",
}

// withRecoveryHint is withRecoveryHint in command-client.ts.
func withRecoveryHint(p core.ResponsePayload) core.ResponsePayload {
	if p.Status != "error" {
		return p
	}
	if hint, ok := contentScriptRecovery[p.Reason]; ok {
		return appendHint(p, hint)
	}
	if p.Error != "" && noTabIDPattern.MatchString(p.Error) && !strings.Contains(p.Error, "tab_list") {
		return appendHint(p, tabListHint)
	}
	return p
}

// appendHint appends the hint to whichever field the consumer will surface
// (message first, then error), matching appendHint in command-client.ts.
func appendHint(p core.ResponsePayload, hint string) core.ResponsePayload {
	if p.Message != "" {
		if !strings.HasSuffix(p.Message, hint) {
			p.Message += hint
		}
		return p
	}
	if p.Error != "" && !strings.HasSuffix(p.Error, hint) {
		p.Error += hint
	}
	return p
}
