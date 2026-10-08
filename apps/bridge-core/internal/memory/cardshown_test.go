package memory

import (
	"encoding/json"
	"strings"
	"testing"

	"browser-bridge/internal/core"
)

// A card that is learned, stored and rendered correctly but never reaches an
// agent looks exactly like a card that does not help. Recording the injection
// is what tells those apart, and it is the one number in the loop an agent
// cannot be trusted to report about itself — an agent that ignored the card
// will cheerfully say it was never there.
func TestCardShownIsRecorded(t *testing.T) {
	m := newTestManager(t, "test")
	host := "example.test"

	// No card on disk yet: no injection, and no record claiming one happened.
	if got := m.TakeSiteNote("navigate", host, 1); got != "" {
		t.Fatalf("a card was invented for a host with no card: %q", got)
	}
	if n := countKind(t, m, KindCardShown); n != 0 {
		t.Fatalf("%d card_shown records for an injection that never happened", n)
	}

	// The card is seeded rather than learned because what is under test is the
	// injection; learning it here would make the test depend on the learner as
	// well, and a failure would not say which half broke.
	seedCard(t, m, host)

	// The landing has to be *recorded* before its card can be taken. Arming
	// happens when the result lands, and the note is only handed back for a call
	// the store has already seen — asking first returns nothing, and a caller
	// that concludes "the feature is broken" from that would be right about the
	// call and wrong about the feature.
	land(t, m, host, "e1")
	if got := m.TakeSiteNote("navigate", host, 1); got == "" {
		t.Fatal("no card at the landing for a host that has one")
	}
	// The snapshot names the same element the seeded card does, so its map
	// resolves and the injection carries a usable ref.
	snapshot(t, m, host, "e2", "link [World] @e1")
	if got := m.TakeSiteNote("snapshot", host, 1); got == "" {
		t.Fatal("no card at the first snapshot")
	}

	recs := recordsOfKind(t, m, KindCardShown)
	if len(recs) != 2 {
		t.Fatalf("%d card_shown records, want 2 — one per injection that actually happened", len(recs))
	}
	for i, r := range recs {
		if r.Host != host {
			t.Errorf("record %d has host %q, want %q", i, r.Host, host)
		}
		if r.Entries <= 0 {
			t.Errorf("record %d carries %d entries; both of these injections had content", i, r.Entries)
		}
	}

	// One landing, one card. Asking again for the same landing must not produce
	// a second injection, or every report would count a card per call.
	land(t, m, host, "e3")
	if got := m.TakeSiteNote("snapshot", host, 1); got != "" {
		// The second snapshot verifies, which is a legitimate injection; what
		// must not happen is a card for a landing that was never recorded.
		t.Logf("verification injection: %q", got)
	}
}

func TestCardShownRecordsAStaleCardAsEmpty(t *testing.T) {
	// A stale card renders as a notice with no entries. That has to stay
	// distinguishable from a card that was never offered: both amount to "the
	// agent got no usable advice", and only one of them is a recall failure.
	m := newTestManager(t, "test")
	host := "example.test"
	seedCard(t, m, host)
	land(t, m, host, "e1")
	m.TakeSiteNote("navigate", host, 1)

	// A page the seeded card's predicate does not match: the entry is dropped,
	// most of the card is gone, and what reaches the agent is the notice.
	snapshot(t, m, host, "e2", "button @e9")
	note := m.TakeSiteNote("snapshot", host, 1)
	if note == "" {
		t.Fatal("a stale card produced no note at all; the agent is left with silence instead of being told to distrust it")
	}
	if !strings.Contains(note, "no longer match") {
		t.Errorf("expected a stale notice, got:\n%s", note)
	}
	recs := recordsOfKind(t, m, KindCardShown)
	last := recs[len(recs)-1]
	if last.Entries != 0 {
		t.Errorf("a stale notice recorded %d entries; it must be 0, or it reads as a card that was never offered",
			last.Entries)
	}
	// And it still counts as shown, because that is the fact a report needs.
	if len(recs) != 2 {
		t.Errorf("%d injections recorded, want 2; a stale notice is still something the agent was handed", len(recs))
	}
}

func land(t *testing.T, m *Manager, host, envelope string) {
	t.Helper()
	m.RecordCommand(envelope, "navigate", host, 1, map[string]any{"url": "https://" + host + "/"})
	m.RecordResult(envelope, "navigate", host, 1, core.ResponsePayload{
		Status: "ok", Data: json.RawMessage(`{"url":"https://` + host + `/"}`)})
}

func snapshot(t *testing.T, m *Manager, host, envelope, body string) {
	t.Helper()
	m.RecordCommand(envelope, "snapshot", host, 1, nil)
	m.RecordResult(envelope, "snapshot", host, 1, core.ResponsePayload{
		Status: "ok",
		Data: json.RawMessage(`{"snapshot":"Page: T | https://` + host + `/\n` + body +
			`","nodes_total":1,"nodes_emitted":1,"tier":0}`),
	})
}

func recordsOfKind(t *testing.T, m *Manager, kind string) []TraceRecord {
	t.Helper()
	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	var out []TraceRecord
	for _, r := range recs {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func countKind(t *testing.T, m *Manager, kind string) int {
	t.Helper()
	return len(recordsOfKind(t, m, kind))
}

// A revision number in the audit trail must name a card that exists.
//
// noteShown observed that a card no longer matched the page and wrote a
// card_revision record claiming card.Revision+1 — a revision it never wrote.
// The learner's next write then claimed the same number with different content
// and a different reason, so `memory history` (the surface ADR-0019 points a
// human at before accepting an automatic update) showed two different contents
// under one number, plus a stale record describing a revision that was never
// on disk.
//
// The rule this pins: the set of card_revision numbers is exactly the set of
// revisions the store actually wrote.
func TestStalenessDoesNotMintARevisionThatWasNeverWritten(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "browser-test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	const host = "shop.example.com"
	card := &SiteCard{
		Host:     host,
		Revision: 3,
		Map: []MapEntry{
			{Purpose: "open cart", Pred: Predicate{Role: "button", Name: "Cart"}},
			{Purpose: "search", Pred: Predicate{Role: "textbox", Name: "Search"}},
			{Purpose: "checkout", Pred: Predicate{Role: "button", Name: "Pay now"}},
		},
	}
	if err = m.store.Put(card); err != nil {
		t.Fatal(err)
	}
	// One real revision, so the ledger has a baseline.
	real := CardRevision{Host: host, Revision: 3, Reason: "learned", AtMs: 2}
	if err = m.stream.Append(TraceRecord{Kind: KindCardRevision, AtMs: 2, Revision: &real}); err != nil {
		t.Fatal(err)
	}

	// A page where two of the three entries no longer resolve — past the
	// majority rule, so the observation fires.
	digest := &PageDigest{
		URL:   "https://shop.example.com/",
		Nodes: []NodeSig{{Role: "button", Name: "Cart", Ref: "@e1", Depth: 1}},
	}
	m.noteShown(card, digest, host)

	revs, err := m.stream.Revisions(host, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("the change ledger has %d entries for one real write: %+v", len(revs), revs)
	}
	if revs[0].Revision != 3 {
		t.Errorf("the surviving entry is rev %d, want the real rev 3", revs[0].Revision)
	}

	// And the observation is still on the record, against the revision it is
	// about — not dropped just because it is no longer a change.
	stale, err := m.stream.Staleness(host, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("Staleness returned %d observations, want 1", len(stale))
	}
	if stale[0].Revision != 3 {
		t.Errorf("the observation names rev %d, want the rev it found stale (3)", stale[0].Revision)
	}
}
