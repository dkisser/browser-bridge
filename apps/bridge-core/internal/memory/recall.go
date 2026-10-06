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
	// OnlyMap renders just the site map, for the second injection point. The
	// landing already said what failed and what worked, moments earlier and in
	// the same task, so repeating it on the snapshot is tokens spent saying the
	// same thing twice.
	OnlyMap bool
	// PageNote names the page a Resolver resolved against. Empty means "the page
	// in hand", which is true of every injection: the resolver is a digest the
	// control plane just fetched, so the default header can claim it.
	//
	// The offline diagnostic passes the provenance of the snapshot it read out of
	// the trace instead, and it has to. "Checked against this page" is a claim
	// about the browser's state right now; a rendering produced from a file
	// cannot make that claim, and the one reader who needs to be able to tell
	// the two apart is a human deciding whether to trust the card (ADR-0026).
	PageNote string
}

// resolved is one entry as it will be shown: the fresh ref from the page in
// hand, or ok=false when the entry's predicate no longer matches — in which
// case it is dropped from the injection and counted as staleness instead.
type resolved struct {
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

	// The compressed view is a cache of a render, so it is only a substitute for
	// the card while it still describes the card. A revision written after the
	// last compression makes it stale, and serving it would hide whatever the
	// newest evidence said — which is how a newly learned failure can go missing
	// from every subsequent injection without a single error anywhere.
	if opts.Compressed && card.Compressed != "" && card.CompressedRev == card.Revision {
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
	// The site map is only rendered when there is a live page to resolve it
	// against. Without one the only thing available to print is the predicate
	// and, at worse, a ref remembered from some earlier snapshot — and a ref the
	// agent cannot act on is worse than no ref at all, because it looks
	// actionable. (An earlier version printed stored refs here; the end-to-end
	// test caught it resolving a landing against the *previous* visit's digest.)
	//
	// So the two injection points carry different halves: the landing carries
	// what is true regardless of the page — the failures and the working
	// sequences — and the first snapshot carries the map, because that is the
	// call where a ref belonging to *this* page can be produced for free.
	if opts.Resolver != nil {
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
			// The name comes along because a map with several "text container →
			// @eN" lines and no labels is a list the agent cannot act on: it does
			// not know which ref is which. The budget guard below still trims the
			// whole section if the card is enormous.
			label := e.Pred.String()
			if e.Pred.Name == "" {
				label = e.Pred.Role
			}
			mapLines = append(mapLines, fmt.Sprintf("  - %s · %s → %s%s", e.Purpose, label, p.ref, seenNote(e)))
		}
		if len(mapLines) > 0 {
			lines = append(lines, "Site map ("+mapNote(opts)+"):")
			lines = append(lines, mapLines...)
		}
	}

	// Tier 2: failures. Cheap, and the most reliable thing on the card.
	var failLines []string
	if !opts.OnlyMap {
		for _, f := range card.Failures {
			failLines = append(failLines, "  - "+failureLine(f))
		}
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
	if !opts.OnlyMap {
		for _, p := range card.Procedures {
			if p.Successes >= ProcedureCorroboration {
				shown = append(shown, p)
			}
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
		// The tense matters for the same reason the map header's does: a card
		// rendered offline is reporting that its entries did not match *that*
		// page, not that they stopped matching *this* one. The advice is the
		// same either way, so only the claim needs rewording.
		if opts.PageNote != "" {
			fmt.Fprintf(&b, "\n  (some notes above did not match that page — take a fresh snapshot to re-check)")
		} else {
			fmt.Fprintf(&b, "\n  (some notes above no longer match this page — re-check with a snapshot)")
		}
	}
	out := b.String()
	if estimateTokens(out) > opts.MaxTokens+16 {
		// The header alone should not blow the budget; if the body still does,
		// cut it hard rather than truncate mid-line.
		out = truncateLines(out, opts.MaxTokens+16)
	}
	return out
}

// mapNote is what the site map section claims it was checked against. It is the
// one line in a card that makes a claim about the world outside the card, so it
// is the one line that has to name its evidence rather than assume it.
func mapNote(opts RenderOptions) string {
	if opts.PageNote == "" {
		return "checked against this page"
	}
	return "resolved against " + opts.PageNote
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func resolveEntry(pred Predicate, resolver func(Predicate) (string, bool)) resolved {
	if resolver == nil {
		return resolved{ok: false}
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
