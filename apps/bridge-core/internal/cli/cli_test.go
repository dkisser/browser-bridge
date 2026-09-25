package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

// runCLI executes the root command with args and captures both streams.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := New("test")
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// fakeBridge upgrades every request to WebSocket and runs handle on the
// connection, reporting its return value on the done channel (the handler
// runs on an HTTP goroutine, so it must not call t.Fatal).
func fakeBridge(t *testing.T, handle func(conn *websocket.Conn) error) (string, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			done <- fmt.Errorf("accept: %w", err)
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		done <- handle(conn)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), done
}

func requireServed(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server handler: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server handler did not finish")
	}
}

// readCommand reads one envelope and checks it against what the CLI should
// send: type command, browser b-1, the given command/tabId/params.
func readCommand(conn *websocket.Conn, wantCommand string, wantTabID int, wantParams map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	env, err := protocol.Decode(string(data))
	if err != nil {
		return "", fmt.Errorf("decode %q: %w", data, err)
	}
	if env.Type != protocol.TypeCommand {
		return "", fmt.Errorf("type = %s, want command", env.Type)
	}
	if env.BrowserID != "b-1" {
		return "", fmt.Errorf("browserId = %q, want b-1", env.BrowserID)
	}
	var payload protocol.CommandPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return "", fmt.Errorf("payload %s: %w", env.Payload, err)
	}
	if payload.Command != wantCommand {
		return "", fmt.Errorf("command = %q, want %q", payload.Command, wantCommand)
	}
	if payload.TabID != wantTabID {
		return "", fmt.Errorf("tabId = %d, want %d", payload.TabID, wantTabID)
	}
	got, _ := json.Marshal(payload.Params)
	want, _ := json.Marshal(wantParams)
	if string(got) != string(want) {
		return "", fmt.Errorf("params = %s, want %s", got, want)
	}
	return env.ID, nil
}

