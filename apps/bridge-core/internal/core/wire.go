package core

// The wire contract, restated on the Go side.
//
// packages/shared/src/types.ts is the single source of truth for the command
// names (CommandType), the per-command params, and the per-command `data`
// shapes (CommandResultMap); the extension's handlers are annotated against
// it. Go cannot import that package, so these declarations are Go's copy of
// it — and they live in core rather than in a consumer because two packages
// need them: internal/http (the MCP tools) and internal/cli (the human CLI
// decoding the same responses). Putting them in either consumer would either
// duplicate them or add a dependency edge between the two, and neither is
// worth it for a set of struct tags.
//
// This used to be the third problem: params were bare `map[string]any`
// literals and results were anonymous structs, so every wire key was a
// hand-typed string duplicated from the input arg struct's json tag with
// nothing linking the two. A rename was invisible to the compiler and to the
// tools/list golden fixture, and the CLI carried its own partial copy of the
// snapshot shape. One declaration per shape now, with the json tag as the
// declaration of the key. internal/http/command_test.go pins the marshaled
// form against the keys the extension reads.

// --- CommandPayload.params, one struct per wire shape ---
//
// Optional fields are pointers with omitempty so an absent argument is
// dropped from the JSON entirely rather than sent as null or zero — the
// extension distinguishes "not provided" from "provided as false"
// (`params.submit === true`, `params.active === true`).

// NavParams is navigate: { url, tabId }.
type NavParams struct {
	URL   string `json:"url"`
	TabID int    `json:"tabId"`
}

// TabParams is every command that only names its target tab:
// goBack / goForward / refresh / tab:close / tab:switch.
type TabParams struct {
	TabID int `json:"tabId"`
}

// BlankParams is tab:list, which the extension answers from chrome.tabs and
// reads no params from. It still marshals to `{}` rather than null.
type BlankParams struct{}

// TabNewParams is tab:new.
type TabNewParams struct {
	URL       *string `json:"url,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	AutoClose *bool   `json:"auto_close,omitempty"`
}

// SelectorTabParams is click / hover / gettext / gethtml.
type SelectorTabParams struct {
	Selector string `json:"selector"`
	TabID    int    `json:"tabId"`
}

// TypeParams is type.
type TypeParams struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	Submit   *bool  `json:"submit,omitempty"`
	TabID    int    `json:"tabId"`
}

// SelectParams is select.
type SelectParams struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
	TabID    int    `json:"tabId"`
}

// ScrollParams is scroll.
type ScrollParams struct {
	Selector string `json:"selector"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	TabID    int    `json:"tabId"`
}

// SnapshotParams is snapshot. Note max_chars is snake_case on the wire while
// its neighbours are camelCase — that is what the extension reads
// (content.ts: normalizeMaxChars(params.max_chars, filter)).
type SnapshotParams struct {
	Selector *string `json:"selector,omitempty"`
	Filter   string  `json:"filter"`
	MaxChars int     `json:"max_chars"`
	TabID    int     `json:"tabId"`
}

// ScreenshotParams is screenshot.
type ScreenshotParams struct {
	FullPage *bool `json:"fullPage,omitempty"`
	TabID    int   `json:"tabId"`
}

// WaitParams is wait:element / wait:navigation, carrying the in-page wait
// budget to the content script.
type WaitParams struct {
	Selector *string `json:"selector,omitempty"`
	Timeout  int     `json:"timeout"`
	TabID    int     `json:"tabId"`
}

// --- CommandResultMap, the entries the control plane decodes ---
//
// Only the commands whose `data` is read into a typed field are materialized:
// pageinfo and tab:list are re-indented verbatim by the MCP layer and need no
// struct, and the rest are rendered from ResponsePayload.Message. Declaring
// shapes for those would be unused code, not contract coverage.

// GettextResult is GettextResult. `text` is `string | null` on the TS side, so
// a null decodes into the nil pointer rather than an empty string.
type GettextResult struct {
	Text *string `json:"text"`
}

// GethtmlResult is GethtmlResult.
type GethtmlResult struct {
	HTML string `json:"html"`
}

// SnapshotResult is SnapshotResult in packages/shared/src/snapshot.ts. The
// MCP layer renders all five fields; the CLI needs a subset, and decoding
// the rest is free.
type SnapshotResult struct {
	Snapshot     string `json:"snapshot"`
	Truncated    bool   `json:"truncated"`
	NodesTotal   int    `json:"nodes_total"`
	NodesEmitted int    `json:"nodes_emitted"`
	Tier         int    `json:"tier"`
}

// ScreenshotResult is ScreenshotResult.
type ScreenshotResult struct {
	DataURL string `json:"dataUrl"`
}
