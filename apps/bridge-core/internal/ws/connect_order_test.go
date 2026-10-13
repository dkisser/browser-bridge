package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"browser-bridge/internal/core"
)

// gateRouter blocks inside MarkBrowserOnline until the test releases it, so
// the test can observe whether the browser server announces the connection
// before or after marking the browser routable.
//
// The ordering it pins: MarkBrowserOnline is what puts the browser in the
// registry, and the registry is the only thing that lets a command through —
// inbound.go answers browser_offline for a browser it has never seen. serveConn
// used to write `{"event":"connected"}` and only then consult the router, so a
// client reading that greeting could send a command (over a *different*
// connection, the inbound one) before the browser was routable.
// e2e/TestCommandRoundTrip lost exactly that race on CI and passed everywhere
// else, because nothing in it waited for the registration to land.
type gateRouter struct {
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (g *gateRouter) BrowserID() string { return "browser-test" }

func (g *gateRouter) MarkBrowserOnline() core.BufferedCommand {
	g.once.Do(func() { close(g.entered) })
	<-g.gate
	return core.BufferedCommand{}
}

func (g *gateRouter) ReplayBufferedCommand(core.BufferedCommand) {}

func (g *gateRouter) HandleBrowserDisconnect() {}

func (g *gateRouter) HandleBrowserEvent(core.Envelope) {}

func (g *gateRouter) HandleBrowserResponse(core.Envelope) {}

// TestConnectedIsAnnouncedOnlyAfterTheRouterIsConsulted: nothing may reach
// the extension before the router has been told the browser connected.
func TestConnectedIsAnnouncedOnlyAfterTheRouterIsConsulted(t *testing.T) {
	const token = "test-extension-token"
	sum := sha256.Sum256([]byte(token))
	pairing := core.NewPairingManager(
		func() string { return hex.EncodeToString(sum[:]) },
		func(string) error { return nil },
	)

	router := &gateRouter{entered: make(chan struct{}), gate: make(chan struct{})}
	port := reservePort(t)
	srv := NewBrowser(BrowserOptions{
		Port:      port,
		Hostname:  "127.0.0.1",
		GetRouter: func() BrowserRouter { return router },
		Pairing:   pairing,
		Logger:    newDiscardLogger(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	dial := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{
			Subprotocols: []string{token},
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return conn
	}

	// A witness connection, spent on the check below: coder/websocket closes
	// a connection whose read context expires (conn.go:193-196), so it cannot
	// be reused afterwards — hence the second dial.
	witness := dial()

	// The server is now parked inside MarkBrowserOnline and cannot get
	// any further until this test releases it.
	select {
	case <-router.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the server never marked the browser online")
	}

	// So nothing may have reached the extension yet. A frame that did arrive
	// here would already be buffered on the socket, so this waits out a
	// frame that is genuinely never sent rather than one that is slow.
	earlyCtx, earlyCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer earlyCancel()
	if _, data, err := witness.Read(earlyCtx); err == nil {
		t.Fatalf("server sent %s before the browser was marked routable; a command sent "+
			"on seeing that greeting can be answered browser_offline", data)
	}
	_ = witness.Close(websocket.StatusNormalClosure, "")

	// Release the router: the announcement follows it.
	close(router.gate)

	conn := dial()
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	greetCtx, greetCancel := context.WithTimeout(ctx, 10*time.Second)
	defer greetCancel()
	_, data, err := conn.Read(greetCtx)
	if err != nil {
		t.Fatalf("read the connected event: %v", err)
	}
	env, err := core.Decode(string(data))
	if err != nil {
		t.Fatalf("decode %q: %v", data, err)
	}
	if env.Type != core.TypeEvent || string(env.Payload) != `{"event":"connected"}` {
		t.Fatalf("greeting = %s %s, want the connected event", env.Type, env.Payload)
	}
}
