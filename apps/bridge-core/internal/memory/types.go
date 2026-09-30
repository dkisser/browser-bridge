// Package memory is the control plane's self-learning store: it records a
// structural Trace of what the agent did (ADR-0018), learns per-host Site cards
// from that Trace in the background, and injects a card back into the result of
// the call that lands the agent on a host it knows (ADR-0019).
//
// The shape follows ADR-0020 and ADR-0021: one typed append-only stream, one
// JSON file per host, no database, no third-party dependency, and a model call
// that is optional and only ever compresses (ADR-0022). Nothing in this
// package may fail a command — every hook is additive.
package memory

import (
	"net/url"
	"strings"
	"time"
)

// Record kinds in the stream (ADR-0020). The set is closed so a reader can
// tell an unknown future kind from a corrupt line.
const (
	KindCommand      = "command"
	KindResponse     = "response"
	KindRouterError  = "router_error"
	KindCardRevision = "card_revision"
	KindLearnRun     = "learn_run"
	// KindCardShown records that a card was actually handed back to an agent,
	// not merely that one existed. The difference is the whole question when
	// asking whether recall is working: a card can be learned correctly, stored
	// correctly, and still never reach an agent, and a measurement that cannot
	// tell those apart reports "the card did not help" for what is really "the
	// card was never offered". It is also the only part of the live benchmark
	// the agent cannot report honestly about itself.
	KindCardShown = "card_shown"
)

// Outcome of a call. ADR-0018's rule is literal: a thrown error is a failure,
// anything else is a success — with one observable exception, an oversized
// read, which is recorded as a soft failure (see SoftFailureThreshold).
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
	OutcomeSoft  = "soft"
)

// Tier is which part of the card an entry belongs to. The tiers exist because
// the three kinds of knowledge are not equally reliable: the site map is read
// off the page structure and needs no success signal, failures are the one
// signal that cannot be misread, and procedures inherit every false positive
// the "no error means success" rule produces.
const (
	TierMap       = "map"
	TierFailure   = "failure"
	TierProcedure = "procedure"
)

// SoftFailureThreshold is the result size at which a successful read is
// recorded as a soft failure instead. MAX_READ_RESULT_CHARS in
// packages/shared/src/constants.ts rejects reads past 100K, and ADR-0003's
// incident was exactly this: `get_html` returned 420K characters of chrome
// with no error raised, which "no error means success" would otherwise record
// as a good procedure. Two thirds of the hard limit is where a read has already
// stopped being an answer and started being a page dump.
const SoftFailureThreshold = 60_000

// TraceRecord is one line of the stream. The field names are the on-disk names;
// there is no cross-process contract here (unlike the envelope, which ADR-0012
// froze), so they are spelled out rather than abbreviated.
type TraceRecord struct {
	Kind     string         `json:"kind"`
	AtMs     int64          `json:"at"`
	Envelope string         `json:"env,omitempty"`
	Command  string         `json:"cmd,omitempty"`
	TabID    int            `json:"tab,omitempty"`
	Browser  string         `json:"browser,omitempty"`
	Host     string         `json:"host,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
	Outcome  string         `json:"out,omitempty"`
	ErrCode  string         `json:"err,omitempty"`
	ErrMsg   string         `json:"msg,omitempty"`
	ResultSz int            `json:"sz,omitempty"`
	Page     *PageDigest    `json:"page,omitempty"`
	Revision *CardRevision  `json:"rev,omitempty"`
	Run      *LearnRun      `json:"run,omitempty"`
	// ContentSz is the size of a *read's content* in characters, set only for
	// the commands whose whole purpose is returning text (`gettext`, `gethtml`).
	// It is separate from ResultSz on purpose: ResultSz is the magnitude of the
	// payload, which is the right question for "did this read return too much",
	// and the wrong question for "is this container worth reading" — a
	// screenshot's payload is a base64 image megabytes long, and ranking
	// containers by it would pin a `visual target` entry to the top of every
	// site map on every site where the agent ever took a screenshot. ContentSz
	// is the only number the site map ranks on, and it is in one unit:
	// characters of content.
	ContentSz int `json:"csz,omitempty"`
	// Entries is the number of card lines an injection carried, on
	// KindCardShown. Zero alongside a non-empty body would mean a stale notice,
	// which is a card that said "none of this matches any more" — worth telling
	// apart from a card with content.
	Entries int `json:"entries,omitempty"`
}

// CardRevision is the audit record of one automatic card update: what was
// learned, from which evidence, and what it replaced. This is what makes an
// auto-updating card reviewable and reversible (ADR-0019's consequences) — the
// reviewer diffs revisions, they do not guess.
type CardRevision struct {
	Host     string   `json:"host"`
	Revision int      `json:"rev"`
	Reason   string   `json:"reason"` // new | rebuilt | stale | manual
	Evidence []string `json:"evidence,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	AtMs     int64    `json:"at"`
}

