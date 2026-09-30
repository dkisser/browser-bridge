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

// portIsFree polls the port until nothing is listening on it or the budget
// runs out, and reports which.
//
// Run binds real listeners, so a leak shows up as the port staying taken — but
// the release is asynchronous: Shutdown closes the listener and returns while
// the Serve goroutine is still unwinding, and a probe issued in that window
// sees a busy port even though the code is correct. An earlier version of
// this helper listened once and returned false, which happened to pass
// because the inbound Shutdown's own work supplied the delay; that is luck,
// not a contract, and it is the kind of test that goes flaky on a slower
// machine instead of failing honestly.
//
// The difference matters for the mutation this guards: a *permanent* leak
// never frees the port, so the poll separates "not yet" from "never".
func portIsFree(t *testing.T, port int) bool {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
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

// The other half of the unwind, and the one that was unguarded.
//
// The existing test holds the MCP port, which exercises the *third* Start —
// both earlier servers are up by then, so the test says nothing about the
// second. Removing the browser unwind from the in.Start error path left all
// 212 TS and every Go test green while leaking the browser listener
// permanently: with the inbound port held instead, the browser port stayed
// bound past any reasonable wait.
//
// Two servers, two failure points, so both get a test. Writing one and
// assuming the other is how the gap survived in the first place.
func TestRunReleasesTheBrowserWhenTheInboundStartFails(t *testing.T) {
	t.Setenv("BB_HOME", t.TempDir())

	// Hold the *inbound* port, so the second Start is the one that fails and
	// the browser — the only server already listening — is the one that has
	// to be released.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy the inbound port: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	blockerAddr, ok := blocker.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", blocker.Addr())
	}

	browserPort := freePort(t)
	mcpPort := freePort(t)

	cfg := Config{
		InboundPort:     blockerAddr.Port,
		InboundHostname: "127.0.0.1",
		BrowserPort:     browserPort,
		BrowserHostname: "127.0.0.1",
		MCPPort:         mcpPort,
		MCPHostname:     "127.0.0.1",
		MCPTimeout:      time.Second,
		Logger:          log.New(io.Discard, "", 0),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Run(ctx, cfg); err == nil {
		t.Fatal("Run succeeded with the inbound port already taken; the test " +
			"cannot tell whether the browser server was released")
	}

	if !portIsFree(t, browserPort) {
		t.Errorf("browser port %d is still bound after the inbound Start failed; "+
			"Run returned with a listener nobody holds a handle to", browserPort)
	}
}
