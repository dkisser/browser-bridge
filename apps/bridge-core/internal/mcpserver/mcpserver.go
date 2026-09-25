// Package mcpserver is the Go port of src/mcp/server.ts, scoped to the
// pageinfo tool for the Phase 1 spike. Where the TS server dials the inbound
// WebSocket as a client, the Go port dispatches commands to the router
// in-process (allowed by the spike brief); the inbound WebSocket behavior on
// 3001 is preserved for real CLI clients.
package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/httpserver"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

// ServerName is the FastMCP server name in src/mcp/server.ts.
const ServerName = "Browser Bridge"

// CommandRouter is the slice of router.Router the tools call.
type CommandRouter interface {
	HandleInboundCommand(envelope protocol.Envelope, sender protocol.TextSender)
}

// BrowserLister is the slice of registry.Registry the tools call.
type BrowserLister interface {
	ListBrowsers() []protocol.BrowserConnection
}

type Options struct {
	Port           int
	Hostname       string
	Router         CommandRouter
	Registry       BrowserLister
	DefaultTimeout time.Duration
	Version        string
	Logger         *log.Logger
}

type Server struct {
	port           int
	hostname       string
	router         CommandRouter
	registry       BrowserLister
	defaultTimeout time.Duration
	version        string
	logger         *log.Logger

	httpServer *http.Server
	tracker    *httpserver.Tracker
}

func New(opts Options) *Server {
	return &Server{
		port:           opts.Port,
		hostname:       opts.Hostname,
		router:         opts.Router,
		registry:       opts.Registry,
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

func (s *Server) Start(ctx context.Context) error {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: s.version}, nil)
	s.registerTools(mcpServer)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	listener, err := net.Listen("tcp", net.JoinHostPort(s.hostname, fmt.Sprint(s.port)))
	if err != nil {
		return fmt.Errorf("mcp server listen on %s:%d: %w", s.hostname, s.port, err)
	}
	tracker := httpserver.NewTracker()
	s.httpServer = &http.Server{Handler: mux, ConnState: tracker.ConnState}
	s.tracker = tracker
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("mcp server: %v", err)
		}
	}()
	go s.watchShutdown(ctx)
	s.logger.Printf("MCP server listening on http://localhost:%d/mcp", s.port)
	return nil
}

// watchShutdown closes the MCP listener when the run context ends; the go-sdk
// StreamableHTTPHandler has no Close of its own, so this is the lifecycle
// hook the other servers get from Shutdown.
func (s *Server) watchShutdown(ctx context.Context) {
	<-ctx.Done()
	// Close tracked connections (an open SSE stream would otherwise block
	// Shutdown until the client goes away).
	s.tracker.CloseAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Printf("mcp server shutdown: %v", err)
	}
}

// commandErrorMessage is commandErrorMessage in src/mcp/command-client.ts:
// prefer the human-readable message, then the machine error, then a
// fallback.
func commandErrorMessage(p protocol.ResponsePayload, fallback string) string {
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
func withRecoveryHint(p protocol.ResponsePayload) protocol.ResponsePayload {
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
func appendHint(p protocol.ResponsePayload, hint string) protocol.ResponsePayload {
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
