// Package protocol defines the wire envelope shared by the control plane's
// WebSocket channels (3001 inbound-facing, 3002 browser-facing).
//
// It mirrors the TS Envelope in packages/shared/src/types.ts. The wire
// format is frozen (ADR-0012): any change here is a protocol change and
// must stay byte-compatible with the published extension.
package protocol

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
}

// Encode serializes a fresh envelope: an empty id gets a random UUID and
// the timestamp is milliseconds since epoch (matching TS Date.now()).
func Encode(t Type, payload json.RawMessage, id, browserID string) (string, error) {
	if id == "" {
		id = NewID()
	}
	env := Envelope{
		ID:        id,
		Type:      t,
		BrowserID: browserID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
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
