// Package e2e is the Phase 1 spike test: it runs the real Go control plane
// on alternate ports (3101/3102/3103 — 3001-3003 belong to the production
// install on the maintainer machine and are never touched) and drives it
// with a protocol-exact fake extension and fake CLI.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/app"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

const (
	inboundPort = 3101
	browserPort = 3102
	mcpPort     = 3103
)

func browserBaseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", browserPort) }
func inboundWSURL() string   { return fmt.Sprintf("ws://127.0.0.1:%d", inboundPort) }
func browserWSURL() string   { return fmt.Sprintf("ws://127.0.0.1:%d", browserPort) }
func mcpURL() string         { return fmt.Sprintf("http://127.0.0.1:%d/mcp", mcpPort) }

// startApp boots the full control plane the same way cmd/bridge-core does,
// on the alternate ports, and tears it down with the test.
func startApp(t *testing.T, mutate func(*app.Config)) {
	t.Helper()
	t.Setenv("BB_HOME", t.TempDir())

	cfg := app.Config{
		InboundPort:     inboundPort,
		InboundHostname: "127.0.0.1",
		BrowserPort:     browserPort,
		BrowserHostname: "127.0.0.1",
		MCPPort:         mcpPort,
		MCPHostname:     "127.0.0.1",
		MCPTimeout:      10 * time.Second,
		Version:         "0.3.2",
		Logger:          log.New(io.Discard, "", 0),
	}
	if mutate != nil {
		mutate(&cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("app.Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("app.Run did not shut down")
		}
	})

	waitFor(t, "control plane HTTP", func() bool {
		resp, err := http.Get(browserBaseURL() + "/api/status")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	// /api/status only proves the browser server is up; the inbound and MCP
	// listeners start later in app.Run. A TCP dial is enough to prove LISTEN
	// without disturbing either protocol.
	waitFor(t, "inbound WS listener", func() bool { return tcpUp(inboundPort) })
	waitFor(t, "MCP HTTP listener", func() bool { return tcpUp(mcpPort) })
}

// tcpUp reports whether something accepts TCP connections on the port.
func tcpUp(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitFor polls cond until it holds, without fixed sleeps.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// --- fake extension ---------------------------------------------------------

// fakeExtension is a protocol-exact stand-in for the Chrome extension: it
// runs the real pairing handshake over HTTP, then connects to the browser
// port with the token as its WebSocket subprotocol.
type fakeExtension struct {
	t         *testing.T
	browserID string
	token     string
	conn      *websocket.Conn
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func pairAndConnect(t *testing.T) *fakeExtension {
	t.Helper()
	fx := &fakeExtension{t: t}

	// 1. Pairing handshake over HTTP.
	resp, err := http.Post(browserBaseURL()+"/api/pair/start", "application/json", nil)
	if err != nil {
		t.Fatalf("pair/start: %v", err)
	}
	var startBody apiEnvelope
	decodeBody(t, resp, &startBody)
	var startData struct {
		Code      string `json:"code"`
		ExpiresIn int    `json:"expiresIn"`
	}
	mustUnmarshal(t, startBody.Data, &startData)
	if startData.ExpiresIn != 5*60*1000 {
		t.Fatalf("expiresIn = %d, want 300000", startData.ExpiresIn)
	}

	confirmResp, err := http.Post(browserBaseURL()+"/api/pair/confirm", "application/json",
		strings.NewReader(fmt.Sprintf(`{"code":%q}`, startData.Code)))
	if err != nil {
		t.Fatalf("pair/confirm: %v", err)
	}
	var confirmBody apiEnvelope
	decodeBody(t, confirmResp, &confirmBody)
	var confirmData struct {
		Token string `json:"token"`
	}
	mustUnmarshal(t, confirmBody.Data, &confirmData)
	fx.token = confirmData.Token

	// 2. Learn the browserId from /api/status, like the extension popup does.
	statusResp, err := http.Get(browserBaseURL() + "/api/status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var statusBody apiEnvelope
	decodeBody(t, statusResp, &statusBody)
	var statusData struct {
		BrowserID string `json:"browserId"`
	}
	mustUnmarshal(t, statusBody.Data, &statusData)
	fx.browserID = statusData.BrowserID

	// 3. Connect with the token as the subprotocol.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, browserWSURL(), &websocket.DialOptions{
		Subprotocols: []string{fx.token},
	})
	if err != nil {
		t.Fatalf("extension dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	fx.conn = conn
	if got := conn.Subprotocol(); got != fx.token {
		t.Fatalf("negotiated subprotocol = %q, want the pairing token", got)
	}

	// 4. The server greets with a connected event.
	greeting := fx.read(t)
	if greeting.Type != protocol.TypeEvent || string(greeting.Payload) != `{"event":"connected"}` {
		t.Fatalf("greeting = %s %s, want the connected event", greeting.Type, greeting.Payload)
	}

	// 5. Register, like the extension's offscreen document does.
	fx.send(protocol.Envelope{
		ID:        protocol.NewID(),
		Type:      protocol.TypeEvent,
		BrowserID: fx.browserID,
		Payload:   json.RawMessage(fmt.Sprintf(`{"event":"register","browserId":%q}`, fx.browserID)),
		Timestamp: time.Now().UnixMilli(),
	})
	return fx
}

func (fx *fakeExtension) send(envelope protocol.Envelope) {
	fx.t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		fx.t.Fatalf("marshal envelope: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := fx.conn.Write(ctx, websocket.MessageText, data); err != nil {
		fx.t.Fatalf("extension write: %v", err)
	}
}

func (fx *fakeExtension) read(t *testing.T) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := fx.conn.Read(ctx)
	if err != nil {
		t.Fatalf("extension read: %v", err)
	}
	env, err := protocol.Decode(string(data))
	if err != nil {
		t.Fatalf("extension decode %q: %v", data, err)
	}
	return env
}

// servePageinfo reads one command and answers it with a fixed pageinfo
// payload. It returns an error instead of calling t.Fatal so it can run on a
// background goroutine; runPageinfoServer wires it to a channel.
func (fx *fakeExtension) servePageinfo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := fx.conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("extension read: %w", err)
	}
	cmd, err := protocol.Decode(string(data))
	if err != nil {
		return fmt.Errorf("extension decode %q: %w", data, err)
	}
	if cmd.Type != protocol.TypeCommand {
		return fmt.Errorf("extension got %s, want command", cmd.Type)
	}
	var payload struct {
		Command string `json:"command"`
		TabID   int    `json:"tabId"`
	}
	if uerr := json.Unmarshal(cmd.Payload, &payload); uerr != nil {
		return fmt.Errorf("command payload %s: %w", cmd.Payload, uerr)
	}
	if payload.Command != "pageinfo" || payload.TabID != 1 {
		return fmt.Errorf("command payload = %s, want pageinfo tabId 1", cmd.Payload)
	}
	resp, err := json.Marshal(protocol.Envelope{
		ID:        cmd.ID,
		Type:      protocol.TypeResponse,
		BrowserID: fx.browserID,
		Payload:   json.RawMessage(`{"status":"ok","data":{"id":1,"url":"https://example.com/","title":"Example Domain","active":true}}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if err := fx.conn.Write(ctx, websocket.MessageText, resp); err != nil {
		return fmt.Errorf("extension write: %w", err)
	}
	return nil
}

// runPageinfoServer serves one pageinfo command in the background.
func runPageinfoServer(fx *fakeExtension) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fx.servePageinfo() }()
	return done
}

// serveCommand reads one command envelope, asserts the command name and the
// given params subset, and replies with respondPayload (a raw ResponsePayload
// JSON). Like servePageinfo it reports errors on a channel-friendly return.
func (fx *fakeExtension) serveCommand(wantCommand string, wantParams map[string]any, respondPayload string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := fx.conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("extension read: %w", err)
	}
	cmd, err := protocol.Decode(string(data))
	if err != nil {
		return fmt.Errorf("extension decode %q: %w", data, err)
	}
	if cmd.Type != protocol.TypeCommand {
		return fmt.Errorf("extension got %s, want command", cmd.Type)
	}
	var payload struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	if uerr := json.Unmarshal(cmd.Payload, &payload); uerr != nil {
		return fmt.Errorf("command payload %s: %w", cmd.Payload, uerr)
	}
	if payload.Command != wantCommand {
		return fmt.Errorf("command = %q, want %q", payload.Command, wantCommand)
	}
	for key, want := range wantParams {
		if got, ok := payload.Params[key]; !ok || fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("params[%q] = %v, want %v (params %s)", key, got, want, cmd.Payload)
		}
	}
	resp, err := json.Marshal(protocol.Envelope{
		ID:        cmd.ID,
		Type:      protocol.TypeResponse,
		BrowserID: fx.browserID,
		Payload:   json.RawMessage(respondPayload),
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if err := fx.conn.Write(ctx, websocket.MessageText, resp); err != nil {
		return fmt.Errorf("extension write: %w", err)
	}
	return nil
}

// runCommandServer serves one command in the background.
func runCommandServer(fx *fakeExtension, wantCommand string, wantParams map[string]any, respondPayload string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fx.serveCommand(wantCommand, wantParams, respondPayload) }()
	return done
}

// requireServed fails the test if the background pageinfo server errored or
// never ran.
func requireServed(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pageinfo server: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("extension never served the pageinfo command")
	}
}

func (fx *fakeExtension) close(t *testing.T) {
	t.Helper()
	if err := fx.conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("extension close: %v", err)
	}
}

// --- fake CLI ---------------------------------------------------------------

// fakeCLI is a protocol-exact stand-in for the CLI on the inbound port.
type fakeCLI struct {
	t    *testing.T
	conn *websocket.Conn
}

func connectCLI(t *testing.T) *fakeCLI {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, inboundWSURL(), nil)
	if err != nil {
		t.Fatalf("cli dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	cli := &fakeCLI{t: t, conn: conn}

	greeting := cli.read(t)
	if greeting.Type != protocol.TypeEvent || string(greeting.Payload) != `{"event":"welcome"}` {
		t.Fatalf("greeting = %s %s, want the welcome event", greeting.Type, greeting.Payload)
	}
	return cli
}

func (cli *fakeCLI) read(t *testing.T) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := cli.conn.Read(ctx)
	if err != nil {
		t.Fatalf("cli read: %v", err)
	}
	env, err := protocol.Decode(string(data))
	if err != nil {
		t.Fatalf("cli decode %q: %v", data, err)
	}
	return env
}

//nolint:unparam // every caller sends pageinfo today; kept for future commands
func (cli *fakeCLI) sendCommand(t *testing.T, id, browserID, command string, tabID int) {
	t.Helper()
	env := protocol.Envelope{
		ID:        id,
		Type:      protocol.TypeCommand,
		BrowserID: browserID,
		Payload:   json.RawMessage(fmt.Sprintf(`{"command":%q,"tabId":%d,"params":{"tabId":%d}}`, command, tabID, tabID)),
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cli.conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("cli write: %v", err)
	}
}

// --- helpers ----------------------------------------------------------------

func decodeBody(t *testing.T, resp *http.Response, into *apiEnvelope) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("decode body %q: %v", data, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, data)
	}
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}

func assertPageinfoPayload(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var payload protocol.ResponsePayload
	mustUnmarshal(t, raw, &payload)
	if payload.Status != "ok" {
		t.Fatalf("payload status = %q (%s)", payload.Status, raw)
	}
	var data struct {
		ID     int    `json:"id"`
		URL    string `json:"url"`
		Title  string `json:"title"`
		Active bool   `json:"active"`
	}
	mustUnmarshal(t, payload.Data, &data)
	if data.ID != 1 || data.URL != "https://example.com/" || data.Title != "Example Domain" || !data.Active {
		t.Fatalf("pageinfo data = %s", payload.Data)
	}
}

// --- tests ------------------------------------------------------------------

// TestCommandRoundTrip drives CLI → inbound → router → extension → back.
func TestCommandRoundTrip(t *testing.T) {
	startApp(t, nil)
	fx := pairAndConnect(t)
	cli := connectCLI(t)

	served := runPageinfoServer(fx)
	cli.sendCommand(t, "cmd-1", fx.browserID, "pageinfo", 1)

	resp := cli.read(t)
	if resp.ID != "cmd-1" {
		t.Fatalf("response id = %q, want cmd-1", resp.ID)
	}
	if resp.Type != protocol.TypeResponse {
		t.Fatalf("response type = %q", resp.Type)
	}
	assertPageinfoPayload(t, resp.Payload)
	requireServed(t, served)
}

// TestBufferedCommandFlushesOnReconnect: the extension drops, a CLI command
// lands while the state is idle_wait, and the reconnecting extension picks
// the buffered command up — the 5s tolerance the router.ts comments
// describe.
func TestBufferedCommandFlushesOnReconnect(t *testing.T) {
	startApp(t, nil)
	fx := pairAndConnect(t)
	cli := connectCLI(t)

	fx.close(t)
	waitFor(t, "extension disconnect", func() bool {
		resp, err := http.Get(browserBaseURL() + "/api/status")
		if err != nil {
			return false
		}
		var body apiEnvelope
		defer func() { _ = resp.Body.Close() }()
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false
		}
		var data struct {
			HasExtension bool `json:"hasExtension"`
		}
		if err := json.Unmarshal(body.Data, &data); err != nil {
			return false
		}
		return !data.HasExtension
	})

	// The command must be buffered: nothing may come back before the
	// extension reconnects.
	cli.sendCommand(t, "cmd-buffered", fx.browserID, "pageinfo", 1)

	// Reconnect with the same pairing token; the server flushes the buffer.
	fx2 := pairAndConnect(t)
	if fx2.browserID != fx.browserID {
		t.Fatalf("browserId changed across reconnect: %q → %q", fx.browserID, fx2.browserID)
	}
	served := runPageinfoServer(fx2)

	resp := cli.read(t)
	if resp.ID != "cmd-buffered" {
		t.Fatalf("response id = %q, want cmd-buffered", resp.ID)
	}
	assertPageinfoPayload(t, resp.Payload)
	requireServed(t, served)
}

// TestBufferedCommandExpiresIntoSWTimeout: no reconnect within the buffer
// budget → the CLI gets sw_timeout. Uses a shortened buffer timeout so the
// test does not sit on the real 5s timer.
func TestBufferedCommandExpiresIntoSWTimeout(t *testing.T) {
	startApp(t, func(cfg *app.Config) { cfg.BufferTimeout = 300 * time.Millisecond })
	fx := pairAndConnect(t)
	cli := connectCLI(t)

	fx.close(t)
	waitFor(t, "extension disconnect", func() bool {
		resp, err := http.Get(browserBaseURL() + "/api/status")
		if err != nil {
			return false
		}
		var body apiEnvelope
		defer func() { _ = resp.Body.Close() }()
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false
		}
		var data struct {
			HasExtension bool `json:"hasExtension"`
		}
		if err := json.Unmarshal(body.Data, &data); err != nil {
			return false
		}
		return !data.HasExtension
	})

	cli.sendCommand(t, "cmd-expire", fx.browserID, "pageinfo", 1)

	resp := cli.read(t)
	if resp.ID != "cmd-expire" {
		t.Fatalf("response id = %q, want cmd-expire", resp.ID)
	}
	var payload protocol.ResponsePayload
	mustUnmarshal(t, resp.Payload, &payload)
	if payload.Status != "error" || payload.Error != "sw_timeout" || payload.Message != "Service worker did not wake up" {
		t.Fatalf("payload = %s, want sw_timeout", resp.Payload)
	}
}

// TestMCPPageinfo drives tools/list + tools/call pageinfo over the MCP
// Streamable HTTP endpoint.
func TestMCPPageinfo(t *testing.T) {
	startApp(t, nil)
	fx := pairAndConnect(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpURL()}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var pageinfo *mcp.Tool
	for _, tool := range tools.Tools {
		if tool.Name == "pageinfo" {
			pageinfo = tool
		}
	}
	if pageinfo == nil {
		t.Fatalf("tools/list = %v tools, want pageinfo among them", len(tools.Tools))
	}
	// The input schema must match the TS zod-derived schema (verified
	// against xsschema output).
	schemaJSON, err := json.Marshal(pageinfo.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"tab_id"`, `"timeout_ms"`, `"required":["tab_id"]`} {
		if !strings.Contains(string(schemaJSON), fragment) {
			t.Fatalf("inputSchema %s missing %s", schemaJSON, fragment)
		}
	}

	served := runPageinfoServer(fx)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "pageinfo",
		Arguments: map[string]any{"tab_id": 1},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if result.IsError {
		t.Fatalf("pageinfo returned an error: %+v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %d parts, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want text", result.Content[0])
	}
	// executePageinfo returns JSON.stringify(data, null, 2).
	var pretty map[string]any
	if err := json.Unmarshal([]byte(text.Text), &pretty); err != nil {
		t.Fatalf("tool text is not JSON: %q", text.Text)
	}
	if pretty["title"] != "Example Domain" || pretty["url"] != "https://example.com/" || pretty["active"] != true {
		t.Fatalf("tool text = %q", text.Text)
	}
	if !strings.Contains(text.Text, "\n  ") {
		t.Fatalf("tool text is not pretty-printed with 2-space indent: %q", text.Text)
	}
	requireServed(t, served)
}

// TestMCPPageinfoNoBrowser: with no extension connected, the tool surfaces
// the resolver failure as an isError text result (FastMCP's thrown-Error
// mapping).
func TestMCPPageinfoNoBrowser(t *testing.T) {
	startApp(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpURL()}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "pageinfo",
		Arguments: map[string]any{"tab_id": 1},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !result.IsError {
		t.Fatalf("pageinfo without a browser should be isError: %+v", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want text", result.Content[0])
	}
	want := "Tool 'pageinfo' execution failed: No browser connected. Start the extension/local-proxy first."
	if text.Text != want {
		t.Fatalf("tool error = %q, want %q", text.Text, want)
	}
}

// TestPairingHTTPContract exercises the HTTP surface: CORS rules, wrong-code
// attempts remaining, and the WS upgrade gate.
func TestPairingHTTPContract(t *testing.T) {
	startApp(t, nil)

	// Web origins are rejected.
	req, _ := http.NewRequest(http.MethodGet, browserBaseURL()+"/api/status", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("web origin status = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Extension origins get reflected CORS headers; preflight answers 204.
	ext := "chrome-extension://abcdefghijklmnopqrstuvwxyz"
	req, _ = http.NewRequest(http.MethodOptions, browserBaseURL()+"/api/status", nil)
	req.Header.Set("Origin", ext)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != ext {
		t.Fatalf("ACAO = %q, want %q", got, ext)
	}

	// Origin-less requests are served without CORS headers.
	resp, err = http.Get(browserBaseURL() + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO = %q, want empty", got)
	}

	// Wrong code → 401 with attemptsRemaining counting down.
	startResp, err := http.Post(browserBaseURL()+"/api/pair/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var startBody apiEnvelope
	decodeBody(t, startResp, &startBody)

	confirmResp, err := http.Post(browserBaseURL()+"/api/pair/confirm", "application/json",
		strings.NewReader(`{"code":"ZZZZZZZZ"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = confirmResp.Body.Close() }()
	if confirmResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("confirm status = %d, want 401", confirmResp.StatusCode)
	}
	var failBody struct {
		Success           bool   `json:"success"`
		Error             string `json:"error"`
		AttemptsRemaining *int   `json:"attemptsRemaining"`
	}
	if err := json.NewDecoder(confirmResp.Body).Decode(&failBody); err != nil {
		t.Fatal(err)
	}
	if failBody.Success || failBody.Error != "invalid_code" {
		t.Fatalf("confirm body = %+v", failBody)
	}
	if failBody.AttemptsRemaining == nil || *failBody.AttemptsRemaining != 4 {
		t.Fatalf("attemptsRemaining = %v, want 4", failBody.AttemptsRemaining)
	}

	// WS upgrade without a token → 403.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, upgradeResp, dialErr := websocket.Dial(ctx, browserWSURL(), nil)
	if dialErr == nil {
		t.Fatal("upgrade without token succeeded")
	}
	if upgradeResp == nil || upgradeResp.StatusCode != http.StatusForbidden {
		t.Fatalf("upgrade rejection status = %v, want 403", upgradeResp)
	}
}

// TestInboundContract: with BRIDGE_API_KEYS unset the loopback default is
// no-auth; an unknown browser id is answered browser_offline; malformed JSON
// is answered invalid_json with an empty id.
func TestInboundContract(t *testing.T) {
	startApp(t, nil)
	cli := connectCLI(t)

	// Unknown browser → browser_offline.
	cli.sendCommand(t, "cmd-unknown", "b-nonexistent", "pageinfo", 1)
	resp := cli.read(t)
	var payload protocol.ResponsePayload
	mustUnmarshal(t, resp.Payload, &payload)
	if payload.Error != "browser_offline" || payload.Message != "Browser b-nonexistent is offline" {
		t.Fatalf("payload = %s", resp.Payload)
	}

	// Malformed JSON → invalid_json with empty id.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cli.conn.Write(ctx, websocket.MessageText, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	resp = cli.read(t)
	if resp.ID != "" {
		t.Fatalf("invalid_json id = %q, want empty", resp.ID)
	}
	mustUnmarshal(t, resp.Payload, &payload)
	if payload.Error != "invalid_json" {
		t.Fatalf("payload = %s", resp.Payload)
	}

	// list_browsers event → empty registry list (no extension connected).
	data, err := json.Marshal(protocol.Envelope{
		ID:        "evt-1",
		Type:      protocol.TypeEvent,
		BrowserID: "",
		Payload:   json.RawMessage(`{"event":"list_browsers"}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	resp = cli.read(t)
	if resp.ID != "evt-1" {
		t.Fatalf("list_browsers response id = %q", resp.ID)
	}
	var listPayload struct {
		Status string            `json:"status"`
		Data   []json.RawMessage `json:"data"`
	}
	mustUnmarshal(t, resp.Payload, &listPayload)
	if listPayload.Status != "ok" {
		t.Fatalf("list_browsers payload = %s", resp.Payload)
	}
	if listPayload.Data == nil {
		t.Fatalf("list_browsers data = null, want []: %s", resp.Payload)
	}
}

// TestMCPSetBrowserThenClick drives set_browser, list_browsers and click over
// the MCP endpoint against a paired fake extension, covering the session-pin
// wiring and the command round trip end to end.
func TestMCPSetBrowserThenClick(t *testing.T) {
	startApp(t, nil)
	fx := pairAndConnect(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpURL()}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	// list_browsers shows the paired fake extension.
	listResult, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_browsers", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("list_browsers: %v", err)
	}
	listContent, ok := listResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("list_browsers content[0] = %T, want text", listResult.Content[0])
	}
	listText := listContent.Text
	if want := fmt.Sprintf("- %s (online)", fx.browserID); listText != want {
		t.Fatalf("list_browsers = %q, want %q", listText, want)
	}

	// set_browser pins this session to the fake extension's browserId.
	setResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_browser",
		Arguments: map[string]any{"browserId": fx.browserID},
	})
	if err != nil {
		t.Fatalf("set_browser: %v", err)
	}
	setContent, ok := setResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("set_browser content[0] = %T, want text", setResult.Content[0])
	}
	setText := setContent.Text
	if want := fmt.Sprintf("Browser set to %q for this session.", fx.browserID); setText != want {
		t.Fatalf("set_browser = %q, want %q", setText, want)
	}

	// click routes through the extension and falls back to the tool's own
	// success text when the response carries no message.
	served := runCommandServer(fx, "click", map[string]any{"selector": "#btn"}, `{"status":"ok"}`)
	clickResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "click",
		Arguments: map[string]any{"selector": "#btn", "tab_id": 3},
	})
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if clickResult.IsError {
		t.Fatalf("click returned an error: %+v", clickResult.Content)
	}
	clickContent, ok := clickResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("click content[0] = %T, want text", clickResult.Content[0])
	}
	if text := clickContent.Text; text != "Clicked #btn" {
		t.Fatalf("click = %q, want %q", text, "Clicked #btn")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("extension serve click: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("extension never served the click command")
	}
}