// LearnRun is one pass of the background learner.
type LearnRun struct {
	From     int64  `json:"from"` // stream offset (line index) the pass started at
	To       int64  `json:"to"`
	Read     int    `json:"read"`
	Skipped  int    `json:"skipped"`
	Hosts    int    `json:"hosts"`
	Written  int    `json:"written"`
	Duration int64  `json:"ms"`
	Error    string `json:"error,omitempty"`
}

// MapEntry is one row of the site map: "the thing that does X is this control".
// Purpose is derived from what the agent actually did with it, never from what
// the control appears to be — an entry exists because a call succeeded on it.
type MapEntry struct {
	Purpose      string    `json:"purpose"`
	Pred         Predicate `json:"pred"`
	Ref          string    `json:"ref,omitempty"` // evidence: the ref it was observed at
	Browser      string    `json:"browser,omitempty"`
	ObservedAtMs int64     `json:"at"`
	Uses         int       `json:"uses,omitempty"`
	// Value is roughly how many characters of content came back when this
	// element was read. It is a count, never the content (ADR-0018), and it
	// exists because "a call succeeded here" is not the same question as "this
	// is the thing on the page worth reading": a folder sidebar, a toolbar and
	// the message list all return text successfully, and an agent handed those
	// three flat and unordered has been told nothing it could not have worked
	// out by trying them. Ranking by this is what turns the map from a log into
	// a shortlist.
	Value int `json:"value,omitempty"`
}

// FailureEntry is one observed failure. The signature groups repeats of the
// same mistake; Sel keeps the concrete guess because that is the part the agent
// will otherwise make again.
type FailureEntry struct {
	Signature string `json:"sig"`
	Command   string `json:"cmd,omitempty"`
	Sel       string `json:"sel,omitempty"`
	ErrCode   string `json:"err,omitempty"`
	Count     int    `json:"n"`
	Browser   string `json:"browser,omitempty"`
	LastAtMs  int64  `json:"at"`
}

// Step is one call in a procedure.
type Step struct {
	Command string `json:"cmd"`
	On      string `json:"on,omitempty"` // a ref from a snapshot, or a selector
	Note    string `json:"note,omitempty"`
}

