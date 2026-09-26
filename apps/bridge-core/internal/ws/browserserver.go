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

	"github.com/coder/websocket"

	"browser-bridge/internal/core"
	"browser-bridge/internal/http"
)

const extensionOriginPrefix = "chrome-extension://"

// BrowserRouter is the slice of core.Router the browser server calls. It is
// injected lazily (GetRouter) because the router and the browser server
// reference each other, mirroring the getRouter closure in index.ts.
type BrowserRouter interface {
	BrowserID() string
	HandleBrowserConnect()
	HandleBrowserDisconnect()
	HandleBrowserResponse(envelope core.Envelope)
	HandleBrowserEvent(envelope core.Envelope)
}

type BrowserOptions struct {
	Port      int
	Hostname  string
	GetRouter func() BrowserRouter
	Pairing   *core.PairingManager
	Logger    *log.Logger
}

// BrowserServer is the Go port of src/browser-server.ts: the browser-facing
// WebSocket server (default port 3002) plus the three HTTP endpoints
// (/api/status, /api/pair/start, /api/pair/confirm).
//
// The pairing token travels in the Sec-WebSocket-Protocol header (browser
// WebSockets cannot set arbitrary headers); it is echoed back as the
// selected subprotocol so the handshake completes. Everything else gets 403.
type BrowserServer struct {
	port      int
	hostname  string
	getRouter func() BrowserRouter
	pairing   *core.PairingManager
	logger    *log.Logger

	httpServer *nethttp.Server
	tracker    *http.Tracker

	mu sync.Mutex
	// ext is the currently tracked extension connection; conns holds every
	// live connection (a replaced stale socket stays open until the extension
	// closes it, as in the Bun original).
	ext   *Conn
	conns map[*Conn]struct{}
	// ctx governs connection reads/writes; canceled on Shutdown.
	ctx    context.Context
	cancel context.CancelFunc
}

func NewBrowser(opts BrowserOptions) *BrowserServer {
	return &BrowserServer{
		port:      opts.Port,
		hostname:  opts.Hostname,
		getRouter: opts.GetRouter,
		pairing:   opts.Pairing,
		logger:    opts.Logger,
		tracker:   http.NewTracker(),
		conns:     make(map[*Conn]struct{}),
	}
}

// Start binds the listener and serves in the background. The context is the
// server lifetime: canceling it (via Shutdown) ends every connection.
func (s *BrowserServer) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	listener, err := net.Listen("tcp", net.JoinHostPort(s.hostname, fmt.Sprint(s.port)))
	if err != nil {
		return fmt.Errorf("browser server listen on %s:%d: %w", s.hostname, s.port, err)
	}
	s.httpServer = &nethttp.Server{Handler: s, ConnState: s.tracker.ConnState}
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != nethttp.ErrServerClosed {
			s.logger.Printf("browser server: %v", err)
		}
	}()
	s.logger.Printf("Browser server listening on ws://localhost:%d", s.port)
	return nil
}

// Shutdown stops accepting connections and closes every open socket,
// mirroring Bun's server.stop().
func (s *BrowserServer) Shutdown(ctx context.Context) error {
	s.cancel()
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.Close(websocket.StatusGoingAway, "server shutdown")
	}
	if s.httpServer != nil {
		// Close tracked HTTP connections first: Shutdown would otherwise
		// linger on never-used keep-alive connections (Go issue 22682).
		s.tracker.CloseAll()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("browser server shutdown: %w", err)
		}
	}
	return nil
}

// HasExtension reports whether an extension connection is tracked.
func (s *BrowserServer) HasExtension() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ext != nil
}

// SendToExtension writes a frame to the tracked extension connection.
// Returns false when there is no tracked connection, AND when the
// underlying Write fails (peer already gone, kernel sent RST, etc.). The
// caller (the router) uses the false return to drop inboundByID[id]; the
// previous implementation always returned true on Write, which left the
// route pinned waiting for a response that the extension would never
// produce.
func (s *BrowserServer) SendToExtension(text string) bool {
	s.mu.Lock()
	ext := s.ext
	s.mu.Unlock()
	if ext == nil {
		return false
	}
	return ext.TrySend(text)
}

