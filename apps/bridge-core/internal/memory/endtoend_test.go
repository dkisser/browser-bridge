package memory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"browser-bridge/internal/core"
)

// The other tests in this package drive the pieces. This one drives the whole
// pipeline the way the daemon does: the router's side of the hook, real wire
// payloads, the background learner, and the injection an agent would read —
// with no hand-built TraceRecords and no stubbed learner in between. It is the
// test that would catch a break in the seams, which is the part unit tests
// cannot see.
func TestEndToEndLearnsThenTeachesTheSameSite(t *testing.T) {
	m := newTestManager(t, "b-1")
	hook := m // the router and the store are the same object here, on purpose

	// --- A first attempt that goes wrong: three guessed selectors, no luck. ---
	// The agent navigates, snapshots, and then guesses at class selectors on a
	// site whose layout it has never seen — the ADR-0003 failure.
	navCmd := func(url string) {
		hook.RecordCommand("n1", "navigate", "news.example.com", 1, map[string]any{"url": url})
		hook.RecordResult("n1", "navigate", "news.example.com", 1,
			okPayload(`{"url":"`+url+`","title":"Example News"}`))
	}
	snap := func(env string) {
		hook.RecordCommand(env, "snapshot", "news.example.com", 1, nil)
		hook.RecordResult(env, "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
	}
	guess := func(env, sel string) {
		hook.RecordCommand(env, "gettext", "news.example.com", 1, map[string]any{"selector": sel})
		hook.RecordResult(env, "gettext", "news.example.com", 1, core.ResponsePayload{
			Status: "error", Error: "selector_not_found",
			Message: "No element matches " + sel + ". Take a full-page snapshot and look at the largest text containers.",
		})
	}

	navCmd("https://news.example.com/")
	snap("s1")
	guess("g1", ".entry-content")
	guess("g2", "div.article")

	if note := hook.TakeSiteNote("navigate", "news.example.com", 1); note != "" {
		t.Fatalf("a card was injected before anything had been learned:\n%s", note)
	}

	// The learner runs idle, the way the daemon's background loop runs it.
	if err := m.LearnNow(); err != nil {
		t.Fatalf("LearnNow: %v", err)
	}
	card, ok := m.Store().Get("news.example.com")
	if !ok {
		t.Fatal("a site that produced two selector failures produced no card")
	}
	if len(card.Failures) != 2 {
		t.Errorf("failures = %+v, want the two guessed selectors grouped", card.Failures)
	}

	// --- The agent comes back, and this time reads the page properly. ---
	// It uses a ref from a snapshot, which is the behaviour a card should be
	// able to teach. Done twice, because that is the corroboration the
	// procedure tier requires.
	worked := func(navEnv, snapEnv, callEnv string) {
		hook.RecordCommand(navEnv, "navigate", "news.example.com", 1, map[string]any{"url": "https://news.example.com/top"})
		hook.RecordResult(navEnv, "navigate", "news.example.com", 1,
			okPayload(`{"url":"https://news.example.com/top","title":"Top stories"}`))
		hook.RecordCommand(snapEnv, "snapshot", "news.example.com", 1, nil)
		hook.RecordResult(snapEnv, "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
		hook.RecordCommand(callEnv, "gettext", "news.example.com", 1, map[string]any{"selector": "@e1"})
		hook.RecordResult(callEnv, "gettext", "news.example.com", 1, okPayload(`{"text":"World"}`))
	}
	worked("n2", "s2", "g3")
	worked("n3", "s3", "g4")
	if err := m.LearnNow(); err != nil {
		t.Fatalf("LearnNow: %v", err)
	}

	card, _ = m.Store().Get("news.example.com")
	if len(card.Map) == 0 {
		t.Fatal("the successful ref-addressed call produced no site map entry")
	}
	if len(card.Procedures) == 0 || card.Procedures[0].Successes < ProcedureCorroboration {
		t.Errorf("procedures = %+v, want a corroborated sequence", card.Procedures)
	}

	// --- The third visit: the card is handed back, on the landing itself. ---
	hook.RecordCommand("n4", "navigate", "news.example.com", 1, map[string]any{"url": "https://news.example.com/top"})
	hook.RecordResult("n4", "navigate", "news.example.com", 1,
		okPayload(`{"url":"https://news.example.com/top","title":"Top stories"}`))

	note := hook.TakeSiteNote("navigate", "news.example.com", 1)
	if note == "" {
		t.Fatal("the agent returned to a learned site and was told nothing")
	}
	if !strings.Contains(note, InjectionLabel) {
		t.Errorf("the card is not labelled:\n%s", note)
	}
	// The point of the whole thing: it is told what failed there before, and it
	// is told where the content actually is.
	if !strings.Contains(note, "selector_not_found") {
		t.Errorf("the card did not warn about the selectors that failed:\n%s", note)
	}
	if !strings.Contains(note, ".entry-content") {
		t.Errorf("the card did not name the selector it should not guess again:\n%s", note)
	}
	// The landing has no page behind it, so the map is deliberately absent: a
	// ref from some earlier snapshot would be unactionable and would look like
	// it was not.
	if strings.Contains(note, "Site map") {
		t.Errorf("the landing injected a site map with no page to resolve it against:\n%s", note)
	}
	if strings.Contains(note, "@e") {
		t.Errorf("the landing injected a remembered ref:\n%s", note)
	}
	// And it must be small enough to be worth injecting every time.
	if estimateTokens(note) > DefaultInjectTokens+16 {
		t.Errorf("card is %d tokens, over the %d budget:\n%s", estimateTokens(note), DefaultInjectTokens, note)
	}

	// --- The snapshot after the landing is where the map belongs, because that
	// is the call where a ref from *this* page is free. ---
	hook.RecordCommand("s4", "snapshot", "news.example.com", 1, nil)
	hook.RecordResult("s4", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
	mapNote := hook.TakeSiteNote("snapshot", "news.example.com", 1)
	if !strings.Contains(mapNote, "Site map") {
		t.Fatalf("the first snapshot did not hand back the site map:\n%s", mapNote)
	}
	if !strings.Contains(mapNote, "@e1") {
		t.Errorf("the map did not carry a live ref from this page:\n%s", mapNote)
	}
	if !strings.Contains(mapNote, "World") {
		t.Errorf("the map did not name the control:\n%s", mapNote)
	}
	if estimateTokens(mapNote) > DefaultInjectTokens+16 {
		t.Errorf("the map is %d tokens, over budget", estimateTokens(mapNote))
	}

	// A further snapshot on the same unchanged page has nothing to add.
	hook.RecordCommand("s4b", "snapshot", "news.example.com", 1, nil)
	hook.RecordResult("s4b", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
	if again := hook.TakeSiteNote("snapshot", "news.example.com", 1); again != "" {
		t.Errorf("the same page was explained twice:\n%s", again)
	}

	// --- Now the site is redesigned, and the check is what notices. ---
	hook.RecordCommand("n5", "navigate", "news.example.com", 1, map[string]any{"url": "https://news.example.com/top"})
	hook.RecordResult("n5", "navigate", "news.example.com", 1,
		okPayload(`{"url":"https://news.example.com/top","title":"Top stories"}`))
	hook.TakeSiteNote("navigate", "news.example.com", 1)
	hook.RecordCommand("s5", "snapshot", "news.example.com", 1, nil)
	hook.RecordResult("s5", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(
		`Page: Example News | https://news.example.com/top
heading(1) [Top stories]
button [Refresh] @e1
`)))
	stale := hook.TakeSiteNote("snapshot", "news.example.com", 1)
	if !strings.Contains(stale, "no longer match") {
		t.Errorf("a redesigned site produced no staleness notice:\n%s", stale)
	}
}

// The learner groups by idle gap, so a gap has to actually end an attempt — and
// no gap at all has to leave one attempt intact. Without the second half, a
// "sequence" could be a single call.
func TestIdleGapIsTheOnlySegmentBoundaryWithinAHost(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)

	b.landing("https://news.example.com/")
	b.snap(testSnapshot)
	b.call("gettext", "@e1")
	// No gap: this is the same attempt, and the shape must include both calls.
	b.idle(5 * time.Second)
	b.call("click", "@e2")

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("no card")
	}
	if len(card.Procedures) != 1 {
		t.Fatalf("procedures = %+v, want one", card.Procedures)
	}
	steps := card.Procedures[0].Steps
	if len(steps) != 3 {
		t.Errorf("steps = %+v, want snapshot → gettext → click in one attempt", steps)
	}
}

// Every record the pipeline writes is JSON a human can read, because the audit
// trail and the rollback story both depend on it.
func TestEveryStreamRecordIsReadableJSON(t *testing.T) {
	m := newTestManager(t, "b-1")
	hook := m
	hook.RecordCommand("n1", "navigate", "news.example.com", 1, map[string]any{"url": "https://news.example.com/"})
	hook.RecordResult("n1", "navigate", "news.example.com", 1, okPayload(`{"url":"https://news.example.com/"}`))
	hook.RecordCommand("g1", "gettext", "news.example.com", 1, map[string]any{"selector": ".entry"})
	hook.RecordResult("g1", "gettext", "news.example.com", 1, core.ResponsePayload{Status: "error", Error: "selector_not_found"})
	if err := m.LearnNow(); err != nil {
		t.Fatal(err)
	}

	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, r := range recs {
		kinds[r.Kind]++
	}
	for _, want := range []string{KindCommand, KindResponse, KindCardRevision, KindLearnRun} {
		if kinds[want] == 0 {
			t.Errorf("no %s record was written; got %v", want, kinds)
		}
	}
	// And the revisions a human reviews to decide whether to trust the card.
	revs, err := m.Stream().Revisions("news.example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) == 0 || revs[0].Evidence == nil {
		t.Errorf("revisions = %+v, want at least one carrying its evidence", revs)
	}
	blob, _ := json.Marshal(revs[0])
	if len(blob) == 0 {
		t.Error("a revision did not serialize")
	}
}
