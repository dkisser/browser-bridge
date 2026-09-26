package ws

import (
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

	"browser-bridge/internal/core"
)

// fakeServer upgrades every request to WebSocket and runs handle on the
// connection, reporting its return value on the done channel (the handler
// runs on an HTTP goroutine, so it must not call t.Fatal).
func fakeServer(t *testing.T, handle func(conn *websocket.Conn) error) (string, <-chan error) {
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

// requireServed fails the test if the handler errored or never ran.
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

func readEnvelope(conn *websocket.Conn) (core.Envelope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return core.Envelope{}, fmt.Errorf("read: %w", err)
	}
	env, err := core.Decode(string(data))
	if err != nil {
		return core.Envelope{}, fmt.Errorf("decode %q: %w", data, err)
	}
	return env, nil
}

func writeResponse(conn *websocket.Conn, id, payload string) error {
	raw, err := core.Encode(core.TypeResponse, json.RawMessage(payload), id, "")
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

func dial(t *testing.T, url string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, url)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestSendCommandRoundTrip(t *testing.T) {
	url, done := fakeServer(t, func(conn *websocket.Conn) error {
		env, err := readEnvelope(conn)
		if err != nil {
			return err
		}
		if env.Type != core.TypeCommand || env.BrowserID != "b-1" {
			return fmt.Errorf("envelope = %s to %q, want command to b-1", env.Type, env.BrowserID)
		}
		var payload core.CommandPayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return fmt.Errorf("payload %s: %w", env.Payload, err)
		}
		if payload.Command != "navigate" || payload.TabID != 7 || payload.Params["url"] != "https://example.com" {
			return fmt.Errorf("payload = %s", env.Payload)
		}
		return writeResponse(conn, env.ID, `{"status":"ok","data":{"title":"Example"}}`)
	})

	client := dial(t, url)
	env, err := client.SendCommand(context.Background(), "b-1", core.CommandPayload{
		Command: "navigate",
		TabID:   7,
		Params:  map[string]any{"url": "https://example.com"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	var payload core.ResponsePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("response payload %s: %v", env.Payload, err)
	}
	if payload.Status != "ok" || string(payload.Data) != `{"title":"Example"}` {
		t.Fatalf("payload = %s", env.Payload)
	}
	requireServed(t, done)
}

func TestRequestRoundTrip(t *testing.T) {
	url, done := fakeServer(t, func(conn *websocket.Conn) error {
		env, err := readEnvelope(conn)
		if err != nil {
			return err
		}
		if env.Type != core.TypeEvent {
			return fmt.Errorf("type = %s, want event", env.Type)
		}
		return writeResponse(conn, env.ID, `{"status":"ok","data":[]}`)
	})

	client := dial(t, url)
	env, err := client.Request(context.Background(), core.TypeEvent, map[string]any{"event": "list_browsers"}, "", 5*time.Second)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if string(env.Payload) != `{"status":"ok","data":[]}` {
		t.Fatalf("payload = %s", env.Payload)
	}
	requireServed(t, done)
}

// TestTimeout: the server never answers, so the client-side timer must fire
// with the TS client's exact timeout text.
func TestTimeout(t *testing.T) {
	tests := []struct {
		name string
		call func(c *Client) error
		want string
	}{
		{
			name: "sendCommand names the command",
			call: func(c *Client) error {
				_, err := c.SendCommand(context.Background(), "b-1", core.CommandPayload{
					Command: "navigate", Params: map[string]any{},
				}, 50*time.Millisecond)
				return err
			},
			want: "timeout: no response for command navigate within 50ms",
		},
		{
			name: "request names the envelope type",
			call: func(c *Client) error {
				_, err := c.Request(context.Background(), core.TypeEvent, map[string]any{}, "", 50*time.Millisecond)
				return err
			},
			want: "timeout: no response for event within 50ms",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			holdOpen := make(chan struct{})
			url, done := fakeServer(t, func(conn *websocket.Conn) error {
				// Read the request so the client-side write succeeds, then
				// hold the connection open until the client has timed out —
				// closing earlier would fail the pending request with
				// ErrConnectionClosed instead of the timeout text.
				if _, err := readEnvelope(conn); err != nil {
					return err
				}
				<-holdOpen
				return nil
			})
			client := dial(t, url)

			err := tt.call(client)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}

			close(holdOpen)
			requireServed(t, done)
		})
	}
}

