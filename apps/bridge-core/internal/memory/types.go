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

	"browser-bridge/internal/core"
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
	// KindCardStale records that a card was *observed* not to match the page
	// it was verified against. It is deliberately not a KindCardRevision: the
	// card did not change, so there is no new revision to number, and the
	// revision record it used to masquerade as claimed card.Revision+1 — a
	// number the next real write would then claim with different content and a
	// different reason. `memory history` is the surface ADR-0019 points a human
	// at before accepting an automatic update, and it showed two different
	// contents under one number.
	//
	// The same reasoning as KindCardShown above: an observation and a change
	// are different facts, and a ledger that cannot tell them apart is worse
	// than one that records less.
	KindCardStale = "card_stale"
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

// SoftFailureThreshold is the size — in *characters of returned content* — at
// which a successful read is recorded as a soft failure instead.
// MAX_READ_RESULT_CHARS in packages/shared/src/constants.ts rejects reads past
// 100K, and ADR-0003's incident was exactly this: `get_html` returned 420K
// characters of chrome with no error raised, which "no error means success"
// would otherwise record as a good procedure. Two thirds of the hard limit is
// where a read has already stopped being an answer and started being a page
// dump.
//
// Characters, not bytes, and reads only. Both halves were wrong: the gate
// measured len(payload) bytes while this doc and the recorded message both said
// characters, so a non-Latin read tripped it far below the intent; and it ran
// on every command, so every screenshot's base64 data URL was filed as an
// oversized read. See the call site in RecordResult.
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
	From    int64 `json:"from"` // stream offset (line index) the pass started at
	To      int64 `json:"to"`
	Read    int   `json:"read"`
	Skipped int   `json:"skipped"`
	Hosts   int   `json:"hosts"`
	// Consumed is lines of trace, not cards written. It used to be called
	// `written` and carried the line count, which is the same one-name-two-units
	// trap ResultSz and OutcomeSoft both were: a reader reasonably counts
	// card_revision records against it and gets a different number. The cards
	// this pass wrote are the revision records in the stream, where they can be
	// counted exactly.
	Consumed int    `json:"consumed"`
	Duration int64  `json:"ms"`
	Error    string `json:"error,omitempty"`
}

// MapEntry is one row of the site map: "the thing that does X is this control".
// Purpose is derived from what the agent actually did with it, never from what
// the control appears to be — an entry exists because a call succeeded on it.
// MapEntry is one line of a site map: a predicate, what the agent did with it,
// and how well it has held up. There is deliberately no ref.
//
// ADR-0018 keeps a ref in the *trace*, where it is intrinsic — the digest is a
// ref-keyed structure and resolving a predicate hands one back. A card is not a
// trace: it is a long-lived claim about a site that is re-resolved against
// whatever page the agent is looking at now, and a stored ref is the one value
// in the file that cannot be. It used to be written "as evidence" and was read
// by nothing, which is the worst of both — see ADR-0023.
type MapEntry struct {
	Purpose      string    `json:"purpose"`
	Pred         Predicate `json:"pred"`
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
	// CompressedRev is the card revision Compressed was built from. The
	// revision rather than a timestamp, because two revisions can land inside
	// one millisecond — which a fixture run produces routinely — and a
	// timestamp comparison would then call a stale view current. A card written
	// before this field existed has 0, which never equals Revision, so it falls
	// back to the real render: the safe direction to be wrong in.
	CompressedRev int `json:"compressedRev,omitempty"`
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
	// A parse failure is not a rejection: it is the signal that this is not a
	// URL. url.Parse rejects a bare "::1" outright — "first path segment in URL
	// cannot contain colon" — which used to return "" before the fallback
	// below ever ran, so `bridge memory rm ::1` reported "not a host name" for
	// a host the store had a card for. The bare-host path is the documented
	// case; the CLI's hand-typed arguments depend on it.
	h := raw
	if u, err := url.Parse(raw); err == nil {
		if parsed := u.Hostname(); parsed != "" {
			h = parsed
		} else if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
	} else if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	// The dot / localhost / IP-literal test is core.HostName's, and it is
	// core's because core is where the router resolves a tab's host from a
	// landing result. This function used to carry its own copy of that test,
	// and the copy had drifted: it was forked before ADR-0032 and never given
	// the fix, so the learner wrote cards for localhost and ::1 through here
	// while the router — reading the same URL through core's copy — resolved
	// no host at all, left tabHost unset, and bailed out of TakeSiteNote. The
	// card existed and was never injected.
	//
	// One rule, one place. What is left here is only the part that is about
	// *this* package: accepting a bare host that url.Parse did not recognise
	// as one, which the CLI's hand-typed `bridge memory` arguments rely on.
	return core.HostName(h)
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
// hasSelectorStructure reports whether sel contains a character doing selector
// work, as opposed to prose that happens to include the same punctuation.
//
// Kept deliberately narrow. A marker counts only in the position a selector
// grammar puts it, because the characters are shared with ordinary English:
//
//   - `.` / `#` / `:` are class, id and pseudo markers only when a name
//     follows immediately. "Contact support." ends on the period, "issue #42"
//     has a digit, "Note: see below" has a space.
//   - `[` opens an attribute selector only when a name or a quote follows.
//     "see [1]" does not.
//   - `=` means an assignment only inside brackets.
//
// `+`, `~`, `*` and `>` are deliberately absent. Each is a real selector token,
// but each also appears as ordinary prose punctuation ("2 + 2", "~5 left", a
// footnote's "*", a breadcrumb's "Home > Inbox"), and every one of their
// genuinely structural uses is covered by something else: `[class*="x"]` by the
// bracket rule, `:nth-child(2n+1)` by the pseudo rule. Dropping them costs a
// bare `a + b` or `div > p` selector its shape — the same trade the paragraph
// above already accepts for descendant selectors — and buys back the prose that
// used them.
func hasSelectorStructure(sel string) bool {
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; c {
		case '.', '#', ':':
			if i+1 < len(sel) && isSelectorNameStart(sel[i+1]) {
				return true
			}
		case '[':
			if i+1 < len(sel) && (isSelectorNameStart(sel[i+1]) || sel[i+1] == '"' || sel[i+1] == '\'') {
				return true
			}
		case '=':
			if strings.LastIndexByte(sel[:i], '[') > strings.LastIndexByte(sel[:i], ']') {
				return true
			}
			// '>' is deliberately absent, for the reason '+' and '~' are: a breadcrumb
			// reads `Home > Inbox`, which is prose an agent really would pass, and
			// no flank test separates it from `div > p`. The space rule above
			// already accepts that a descendant combinator loses its shape; a child
			// combinator loses it for the same reason and by the same trade.
		}
	}
	return false
}