func respond(conn *websocket.Conn, id, payload string) error {
	raw, err := protocol.Encode(protocol.TypeResponse, json.RawMessage(payload), id, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// serveCommand checks one command envelope and replies with respondPayload.
func serveCommand(wantCommand string, wantTabID int, wantParams map[string]any, respondPayload string) func(conn *websocket.Conn) error {
	return func(conn *websocket.Conn) error {
		id, err := readCommand(conn, wantCommand, wantTabID, wantParams)
		if err != nil {
			return err
		}
		return respond(conn, id, respondPayload)
	}
}

// TestCommandConstruction drives every browser subcommand against the fake
// server and checks the wire command, tab id, and params.
func TestCommandConstruction(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCmd    string
		wantParams map[string]any
		// jsonMode runs with --json so the bespoke human output of snapshot
		// does not fight the shared compact-JSON assertion.
		jsonMode bool
	}{
		{name: "navigate", args: []string{"navigate", "https://example.com"}, wantCmd: "navigate", wantParams: map[string]any{"url": "https://example.com"}},
		{name: "go-back", args: []string{"go-back"}, wantCmd: "goBack", wantParams: map[string]any{}},
		{name: "goBack alias", args: []string{"goBack"}, wantCmd: "goBack", wantParams: map[string]any{}},
		{name: "go-forward", args: []string{"go-forward"}, wantCmd: "goForward", wantParams: map[string]any{}},
		{name: "goForward alias", args: []string{"goForward"}, wantCmd: "goForward", wantParams: map[string]any{}},
		{name: "refresh", args: []string{"refresh"}, wantCmd: "refresh", wantParams: map[string]any{}},
		{name: "tab:list", args: []string{"tab:list"}, wantCmd: "tab:list", wantParams: map[string]any{}},
		{name: "tab:new without url", args: []string{"tab:new"}, wantCmd: "tab:new", wantParams: map[string]any{}},
		{name: "tab:new with url", args: []string{"tab:new", "https://example.com"}, wantCmd: "tab:new", wantParams: map[string]any{"url": "https://example.com"}},
		{name: "tab:close", args: []string{"tab:close", "5"}, wantCmd: "tab:close", wantParams: map[string]any{"tabId": float64(5)}},
		{name: "tab:switch", args: []string{"tab:switch", "5"}, wantCmd: "tab:switch", wantParams: map[string]any{"tabId": float64(5)}},
		{name: "click", args: []string{"click", "#btn"}, wantCmd: "click", wantParams: map[string]any{"selector": "#btn"}},
		{name: "type", args: []string{"type", "#input", "hello"}, wantCmd: "type", wantParams: map[string]any{"selector": "#input", "text": "hello"}},
		{name: "select", args: []string{"select", "#dd", "v1"}, wantCmd: "select", wantParams: map[string]any{"selector": "#dd", "value": "v1"}},
		{name: "scroll", args: []string{"scroll", "100", "200"}, wantCmd: "scroll", wantParams: map[string]any{"selector": "page", "x": float64(100), "y": float64(200)}},
		{name: "hover", args: []string{"hover", "#btn"}, wantCmd: "hover", wantParams: map[string]any{"selector": "#btn"}},
		{name: "gettext", args: []string{"gettext", "#btn"}, wantCmd: "gettext", wantParams: map[string]any{"selector": "#btn"}},
		{name: "gethtml", args: []string{"gethtml", "#btn"}, wantCmd: "gethtml", wantParams: map[string]any{"selector": "#btn"}},
		{name: "screenshot", args: []string{"screenshot"}, wantCmd: "screenshot", wantParams: map[string]any{}},
		{name: "pageinfo", args: []string{"pageinfo"}, wantCmd: "pageinfo", wantParams: map[string]any{}},
		{name: "snapshot bare", args: []string{"snapshot"}, wantCmd: "snapshot", wantParams: map[string]any{}, jsonMode: true},
		{name: "snapshot full options", args: []string{"snapshot", "--selector", "#app", "--filter", "full", "--max-chars", "5000"}, wantCmd: "snapshot", wantParams: map[string]any{"selector": "#app", "filter": "full", "max_chars": float64(5000)}, jsonMode: true},
		{name: "snapshot filter interactive is omitted", args: []string{"snapshot", "--filter", "interactive"}, wantCmd: "snapshot", wantParams: map[string]any{}, jsonMode: true},
		{name: "wait:element default timeout", args: []string{"wait:element", "#x"}, wantCmd: "wait:element", wantParams: map[string]any{"selector": "#x", "timeout": float64(10000)}},
		{name: "wait:element local timeout", args: []string{"wait:element", "#x", "--timeout", "500"}, wantCmd: "wait:element", wantParams: map[string]any{"selector": "#x", "timeout": float64(500)}},
		{name: "wait:navigation", args: []string{"wait:navigation"}, wantCmd: "wait:navigation", wantParams: map[string]any{"timeout": float64(10000)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, done := fakeBridge(t, serveCommand(tt.wantCmd, 7, tt.wantParams, `{"status":"ok","data":{"ok":true}}`))
			args := []string{"--server", url, "--browser", "b-1", "--tab", "7"}
			if tt.jsonMode {
				args = append(args, "--json")
			}
			args = append(args, tt.args...)
			stdout, stderr, err := runCLI(t, args...)
			if err != nil {
				t.Fatalf("Execute: %v (stderr %q)", err, stderr)
			}
			want := "{\"ok\":true}\n"
			if tt.jsonMode {
				want = "{\n  \"ok\": true\n}\n"
			}
			if stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			requireServed(t, done)
		})
	}
}

func TestJSONOutput(t *testing.T) {
	url, done := fakeBridge(t, serveCommand("pageinfo", 0, map[string]any{}, `{"status":"ok","data":{"id":1,"url":"https://example.com/"}}`))
	stdout, stderr, err := runCLI(t, "--server", url, "--browser", "b-1", "--json", "pageinfo")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "{\n  \"id\": 1,\n  \"url\": \"https://example.com/\"\n}\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	requireServed(t, done)
}

// TestHumanStringOutput: a string data payload prints raw (console.log of a
// string), not as a JSON literal.
func TestHumanStringOutput(t *testing.T) {
	url, done := fakeBridge(t, serveCommand("gettext", 0, map[string]any{"selector": "#x"}, `{"status":"ok","data":"just text"}`))
	stdout, _, err := runCLI(t, "--server", url, "--browser", "b-1", "gettext", "#x")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "just text\n" {
		t.Errorf("stdout = %q, want the raw string", stdout)
	}
	requireServed(t, done)
}

// TestNoDataBecomesStatusOK mirrors the TS `payload.data ?? {status:'ok'}`.
func TestNoDataBecomesStatusOK(t *testing.T) {
	url, done := fakeBridge(t, serveCommand("refresh", 0, map[string]any{}, `{"status":"ok"}`))
	stdout, _, err := runCLI(t, "--server", url, "--browser", "b-1", "refresh")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "{\"status\":\"ok\"}\n" {
		t.Errorf("stdout = %q", stdout)
	}
	requireServed(t, done)
}

// TestServerErrorMapping: message wins over error, the TS
// `payload.message ?? payload.error ?? 'Unknown error'`.
func TestServerErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantErr  string
		wantJSON string
	}{
		{
			name:     "message preferred",
			payload:  `{"status":"error","error":"command_error","message":"It failed"}`,
			wantErr:  "Error: It failed\n",
			wantJSON: "{\"status\":\"error\",\"error\":\"command_failed\",\"message\":\"It failed\"}\n",
		},
		{
			name:     "error field fallback",
			payload:  `{"status":"error","error":"No tab with id: 0"}`,
			wantErr:  "Error: No tab with id: 0\n",
			wantJSON: "{\"status\":\"error\",\"error\":\"command_failed\",\"message\":\"No tab with id: 0\"}\n",
		},
		{
			name:     "neither field",
			payload:  `{"status":"error"}`,
			wantErr:  "Error: Unknown error\n",
			wantJSON: "{\"status\":\"error\",\"error\":\"command_failed\",\"message\":\"Unknown error\"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, done := fakeBridge(t, serveCommand("pageinfo", 0, map[string]any{}, tt.payload))
			stdout, stderr, err := runCLI(t, "--server", url, "--browser", "b-1", "pageinfo")
			if !errors.Is(err, ErrReported) {
				t.Fatalf("err = %v, want ErrReported", err)
			}
			if stderr != tt.wantErr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantErr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			requireServed(t, done)

			// --json mode: the error object goes to stdout, nothing to stderr.
			url, done = fakeBridge(t, serveCommand("pageinfo", 0, map[string]any{}, tt.payload))
			stdout, stderr, err = runCLI(t, "--server", url, "--browser", "b-1", "--json", "pageinfo")
			if !errors.Is(err, ErrReported) {
				t.Fatalf("err = %v, want ErrReported", err)
			}
			if stdout != tt.wantJSON {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantJSON)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			requireServed(t, done)
		})
	}
}

