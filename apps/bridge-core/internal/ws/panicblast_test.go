package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"

	"browser-bridge/internal/core"
)

// panicRouter stands in for the real router at the point the review found the
// defect: HandleBrowserResponse is a synchronous call into the memory hook,
// and splitPageLine used to panic there on a page-controlled snapshot line.
type panicRouter struct {
	panicked chan struct{}
	served   chan struct{}
}

func (p *panicRouter) BrowserID() string { return "browser-test" }

func (p *panicRouter) HandleBrowserConnect() {}

func (p *panicRouter) HandleBrowserDisconnect() {}

func (p *panicRouter) HandleBrowserEvent(core.Envelope) {}

func (p *panicRouter) HandleBrowserResponse(core.Envelope) {
	select {
	case <-p.panicked:
	default:
		close(p.panicked)
		panic("index out of range [7] with length 7")
	}
	select {
	case <-p.served:
	default:
		close(p.served)
	}
}

// reservePort takes an ephemeral port and hands it back, so the server under
// test binds a known address the way lifecycle_test.go does.
func reservePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", l.Addr())
	}
	port := tcpAddr.Port
	if err = l.Close(); err != nil {
		t.Fatalf("release the reservation: %v", err)
	}
	return port
}

// A panic on the browser read path must cost the connection, not the daemon.
//
// splitPageLine ran on every snapshot, inside the read loop that
// HandleBrowserResponse feeds — a page-controlled `document.title` was enough
// to reach it. The review called the blast radius "the whole daemon: the
// browser, the inbound server and the MCP server with it". This pins down what
// actually happens, because the answer decides whether the fix belongs in
// splitPageLine alone or also needs a recover on the read loop.
//
// serveConn is reached only through ServeHTTP, so net/http's per-connection
// recover is what stands between a page-controlled title and process death.
func TestPanicOnTheBrowserReadPathCostsTheConnectionNotTheServer(t *testing.T) {
	const token = "test-extension-token"
	sum := sha256.Sum256([]byte(token))
	pairing := core.NewPairingManager(
		func() string { return hex.EncodeToString(sum[:]) },
		func(string) error { return nil },
	)

	router := &panicRouter{panicked: make(chan struct{}), served: make(chan struct{})}
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

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	env, err := core.Encode(core.TypeResponse,
		json.RawMessage(`{"data":{"snapshot":"Page: Gmail |","nodesTotal":0}}`),
		"resp-1", "browser-test")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	dialAndSend := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws://"+addr, &websocket.DialOptions{
			Subprotocols: []string{token},
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		if err = conn.Write(ctx, websocket.MessageText, []byte(env)); err != nil {
			t.Fatalf("write: %v", err)
		}
		return conn
	}

	// First connection: the router panics while handling its response.
	first := dialAndSend()
	defer func() { _ = first.Close(websocket.StatusNormalClosure, "") }()
	select {
	case <-router.panicked:
	case <-time.After(5 * time.Second):
		t.Fatal("the router was never reached")
	}

	// Second connection on the same server. If the panic had taken the process
	// down this would never dial; if it had poisoned shared server state, the
	// router would never be reached again.
	second := dialAndSend()
	defer func() { _ = second.Close(websocket.StatusNormalClosure, "") }()
	select {
	case <-router.served:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not serve a connection after the panic")
	}

	// And the server still shuts down cleanly, which it cannot do if a
	// connection goroutine died holding its state.
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown after a panic on the read path = %v, want nil", err)
	}
}
