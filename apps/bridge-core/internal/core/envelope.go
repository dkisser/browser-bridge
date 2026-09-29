// Package core is the control plane's domain layer: the wire envelope shared
// by the WebSocket channels (3001 inbound-facing, 3002 browser-facing), the
// in-process router between inbound clients and the extension, the browser
// registry, the persisted bridge state, and the pairing handshake.
//
// The envelope mirrors the TS Envelope in packages/shared/src/types.ts. The
// wire format is frozen (ADR-0012): any change here is a protocol change and
// must stay byte-compatible with the published extension.
package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

// Type is the envelope kind.
type Type string

const (
	TypeCommand  Type = "command"
	TypeResponse Type = "response"
	TypeEvent    Type = "event"
)

// Envelope is the single message shape on both WebSocket channels.
//
// Payload is kept raw: the control plane routes envelopes without
// interpreting command payloads, so json.RawMessage is passed through
// untouched. omitempty matches the TS encoder, which drops undefined
// payloads from the JSON object entirely.
type Envelope struct {
	ID        string          `json:"id"`
	Type      Type            `json:"type"`
	BrowserID string          `json:"browserId"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp int64           `json:"timestamp"`
	// TimeoutMs is how long the *sender* is willing to wait for this
	// envelope's response, in milliseconds. Only command envelopes from an
	// inbound client set it (the CLI's global --timeout); it is what lets the
	// router's TTL backstop be raised to the caller's own deadline instead of
	// cutting a legitimate long command short at the default. A duration
	// rather than an absolute instant so it means the same thing on a
	// buffered command as on an immediately-dispatched one.
	//
	// The extension ignores it (unknown field), and the router drops it when
	// forwarding to the extension — by then the TTL is already armed.
	TimeoutMs int64 `json:"timeoutMs,omitempty"`
}

// Encode serializes a fresh envelope: an empty id gets a random UUID and
// the timestamp is milliseconds since epoch (matching TS Date.now()).
func Encode(t Type, payload json.RawMessage, id, browserID string) (string, error) {
	return EncodeWithTimeout(t, payload, id, browserID, 0)
}

// EncodeWithTimeout is Encode plus the sender's stated deadline. Only an
// inbound client that owns a timeout of its own (the CLI, over the inbound
// WebSocket) needs it; every other caller leaves it at 0, which the router
// reads as "no stated deadline, use the default backstop".
func EncodeWithTimeout(t Type, payload json.RawMessage, id, browserID string, timeout time.Duration) (string, error) {
	if id == "" {
		id = NewID()
	}
	env := Envelope{
		ID:        id,
		Type:      t,
		BrowserID: browserID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
		TimeoutMs: timeout.Milliseconds(),
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("encode envelope: %w", err)
	}
	return string(raw), nil
}

// Decode parses one envelope from the wire. Unknown fields are ignored,
// matching the TS decode (a bare cast after JSON.parse).
func Decode(raw string) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}

// NewID returns a random UUIDv4 built on crypto/rand (no dependency).
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