// ProcedureEntry is a call sequence observed to succeed on a host. Only ever
// written after ProcedureCorroboration distinct successes, because the success
// signal is the weakest one available.
type ProcedureEntry struct {
	Goal      string   `json:"goal"`
	Steps     []Step   `json:"steps"`
	Successes int      `json:"ok"`
	Browser   string   `json:"browser,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
	LastAtMs  int64    `json:"at"`
}

// ProcedureCorroboration is how many distinct successful observations a
// procedure needs before it is written. One success is a coincidence often
// enough to be worth refusing.
const ProcedureCorroboration = 2

// SiteCard is the Memory for one host (CONTEXT.md).
type SiteCard struct {
	Host        string           `json:"host"`
	Revision    int              `json:"rev"`
	UpdatedAtMs int64            `json:"updatedAt"`
	CreatedAtMs int64            `json:"createdAt"`
	Map         []MapEntry       `json:"map,omitempty"`
	Failures    []FailureEntry   `json:"failures,omitempty"`
	Procedures  []ProcedureEntry `json:"procedures,omitempty"`
	// Compressed is the ADR-0022 optional model view. It is a cache of a
	// rendering, never the truth: with no key it is simply absent, and every
	// reader falls back to rendering the fields above.
	Compressed     string `json:"compressed,omitempty"`
	CompressedAtMs int64  `json:"compressedAt,omitempty"`
}

// Host is the scope key (ADR-0019): one card per host, lowercased, port and
// path dropped. Not a registrable domain — news.google.com and gmail.com are
// different operations, which is why they are different cards — and www is not
// folded away, because a host that serves a different layout is a different
// site as far as a card is concerned.
func Host(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	h := u.Hostname()
	if h == "" {
		// Not a URL at all (a bare host, or a scheme-less path).
		h = raw
		if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
	}
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "" {
		return ""
	}
	if !strings.Contains(h, ".") {
		return "" // a scheme like "about:" or a stray token is not a site
	}
	return h
}

// HostFromURL is Host with the parse failure folded in, for call sites that
// already have a URL and only care whether it named a site.
func HostFromURL(raw string) string { return Host(raw) }

// redactArgs reduces a command's params to the structural facts worth
// recording. This is the other half of ADR-0018: what the user typed never
// reaches disk, and neither does what the page said.
func redactArgs(command string, args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case "text", "value":
			// Typed content: length only.
			if s, ok := v.(string); ok {
				out[k+"_len"] = len([]rune(s))
			}
		case "url", "uri":
			if s, ok := v.(string); ok {
				out[k] = safeURL(s)
			}
		case "selector":
			// The element address, reduced to its shape. See safeSelector for
			// why this is not a formality.
			if s, ok := v.(string); ok {
				out[k] = safeSelector(s)
			}
		default:
			out[k] = v
		}
	}
	if command == "type" {
		delete(out, "value")
	}
	return out
}

// safeSelector reduces an element address to the part that generalises.
//
// A selector looks structural and is not. Two channels carry arbitrary text
// through it, and both end up in a card file that outlives the visit:
//
//   - Any attribute selector can hold a quoted literal, and literals are
//     page-derived by construction. `[data-message-subject="Standup notes"]`
//     is a guess *about* page content, and the guess is the content.
//   - `querySelectorByText` in the extension (apps/extension/src/content.ts)
//     accepts a bare string as a selector and matches it against every element's
//     textContent. So `get_text "her lawyer's private note"` is a legal call
//     whose selector *is* the prose.
//
// What is kept is the shape — tag, class, id, attribute names, combinators —
// because that is the part an agent can act on next time: "a data-message-subject
// attribute selector does not resolve here" generalises across every value of
// that attribute, where the value itself does not. A bare-text selector has no
// shape, so it reduces to the fact that it was text, which is the actionable
// part: do not look for elements by their text on this site.
//
// The trade is that a card can no longer be matched against the *exact* guess
// that failed, only against its shape. That is the better half of the deal: the
// next guess will be a different value of the same attribute, and the shape is
// what says "that will not work here either".
//
// The extension's own not-found message follows the same habit — it describes
// the page's largest containers as `#id` / `tag.class` with a character count and
// never quotes the page (describeCandidate, ADR-0003) — so the shape it returns
// is already safe and is not re-reduced here.
func safeSelector(sel string) string {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return ""
	}
	// A ref is not a selector. `@e14` is an opaque handle the extension minted
	// from DOM order: it carries no text, no attribute, and nothing about the
	// page beyond how many addressable nodes precede it. It is also the one
	// address form the whole feature is built on — a card that cannot name the
	// ref it observed is a card that cannot be checked against a later page — so
	// reducing it would break the feature to protect against nothing.
	if strings.HasPrefix(sel, "@") {
		return sel
	}
	// A bare string is text, by the querySelectorByText path. There is no
	// structure to keep.
	if !strings.ContainsAny(sel, ".#[]():>+~*=, ") {
		return "…"
	}
	var b strings.Builder
	b.Grow(len(sel))
	for i := 0; i < len(sel); {
		switch q := sel[i]; q {
		case '"', '\'':
			// Skip the whole quoted run, whatever is inside it, including
			// escapes and a quote character of the other kind.
			i++
			for i < len(sel) {
				if sel[i] == '\\' {
					i += 2
					continue
				}
				if sel[i] == q {
					i++
					break
				}
				i++
			}
			b.WriteString("…")
		default:
			b.WriteByte(q)
			i++
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// safeURL keeps the part of a URL that identifies a site and drops the part
// that can carry something the user typed.
//
// A query string is a search term, a session token, a document id or a
// redirect target depending on the site, and none of those teach the learner
// anything: every rule in this package keys off the host. Keeping the whole URL
// put all of it in a card file that lives until someone deletes the card, in
// exchange for information nothing reads. An unparseable URL is dropped
// entirely rather than stored raw, because an unparseable string is exactly the
// case where there is no reason to believe it is harmless.
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.User = nil
	return u.String()
}

// selectorOf returns the element address a command was aimed at, preferring a
// ref (which is what a snapshot-respecting agent sends) over a raw selector.
func selectorOf(args map[string]any) string {
	if args == nil {
		return ""
	}
	if s, ok := args["selector"].(string); ok {
		return s
	}
	return ""
}

func nowMs() int64 { return time.Now().UnixMilli() }
