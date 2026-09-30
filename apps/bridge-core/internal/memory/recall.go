package memory

import (
	"fmt"
	"strings"
)

// Recall is ADR-0019: the card goes back to the agent in-band, on the result of
// the call that lands it on a known host, labelled so the agent can tell
// learned site knowledge from something the tool itself said.
//
// The two things that keep this cheap are both here. The injection rides on a
// result that already exists, so a wrong card costs tokens and never fails a
// command. And verification is free, because the resolve function is handed the
// snapshot the agent was going to take anyway.

// InjectionLabel is the marker the card is appended under. It is deliberately a
// label rather than prose: the agent has to be able to tell this apart from the
// tool's own output, because the card is a claim about a site that may be out
// of date, not a fact about this page.
const InjectionLabel = "可参考的站点访问模式"

// DefaultInjectTokens is the hard cap from ADR-0019. Past a few hundred tokens
// the injection costs more context than the card saves, so the tiers are
// trimmed in reverse order of trust: procedures (weakest signal) go first, then
// failures, and the site map is kept because it is the only tier that needs no
// success signal to be true.
const DefaultInjectTokens = 400

// RenderOptions controls one rendering. Resolver is the fingerprint check: give
// it the snapshot that was fetched anyway and each entry reports the ref it
// resolves to *now*, or nothing at all.
type RenderOptions struct {
	MaxTokens int
	BrowserID string
	Resolver  func(Predicate) (string, bool)
	// Compressed prefers the ADR-0022 model view when one exists.
	Compressed bool
}

func defaultRenderOptions() RenderOptions {
	return RenderOptions{MaxTokens: DefaultInjectTokens, Compressed: true}
}

// resolved is one entry as it will be shown: the fresh ref, or the predicate
// alone when there is no snapshot to check against.
type resolved struct {
	text string
	// ok is false when the entry's predicate no longer matches the page. It is
	// dropped from the injection and reported as staleness instead.
	ok  bool
	ref string
}

// RenderCard produces the text injected after a call result, or "" when there
// is nothing worth saying.
func RenderCard(card *SiteCard, opts RenderOptions) string {
	if card == nil {
		return ""
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = DefaultInjectTokens
	}

	if opts.Compressed && card.Compressed != "" {
		if out := finish(opts, card.Host, card.Compressed, false); out != "" {
			return out
		}
	}

	stale := 0
	var lines []string

	// Tier 1: the site map. Resolve every predicate against the live page so
	// the agent gets a ref from *this* snapshot, never a stored one. The header
	// is only emitted if an entry survived: a section that says "here is what I
	// know" and then lists nothing is worse than saying nothing, because it
	// reads as though something was withheld.
	//
	// The header also only claims verification when a page was actually checked.
	// `bridge memory show` renders with no page in hand, and telling a reader
	// those notes were "verified against this page" when no page was involved
	// is exactly the kind of unearned confidence this feature exists to remove.
	mapHeader := "Site map:"
	if opts.Resolver != nil {
		mapHeader = "Site map (checked against this page):"
	}
	var mapLines []string
	for _, e := range card.Map {
		if opts.BrowserID != "" && e.Browser != "" && e.Browser != opts.BrowserID {
			continue // observed under a different browser profile (ADR-0019)
		}
		p := resolveEntry(e.Pred, opts.Resolver)
		if !p.ok {
			stale++
			continue
		}
		mapLines = append(mapLines, fmt.Sprintf("  - %s → %s%s", e.Purpose, p.ref, seenNote(e)))
	}
	if len(mapLines) > 0 {
		lines = append(lines, mapHeader)
		lines = append(lines, mapLines...)
	}

	// Tier 2: failures. Cheap, and the most reliable thing on the card.
	var failLines []string
	for _, f := range card.Failures {
		failLines = append(failLines, "  - "+failureLine(f))
	}
	if len(failLines) > 0 {
		lines = append(lines, "Observed to fail here (do not repeat):")
		lines = append(lines, failLines...)
	}

	// Tier 3: procedures, and only the corroborated ones. A candidate seen once
	// is on the card (that is how the count survives a restart) but stays
	// hidden: it is the one tier whose evidence is a "no error", and a single
	// no-error is exactly the false positive ADR-0003's 420K get_html would
	// have produced. Trimmed first, so it is built last and dropped first.
	procIdx := -1
	var shown []ProcedureEntry
	for _, p := range card.Procedures {
		if p.Successes >= ProcedureCorroboration {
			shown = append(shown, p)
		}
	}
	if len(shown) > 0 {
		procIdx = len(lines)
		lines = append(lines, "Sequences that worked here:")
		for _, p := range shown {
			lines = append(lines, "  - "+procedureLine(p))
		}
	}

	// Trim until it fits, dropping the weakest tier first.
	for {
		body := joinLines(lines)
		if estimateTokens(body) <= opts.MaxTokens {
			break
		}
		if procIdx >= 0 && procIdx < len(lines) {
			lines = lines[:procIdx]
			procIdx = -1
			continue
		}
		// Nothing left to drop by tier: shed individual lines from the end of
		// the list until it fits. A truncated card is still useful; an
		// oversized one costs the context it was meant to save.
		if len(lines) <= 1 {
			break
		}
		lines = lines[:len(lines)-1]
	}

	if len(lines) == 0 {
		// Everything the card claimed is gone. Saying so is more useful than
		// saying nothing: the agent should fall back to a fresh snapshot.
		if stale > 0 {
			return finish(opts, card.Host, "This card no longer matches the page; take a fresh snapshot and do not trust these notes.", true)
		}
		return ""
	}
	return finish(opts, card.Host, joinLines(lines), stale > 0)
}

