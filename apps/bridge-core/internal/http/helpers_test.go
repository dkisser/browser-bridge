package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	nethttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
)

// capturedCommand is what the fake router saw: the decoded command payload
// plus the envelope's target browserId.
type capturedCommand struct {
	browserID string
	command   string
	tabID     int
	params    map[string]any
}

// fakeRouter implements CommandRouter: it records every inbound command and
// replies through the sender when the script says so (ok=true).
type fakeRouter struct {
	mu       sync.Mutex
	commands []capturedCommand
	script   func(c capturedCommand) (core.ResponsePayload, bool)
}

func (f *fakeRouter) HandleInboundCommand(envelope core.Envelope, sender core.TextSender) {
	var payload struct {
		Command string         `json:"command"`
		TabID   int            `json:"tabId"`
		Params  map[string]any `json:"params"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		panic(fmt.Sprintf("fake router: undecodable payload %s: %v", envelope.Payload, err))
	}
	c := capturedCommand{
		browserID: envelope.BrowserID,
		command:   payload.Command,
		tabID:     payload.TabID,
		params:    payload.Params,
	}
	f.mu.Lock()
	f.commands = append(f.commands, c)
	script := f.script
	f.mu.Unlock()
	if script == nil {
		return
	}
	response, ok := script(c)
	if !ok {
		return
	}
	raw, err := json.Marshal(response)
	if err != nil {
		panic(fmt.Sprintf("fake router: marshal response: %v", err))
	}
	text, err := core.Encode(core.TypeResponse, raw, envelope.ID, envelope.BrowserID)
	if err != nil {
		panic(fmt.Sprintf("fake router: encode response: %v", err))
	}
	sender.Send(text)
}

func (f *fakeRouter) captured() []capturedCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedCommand{}, f.commands...)
}

// fakeRegistry implements BrowserLister with a canned browser list.
type fakeRegistry struct {
	browsers []core.BrowserConnection
}

func (f fakeRegistry) ListBrowsers() []core.BrowserConnection { return f.browsers }

func onlineBrowser(id string) core.BrowserConnection {
	return core.BrowserConnection{BrowserID: id, UserID: "extension", Status: core.StatusOnline, LastSeen: 1}
}

// newTestServer builds an MCPServer with the fakes and a silent logger.
func newTestServer(router *fakeRouter, browsers []core.BrowserConnection) *MCPServer {
	return NewMCP(MCPOptions{
		Router:         router,
		Registry:       fakeRegistry{browsers: browsers},
		DefaultTimeout: 10 * time.Second,
		Version:        "0.3.2",
		Logger:         log.New(io.Discard, "", 0),
	})
}

// newTestClient mounts srv's tools over httptest (StreamableHTTP) and
// connects an initialized client session.
func newTestClient(t *testing.T, srv *MCPServer) *mcp.ClientSession {
	t.Helper()
	handler := mcp.NewStreamableHTTPHandler(func(*nethttp.Request) *mcp.Server {
		return srv.buildServer()
	}, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool invokes a tool and fails the test on transport-level errors.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	return result
}

// resultText extracts the single text content part of a result.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("content = %d parts, want 1: %+v", len(result.Content), result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want text", result.Content[0])
	}
	return text.Text
}
