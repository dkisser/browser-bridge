package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"browser-bridge/internal/core"
)

func newTestManager(t *testing.T, browserID string) *Manager {
	t.Helper()
	m, err := New(Options{DataDir: t.TempDir(), BrowserID: browserID})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// seedCard puts a card on disk directly, standing in for a learner that has
// already run. The injection path is what is under test here.
func seedCard(t *testing.T, m *Manager, host string) *SiteCard { //nolint:unparam // varied per test as coverage grows
	t.Helper()
	card := &SiteCard{
		Host:     host,
		Revision: 1,
		Map: []MapEntry{
			{Purpose: "text container", Pred: Predicate{Role: "link", Name: "World"}, Browser: m.browserID},
		},
		Failures: []FailureEntry{
			{Signature: "selector_not_found", Command: "gettext", Sel: ".entry-content", Count: 3},
		},
		Procedures: []ProcedureEntry{
			{
				Goal:      "then snapshot → gettext",
				Steps:     []Step{{Command: "snapshot"}, {Command: "gettext", On: "@e1"}},
				Successes: ProcedureCorroboration,
			},
		},
	}
	if err := m.Store().Put(card); err != nil {
		t.Fatalf("seed card: %v", err)
	}
	return card
}

func okPayload(data string) core.ResponsePayload {
	return core.ResponsePayload{Status: "ok", Data: json.RawMessage(data)}
}

// recordNavigate drives the hook exactly as the router does for a landing: the
// router resolves the host from the landing result and tells the store.
func recordNavigate(t *testing.T, m *Manager, env, url string, tab int) { //nolint:unparam // varied per test as coverage grows
	t.Helper()
	host := Host(url)
	m.RecordCommand(env, "navigate", host, tab, map[string]any{"url": url})
	m.RecordResult(env, "navigate", host, tab, okPayload(`{"url":"`+url+`","title":"Example News"}`))
	m.RecordCommand(env, "snapshot", host, tab, nil)
	m.RecordResult(env, "snapshot", host, tab, okPayload(snapshotJSON(testSnapshot)))
}

func TestCardIsInjectedOnLanding(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	recordNavigate(t, m, "e1", "https://news.example.com/top", 1)

	note := m.TakeSiteNote("navigate", "news.example.com", 1)
	if note == "" {
		t.Fatal("no card injected on landing for a host that has one")
	}
	if !strings.Contains(note, InjectionLabel) {
		t.Errorf("injected card is not labelled:\n%s", note)
	}
	if !strings.Contains(note, "news.example.com") {
		t.Errorf("injected card does not name the host:\n%s", note)
	}
	if !strings.Contains(note, "selector_not_found") {
		t.Errorf("injected card omitted the failure list:\n%s", note)
	}
}

func TestCardIsInjectedOncePerLanding(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)

	if m.TakeSiteNote("navigate", "news.example.com", 1) == "" {
		t.Fatal("first take returned nothing")
	}
	// A later command on the same landing must not repeat the card: the token
	// cost is paid every time.
	if note := m.TakeSiteNote("navigate", "news.example.com", 1); note != "" {
		t.Errorf("the card was injected twice for one landing:\n%s", note)
	}
}

func TestNoteIsNotSwallowedByAnInterveningCall(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)

	// An agent that reaches for get_text without snapshotting first must not
	// silently eat the card.
	if note := m.TakeSiteNote("gettext", "news.example.com", 1); note != "" {
		t.Errorf("a non-landing command produced a card:\n%s", note)
	}
	if m.TakeSiteNote("navigate", "news.example.com", 1) == "" {
		t.Error("the card was lost because an unrelated call asked first")
	}
}