func (s *BrowserServer) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	if isWebSocketUpgrade(r) {
		s.handleUpgrade(w, r)
		return
	}

	cors := corsHeaders(r)

	if r.Method == nethttp.MethodOptions {
		writeHeaders(w, cors)
		w.WriteHeader(nethttp.StatusNoContent)
		return
	}

	// A web page (non-extension origin) must not drive the proxy's API: no
	// CORS headers means the browser blocks the read, and this guard blocks
	// the write side of simple requests.
	if isWebOrigin(r) {
		writeJSON(w, nethttp.StatusForbidden, failBody{Success: false, Error: "forbidden_origin"}, nil)
		return
	}

	switch {
	case r.URL.Path == "/api/status":
		s.handleStatus(w, cors)
	case r.URL.Path == "/api/pair/start" && r.Method == nethttp.MethodPost:
		s.handlePairStart(w, cors)
	case r.URL.Path == "/api/pair/confirm" && r.Method == nethttp.MethodPost:
		s.handlePairConfirm(w, r, cors)
	default:
		w.WriteHeader(nethttp.StatusOK)
		_, _ = io.WriteString(w, "Browser Bridge Browser Server")
	}
}

func isWebSocketUpgrade(r *nethttp.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func (s *BrowserServer) handleUpgrade(w nethttp.ResponseWriter, r *nethttp.Request) {
	// The token is the first non-empty subprotocol entry (browser-server.ts
	// splits on ',' and trims).
	var token string
	for _, entry := range strings.Split(r.Header.Get("Sec-Websocket-Protocol"), ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			token = trimmed
			break
		}
	}
	if token == "" || !s.pairing.Verify(token) {
		w.WriteHeader(nethttp.StatusForbidden)
		_, _ = io.WriteString(w, "unauthorized")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Echo the token back as the selected subprotocol so the handshake
		// completes (the extension passes the token as its only subprotocol).
		Subprotocols: []string{token},
		// The Bun original performs no Origin check on the upgrade — the
		// bearer token in the subprotocol header is the entire gate.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Printf("browser upgrade: %v", err)
		return
	}
	s.serveConn(conn)
}

func (s *BrowserServer) serveConn(conn *websocket.Conn) {
	c := NewConn(s.ctx, conn, s.logger)

	s.mu.Lock()
	s.ext = c
	s.conns[c] = struct{}{}
	s.mu.Unlock()

	s.logger.Println("[browser] Extension connected")
	if text, err := core.Encode(core.TypeEvent, json.RawMessage(`{"event":"connected"}`), "", ""); err != nil {
		s.logger.Printf("encode connected event: %v", err)
	} else {
		c.Send(text)
	}
	s.getRouter().HandleBrowserConnect()

	defer func() {
		s.mu.Lock()
		// Only the socket we are currently tracking may report a disconnect.
		// The extension closes its stale socket and reconnects immediately
		// (offscreen.ts: connect_ws closes `stale` then calls connect()), and
		// a close is delivered asynchronously — so the replacement socket's
		// open can be processed first. Clearing unconditionally would blank
		// the live connection, mark the browser offline, and leave commands
		// buffering for 5s into a sw_timeout while the extension sits there
		// believing it is connected.
		current := s.ext == c
		if current {
			s.ext = nil
		}
		delete(s.conns, c)
		s.mu.Unlock()
		c.Close(websocket.StatusNormalClosure, "")
		if current {
			s.logger.Println("[browser] Extension disconnected")
			s.getRouter().HandleBrowserDisconnect()
		}
	}()

	for {
		_, data, err := c.Read(s.ctx)
		if err != nil {
			return
		}
		// TS: JSON.parse throws on malformed frames (logged); a valid JSON
		// value of the wrong shape decodes to an object with undefined type
		// and is silently ignored.
		if !json.Valid(data) {
			s.logger.Println("[browser] invalid message from Extension")
			continue
		}
		envelope, err := core.Decode(string(data))
		if err != nil {
			continue
		}
		switch envelope.Type {
		case core.TypeResponse:
			s.getRouter().HandleBrowserResponse(envelope)
		case core.TypeEvent:
			s.getRouter().HandleBrowserEvent(envelope)
		}
	}
}

