package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/core"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/http"
)

// InboundRouter is the slice of core.Router the inbound server calls.
type InboundRouter interface {
	HandleInboundCommand(envelope core.Envelope, sender core.TextSender)
}

type InboundOptions struct {
	Port     int
	Hostname string
	Auth     Authorizer
	Router   InboundRouter
	Registry *core.Registry
	Logger   *log.Logger
}

// InboundServer is the Go port of src/server/inbound.ts: the inbound-facing
// WebSocket server (default port 3001) that accepts CLI/MCP client
// connections, authenticates them, and dispatches commands to the in-process
// router.
type InboundServer struct {
	port     int
	hostname string
	auth     Authorizer
	router   InboundRouter
	registry *core.Registry
	logger   *log.Logger

	httpServer *nethttp.Server
	tracker    *http.Tracker

	mu sync.Mutex
	// conns mirrors the TS cliConnections set.
	conns map[*clientConn]struct{}

	// ctx governs connection reads/writes; canceled on Shutdown.
	ctx    context.Context
	cancel context.CancelFunc
}

// clientConn is one CLI connection: the socket plus the WsData fields.
type clientConn struct {
	*Conn
	connectionID string
	userID       string
}

func NewInbound(opts InboundOptions) *InboundServer {
	auth := opts.Auth
	if auth == nil {
		auth = NoopAuthorizer{}
	}
	return &InboundServer{
		port:     opts.Port,
		hostname: opts.Hostname,
		auth:     auth,
		router:   opts.Router,
		registry: opts.Registry,
		logger:   opts.Logger,
		tracker:  http.NewTracker(),
		conns:    make(map[*clientConn]struct{}),
	}
}

func (s *InboundServer) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	listener, err := net.Listen("tcp", net.JoinHostPort(s.hostname, fmt.Sprint(s.port)))
	if err != nil {
		return fmt.Errorf("inbound server listen on %s:%d: %w", s.hostname, s.port, err)
	}
	s.httpServer = &nethttp.Server{Handler: s, ConnState: s.tracker.ConnState}
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != nethttp.ErrServerClosed {
			s.logger.Printf("inbound server: %v", err)
		}
	}()
	s.logger.Printf("Inbound server running on ws://localhost:%d", s.port)
	return nil
}

// Shutdown stops accepting connections and closes every open client socket,
// mirroring Bun's server.stop().
func (s *InboundServer) Shutdown(ctx context.Context) error {
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

func (s *InboundServer) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	// Note on inbound.ts's `url.protocol === 'ws:' && !isLocalhost(host)` →
	// 426 check: it is unreachable in the Bun original — Bun reports upgrade
	// requests with protocol http: and rejects absolute-form ws: targets with
	// 400 before the handler runs. Verified against Bun 1.3.14; not ported.
	authResult := s.auth.ValidateHeader(r.Header.Get("Authorization"))

	if !isUpgrade(r) {
		w.WriteHeader(nethttp.StatusOK)
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
		Conn:         NewConn(s.ctx, conn, s.logger),
		connectionID: core.NewID(),
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

func (s *InboundServer) serveConn(c *clientConn) {
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
		envelope, err := core.Decode(string(data))
		if err != nil {
			continue
		}

		switch envelope.Type {
		case core.TypeEvent:
			var event struct {
				Event string `json:"event"`
			}
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				continue
			}
			if event.Event == "list_browsers" {
				s.sendBrowserList(c, envelope.ID)
			}
		case core.TypeCommand:
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
		case core.TypeResponse:
			// Forward CLI→browser responses (rare; the browser normally
			// produces responses) to all other CLI sockets.
			s.forwardToOthers(c, string(data))
		}
	}
}

// send encodes and sends an envelope; failures are logged, never surfaced
// (Bun's ws.send is likewise fire-and-forget).
func (s *InboundServer) send(c *clientConn, t core.Type, payload json.RawMessage, id, browserID string) {
	text, err := core.Encode(t, payload, id, browserID)
	if err != nil {
		s.logger.Printf("encode %s envelope: %v", t, err)
		return
	}
	c.Send(text)
}

func (s *InboundServer) sendEvent(c *clientConn, payload json.RawMessage) {
	s.send(c, core.TypeEvent, payload, "", "")
}

func (s *InboundServer) sendBrowserList(c *clientConn, id string) {
	// core.ListBrowsers never returns nil, so data marshals as [] for an
	// empty registry (matching the TS array), never null.
	data, err := json.Marshal(s.registry.ListBrowsers())
	if err != nil {
		s.logger.Printf("encode browser list: %v", err)
		return
	}
	payload, err := json.Marshal(core.ResponsePayload{Status: "ok", Data: data})
	if err != nil {
		s.logger.Printf("encode list_browsers payload: %v", err)
		return
	}
	s.send(c, core.TypeResponse, payload, id, "")
}

// sendError renders encode('response', {status, error, message}, {id,
// browserId}) from inbound.ts.
func (s *InboundServer) sendError(c *clientConn, errCode, message, id, browserID string) {
	payload, err := json.Marshal(core.ResponsePayload{
		Status:  "error",
		Error:   errCode,
		Message: message,
	})
	if err != nil {
		s.logger.Printf("encode error payload: %v", err)
		return
	}
	s.send(c, core.TypeResponse, payload, id, browserID)
}

// sendInvalidJSON is encode('response', {status:'error',
// error:'invalid_json'}, {id: ”}) — note the empty id: the TS encode keeps
// an explicitly provided empty id rather than generating one, so this cannot
// go through core.Encode (which generates an id for the empty string).
func (s *InboundServer) sendInvalidJSON(c *clientConn) {
	payload, err := json.Marshal(core.ResponsePayload{Status: "error", Error: "invalid_json"})
	if err != nil {
		s.logger.Printf("encode invalid_json payload: %v", err)
		return
	}
	text, err := json.Marshal(core.Envelope{
		ID:        "",
		Type:      core.TypeResponse,
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

func (s *InboundServer) forwardToOthers(sender *clientConn, text string) {
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

func isUpgrade(r *nethttp.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}