// The two injection points carry different halves. The landing says what is
// true of the site regardless of the page — the failures and the sequences that
// worked. The first snapshot says where things are, because that is the one
// call where a ref from the page in hand costs nothing. Both are small, neither
// is repeated, and the second visit to an unchanged page says nothing at all.
func TestLandingCarriesFailuresAndSnapshotCarriesTheMap(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)

	landing := m.TakeSiteNote("navigate", "news.example.com", 1)
	if landing == "" {
		t.Fatal("the landing carried nothing")
	}
	if !strings.Contains(landing, "selector_not_found") {
		t.Errorf("the landing omitted the failures:\n%s", landing)
	}
	if strings.Contains(landing, "Site map") || strings.Contains(landing, "@e") {
		t.Errorf("the landing carried a map or a remembered ref, with no page behind it:\n%s", landing)
	}

	m.RecordCommand("e2", "snapshot", "news.example.com", 1, nil)
	m.RecordResult("e2", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
	mapped := m.TakeSiteNote("snapshot", "news.example.com", 1)
	if !strings.Contains(mapped, "Site map") || !strings.Contains(mapped, "@e1") {
		t.Errorf("the first snapshot did not carry the map with a live ref:\n%s", mapped)
	}

	// Nothing changed, so there is nothing to add.
	m.RecordCommand("e3", "snapshot", "news.example.com", 1, nil)
	m.RecordResult("e3", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))
	if again := m.TakeSiteNote("snapshot", "news.example.com", 1); again != "" {
		t.Errorf("an unchanged page was explained twice:\n%s", again)
	}
}

func TestSnapshotReportsAStaleCard(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)
	m.TakeSiteNote("navigate", "news.example.com", 1)

	// The site was redesigned: the control the card names is gone.
	changed := `Page: Example News | https://news.example.com/
heading(1) [Top stories]
button [Refresh] @e1
`
	m.RecordCommand("e2", "snapshot", "news.example.com", 1, nil)
	m.RecordResult("e2", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(changed)))

	note := m.TakeSiteNote("snapshot", "news.example.com", 1)
	if note == "" {
		t.Fatal("a card whose every entry stopped matching produced no notice")
	}
	if !strings.Contains(note, "no longer match") {
		t.Errorf("stale notice does not say so:\n%s", note)
	}
}

func TestSnapshotInjectsUnverifiedCardWhenTheAgentSkippedNavigate(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	// The tab was already open when the daemon started, so no landing was seen.
	// The first snapshot is the first thing that reveals the host, and it is
	// also the one call where the card can be verified — so the card arrives
	// here with live refs rather than unverified.
	m.RecordCommand("e1", "snapshot", "news.example.com", 1, nil)
	m.RecordResult("e1", "snapshot", "news.example.com", 1, okPayload(snapshotJSON(testSnapshot)))

	note := m.TakeSiteNote("snapshot", "news.example.com", 1)
	if note == "" {
		t.Fatal("no card injected on the snapshot that revealed a known host")
	}
	if !strings.Contains(note, "@e1") {
		t.Errorf("the card did not hand back a live ref from this page:\n%s", note)
	}
}

func TestNoCardForAnUnknownHost(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")
	recordNavigate(t, m, "e1", "https://other.example.org/", 1)
	// The router reports where the tab actually landed, which is a host with no
	// card — so the lookup is for other.example.org, not for the seeded one.
	if note := m.TakeSiteNote("navigate", "other.example.org", 1); note != "" {
		t.Errorf("a card was injected for a host that has none:\n%s", note)
	}
}

func TestCardIsWithheldFromAnotherBrowserProfile(t *testing.T) {
	m := newTestManager(t, "b-2")
	// Seeded under a different profile.
	card := &SiteCard{Host: "news.example.com", Revision: 1, Map: []MapEntry{
		{Purpose: "text container", Pred: Predicate{Role: "link", Name: "World"}, Browser: "b-1"},
	}}
	if err := m.Store().Put(card); err != nil {
		t.Fatal(err)
	}
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)
	note := m.TakeSiteNote("navigate", "news.example.com", 1)
	if note != "" {
		t.Errorf("a card observed under another profile was injected here:\n%s", note)
	}
}

