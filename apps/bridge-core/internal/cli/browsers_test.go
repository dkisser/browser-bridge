package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"

	"browser-bridge/internal/core"
)

// readEvent reads one envelope and checks it is the list_browsers event
// (no browserId, event type).
func readEvent(conn *websocket.Conn) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	env, err := core.Decode(string(data))
	if err != nil {
		return "", fmt.Errorf("decode %q: %w", data, err)
	}
	if env.Type != core.TypeEvent {
		return "", fmt.Errorf("type = %s, want event", env.Type)
	}
	if env.BrowserID != "" {
		return "", fmt.Errorf("browserId = %q, want empty", env.BrowserID)
	}
	var payload struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return "", fmt.Errorf("payload %s: %w", env.Payload, err)
	}
	if payload.Event != "list_browsers" {
		return "", fmt.Errorf("event = %q, want list_browsers", payload.Event)
	}
	return env.ID, nil
}

func serveList(respondPayload string) func(conn *websocket.Conn) error {
	return func(conn *websocket.Conn) error {
		id, err := readEvent(conn)
		if err != nil {
			return err
		}
		return respond(conn, id, respondPayload)
	}
}

func TestBrowserListHuman(t *testing.T) {
	lastSeen := int64(1758000000000)
	payload := fmt.Sprintf(`{"status":"ok","data":[{"browserId":"b-1","userId":"u","status":"online","lastSeen":%d}]}`, lastSeen)
	url, done := fakeBridge(t, serveList(payload))

	stdout, stderr, err := runCLI(t, "--server", url, "browser:list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Connected browsers:\n" +
		fmt.Sprintf("  - b-1 (status: online, lastSeen: %s)\n",
			time.UnixMilli(lastSeen).Local().Format("2006-01-02 15:04:05"))
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	requireServed(t, done)
}

func TestBrowserListEmpty(t *testing.T) {
	url, done := fakeBridge(t, serveList(`{"status":"ok","data":[]}`))
	stdout, _, err := runCLI(t, "--server", url, "browser:list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "No connected browsers.\n" {
		t.Errorf("stdout = %q", stdout)
	}
	requireServed(t, done)
}

// TestBrowserListNullData: a null data payload maps to the empty list (the
// TS `?? []`).
func TestBrowserListNullData(t *testing.T) {
	url, done := fakeBridge(t, serveList(`{"status":"ok"}`))
	stdout, _, err := runCLI(t, "--server", url, "browser:list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "No connected browsers.\n" {
		t.Errorf("stdout = %q", stdout)
	}
	requireServed(t, done)
}

func TestBrowserListJSON(t *testing.T) {
	url, done := fakeBridge(t, serveList(`{"status":"ok","data":[{"browserId":"b-1","userId":"u","status":"online","lastSeen":1758000000000}]}`))
	stdout, _, err := runCLI(t, "--server", url, "--json", "browser:list")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[\n  {\n    \"browserId\": \"b-1\",\n    \"userId\": \"u\",\n    \"status\": \"online\",\n    \"lastSeen\": 1758000000000\n  }\n]\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	requireServed(t, done)
}

func TestBrowserListServerError(t *testing.T) {
	url, done := fakeBridge(t, serveList(`{"status":"error"}`))
	_, stderr, err := runCLI(t, "--server", url, "browser:list")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	if stderr != "Error: unknown\n" {
		t.Errorf("stderr = %q", stderr)
	}
	requireServed(t, done)
}

// TestBrowserListJSONError: the list_failed error kind is used in --json.
func TestBrowserListJSONError(t *testing.T) {
	url, done := fakeBridge(t, serveList(`{"status":"error","message":"boom"}`))
	stdout, stderr, err := runCLI(t, "--server", url, "--json", "browser:list")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := "{\"status\":\"error\",\"error\":\"list_failed\",\"message\":\"boom\"}\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	requireServed(t, done)
}
