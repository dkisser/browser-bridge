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
// The control plane only ever constructs status/error/message itself; data,
// reason and denied pass through from the extension. Field order matches the
// TS object literals ({status, error, message} / {status, data} and, for a
// policy rejection, {status, error, message, denied}).
//
// Denied is the structured Denial the extension attaches to every policy
// rejection. It used to be missing here while this comment claimed to mirror
// the TS type, so the whole denial was dropped at unmarshal and only the
// reason code in Error survived — leaving any consumer that branched on the
// structured denial reading nil. Error and Message still carry the
// human-readable text; Denied is there for code that needs the origin,
// capability or command rather than prose.
type ResponsePayload struct {
	Status  string          `json:"status"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Message string          `json:"message,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	Denied  *Denial         `json:"denied,omitempty"`
	// SiteNote is the learned site card the Memory hook wants appended to this
	// result (ADR-0019). It is deliberately NOT on the wire (`json:"-"`): the
	// card is presentation, and each adapter composes its own text, so putting
	// it in the payload would either change the frozen envelope (ADR-0012) or
	// be dropped by the field-by-field renderers. It rides in-process and is
	// appended by whichever executor builds the string the agent reads.
	SiteNote string `json:"-"`
}

// TextSender is the fire-and-forget text-frame sink used to route a response
// back to the connection that submitted a command (the TS code holds a
// ServerWebSocket and calls ws.send). Send must be safe for concurrent use.
type TextSender interface {
	Send(text string)
}

// MemoryHook is the control plane's optional self-learning collaborator
// (ADRs 0018-0020). The router calls it on the two paths every call takes, and
// nowhere else, so a Trace covers both Inbound adapters — the MCP server and
// the CLI — rather than only the layer that happens to own the tool definitions.
//
// The interface is deliberately split into recording and presentation. Recording
// belongs to the router because it is the one place guaranteed to see every call.
// Presentation does not: the card is text an agent reads, and the adapter that
// renders a result owns how that text is composed. Asking each adapter for the
// note keeps the injection in the same place the tool's own output is built,
// which is why no tool is added and the tools/list contract is untouched.
type MemoryHook interface {
	// RecordCommand is the outbound half of a call: what was asked, of which
	// tab, on which site, with its arguments already reduced to structural
	// facts.
	RecordCommand(envelopeID, command, host string, tabID int, args map[string]any)

	// RecordResult is the inbound half: the outcome the *extension* reported,
	// and for a snapshot the page's structural digest.
	RecordResult(envelopeID, command, host string, tabID int, payload ResponsePayload)

	// RecordRouterError is the inbound half for a payload the router
	// synthesized itself — browser offline, cannot_buffer, sw_timeout.
	//
	// Separate from RecordResult because the two are claims about different
	// things. A ResponsePayload from the extension is evidence about the page:
	// "no such element here" is a fact about the site, which is what a Site
	// card is for. A router-synthesized one is evidence about the control
	// plane's own state: the browser was not connected, or the command could
	// not be buffered. Recording them alike put six `browser_offline` results
	// into the failure tier of whatever site the tab happened to be on, and
	// that tier is injected as "Observed to fail here (do not repeat)" — a
	// claim about a site whose controls work fine. sw_timeout is worse: it
	// times out against the tab's *previous* host, so the card asserts that
	// the wrong site times out. And because a failure is one of the two ways a
	// card comes into existence, transport noise minted cards by itself.
	//
	// The distinction this preserves is the one ADR-0030 §1 demanded for the
	// error *code*; this carries it in the record kind as well, so the trace
	// keeps the diagnostics and the card keeps only what is about the site.
	RecordRouterError(envelopeID, command, host string, tabID int, payload ResponsePayload)

	// TakeSiteNote returns the text an adapter should append to the result it
	// is about to show, or "" for nothing. It yields a card at most once per
	// landing, so a second call for the same landing returns "" rather than
	// repeating it.
	//
	// host is supplied rather than derived. The control plane owns where a tab
	// is; the learning store owns what a page looked like. A store that also
	// tracked tabs would be a second, independently-updated copy of browser
	// state, and the two would drift the first time a tab was closed, renamed
	// or grouped (ADR-0014).
	TakeSiteNote(command, host string, tabID int) string
}