func TestOversizedResultIsRecordedAsAFailure(t *testing.T) {
	m := newTestManager(t, "b-1")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)

	big := strings.Repeat("x", SoftFailureThreshold+1)
	m.RecordCommand("e2", "gethtml", "news.example.com", 1, map[string]any{"selector": "body"})
	m.RecordResult("e2", "gethtml", "news.example.com", 1, core.ResponsePayload{Status: "ok", Data: json.RawMessage(`{"html":"` + big + `"}`)})

	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range recs {
		if r.Command == "gethtml" && r.Outcome == OutcomeSoft {
			found = true
			if r.ErrCode != "oversized_result" {
				t.Errorf("soft failure code = %q", r.ErrCode)
			}
		}
	}
	if !found {
		t.Error("a read past the soft limit was not recorded as a soft failure")
	}
}

func TestTypedInputNeverReachesTheStream(t *testing.T) {
	m := newTestManager(t, "b-1")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)
	m.RecordCommand("e2", "type", "news.example.com", 1, map[string]any{"selector": "#q", "text": "hunter2", "submit": true})
	m.RecordResult("e2", "type", "news.example.com", 1, okPayload(`{"typed":"#q"}`))

	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Kind != KindCommand || r.Command != "type" {
			continue
		}
		if _, ok := r.Args["text"]; ok {
			t.Error("the typed text was written to the stream")
		}
		if r.Args["selector"] != "#q" {
			t.Errorf("selector = %v, want it kept — it is structure, not content", r.Args["selector"])
		}
	}
}

func TestNonSitePagesAreIgnored(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	// Written out rather than through recordNavigate, because that helper also
	// snapshots testSnapshot — a news.example.com page. Pairing "navigated to
	// about:blank" with a news snapshot made this assert the opposite of what
	// it says, and it passed only because the misattributed snapshot was filed
	// under an empty host and armed nothing. A snapshot now takes the site from
	// its own `Page:` line, so the contradiction is no longer papered over.
	m.RecordCommand("e1", "navigate", "", 1, map[string]any{"url": "about:blank"})
	m.RecordResult("e1", "navigate", "", 1, okPayload(`{"url":"about:blank","title":"New Tab"}`))

	if note := m.TakeSiteNote("navigate", "news.example.com", 1); note != "" {
		t.Errorf("a card was injected on about:blank:\n%s", note)
	}
	// And a snapshot of a non-site page arms nothing either.
	m.RecordCommand("e2", "snapshot", "", 1, nil)
	m.RecordResult("e2", "snapshot", "", 1, okPayload(snapshotJSON("Page: New Tab | about:blank\n")))
	if note := m.TakeSiteNote("snapshot", "", 1); note != "" {
		t.Errorf("a card was injected for a snapshot of about:blank:\n%s", note)
	}
}

func TestManagerSatisfiesTheRouterHook(t *testing.T) {
	// Compile-time proof that the control plane's interface and the store agree.
	var _ core.MemoryHook = (*Manager)(nil)
}

func snapshotJSON(text string) string {
	lines := strings.Split(text, "\n")
	return `{"snapshot":` + mustJSON(strings.Join(lines, "\n")) +
		`,"truncated":false,"nodes_total":8,"nodes_emitted":6,"tier":0}`
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// The soft-failure gate has to measure what its own constant describes.
//
// It read len(p.Data) — JSON bytes — while the constant, its doc and the
// recorded message all spoke in characters. Two consequences, both silent:
//
//   - A read in a non-Latin script tripped it far below the intent. 30,000 CJK
//     characters is 90,011 bytes against a 60,000-*character* threshold, so
//     every agent working in CJK or Japanese permanently learned "reads fail
//     here" — and because mergeProcedure refuses any segment carrying a
//     failure, the working sequence was never learned either.
//   - It ran on every command, not the reads it is about, so every screenshot
//     was filed as oversized_result. A screenshot's base64 data URL is always
//     past the limit, so no screenshot could ever be recorded as a success, in
//     the tier ADR-0018 calls the one signal it trusts most.
func TestTheSoftFailureGateMeasuresReadCharacters(t *testing.T) {
	m := newTestManager(t, "b-1")
	recordNavigate(t, m, "e1", "https://news.example.com/", 1)

	// 30,000 CJK characters: three bytes each, so 90KB and well under the
	// 60,000-character intent.
	cjk := strings.Repeat("読", 30_000)
	m.RecordCommand("e2", "gettext", "news.example.com", 1, map[string]any{"selector": "body"})
	m.RecordResult("e2", "gettext", "news.example.com", 1,
		core.ResponsePayload{Status: "ok", Data: json.RawMessage(`{"text":"` + cjk + `"}`)})

	// A screenshot is large by construction and is not a page dump.
	m.RecordCommand("e3", "screenshot", "news.example.com", 1, nil)
	m.RecordResult("e3", "screenshot", "news.example.com", 1,
		core.ResponsePayload{Status: "ok", Data: json.RawMessage(`{"dataUrl":"data:image/png;base64,` + strings.Repeat("A", 90_000) + `"}`)})

	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Outcome != OutcomeSoft {
			continue
		}
		t.Errorf("%s was recorded as a soft failure (%s): a %s-sized result is not a page dump",
			r.Command, r.ErrCode, map[string]string{"gettext": "30,000-character", "screenshot": "90KB"}[r.Command])
	}
}

