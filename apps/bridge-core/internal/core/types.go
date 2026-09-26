package core

import "encoding/json"

// BrowserStatus mirrors BrowserStatus in packages/shared/src/types.ts.
type BrowserStatus string

const (
	StatusOnline   BrowserStatus = "online"
	StatusIdleWait BrowserStatus = "idle_wait"
	StatusOffline  BrowserStatus = "offline"
)

// BrowserConnection is the registry list shape, mirroring BrowserConnection
// in packages/shared/src/types.ts. Field order matches the TS object so the
// JSON encoding carries the same key order.
type BrowserConnection struct {
	BrowserID string        `json:"browserId"`
	UserID    string        `json:"userId"`
	Status    BrowserStatus `json:"status"`
	LastSeen  int64         `json:"lastSeen"`
}

// CommandPayload mirrors CommandPayload in packages/shared/src/types.ts.
// Params is always sent (TS defaults it to {}), so callers must pass a
// non-nil map — a nil map would encode as null.
type CommandPayload struct {
	Command string         `json:"command"`
	TabID   int            `json:"tabId"`
	Params  map[string]any `json:"params"`
}

// ResponsePayload mirrors ResponsePayload in packages/shared/src/types.ts.
// The control plane only ever constructs status/error/message itself; data
// and reason pass through from the extension. Field order matches the TS
// object literals ({status, error, message} / {status, data}).
type ResponsePayload struct {
	Status  string          `json:"status"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Message string          `json:"message,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// TextSender is the fire-and-forget text-frame sink used to route a response
// back to the connection that submitted a command (the TS code holds a
// ServerWebSocket and calls ws.send). Send must be safe for concurrent use.
type TextSender interface {
	Send(text string)
}
