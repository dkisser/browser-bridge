package ws

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"testing"
	"time"
)

func newDiscardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// Shutdown-before-Start is a no-op, not a crash.
//
// MCPServer.Shutdown has always returned nil in this case (and has a test
// for it). The two WebSocket servers did not: s.cancel is only assigned
// inside Start, so calling Shutdown on a constructed-but-unstarted server
// dereferenced nil and took the process down. `defer s.Shutdown(ctx)` right
// after construction is a reasonable thing for a caller to write, and the
// lifecycle method refused to be written.
//
// This is also what makes the unwind in app.Run expressible: it calls
// Shutdown on whichever servers started, so it needs the ones that did not to
// be harmless.
func TestShutdownBeforeStartIsANoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	browser := NewBrowser(BrowserOptions{Hostname: "127.0.0.1", Logger: newDiscardLogger()})
	if err := browser.Shutdown(ctx); err != nil {
		t.Errorf("BrowserServer.Shutdown before Start = %v, want nil", err)
	}

	inbound := NewInbound(InboundOptions{Hostname: "127.0.0.1", Logger: newDiscardLogger()})
	if err := inbound.Shutdown(ctx); err != nil {
		t.Errorf("InboundServer.Shutdown before Start = %v, want nil", err)
	}
}

// The guard must not swallow the real path: a server that did start still
// releases its port, and a second Shutdown — the sequence an unwinding caller
// produces when a later start fails — stays harmless rather than panicking on
// an already-invoked cancel.
func TestShutdownAfterStartReleasesThePortAndRepeatsCleanly(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	tcpAddr, ok := reserved.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", reserved.Addr())
	}
	port := tcpAddr.Port
	if err = reserved.Close(); err != nil {
		t.Fatalf("release the reservation: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	srv := NewBrowser(BrowserOptions{Port: port, Hostname: "127.0.0.1", Logger: newDiscardLogger()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err = srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err = srv.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown = %v, want nil", err)
	}

	probe, probeErr := net.Listen("tcp", addr)
	if probeErr == nil {
		_ = probe.Close()
		return
	}
	// Shutdown closes the listener, but the Serve goroutine is still
	// unwinding when it returns, and the port is not rebindable for a beat
	// after. Poll rather than assert instantly — the claim under test is that
	// the port comes back, not that it comes back within one syscall.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		probe, probeErr = net.Listen("tcp", addr)
		if probeErr == nil {
			_ = probe.Close()
			return
		}
	}
	t.Fatalf("port %d is still bound 2s after Shutdown: %v", port, probeErr)
}