// New must not leak the stream's file handle when a later step fails.
//
// OpenStream holds an append handle for the life of the manager, and the caller
// treats New's error as recoverable: app.go logs "self-learning disabled" and
// brings the control plane up anyway. So a leaked handle is not a transient —
// the daemon lives on with an unclosed append fd, reachable whenever
// data/cards exists as a regular file.
func TestNewReleasesTheStreamWhenALaterOpenFails(t *testing.T) {
	dir := t.TempDir()
	// Make OpenStore fail: the cards path exists, as a regular file.
	if err := os.WriteFile(filepath.Join(dir, "cards"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := openFDs(t)
	if _, err := New(Options{DataDir: dir, BrowserID: "b"}); err == nil {
		t.Fatal("New succeeded with an unusable cards path")
	}
	after := openFDs(t)
	if after > before {
		t.Errorf("New leaked %d file descriptor(s): %d before, %d after", after-before, before, after)
	}
}

// openFDs counts this process's open file descriptors, or returns 0 where the
// platform does not expose the directory (in which case the assertion above is
// a no-op rather than a false failure).
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return 0
	}
	return len(entries)
}

// A process that does not own $BB_HOME/data must not roll its stream over.
//
// `bridge memory learn` and `bench record` open a second Manager over the live
// directory while the daemon holds its own handle. Rotate closes the caller's
// fd, renames stream.jsonl to stream.jsonl.1 and creates a fresh empty
// stream.jsonl — so the daemon's already-open fd follows the inode to the
// renamed file and keeps appending there, while its own ReadFrom reads the new
// empty one and never sees another record. Learning stops for the rest of that
// daemon's life and nothing reports it.
func TestOnlyTheOwningManagerRotatesTheStream(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owns     bool
		wantFile bool // whether stream.jsonl.1 should exist afterwards
	}{
		{"the daemon rotates", true, true},
		{"a second Manager over a live dir does not", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// A stream past the ceiling, fully consumed, which is the state
			// Rotate requires.
			stream, err := OpenStream(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err = stream.Append(TraceRecord{
				Kind: KindCommand, AtMs: 1, Envelope: "e1", Command: "gettext",
				Args: map[string]any{"selector": strings.Repeat("x", streamRotateBytes)},
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err = stream.ReadFrom(0); err != nil {
				t.Fatal(err)
			}
			if err = stream.Close(); err != nil {
				t.Fatal(err)
			}
			cursor := LoadCursor(dir)
			if err = cursor.Set(1); err != nil {
				t.Fatal(err)
			}

			m, err := New(Options{DataDir: dir, BrowserID: "b", OwnsRotation: tc.owns})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close() })

			_, statErr := os.Stat(filepath.Join(dir, "stream.jsonl.1"))
			if got := statErr == nil; got != tc.wantFile {
				t.Errorf("retained generation present = %v, want %v", got, tc.wantFile)
			}
		})
	}
}

