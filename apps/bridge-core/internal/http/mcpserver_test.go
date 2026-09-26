package http

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"testing"
	"time"
)

// TestShutdownBeforeStartIsNoop documents that Shutdown on a never-Started
// server does not block forever waiting for a goroutine that was never
// launched.
func TestShutdownBeforeStartIsNoop(t *testing.T) {
	srv := NewMCP(MCPOptions{Logger: log.New(io.Discard, "", 0)})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start: %v", err)
	}
}

// TestShutdownWaitsForWatchShutdown closes the race that existed before the
// fix: app.Run used to return the moment its run context was canceled, while
// MCPServer's watchShutdown goroutine was still inside httpServer.Shutdown.
// We verify here that Shutdown blocks until the goroutine is done, and that
// the underlying listener is actually closed before Shutdown returns.
func TestShutdownWaitsForWatchShutdown(t *testing.T) {
	port := pickFreePort(t)
	srv := NewMCP(MCPOptions{
		Port:           port,
		Hostname:       "127.0.0.1",
		DefaultTimeout: time.Second,
		Version:        "0.3.2",
		Logger:         log.New(io.Discard, "", 0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Probe the listener once to prove it is live before we cancel.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err != nil {
		t.Fatalf("warmup dial: %v", err)
	} else {
		_ = conn.Close()
	}

	cancel() // signal watchShutdown to fire

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// After Shutdown returns, the listener must be gone. Any new connect
	// must fail rather than race the still-in-flight httpServer.Shutdown.
	dialer := net.Dialer{Timeout: time.Second}
	if conn, err := dialer.Dial("tcp", addr); err == nil {
		_ = conn.Close()
		t.Fatal("listener still accepting connections after Shutdown returned")
	}
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr = %T, want *net.TCPAddr", l.Addr())
	}
	return addr.Port
}
