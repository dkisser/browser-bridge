package core

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"
)

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

// InPageTimeoutSlack is the headroom between an in-page wait budget handed to
// the extension and the deadline a caller waits for the answer.
//
// The extension rejects with "Navigation timeout" or "Element not found
// within Nms" when its own budget expires. A caller's clock starts when the
// command is written; the extension's starts only once the router has
// delivered it. With equal budgets the caller always gives up first, so the
// one diagnostic worth seeing arrives after nobody is listening, and the
// user is told a control plane that was behaving correctly has stopped
// responding.
//
// Declared once here because two callers need it and they drifted when each
// held its own copy: the MCP tools and the CLI budget the in-page wait
// identically, so a change to one must change the other.
const InPageTimeoutSlack = 500 * time.Millisecond

// --- CommandPayload.params, one struct per wire shape ---
//
// Optional fields are pointers with omitempty so an absent argument is
// dropped from the JSON entirely rather than sent as null or zero — the
// extension distinguishes "not provided" from "provided as false"
// (`params.submit === true`, `params.active === true`).

// NavParams is navigate: { url, tabId, timeout }.
//
// Timeout is the in-page budget for the navigation to reach 'complete'. The
// extension used to wait for that event with no bound and no way to notice
// it had already fired, so a fast-loading target lost its completion event
// and the command hung until the caller gave up. The control plane sends its
// own budget (minus waitSlack) the same way it does for the wait commands.
type NavParams struct {
	URL     string `json:"url"`
	TabID   int    `json:"tabId"`
	Timeout int    `json:"timeout"`
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
	URL    *string `json:"url,omitempty"`
	Active *bool   `json:"active,omitempty"`
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
	TabID int `json:"tabId"`
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

// Denial is Denial in packages/shared/src/policy.ts: the structured form of
// a policy rejection. The extension sends it alongside the human-readable
// `error` / `message` pair; Reason repeats `error` so a consumer that only
// decodes the struct still has the code.
type Denial struct {
	Reason     string `json:"reason"`
	Command    string `json:"command"`
	Origin     string `json:"origin,omitempty"`
	Capability string `json:"capability,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// IsLandingCommand reports the commands after which the control plane can say
// which site a tab is on. navigate, goBack, goForward, refresh and tab:new /
// tab:switch / wait:navigation all answer with the URL that resulted.
//
// This lives in core, beside the rest of the wire contract, for the reason the
// file's own header gives: it is a fact about the protocol, and the consumers
// that need it are the router (which resolves a tab's host) and the learner
// (which splits an attempt when the agent declares it is going somewhere new).
// A consumer that invented its own list would drift the moment a command was
// added.
func IsLandingCommand(command string) bool {
	switch command {
	case "navigate", "goBack", "goForward", "refresh", "wait:navigation", "tab:new", "tab:switch":
		return true
	default:
		return false
	}
}

// LandingHost extracts the site a landing command arrived at, from the url in
// its result body. It returns "" when the result names no site, which is the
// normal case for chrome:// and about:blank.
func LandingHost(payload ResponsePayload) string {
	if len(payload.Data) == 0 {
		return ""
	}
	var out struct {
		URL   string `json:"url"`
		URLs  []any  `json:"urls"`
		Items []struct {
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(payload.Data, &out); err != nil {
		return ""
	}
	if out.URL != "" {
		if h := HostOf(out.URL); h != "" {
			return h
		}
	}
	// tab:new and tab:switch answer with the tab they landed on.
	for _, it := range out.Items {
		if h := HostOf(it.URL); h != "" {
			return h
		}
	}
	return ""
}

// LandedTabID reports which tab a command left the agent on, when that is not
// the tab the command was addressed to.
//
// The tab-opening and tab-switching commands both move the answer, and both
// say which tab in the result body (background.ts returns {id, url, title}).
// tab:new is the one command sent without a target tab — it is what opens the
// next one. tab:switch is sent *to* the current tab and carries its target in
// params.tabId, which the envelope's top-level tabId does not reflect: the CLI
// builds `CommandPayload{Command, TabID: g.tab, Params: params}`, so
// pendingCallFrom sees the global default (0 unless --tab was passed) and
// nothing else looks at params.
//
// Keying either result under the addressed tab put a real tab's host under key
// 0, which nothing ever asks for, and left the tab that actually exists with no
// host at all. The cost was concrete: the agent's next snapshot on that tab
// resolved no host, so the ADR-0019 second injection point — the only one that
// hands back resolved refs — never fired, and the learner attributed the
// snapshot to no site. For tab:switch it also collided every switched-to tab
// on tab 0's single tabState.
func LandedTabID(command string, payload ResponsePayload, addressed int) int {
	if command != "tab:new" && command != "tab:switch" {
		return addressed
	}
	if len(payload.Data) == 0 {
		return addressed
	}
	var out struct {
		ID *int `json:"id"`
	}
	if err := json.Unmarshal(payload.Data, &out); err != nil || out.ID == nil || *out.ID < 0 {
		return addressed
	}
	return *out.ID
}

// HostOf is the host a URL belongs to, lowercased, or "" for anything that is
// not a site (a bare scheme, a relative path, an empty string).
//
// This is the one copy of the rule. It used to be a second one, forked before
// ADR-0032 and never given the fix: it kept the old `has a dot in it` test
// while memory.Host learned that localhost, *.localhost and IP literals are
// sites. The two then disagreed on exactly the hosts ADR-0032 was written
// about — the learner happily wrote cards/localhost.json through HostFromURL
// while the router never populated tabHost, so HostForTab returned "" and
// TakeSiteNote bailed at its own guard. The card existed and was never
// injected, which is the silence ADR-0032 promised to end.
//
// memory imports core, so memory.Host delegates here rather than the other
// way round: a rule this file's header insists consumers must not each invent
// has to be the one they all call.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return HostName(u.Hostname())
}

// HostName is HostOf for a host that has already been extracted. It applies
// the same test, and the same reasoning: a registrable domain always has a
// dot, so a dotless name is a loopback name, an IP literal, or a token that is
// not a site at all. A dotless name is still not admitted in general — "about"
// and stray tokens reach here too, and every rule in the control plane keys
// off the host, so taking them would mint a card per piece of junk.
func HostName(h string) string {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" {
		return ""
	}
	if strings.Contains(h, ".") {
		return h
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return h
	}
	if net.ParseIP(h) != nil {
		return h
	}
	return ""
}