// TestMissingBrowser: the --browser check fires before any dial, so a bogus
// server URL proves no connection was attempted.
func TestMissingBrowser(t *testing.T) {
	stdout, stderr, err := runCLI(t, "--server", "ws://127.0.0.1:1", "pageinfo")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	if stderr != "Error: Required: --browser <id>\n" {
		t.Errorf("stderr = %q", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestConnectFailure(t *testing.T) {
	// Bind and release a port to get an address that refuses connections.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	if closeErr := lis.Close(); closeErr != nil {
		t.Fatalf("close listener: %v", closeErr)
	}

	server := "ws://" + addr
	_, stderr, err := runCLI(t, "--server", server, "--browser", "b-1", "pageinfo")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := fmt.Sprintf("Error: Could not connect to the bridge server at %s. Is the service running? Start it with: bridge service up\n", server)
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestCommandTimeout(t *testing.T) {
	holdOpen := make(chan struct{})
	url, done := fakeBridge(t, func(conn *websocket.Conn) error {
		if _, err := readCommand(conn, "pageinfo", 0, map[string]any{}); err != nil {
			return err
		}
		<-holdOpen
		return nil
	})
	_, stderr, err := runCLI(t, "--server", url, "--browser", "b-1", "--timeout", "50", "pageinfo")
	close(holdOpen)
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	if stderr != "Error: timeout: no response for command pageinfo within 50ms\n" {
		t.Errorf("stderr = %q", stderr)
	}
	requireServed(t, done)
}

// TestSnapshotHumanOutput: the snapshot command prints the tree plus the
// stats line, not the raw JSON.
func TestSnapshotHumanOutput(t *testing.T) {
	payload := `{"status":"ok","data":{"snapshot":"@e1 button [Submit]","nodes_emitted":1,"nodes_total":9,"tier":0,"truncated":false}}`
	url, done := fakeBridge(t, serveCommand("snapshot", 0, map[string]any{}, payload))
	stdout, _, err := runCLI(t, "--server", url, "--browser", "b-1", "snapshot")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "@e1 button [Submit]\n[nodes: 1/9 | tier: 0 | truncated: false]\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	requireServed(t, done)
}

// TestSnapshotJSONOutput: --json prints the whole SnapshotResult object.
func TestSnapshotJSONOutput(t *testing.T) {
	payload := `{"status":"ok","data":{"snapshot":"@e1 button [Submit]","nodes_emitted":1,"nodes_total":9,"tier":0,"truncated":false}}`
	url, done := fakeBridge(t, serveCommand("snapshot", 0, map[string]any{}, payload))
	stdout, _, err := runCLI(t, "--server", url, "--browser", "b-1", "--json", "snapshot")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "{\n" +
		"  \"snapshot\": \"@e1 button [Submit]\",\n" +
		"  \"nodes_emitted\": 1,\n" +
		"  \"nodes_total\": 9,\n" +
		"  \"tier\": 0,\n" +
		"  \"truncated\": false\n" +
		"}\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	requireServed(t, done)
}

func TestInvalidTabID(t *testing.T) {
	_, stderr, err := runCLI(t, "--server", "ws://127.0.0.1:1", "--browser", "b-1", "tab:close", "abc")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	if stderr != "Error: invalid tabId \"abc\"\n" {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestBridgeHost: the reserved stub errors out exactly like the TS one.
func TestBridgeHost(t *testing.T) {
	_, stderr, err := runCLI(t, "bridge-host")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := "bridge-host: not yet implemented. See docs/superpowers/specs/2026-06-15-distribution-design.md\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