// The ceiling has to be enforced for the whole life of the process, not only at
// startup.
//
// Rotate ran from one place: New. A daemon left running for a week therefore
// accumulated records without bound — and because ReadFrom scans from the
// cursor each pass while the file keeps growing, idle CPU stayed a function of
// uptime, which is the specific cost ADR-0032 says it fixed. The check is
// repeated after each learn pass for a Manager that owns the stream.
func TestTheOwningManagerRotatesDuringItsLifeNotOnlyAtStartup(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "b", OwnsRotation: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })

	// Nothing to rotate yet.
	m.rotateIfOwned()
	if _, statErr := os.Stat(filepath.Join(dir, "stream.jsonl.1")); statErr == nil {
		t.Fatal("rotated an empty stream; the test is not testing the ceiling")
	}

	// Outgrow the ceiling in one record, well after startup.
	if err = m.Stream().Append(TraceRecord{
		Kind: KindCommand, AtMs: 2, Envelope: "e1", Command: "gettext",
		Args: map[string]any{"selector": strings.Repeat("x", streamRotateBytes)},
	}); err != nil {
		t.Fatal(err)
	}
	// The learner has to have consumed it, which is the state Rotate requires.
	if err = m.LearnNow(); err != nil {
		t.Fatalf("learn pass: %v", err)
	}
	m.rotateIfOwned()

	if _, statErr := os.Stat(filepath.Join(dir, "stream.jsonl.1")); statErr != nil {
		t.Errorf("the stream outgrew its ceiling during the process's life and was not rotated: %v", statErr)
	}
}

// The store's per-tab scratch state must be pruned too, not only the router's.
//
// tabLocked creates a tabState on first sight and nothing ever deleted one, so
// the two maps grew together, one entry per tab id Chrome ever assigned. It
// matters beyond the memory: Chrome reuses ids, so a card still armed for a
// closed tab would be verified against whatever page the *next* tab with that
// id showed.
func TestTheStoreForgetsAClosedTabsScratchState(t *testing.T) {
	m := newTestManager(t, "b-1")
	recordNavigate(t, m, "e1", "https://news.example.com/", 7)

	m.mu.Lock()
	_, had := m.tabs[tabKey(7)]
	m.mu.Unlock()
	if !had {
		t.Fatal("the tab was never tracked; the test is not testing the prune")
	}

	m.RecordResult("e2", "tab:close", "news.example.com", 7,
		core.ResponsePayload{Status: "ok", Data: json.RawMessage(`{"ok":true}`)})

	m.mu.Lock()
	_, still := m.tabs[tabKey(7)]
	m.mu.Unlock()
	if still {
		t.Error("the tab's scratch state survived its close")
	}
}

