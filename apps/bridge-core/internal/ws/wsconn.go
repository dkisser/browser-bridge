// Package ws is the control plane's WebSocket layer: the InboundServer
// (default port 3001) faces CLI/MCP clients, the BrowserServer (default port
// 3002) faces the extension, Client is the CLI-side inbound client, and Conn
// is the shared connection wrapper.
package ws

import (
	"context"
	"log"
	"sync"

	"github.com/coder/websocket"
)

// Conn wraps a coder/websocket connection with the serialized,
// fire-and-forget text writes the TS code gets from Bun's ServerWebSocket.
// coder/websocket allows a single concurrent writer; both servers route
// frames from foreign goroutines (the router fan-out), so writes are
// serialized here.
type Conn struct {
	conn   *websocket.Conn
	ctx    context.Context // governs writes; canceled on server shutdown
	logger *log.Logger
	wmu    sync.Mutex
	// closed is set once by Close; subsequent Send / TrySend return early
	// without touching the underlying socket, which keeps the
	// "ws write failed" log line from triggering on every fan-out to a
	// peer that just disconnected (the original TS filtered on
	// cliWs.readyState === 1 before sending).
	closed bool
}

func NewConn(ctx context.Context, conn *websocket.Conn, logger *log.Logger) *Conn {
	return &Conn{conn: conn, ctx: ctx, logger: logger}
}

// Send writes one text frame. Like Bun's ws.send it does not surface errors
// to the caller; failures (a peer that went away) are logged and dropped.
func (c *Conn) Send(text string) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte(text)); err != nil {
		c.logger.Printf("ws write failed: %v", err)
	}
}

// TrySend is Send that reports success as a bool. Used by the router's
// SendToExtension path so a Write that fails (peer already gone) can be
// distinguished from "no tracked connection" — without it the router
// leaves inboundByID[id] pinned waiting for a response that will never
// arrive, because the frame never reached the extension.
func (c *Conn) TrySend(text string) bool {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return false
	}
	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte(text)); err != nil {
		c.logger.Printf("ws write failed: %v", err)
		return false
	}
	return true
}

// IsClosed reports whether Close has run on this connection. Inbound
// fan-out uses it to skip peers that have already disconnected (matching
// the TS `cliWs.readyState === 1` filter).
func (c *Conn) IsClosed() bool {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.closed
}

func (c *Conn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	return c.conn.Read(ctx)
}

func (c *Conn) Close(code websocket.StatusCode, reason string) {
	// Close is idempotent in coder/websocket; a peer that already went away
	// turns this into a no-op error, which TS likewise ignores. Mark the
	// connection closed first so any concurrent Send / TrySend short-circuit
	// and any subsequent IsClosed call returns true.
	c.wmu.Lock()
	c.closed = true
	c.wmu.Unlock()
	_ = c.conn.Close(code, reason)
}
