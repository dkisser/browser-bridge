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
}

// FailureEntry is one observed failure. The signature groups repeats of the
// same mistake; Sel keeps the concrete guess because that is the part the agent
// will otherwise make again.
type FailureEntry struct {
	Signature string `json:"sig"`
	Command   string `json:"cmd,omitempty"`
	Sel       string `json:"sel,omitempty"`
	ErrCode   string `json:"err,omitempty"`
	Hint      string `json:"hint,omitempty"`
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
	Digest      *PageDigest      `json:"digest,omitempty"`
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
// recording. This is the other half of ADR-0018: `type`'s text is the user's
// keystrokes and never reaches disk, so only its length and target survive.
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
		case "url":
			if s, ok := v.(string); ok {
				out[k] = s
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