// isSelectorNameStart reports whether b can begin a tag, class, id or attribute
// name. A digit deliberately does not: it is what tells `#main` from the
// "issue #42" that prose produces.
func isSelectorNameStart(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '-' || b == '_'
}

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
	//
	// The test is not "does this contain a selector character" — that is what
	// it used to be, and a period ends most English sentences, so
	// `get_text "Contact support."` was classified as structural and copied
	// byte for byte into a card file that outlives the visit. The leak the
	// function documents was closed only for prose that happened to avoid a
	// period. `.`, `#` and `:` also appear in ordinary prose ("issue #42",
	// "Note: see below", "v1.2 released").
	//
	// So a marker counts only where it is doing selector work: attached to a
	// name (`.entry-content`, `#main`, `li:nth-child(2)`), opening an attribute
	// selector (`[name]`), or standing as a combinator (`div > p`). Detached,
	// the same characters are punctuation. The space is still deliberately not
	// a marker: it cannot tell "div span" from "her lawyer private note", and
	// the ambiguity has to resolve toward the side that cannot leak. A real
	// descendant selector therefore reduces to "…", which still teaches the
	// thing that matters (a bare selector did not resolve here), where prose
	// would be written down permanently.
	if !hasSelectorStructure(sel) {
		return "…"
	}
	var b strings.Builder
	b.Grow(len(sel))
	depth := 0
	for i := 0; i < len(sel); {
		switch q := sel[i]; q {
		case '[':
			depth++
			b.WriteByte(q)
			i++
		case ']':
			if depth > 0 {
				depth--
			}
			b.WriteByte(q)
			i++
		case '"', '\'':
			// A quoted run outside an assignment is a literal the selector
			// carries; there is no structure in it to keep.
			i = skipQuoted(sel, i)
			b.WriteString("…")
		case '=':
			// An attribute *value* — quoted or not. These were treated as two
			// different things and they are one: `[data-message-subject="Standup
			// notes"]` leaked its value through the quoted-run case and
			// `[data-order-id=ORD12345]` leaked it byte for byte through this
			// one, because the loop only ever looked for quotes. Both are
			// page-derived by construction — an order id, a user id, a document
			// key — and both were written to the stream and then to
			// cards/<host>.json, outliving the visit and leaving the machine
			// when the compression endpoint is configured. The doc above calls
			// the literal "a guess *about* page content, and the guess is the
			// content"; unquoted it is exactly as much the content.
			b.WriteByte('=')
			i++
			if depth > 0 {
				if i < len(sel) && (sel[i] == '"' || sel[i] == '\'') {
					i = skipQuoted(sel, i)
				} else {
					for i < len(sel) && sel[i] != ']' && sel[i] != ' ' && sel[i] != '\t' {
						i++
					}
				}
				b.WriteString("…")
			}
		default:
			b.WriteByte(q)
			i++
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// skipQuoted returns the index just past the quoted run starting at i, whatever
// the run contains — escapes, and a quote character of the other kind.
func skipQuoted(sel string, i int) int {
	q := sel[i]
	i++
	for i < len(sel) {
		if sel[i] == '\\' {
			i += 2
			continue
		}
		if sel[i] == q {
			return i + 1
		}
		i++
	}
	return i
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

// maxCommandLen bounds a stored command name. Every name on the wire is well
// under this; the bound exists so a long one cannot become a card's largest
// field by being stored instead of refused.
const maxCommandLen = 24

// safeCommand reduces a command name to the shape one can be in.
//
// The name arrives from the client with no validation — core.pendingCallFrom
// unmarshals it straight off the wire — and it was the one text channel into a
// card that every other reduction (redactArgs, errCode, safeSelector,
// attrValue, normalizeSpace) did not cover. It reaches the agent unquoted in
// failureLine and procedureLine, so a name carrying a newline could forge a
// section header and a `@eN` handle directly under the injection's trust label.
// The failure tier needs no corroboration, so one request is enough and it
// persists on disk.
//
// Lower case, digits, underscore and colon is what the wire actually uses —
// wait:element and wait:navigation carry the colon — and nothing else survives.
// Anything else becomes "unknown", which is what purposeOf already did with a
// name it did not recognise, so the loss is a purpose label rather than an
// entry.
func safeCommand(cmd string) string {
	if cmd == "" || len(cmd) > maxCommandLen {
		return "unknown"
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == ':' || c == '_' {
			continue
		}
		return "unknown"
	}
	return cmd
}

// errCode reduces a wire error to a code.
//
// Two kinds of value arrive on the `error` field. The control plane's own
// errors are already codes — browser_offline, cannot_buffer, sw_timeout, plus
// the ones the extension mints like forbidden_sender and
// bb_sensitive_field_at_execution — and those pass through untouched. The
// extension's DOM errors are `err.message`, which is prose: the not-found
// message quotes the selector back verbatim, and a bare-text selector *is* page
// text (see safeSelector). Recording that wrote the page's own words into a
// card file that outlives the visit, through the one channel the argument
// reduction above does not reach.
//
// So: a code is already the generalising part and is kept; prose is classified
// on the fixed head of the template literal that produced it (content.ts,
// background.ts, messages.ts), so the argument it embeds is never read, let
// alone stored. Anything unrecognised becomes "unknown" — an unrecognised
// failure is still a failure, and the signature only has to say that this call
// failed this way, not reproduce how it said it.
func errCode(msg string) string {
	if msg == "" {
		return ""
	}
	// Already a code: the characters a code is allowed to be made of, and
	// nothing else. A sentence cannot pass this — it has spaces, capitals or
	// punctuation in it.
	if isErrorCode(msg) {
		return msg
	}
	switch {
	case strings.HasPrefix(msg, "No element found for"),
		strings.HasPrefix(msg, "Element not found:"),
		// wait:element times out with its own wording, and it is the one
		// not-found the extension produces after waiting rather than at once.
		strings.HasPrefix(msg, "Element not found within"),
		strings.HasPrefix(msg, "Element with text not found:"):
		return "no_element"
	case strings.HasPrefix(msg, "The element matched by"):
		return "empty_element"
	case strings.HasPrefix(msg, "Invalid ref selector:"),
		strings.HasPrefix(msg, "Ref @e"):
		return "bad_ref"
	case strings.HasPrefix(msg, "Unknown DOM command:"),
		strings.HasPrefix(msg, "Unknown command:"):
		return "unknown_command"
	case msg == "Missing required tabId":
		return "missing_tab"
	default:
		return "unknown"
	}
}

// isErrorCode reports whether s is a bare identifier — lower case, digits and
// underscores — which is the shape every code on the wire already has.
func isErrorCode(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
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