// CORS: only extension pages may read API responses. Requests without an
// Origin (curl, the CLI, local probes) are served but get no CORS headers.
func corsHeaders(r *nethttp.Request) nethttp.Header {
	origin := r.Header.Get("Origin")
	if !strings.HasPrefix(origin, extensionOriginPrefix) {
		return nil
	}
	return nethttp.Header{
		"Access-Control-Allow-Origin":  {origin},
		"Vary":                         {"Origin"},
		"Access-Control-Allow-Methods": {"GET, POST, OPTIONS"},
		"Access-Control-Allow-Headers": {"Content-Type"},
	}
}

func isWebOrigin(r *nethttp.Request) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && !strings.HasPrefix(origin, extensionOriginPrefix)
}

// Response body shapes, with field order matching the TS object literals
// ({success, data} / {success, error, attemptsRemaining}).

type okBody struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

type failBody struct {
	Success           bool   `json:"success"`
	Error             string `json:"error"`
	AttemptsRemaining *int   `json:"attemptsRemaining,omitempty"`
}

type statusData struct {
	Paired       bool   `json:"paired"`
	HasExtension bool   `json:"hasExtension"`
	BrowserID    string `json:"browserId"`
}

type pairStartData struct {
	Code      string `json:"code"`
	ExpiresIn int    `json:"expiresIn"`
}

type pairConfirmData struct {
	Token string `json:"token"`
}

func (s *BrowserServer) handleStatus(w nethttp.ResponseWriter, cors nethttp.Header) {
	writeJSON(w, nethttp.StatusOK, okBody{
		Success: true,
		Data: statusData{
			Paired:       s.pairing.IsPaired(),
			HasExtension: s.HasExtension(),
			BrowserID:    s.getRouter().BrowserID(),
		},
	}, cors)
}

func (s *BrowserServer) handlePairStart(w nethttp.ResponseWriter, cors nethttp.Header) {
	code, expiresIn := s.pairing.Start()
	writeJSON(w, nethttp.StatusOK, okBody{Success: true, Data: pairStartData{Code: code, ExpiresIn: expiresIn}}, cors)
}

func (s *BrowserServer) handlePairConfirm(w nethttp.ResponseWriter, r *nethttp.Request, cors nethttp.Header) {
	// A malformed body falls through to the invalid-code path (browser-server.ts).
	var code string
	if body, err := io.ReadAll(r.Body); err == nil {
		var payload struct {
			Code any `json:"code"`
		}
		if err := json.Unmarshal(body, &payload); err == nil {
			if str, ok := payload.Code.(string); ok {
				code = str
			}
		}
	}
	result, err := s.pairing.Confirm(code)
	if err != nil {
		s.logger.Printf("pair confirm: %v", err)
		w.WriteHeader(nethttp.StatusInternalServerError)
		return
	}
	if result.OK {
		writeJSON(w, nethttp.StatusOK, okBody{Success: true, Data: pairConfirmData{Token: result.Token}}, cors)
		return
	}
	writeJSON(w, nethttp.StatusUnauthorized, failBody{
		Success:           false,
		Error:             string(result.Failure),
		AttemptsRemaining: result.AttemptsRemaining,
	}, cors)
}

func writeHeaders(w nethttp.ResponseWriter, headers nethttp.Header) {
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}

func writeJSON(w nethttp.ResponseWriter, status int, body any, headers nethttp.Header) {
	writeHeaders(w, headers)
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(nethttp.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
