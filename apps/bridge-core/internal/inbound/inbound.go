// Package inbound is the Go port of src/server/inbound.ts: the
// inbound-facing WebSocket server (default port 3001) that accepts CLI / MCP
// client connections, authenticates them, and dispatches commands to the
// in-process router.
package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/httpserver"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/registry"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/wsconn"
)

// Router is the slice of router.Router the inbound server calls.
type Router interface {
	HandleInboundCommand(envelope protocol.Envelope, sender protocol.TextSender)
}

type Options struct {
	Port     int
	Hostname string
	Auth     Authorizer
	Router   Router
	Registry *registry.Registry
	Logger   *log.Logger
}

type Server struct {
	port     int
	hostname string
	auth     Authorizer
	router   Router
	registry *registry.Registry
	logger   *log.Logger

	httpServer *http.Server
	tracker    *httpserver.Tracker

	mu sync.Mutex
	// conns mirrors the TS cliConnections set.
	conns map[*clientConn]struct{}

	// ctx governs connection reads/writes; canceled on Shutdown.
	ctx    context.Context
	cancel context.CancelFunc
}

// clientConn is one CLI connection: the socket plus the WsData fields.
type clientConn struct {
	*wsconn.Conn
	connectionID string
	userID       string
}

func New(opts Options) *Server {
	auth := opts.Auth
	if auth == nil {
		auth = NoopAuthorizer{}
	}
	return &Server{
		port:     opts.Port,
		hostname: opts.Hostname,
		auth:     auth,
		router:   opts.Router,
		registry: opts.Registry,
		logger:   opts.Logger,
		tracker:  httpserver.NewTracker(),
		conns:    make(map[*clientConn]struct{}),
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	listener, err := net.Listen("tcp", net.JoinHostPort(s.hostname, fmt.Sprint(s.port)))
	if err != nil {
		return fmt.Errorf("inbound server listen on %s:%d: %w", s.hostname, s.port, err)
	}
	s.httpServer = &http.Server{Handler: s, ConnState: s.tracker.ConnState}
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("inbound server: %v", err)
		}
	}()
	s.logger.Printf("Inbound server running on ws://localhost:%d", s.port)
	return nil
}

// Shutdown stops accepting connections and closes every open client socket,
// mirroring Bun's server.stop().
func (s *Server) Shutdown(ctx context.Context) error {
	s.cancel()
	s.mu.Lock()
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.Close(websocket.StatusGoingAway, "server shutdown")
	}
	if s.httpServer != nil {
		s.tracker.CloseAll()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("inbound server shutdown: %w", err)
		}
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Note on inbound.ts's `url.protocol === 'ws:' && !isLocalhost(host)` →
	// 426 check: it is unreachable in the Bun original — Bun reports upgrade
	// requests with protocol http: and rejects absolute-form ws: targets with
	// 400 before the handler runs. Verified against Bun 1.3.14; not ported.
	authResult := s.auth.ValidateHeader(r.Header.Get("Authorization"))

	if !isUpgrade(r) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "Browser Bridge control plane")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The Bun original performs no Origin check on inbound upgrades; the
		// Authorization bearer is the gate.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Printf("inbound upgrade: %v", err)
		return
	}

	c := &clientConn{
		Conn:         wsconn.New(s.ctx, conn, s.logger),
		connectionID: protocol.NewID(),
		userID:       authResult.UserID,
	}

	// TS upgrades first and closes with 4001 from the open handler, so the
	// client observes a completed handshake followed by the close frame.
	if !authResult.Valid {
		c.Close(4001, "unauthorized")
		return
	}

	s.logger.Printf("Client connected: %s (user: %s)", c.connectionID, c.userID)
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	s.sendEvent(c, json.RawMessage(`{"event":"welcome"}`))

	s.serveConn(c)
}