// TestConcurrentRequestsResolveByID: responses arriving out of order still
// land on the right caller.
func TestConcurrentRequestsResolveByID(t *testing.T) {
	url, done := fakeServer(t, func(conn *websocket.Conn) error {
		first, err := readEnvelope(conn)
		if err != nil {
			return err
		}
		second, err := readEnvelope(conn)
		if err != nil {
			return err
		}
		// Answer in reverse order; each response echoes its request id.
		if err := writeResponse(conn, second.ID, `{"status":"ok","data":{"n":2}}`); err != nil {
			return err
		}
		return writeResponse(conn, first.ID, `{"status":"ok","data":{"n":1}}`)
	})
	client := dial(t, url)

	type result struct {
		n   int
		err error
	}
	results := make(chan result, 2)
	for _, n := range []int{1, 2} {
		go func() {
			env, err := client.SendCommand(context.Background(), "b-1", core.CommandPayload{
				Command: "pageinfo", Params: map[string]any{"n": n},
			}, 5*time.Second)
			if err != nil {
				results <- result{err: err}
				return
			}
			var payload struct {
				Data struct {
					N int `json:"n"`
				} `json:"data"`
			}
			if err := json.Unmarshal(env.Payload, &payload); err != nil {
				results <- result{err: err}
				return
			}
			results <- result{n: payload.Data.N}
		}()
	}
	seen := map[int]bool{}
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("SendCommand: %v", r.err)
		}
		seen[r.n] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("seen = %v, want both responses", seen)
	}
	requireServed(t, done)
}

// TestPeerCloseRejectsPending: the server drops the connection without
// answering; the pending request fails with ErrConnectionClosed.
func TestPeerCloseRejectsPending(t *testing.T) {
	url, done := fakeServer(t, func(conn *websocket.Conn) error {
		if _, err := readEnvelope(conn); err != nil {
			return err
		}
		return conn.Close(websocket.StatusGoingAway, "")
	})
	client := dial(t, url)

	_, err := client.SendCommand(context.Background(), "b-1", core.CommandPayload{
		Command: "pageinfo", Params: map[string]any{},
	}, 5*time.Second)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("error = %v, want ErrConnectionClosed", err)
	}
	requireServed(t, done)
}

// TestClientCloseRejectsPending: closing the client mid-request fails the
// pending caller instead of hanging it.
func TestClientCloseRejectsPending(t *testing.T) {
	received := make(chan struct{})
	holdOpen := make(chan struct{})
	url, done := fakeServer(t, func(conn *websocket.Conn) error {
		if _, err := readEnvelope(conn); err != nil {
			return err
		}
		close(received)
		<-holdOpen
		return nil
	})
	client := dial(t, url)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.SendCommand(context.Background(), "b-1", core.CommandPayload{
			Command: "pageinfo", Params: map[string]any{},
		}, 5*time.Second)
		errCh <- err
	}()
	// Wait until the server got the command: at that point the request is
	// registered as pending, so Close must reject it.
	<-received
	client.Close()
	if err := <-errCh; !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("error = %v, want ErrConnectionClosed", err)
	}
	close(holdOpen)
	requireServed(t, done)
}

func TestDialRefused(t *testing.T) {
	// Bind and release a port to get an address that refuses connections.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	if closeErr := lis.Close(); closeErr != nil {
		t.Fatalf("close listener: %v", closeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Dial(ctx, "ws://"+addr)
	if err == nil {
		t.Fatal("Dial to a closed port succeeded")
	}
	if !strings.Contains(err.Error(), "dial ws://") {
		t.Fatalf("error = %v, want the dial context in the message", err)
	}
}
