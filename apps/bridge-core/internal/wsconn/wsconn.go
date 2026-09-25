// Package wsconn wraps a coder/websocket connection with the serialized,
// fire-and-forget text writes the TS code gets from Bun's ServerWebSocket.
// coder/websocket allows a single concurrent writer; both servers route
// frames from foreign goroutines (the router fan-out), so writes are
// serialized here.
package wsconn

import (
	"context"
	"log"
	"sync"

	"github.com/coder/websocket"
)

type Conn struct {
	conn   *websocket.Conn
	ctx    context.Context // governs writes; canceled on server shutdown
	logger *log.Logger
	wmu    sync.Mutex
}

func New(ctx context.Context, conn *websocket.Conn, logger *log.Logger) *Conn {
	return &Conn{conn: conn, ctx: ctx, logger: logger}
}

// Send writes one text frame. Like Bun's ws.send it does not surface errors
// to the caller; failures (a peer that went away) are logged and dropped.
func (c *Conn) Send(text string) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte(text)); err != nil {
		c.logger.Printf("ws write failed: %v", err)
	}
}

func (c *Conn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	return c.conn.Read(ctx)
}

func (c *Conn) Close(code websocket.StatusCode, reason string) {
	// Close is idempotent in coder/websocket; a peer that already went away
	// turns this into a no-op error, which TS likewise ignores.
	_ = c.conn.Close(code, reason)
}