func (s *Server) serveConn(c *clientConn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		c.Close(websocket.StatusNormalClosure, "")
		s.logger.Printf("Client disconnected: %s", c.connectionID)
	}()

	for {
		_, data, err := c.Read(s.ctx)
		if err != nil {
			return
		}
		// TS: JSON.parse failure → invalid_json response; a valid JSON value
		// of the wrong shape decodes with an undefined type and falls through
		// the switch unanswered.
		if !json.Valid(data) {
			s.sendInvalidJSON(c)
			continue
		}
		envelope, err := protocol.Decode(string(data))
		if err != nil {
			continue
		}

		switch envelope.Type {
		case protocol.TypeEvent:
			var event struct {
				Event string `json:"event"`
			}
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				continue
			}
			if event.Event == "list_browsers" {
				s.sendBrowserList(c, envelope.ID)
			}
		case protocol.TypeCommand:
			browserID := envelope.BrowserID
			// Reject only browsers the registry has never seen. Reachability
			// of a *known* browser is the router's call: it accepts online and
			// idle_wait (buffering for a fast reconnect) and answers
			// browser_offline / cannot_buffer itself. Rejecting 'offline' here
			// as well would short-circuit that buffer, because
			// handleBrowserDisconnect marks the registry offline while state
			// is still idle_wait — so the 5s tolerance never applied.
			if _, known := s.registry.GetStatus(browserID); !known {
				s.sendError(c, "browser_offline", fmt.Sprintf("Browser %s is offline", browserID), envelope.ID, browserID)
				continue
			}
			s.router.HandleInboundCommand(envelope, c)
		case protocol.TypeResponse:
			// Forward CLI→browser responses (rare; the browser normally
			// produces responses) to all other CLI sockets.
			s.forwardToOthers(c, string(data))
		}
	}
}

// send encodes and sends an envelope; failures are logged, never surfaced
// (Bun's ws.send is likewise fire-and-forget).
func (s *Server) send(c *clientConn, t protocol.Type, payload json.RawMessage, id, browserID string) {
	text, err := protocol.Encode(t, payload, id, browserID)
	if err != nil {
		s.logger.Printf("encode %s envelope: %v", t, err)
		return
	}
	c.Send(text)
}

func (s *Server) sendEvent(c *clientConn, payload json.RawMessage) {
	s.send(c, protocol.TypeEvent, payload, "", "")
}

func (s *Server) sendBrowserList(c *clientConn, id string) {
	// registry.ListBrowsers never returns nil, so data marshals as [] for an
	// empty registry (matching the TS array), never null.
	data, err := json.Marshal(s.registry.ListBrowsers())
	if err != nil {
		s.logger.Printf("encode browser list: %v", err)
		return
	}
	payload, err := json.Marshal(protocol.ResponsePayload{Status: "ok", Data: data})
	if err != nil {
		s.logger.Printf("encode list_browsers payload: %v", err)
		return
	}
	s.send(c, protocol.TypeResponse, payload, id, "")
}

// sendError renders encode('response', {status, error, message}, {id,
// browserId}) from inbound.ts.
func (s *Server) sendError(c *clientConn, errCode, message, id, browserID string) {
	payload, err := json.Marshal(protocol.ResponsePayload{
		Status:  "error",
		Error:   errCode,
		Message: message,
	})
	if err != nil {
		s.logger.Printf("encode error payload: %v", err)
		return
	}
	s.send(c, protocol.TypeResponse, payload, id, browserID)
}

// sendInvalidJSON is encode('response', {status:'error',
// error:'invalid_json'}, {id: ”}) — note the empty id: the TS encode keeps
// an explicitly provided empty id rather than generating one, so this cannot
// go through protocol.Encode (which generates an id for the empty string).
func (s *Server) sendInvalidJSON(c *clientConn) {
	payload, err := json.Marshal(protocol.ResponsePayload{Status: "error", Error: "invalid_json"})
	if err != nil {
		s.logger.Printf("encode invalid_json payload: %v", err)
		return
	}
	text, err := json.Marshal(protocol.Envelope{
		ID:        "",
		Type:      protocol.TypeResponse,
		BrowserID: "",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		s.logger.Printf("encode invalid_json envelope: %v", err)
		return
	}
	c.Send(string(text))
}

func (s *Server) forwardToOthers(sender *clientConn, text string) {
	s.mu.Lock()
	others := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		if c != sender {
			others = append(others, c)
		}
	}
	s.mu.Unlock()
	for _, c := range others {
		c.Send(text)
	}
}

func isUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}
