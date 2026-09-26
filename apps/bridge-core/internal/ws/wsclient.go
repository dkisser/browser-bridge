package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/core"
)

// ErrConnectionClosed rejects every pending request when the connection
// drops, matching the TS client's close handler ("connection closed").
var ErrConnectionClosed = errors.New("connection closed")

// reply is the internal resolution of one pending request: exactly one of
// env/err is set.
type reply struct {
	env core.Envelope
	err error
}

// Client is the inbound-side WebSocket client used by the CLI: it dials the
// bridge-core inbound port, sends command/event envelopes, and correlates
// responses by envelope id. Go port of the deleted apps/cli/src/client.ts +
// managedClient.ts (ADR-0012 phase 2b). A Client is single-use; it is safe
// for concurrent use, but the CLI issues one request per process and closes.
type Client struct {
	conn   *websocket.Conn
	ctx    context.Context // governs reads/writes; canceled by Close
	cancel context.CancelFunc

	wmu     sync.Mutex // coder/websocket allows a single concurrent writer
	mu      sync.Mutex
	pending map[string]chan reply
}

// Dial connects to url; ctx bounds the handshake (the TS CLI waits up to
// 5s in waitForOpen). A background read loop runs until Close.
func Dial(ctx context.Context, url string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		pending: make(map[string]chan reply),
	}
	go c.readLoop()
	return c, nil
}

// SendCommand sends a command envelope and waits for its response, the TS
// client's sendCommand. The timeout error message matches the TS text.
func (c *Client) SendCommand(ctx context.Context, browserID string, cmd core.CommandPayload, timeout time.Duration) (core.Envelope, error) {
	return c.roundTrip(ctx, core.TypeCommand, cmd, browserID, timeout, "command "+cmd.Command)
}

// Request sends an envelope of any type and waits for its response, the TS
// client's request (used for the list_browsers event).
func (c *Client) Request(ctx context.Context, typ core.Type, payload any, browserID string, timeout time.Duration) (core.Envelope, error) {
	return c.roundTrip(ctx, typ, payload, browserID, timeout, string(typ))
}

func (c *Client) roundTrip(ctx context.Context, typ core.Type, payload any, browserID string, timeout time.Duration, timeoutSubject string) (core.Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return core.Envelope{}, fmt.Errorf("marshal %s payload: %w", typ, err)
	}
	id := core.NewID()
	text, err := core.Encode(typ, raw, id, browserID)
	if err != nil {
		return core.Envelope{}, err
	}

	// Register before sending so a fast response cannot arrive first.
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(text); err != nil {
		c.removePending(id)
		return core.Envelope{}, fmt.Errorf("send %s: %w", typ, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.env, r.err
	case <-timer.C:
		c.removePending(id)
		return core.Envelope{}, fmt.Errorf("timeout: no response for %s within %dms", timeoutSubject, timeout.Milliseconds())
	case <-ctx.Done():
		c.removePending(id)
		return core.Envelope{}, fmt.Errorf("%s: %w", timeoutSubject, ctx.Err())
	}
}

// Close shuts the connection down; pending requests fail with
// ErrConnectionClosed via the read loop. Like Conn.Close it is idempotent
// and swallows the no-op error of an already-dead peer.
func (c *Client) Close() {
	c.cancel()
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// readLoop dispatches response envelopes to their pending requests. Any
// terminal read error (including a peer close) fails all pending requests;
// non-response frames and undecodable frames are dropped, as the TS CLI's
// message handler does (it has no onMessage consumer).
func (c *Client) readLoop() {
	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			c.failPending(ErrConnectionClosed)
			return
		}
		env, err := core.Decode(string(data))
		if err != nil || env.Type != core.TypeResponse {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[env.ID]
		delete(c.pending, env.ID)
		c.mu.Unlock()
		if ok {
			ch <- reply{env: env} // buffered, never blocks
		}
	}
}

func (c *Client) write(text string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte(text)); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// failPending rejects every pending request, matching the TS close handler.
func (c *Client) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan reply)
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- reply{err: err} // buffered, never blocks
	}
}
