package app

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", ln.Addr())
	}
	return addr.Port
}

// portIsFree reports whether nothing is still listening on the port. Run
// binds real listeners, so a leak shows up as the port staying taken.
func portIsFree(t *testing.T, port int) bool {
	t.Helper()
	ln, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if listenErr != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// A failure part-way through Run used to return with the servers before it
// still bound and accepting. A caller that handles the error rather than
// exiting is then holding listeners it has no handle to — and in this binary
// `bridge serve` reports the failure upward, so the process can outlive it.
//
// The port that is taken is the MCP one, so browser and inbound both start
// before the third Start fails. Both have to come back.
func TestRunReleasesEarlierServersWhenALaterStartFails(t *testing.T) {
	t.Setenv("BB_HOME", t.TempDir())

	// Hold the MCP port so the third Start cannot bind it.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy the MCP port: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	blockerAddr, ok := blocker.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", blocker.Addr())
	}
	mcpPort := blockerAddr.Port

	browserPort := freePort(t)
	inboundPort := freePort(t)

	cfg := Config{
		InboundPort:     inboundPort,
		InboundHostname: "127.0.0.1",
		BrowserPort:     browserPort,
		BrowserHostname: "127.0.0.1",
		MCPPort:         mcpPort,
		MCPHostname:     "127.0.0.1",
		MCPTimeout:      time.Second,
		Logger:          log.New(io.Discard, "", 0),
	}

	// The MCP port is occupied, so Run must give up rather than block.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Run(ctx, cfg); err == nil {
		t.Fatal("Run succeeded with the MCP port already taken; the test " +
			"cannot tell whether the earlier servers were released")
	}

	for name, port := range map[string]int{
		"browser": browserPort,
		"inbound": inboundPort,
	} {
		if !portIsFree(t, port) {
			t.Errorf("%s port %d is still bound after Run failed; a listener "+
				"leaked with no handle to it", name, port)
		}
	}
}