// The learner must make progress on its own after a restart, with no traffic.
//
// The loop created an armed timer and immediately Stopped it — the standard
// "hand me a stopped timer" idiom is a no-op on a fresh one, so Stop returned
// true and nothing was drained. Only the signal branch called Reset, leaving
// `case <-timer.C` unreachable until the first command arrived. So a daemon
// started over a backlog of unread records ran no pass at all, which is the
// one thing the timer branch's own comment says it is there for.
func TestTheLearnerRunsAPassOnItsOwnAfterARestart(t *testing.T) {
	dir := t.TempDir()
	// A backlog written by a previous process, which nobody will signal.
	prev, err := OpenStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	const host = "shop.example.com"
	nav := "nav-1"
	if err = prev.Append(TraceRecord{Kind: KindCommand, AtMs: 1, Envelope: nav, Command: "navigate", TabID: 1}); err != nil {
		t.Fatal(err)
	}
	if err = prev.Append(TraceRecord{Kind: KindResponse, AtMs: 2, Envelope: nav, Command: "navigate", TabID: 1,
		Outcome: OutcomeOK, Host: host}); err != nil {
		t.Fatal(err)
	}
	const env = "click-1"
	if err = prev.Append(TraceRecord{Kind: KindCommand, AtMs: 3, Envelope: env, Command: "click", TabID: 1,
		Args: map[string]any{"selector": ".buy"}}); err != nil {
		t.Fatal(err)
	}
	if err = prev.Append(TraceRecord{Kind: KindResponse, AtMs: 4, Envelope: env, Command: "click", TabID: 1,
		Outcome: OutcomeError, ErrCode: "no_element", Host: host}); err != nil {
		t.Fatal(err)
	}
	if err = prev.Close(); err != nil {
		t.Fatal(err)
	}

	// A fresh manager over the same data dir, and nothing ever signals it.
	m, err := New(Options{DataDir: dir, BrowserID: "b", IdleAfter: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.store.Get(host); ok {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	_, ok := m.store.Get(host)
	t.Errorf("no pass ran without a signal, so the backlog stayed unlearned (card present: %v)", ok)
}

// A snapshot must be filed under the site the page is on, not the one the
// router last saw.
//
// The router's host comes from a landing command, and a click is not one — so a
// link that navigates leaves it naming the site the agent just left. The
// snapshot's own `Page:` line names the site it is actually on, and that is the
// only source that cannot be stale. Recording under the router's host made
// TakeSiteNote verify the old site's card against the new page, noteShown
// record the healthy card as stale, and the learner merge the new page's
// controls into the old site's map.
func TestASnapshotIsFiledUnderTheSiteItsOwnPageNames(t *testing.T) {
	m := newTestManager(t, "b-1")
	// The tab is known to be on the old site — a landing said so.
	recordNavigate(t, m, "e1", "https://mail.example.com/u/0/", 1)

	// The agent clicks a link and the next snapshot is the new site.
	m.RecordCommand("e2", "snapshot", "mail.example.com", 1, nil)
	m.RecordResult("e2", "snapshot", "mail.example.com", 1, core.ResponsePayload{
		Status: "ok",
		Data:   json.RawMessage(`{"snapshot":"Page: News | https://news.example.com/\nheading @e1\n","nodes_total":1,"nodes_emitted":1}`),
	})

	recs, _, _, err := m.Stream().ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	var snap *TraceRecord
	for i := range recs {
		if recs[i].Command == "snapshot" && recs[i].Page != nil {
			snap = &recs[i]
		}
	}
	if snap == nil {
		t.Fatal("no snapshot record found")
	}
	if snap.Page.URL != "https://news.example.com/" {
		t.Errorf("the digest's own url = %q, want the news site", snap.Page.URL)
	}
	if snap.Host != "news.example.com" {
		t.Errorf("the snapshot is filed under %q; the news page was merged into the mail card", snap.Host)
	}
}

// A card must be handed over once per site, not once per read.
//
// The snapshot-arming branch — the one that covers a tab the agent never
// navigated — was guarded by `!ts.sawLanding`, which asks whether a *landing*
// was seen on this tab. A tab the router learned about from pageinfo or
// tab:list never sees one, so the flag stayed false for the whole session and
// every snapshot re-armed: ~400 tokens appended to each read plus a card_shown
// record per read, on the most common MCP flow there is.
//
// It went unnoticed because the branch was unreachable until host-reporting
// commands gave a tab a host without a landing — the fix that made the
// documented flow work is what exposed it.
func TestACardIsAnnouncedOncePerSiteNotOncePerRead(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	// The documented flow: the router learns the tab from a host report, and
	// the agent reads the page. No landing ever happens on this tab.
	m.RecordCommand("e1", "snapshot", "news.example.com", 5, nil)
	m.RecordResult("e1", "snapshot", "news.example.com", 5, okPayload(snapshotJSON(testSnapshot)))

	injected := 0
	for i := 0; i < 4; i++ {
		env := "read-" + strconv.Itoa(i)
		m.RecordCommand(env, "snapshot", "news.example.com", 5, nil)
		m.RecordResult(env, "snapshot", "news.example.com", 5, okPayload(snapshotJSON(testSnapshot)))
		if note := m.TakeSiteNote("snapshot", "news.example.com", 5); note != "" {
			injected++
		}
	}
	if injected > 1 {
		t.Errorf("the same card was injected %d times across 4 reads of one page; "+
			"it must be announced once and then verified silently", injected)
	}
	if injected == 0 {
		t.Error("nothing was ever injected, so the fix went too far the other way")
	}

	// A tab that moves to a *different* site arms again, which is the other
	// half of the rule.
	m.RecordCommand("other", "navigate", "other.example.com", 5, nil)
	m.RecordResult("other", "navigate", "other.example.com", 5, okPayload(`{"url":"https://other.example.com/"}`))
	seedCard(t, m, "other.example.com")
	m.RecordCommand("o1", "snapshot", "other.example.com", 5, nil)
	m.RecordResult("o1", "snapshot", "other.example.com", 5, okPayload(snapshotJSON(testSnapshot)))
	if note := m.TakeSiteNote("snapshot", "other.example.com", 5); note == "" {
		t.Error("moving to a new site did not re-announce, so a tab that navigates " +
			"without a landing would never learn it has a card")
	}
}

// When the snapshot itself is the only injection, it must carry the whole card.
//
// OnlyMap exists for the *second* injection point: the landing already said
// what failed and what worked, moments earlier in the same task, so repeating
// it on the snapshot is tokens spent twice. But a tab the agent never
// navigated — discovered by tab:list or pageinfo — has no landing, so the
// snapshot is the only injection there will ever be. Truncating it to the map
// handed the agent a bare site-map header while the failure tier, the one
// signal this package calls unmissable, was dropped on the floor.
func TestASnapshotThatArmsCarriesTheWholeCard(t *testing.T) {
	m := newTestManager(t, "b-1")
	card := seedCard(t, m, "news.example.com")
	if len(card.Failures) == 0 || len(card.Procedures) == 0 {
		t.Fatal("the seeded card has no failure or procedure tier to lose")
	}

	// No landing on this tab: the router learned it from a host report.
	m.RecordCommand("e1", "snapshot", "news.example.com", 5, nil)
	m.RecordResult("e1", "snapshot", "news.example.com", 5, okPayload(snapshotJSON(testSnapshot)))
	note := m.TakeSiteNote("snapshot", "news.example.com", 5)
	if note == "" {
		t.Fatal("nothing was injected on the arming snapshot")
	}
	if !strings.Contains(note, "selector_not_found") {
		t.Errorf("the failure tier did not reach the agent on the only injection:\n%s", note)
	}
	if !strings.Contains(note, "Sequences that worked here") {
		t.Errorf("the procedure tier did not reach the agent on the only injection:\n%s", note)
	}
	// And the map is still there — this is the whole card, not a different one.
	if !strings.Contains(note, "Site map") {
		t.Errorf("the site map is missing:\n%s", note)
	}
}

// A rotation must not re-apply what the learner already applied.
//
// Rotate's condition requires the learner to have consumed everything, so the
// cursor holds the combined line count of both generations. After the rename the
// retained generation is the old active file and the new one is empty — the
// same total, numbered the same way — so the cursor lands where it always did
// and needs no reset.
//
// It was being reset to 0, which re-read the retained generation from the start.
// applySegment increments Uses, Count and Revision rather than recomputing them,
// so a single rotation with no traffic at all took every card to a phantom
// revision and doubled its counters. Stream.Rotate's comment claimed that re-read
// was idempotent "because rebuilding a card from the same records produces the
// same card"; Learn.Run's own doc says the opposite and is right.
//
// It went unnoticed because the rotation condition was unreachable, so this had
// never run.
func TestARotationDoesNotReapplyWhatWasAlreadyLearned(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "b", OwnsRotation: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })

	const host = "shop.example.com"
	nav := "nav-1"
	if err = m.Stream().Append(TraceRecord{Kind: KindCommand, AtMs: 1, Envelope: nav, Command: "navigate", TabID: 1}); err != nil {
		t.Fatal(err)
	}
	if err = m.Stream().Append(TraceRecord{Kind: KindResponse, AtMs: 2, Envelope: nav, Command: "navigate", TabID: 1,
		Outcome: OutcomeOK, Host: host}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		env := "click-" + strconv.Itoa(i)
		if err = m.Stream().Append(TraceRecord{Kind: KindCommand, AtMs: int64(10 + i), Envelope: env, Command: "click", TabID: 1,
			Args: map[string]any{"selector": ".buy"}}); err != nil {
			t.Fatal(err)
		}
		if err = m.Stream().Append(TraceRecord{Kind: KindResponse, AtMs: int64(11 + i), Envelope: env, Command: "click", TabID: 1,
			Outcome: OutcomeError, ErrCode: "no_element", Host: host}); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.LearnNow(); err != nil {
		t.Fatal(err)
	}
	before, ok := m.store.Get(host)
	if !ok {
		t.Fatal("nothing was learned, so this test cannot see a re-apply")
	}

	// Outgrow the ceiling, learn it, and rotate. No traffic in between.
	if err = m.Stream().Append(TraceRecord{
		Kind: KindCommand, AtMs: 100, Envelope: "big", Command: "gettext",
		Args: map[string]any{"selector": strings.Repeat("x", streamRotateBytes)},
	}); err != nil {
		t.Fatal(err)
	}
	if err = m.LearnNow(); err != nil {
		t.Fatal(err)
	}
	beforeRev, beforeUses := before.Revision, 0
	for _, e := range before.Map {
		beforeUses += e.Uses
	}
	beforeFails := 0
	for _, f := range before.Failures {
		beforeFails += f.Count
	}

	m.rotateIfOwned()
	if _, statErr := os.Stat(filepath.Join(dir, "stream.jsonl.1")); statErr != nil {
		t.Fatalf("the stream did not rotate, so the test is not exercising the reset: %v", statErr)
	}

	// The next pass re-reads nothing, because the cursor still points past the
	// retained generation.
	if err = m.LearnNow(); err != nil {
		t.Fatal(err)
	}
	after, ok := m.store.Get(host)
	if !ok {
		t.Fatal("the card disappeared")
	}
	afterUses, afterFails := 0, 0
	for _, e := range after.Map {
		afterUses += e.Uses
	}
	for _, f := range after.Failures {
		afterFails += f.Count
	}
	if after.Revision != beforeRev {
		t.Errorf("revision went %d → %d with no traffic: a rotation re-applied what was already learned",
			beforeRev, after.Revision)
	}
	if afterUses != beforeUses {
		t.Errorf("map Uses went %d → %d with no traffic", beforeUses, afterUses)
	}
	if afterFails != beforeFails {
		t.Errorf("failure Count went %d → %d with no traffic", beforeFails, afterFails)
	}
}

// A trace record must own its args, not share them with the inflight table.
//
// redactArgs builds one map, RecordCommand stores that same object in
// m.inflight, and takeInflight handed the very same object to the caller's
// TraceRecord. Nothing writes to it today — which is why it reads as harmless —
// but the day someone adds a line like
// `rec.Args["selector"] = safeSelector(...)` to "reduce it again to be safe",
// that line edits the table's object too, and it looks like a pure function.
// The project rule is that data is copied rather than shared, so the record
// gets its own.
func TestATraceRecordOwnsItsArgs(t *testing.T) {
	m := newTestManager(t, "b-1")
	m.RecordCommand("e1", "gettext", "news.example.com", 1, map[string]any{"selector": "#entry"})

	// Hold the same map the table holds, by reading it under the lock.
	m.mu.Lock()
	shared := m.inflight["e1"].args
	m.mu.Unlock()

	got := m.takeInflight("e1")
	if got == nil {
		t.Fatal("takeInflight returned nothing")
	}
	if reflect.ValueOf(got).Pointer() == reflect.ValueOf(shared).Pointer() {
		t.Fatal("takeInflight handed back the table's own map, so the record aliases it")
	}

	// Mutating what the caller received must not reach back into the table.
	got["selector"] = "mutated"
	if shared["selector"] == "mutated" {
		t.Error("writing through the returned map changed the inflight table's object")
	}
}