func finish(opts RenderOptions, host, body string, stale bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s\n", InjectionLabel, host)
	b.WriteString(body)
	if stale {
		fmt.Fprintf(&b, "\n  (some notes above no longer match this page — re-check with a snapshot)")
	}
	out := b.String()
	if estimateTokens(out) > opts.MaxTokens+16 {
		// The header alone should not blow the budget; if the body still does,
		// cut it hard rather than truncate mid-line.
		out = truncateLines(out, opts.MaxTokens+16)
	}
	return out
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func resolveEntry(pred Predicate, resolver func(Predicate) (string, bool)) resolved {
	if resolver == nil {
		// No page to check against: show the predicate, not a ref we cannot
		// vouch for. An unverified ref would be a guess wearing a learned coat.
		return resolved{text: pred.String(), ok: true, ref: pred.String()}
	}
	ref, ok := resolver(pred)
	if !ok {
		return resolved{ok: false}
	}
	return resolved{ok: true, ref: "@" + strings.TrimPrefix(ref, "@")}
}

// seenNote reports how often a call succeeded on this control. The predicate is
// deliberately not repeated here: when there is no page to resolve against, the
// line already shows the predicate as its target.
func seenNote(e MapEntry) string {
	if e.Uses > 1 {
		return fmt.Sprintf(" (%dx)", e.Uses)
	}
	return ""
}

func failureLine(f FailureEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s", f.Signature, orAny(f.Command, "this page"))
	if f.Sel != "" {
		fmt.Fprintf(&b, " with %q", f.Sel)
	}
	if f.Count > 1 {
		fmt.Fprintf(&b, " (%dx)", f.Count)
	}
	return b.String()
}

// procedureLine renders the command sequence only.
//
// Two things are deliberately dropped. The Goal is derived from the same
// sequence (`goalOf` is a function of it), so prefixing it would state the
// sequence twice. And a step's recorded ref is dropped because it is a ref from
// *some earlier snapshot*: it addresses nothing on the page the agent is looking
// at now. The site map is where a usable handle comes from, and it is resolved
// against the live page above. A procedure says what order worked; the map says
// where the things are.
func procedureLine(p ProcedureEntry) string {
	parts := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		parts = append(parts, s.Command)
	}
	return strings.Join(parts, " → ")
}

func orAny(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// estimateTokens is a deliberately cheap approximation. Latin text runs about
// four characters to a token; a CJK character is close to one token on its own.
// This is used only to enforce a budget, so being within a few percent is
// fine — being predictable is what matters, since an estimator that drifts
// between runs would make the cap untestable.
func estimateTokens(s string) int {
	tokens := 0
	asciiRun := 0
	flush := func() {
		tokens += asciiRun / 4
		asciiRun = 0
	}
	for _, r := range s {
		if r < 0x80 {
			asciiRun++
			continue
		}
		flush()
		tokens++
	}
	flush()
	return tokens
}

func truncateLines(s string, maxTokens int) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 1 && estimateTokens(strings.Join(lines, "\n")) > maxTokens {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n…"
}

// RenderStaleNotice is the short form used by the verification pass: the card
// was already shown at the landing, and this says only that it no longer fits
// the page. A note that stayed silent while it still matched is what makes
// speaking up here worth the tokens.
func RenderStaleNotice(host string) string {
	return fmt.Sprintf("[%s] %s\nThese notes no longer match this page — take a fresh snapshot and treat the earlier ones as out of date.",
		InjectionLabel, host)
}

// VerifyCard reports how many of a card's entries still match the page, and the
// fresh refs for the ones that do. The injection path uses the refs; the
// learner uses the count to decide a card is stale enough to rebuild.
func VerifyCard(card *SiteCard, digest *PageDigest, browserID string) (refs map[string]string, missing int) {
	refs = make(map[string]string)
	if card == nil {
		return refs, 0
	}
	if digest == nil {
		return refs, len(card.Map)
	}
	for _, e := range card.Map {
		if browserID != "" && e.Browser != "" && e.Browser != browserID {
			continue
		}
		ref, ok := digest.Resolve(e.Pred)
		if !ok {
			missing++
			continue
		}
		refs[e.Pred.String()] = "@" + strings.TrimPrefix(ref, "@")
	}
	return refs, missing
}
