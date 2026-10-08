package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"browser-bridge/internal/core"
)

// Manager is the control plane's self-learning collaborator: the thing the
// router calls on every command and every result.
//
// It has three jobs and no others. It records (ADR-0018), it hands a card back
// when the agent lands somewhere known (ADR-0019), and it drives the background
// learner that turns one into the other (ADR-0020).
//
// The rule that governs the whole file: nothing here may fail a command. Every
// method is best-effort and swallows its own errors, because a learning system
// that can break the browser it is watching has made itself worse than useless.
type Manager struct {
	stream *Stream
	store  *Store
	cursor *Cursor
	learn  *Learn
	logf   func(format string, args ...any)
	// dataDir is Options.DataDir, kept because the store only knows its own
	// cards/ subdirectory and the guides live beside it (ADR-0034).
	dataDir string
	// ownsStream says this Manager may rotate the stream. False for a CLI or
	// bench process that opened a second Manager over the live directory while
	// the daemon holds its own handle — rotating from there renames the file
	// out from under the daemon and stops its learning silently.
	ownsStream bool

	// signal is the coalescing wakeup (ADR-0020). Capacity one is the whole
	// design: the unread suffix of the stream is the record of the work, so
	// losing a signal loses nothing.
	signal chan struct{}

	// idleAfter is how long the learner waits for the stream to go quiet before
	// starting a pass, so a burst of activity is learned as one batch.
	idleAfter time.Duration

	mu   sync.Mutex
	tabs map[int]*tabState
	// inflight carries a command's host and arguments from RecordCommand to
	// RecordResult, and is cleared as each result lands.
	//
	// "Cleared as each result lands" was the whole of the bound, and it is not
	// one: three router paths drop a command without ever producing a result —
	// send-to-extension fails, the MCP sendCommand timeout calls RemoveRoute, and
	// the client disconnects. Every one of those strands an entry for as long as
	// the daemon lives, and the timeout path makes it routine rather than
	// exotic. So the map is capped and evicts the oldest, which loses a record
	// nobody is going to read rather than growing without bound.
	inflight map[string]inflightCmd
	// inflightOrder is the insertion order, for that eviction.
	inflightOrder []string
	// tabs is per-tab scratch state, and like inflight it was only ever added
	// to: tabLocked created an entry on first sight and nothing ever deleted
	// one. A long-running session that opens and closes thousands of tabs
	// accumulated a tabState per id Chrome ever assigned, growing alongside the
	// router's own tabHost. tab:close now prunes it, and the map is capped for
	// the closes the router never sees — a tab the *user* closed.
	tabsOrder []int
	// browserID is the profile cards are attributed to (ADR-0019's
	// per-browser annotation). A card entry remembers the browser it was
	// observed under and is withheld from the others.
	browserID string
}

type inflightCmd struct {
	args map[string]any
}

// maxInflight caps the stranded-command map. A browser session issues commands
// in the hundreds, not the tens of thousands, so this is generous enough that a
// live command is never the thing evicted, and small enough that a leak is
// bounded rather than merely slow.
const maxInflight = 4096

// maxTabs caps the per-tab maps (this one and the router's tabHost, which
// bounds at the same number). A browser session works with tabs in the tens,
// so this is generous; the eviction is safe because a forgotten tab reads as
// "no host known yet", which is the state every fresh tab is in anyway. It is
// the same trade maxInflight makes, for the same reason: a leak that is
// bounded beats one that is merely slow.
const maxTabs = 4096

// tabState is what the store keeps per tab, and it is deliberately almost
// nothing. Where the tab *is* belongs to the control plane, which is told and
// passes the host in; this is only the store's own artifacts — the digest of the
// last snapshot, and whether it has already handed out a card for the landing
// that just happened.
type tabState struct {
	// noteArmed is set when a landing landed on a host the caller named, and the
	// card for it has not been handed out yet. TakeSiteNote clears it, so the
	// card is shown once per landing rather than on every command.
	noteArmed bool
	// verifyOn is the host whose card should be *checked* against the next
	// snapshot. This is what makes verification free: the snapshot was going to
	// be fetched anyway.
	verifyOn   string
	lastDigest *PageDigest
	// sawLanding records that a landing command was seen for this tab. A
	// snapshot arms the card only while that is false: it covers the tab that
	// was already open when the daemon started, and stops the arming from
	// repeating on every snapshot for the rest of the session.
	sawLanding bool
}

// Options configures a Manager.
type Options struct {
	DataDir   string
	BrowserID string
	// Compressor is optional (ADR-0022); nil disables the model call.
	Compressor Compressor
	// IdleAfter overrides how long the learner waits for quiet.
	IdleAfter time.Duration
	Logf      func(format string, args ...any)
	// OwnsRotation says this Manager may roll the stream over: at open, and
	// again after every learn pass so the ceiling is enforced for the whole
	// life of the process rather than only at startup.
	//
	// Only the process that owns $BB_HOME/data may set it — the daemon.
	// `bridge memory learn` and `bench record` open a *second* Manager over
	// the live directory while the daemon still holds its own handle, and
	// Rotate renames stream.jsonl out from under it: the daemon's open fd
	// follows the inode to stream.jsonl.1 and keeps appending there, while its
	// ReadFrom reads the new empty file and never sees another record. Learning
	// stops for the rest of that daemon's life and nothing reports it.
	OwnsRotation bool
}

// rotateOwnedStream rolls the stream over when it has outgrown its ceiling and
// the learner has consumed all of it, resetting the cursor to the start of the
// retained generation.
//
// Called at open and again after every learn pass. A ceiling checked once per
// process start is not a ceiling on the process that matters: a daemon up for a
// week is the one that accumulates records without bound, and because ReadFrom
// scans from the cursor each pass while the file keeps growing, idle CPU stays a
// function of uptime — the exact cost ADR-0032 says it fixed.
func rotateOwnedStream(stream *Stream, cursor *Cursor, logf func(string, ...any)) {
	rotated, err := stream.Rotate(cursor.Get())
	if err != nil {
		logf("memory: rotate stream: %v", err)
		return
	}
	if !rotated {
		return
	}
	if err := cursor.Set(0); err != nil {
		logf("memory: reset cursor after rotation: %v", err)
	}
	logf("memory: rotated the trace stream")
}

// defaultIdleAfter is long enough that a session's back-and-forth is one batch,
// short enough that a card exists before the next thing the agent does.
const defaultIdleAfter = 20 * time.Second

// New builds a Manager over $BB_HOME/data.
func New(opts Options) (*Manager, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	stream, err := OpenStream(opts.DataDir)
	if err != nil {
		return nil, err
	}
	// OpenStream holds an append handle for the life of the manager, so a
	// failure on any later step has to release it. Without this the handle was
	// dropped on the floor and leaked: the caller treats the error as
	// recoverable — app.go logs "self-learning disabled" and runs anyway — so
	// the daemon lived on with an unclosed append handle, reachable whenever
	// data/cards exists as a regular file or the data dir turns read-only
	// between the two opens.
	ok := false
	defer func() {
		if !ok {
			_ = stream.Close()
		}
	}()
	store, err := OpenStore(opts.DataDir)
	if err != nil {
		return nil, err
	}
	cursor := LoadCursor(opts.DataDir)
	// Roll the stream over if it has outgrown its ceiling *and* the learner has
	// consumed all of it. See Stream.Rotate for why the second half is the
	// condition that makes it safe. Restarting the cursor is the whole cost, and
	// it costs one idempotent re-read of the retained generation.
	//
	// Gated on owning the stream, and repeated after every learn pass — a
	// check that runs once per process start does not enforce a ceiling on a
	// daemon that stays up for a week, which is the only process that
	// accumulates enough to matter.
	if opts.OwnsRotation {
		rotateOwnedStream(stream, cursor, logf)
	}
	learn := NewLearn(stream, store, cursor, opts.Compressor, logf)
	idle := opts.IdleAfter
	if idle <= 0 {
		idle = defaultIdleAfter
	}
	ok = true
	return &Manager{
		stream:     stream,
		store:      store,
		cursor:     cursor,
		learn:      learn,
		logf:       logf,
		ownsStream: opts.OwnsRotation,
		dataDir:    opts.DataDir,
		signal:     make(chan struct{}, 1),
		idleAfter:  idle,
		tabs:       make(map[int]*tabState),
		inflight:   make(map[string]inflightCmd),
		browserID:  opts.BrowserID,
	}, nil
}

// Store exposes the card store for `bridge memory`.
func (m *Manager) Store() *Store { return m.store }

// Stream exposes the append-only stream for `bridge memory history`.
func (m *Manager) Stream() *Stream { return m.stream }

// Cursor exposes the learner cursor for `bridge memory status`.
func (m *Manager) Cursor() *Cursor { return m.cursor }

// RecordCommand records the outbound half of a call. Called by the router for
// every command from every adapter, so the learner sees CLI traffic as well as
// MCP traffic.
func (m *Manager) RecordCommand(envelopeID, command, host string, tabID int, args map[string]any) {
	if m == nil {
		return
	}
	redacted := redactArgs(command, args)
	rec := TraceRecord{
		Kind:     KindCommand,
		Envelope: envelopeID,
		// Reduced at the point it enters the trace, not at each of the three
		// places a card renders it. The stream is the raw material for the card
		// and for `memory history`, so a name that never lands here cannot be
		// quoted in one place and forgotten in another. See safeCommand.
		Command: safeCommand(command),
		TabID:   tabID,
		Browser: m.browserID,
		Host:    host,
		Args:    redacted,
		AtMs:    nowMs(),
	}
	if err := m.stream.Append(rec); err != nil {
		m.logf("memory: record command %s: %v", command, err)
	}
	m.mu.Lock()
	m.inflight[envelopeID] = inflightCmd{args: redacted}
	m.inflightOrder = append(m.inflightOrder, envelopeID)
	for len(m.inflightOrder) > maxInflight {
		oldest := m.inflightOrder[0]
		m.inflightOrder = m.inflightOrder[1:]
		delete(m.inflight, oldest)
	}
	m.mu.Unlock()
	m.wake()
}

// RecordRouterError records a payload the control plane synthesized itself.
//
// It goes into the trace with KindRouterError — the kind the reader already
// knows about — and never into the failure tier. See core.MemoryHook for why
// the two are separate: a browser that is not connected is not a fact about the
// site the tab happens to be on, and the failure tier is rendered to future
// agents as "Observed to fail here (do not repeat)".
//
// The record is deliberately minimal. No args lookup (the command did not
// execute), no payload magnitude (a synthesized error body is a handful of
// bytes and says nothing about a page), and no soft-failure gate (that rule is
// about reads).
func (m *Manager) RecordRouterError(envelopeID, command, host string, tabID int, p core.ResponsePayload) {
	if m == nil {
		return
	}
	rec := TraceRecord{
		Kind:     KindRouterError,
		AtMs:     nowMs(),
		Envelope: envelopeID,
		Command:  safeCommand(command),
		TabID:    tabID,
		Browser:  m.browserID,
		Host:     host,
		Outcome:  OutcomeError,
		ErrCode:  errCode(p.Error),
	}
	if err := m.stream.Append(rec); err != nil {
		m.logf("memory: record router error %s: %v", command, err)
	}
	// The learner is woken so the cursor keeps pace with the trace, but there
	// is nothing here for it to learn: buildSegments drops the record before it
	// can reach a segment.
	m.wake()
}

// RecordResult records the inbound half and updates the tab's view of where it
// is. This is the extension's answer about the page — see RecordRouterError for
// the payloads the control plane mints for itself.
func (m *Manager) RecordResult(envelopeID, command, host string, tabID int, p core.ResponsePayload) {
	if m == nil {
		return
	}
	rec := TraceRecord{
		Kind:     KindResponse,
		AtMs:     nowMs(),
		Envelope: envelopeID,
		Command:  safeCommand(command),
		TabID:    tabID,
		Browser:  m.browserID,
		Host:     host,
		Outcome:  OutcomeOK,
	}
	if p.Error != "" || p.Status == "error" {
		rec.Outcome = OutcomeError
		// The code, never the message: `error` on the wire is the extension's
		// `err.message`, and the not-found one quotes the selector back — which
		// for the querySelectorByText path is the page's own text. p.Message is
		// dropped for the same reason and for a second one: it comes from
		// chrome.runtime.lastError, which can carry the tab's URL. See errCode.
		rec.ErrCode = errCode(p.Error)
	}

	// The response record carries the command's host and arguments. Without
	// them only `navigate` can ever teach the learner anything, because a
	// click or a get_text names no site — so the commonest MCP flow (switch to
	// an open tab, then work) would produce no card at all.
	rec.Args = m.takeInflight(envelopeID)

	tab := tabKey(tabID)

	// A closed tab is gone, so its scratch state is too. Pruned here as well as
	// bounded above: the bound covers the closes the router never sees — a tab
	// the user closed — while this keeps the common case exact. Guarded on the
	// close having succeeded, because a failed close leaves the tab open and
	// its digest worth keeping.
	if command == "tab:close" && p.Status != "error" {
		m.mu.Lock()
		m.forgetTabLocked(tabID)
		m.mu.Unlock()
	}

	if p.Status == "error" {
		// A failure is worth waking the learner for immediately: it is the
		// signal that creates a card for a host (the cold-start rule) and the
		// only tier that cannot be misread.
		if err := m.stream.Append(rec); err != nil {
			m.logf("memory: record failure %s: %v", command, err)
		}
		m.wake()
		return
	}

	// Payload magnitude on every result, not only the ones past the soft limit:
	// the threshold is the only consumer that matters, but a size that is
	// recorded only when it trips a threshold is a size you cannot reason about
	// after the fact.
	rec.ResultSz = len(p.Data)
	// Content size, for the reads whose result *is* content. This is the only
	// number the site map ranks on, and it is deliberately a different field
	// from ResultSz rather than a reinterpretation of it — see TraceRecord.
	rec.ContentSz = contentSize(command, p.Data)

	// Soft failure: a read that "succeeded" by returning most of a page is the
	// ADR-0003 incident, and no-error-means-success would otherwise record it as
	// a good procedure.
	//
	// Measured on ContentSz, not ResultSz, and each half of that matters.
	//
	// ResultSz is len(p.Data) — JSON *bytes*. The threshold's own doc and the
	// message below both speak in characters, so a payload of 30,000 CJK
	// characters is 90,011 bytes and was recorded as an oversized read at well
	// under the 60,000-character intent. Every agent working in a non-Latin
	// script permanently learned "reads fail here", and mergeProcedure refuses
	// any segment carrying a failure, so the working sequence was never learned
	// either. contentSize already counts runes, which is what the constant was
	// always about.
	//
	// And the gate applied to *every* command, not the reads it describes. A
	// screenshot's base64 data URL is always past 60KB, so every screenshot was
	// recorded as oversized_result — permanently in the failure tier, the one
	// ADR-0018 calls the signal it trusts most. contentSize is 0 for anything
	// that is not a content read, which is exactly the scoping wanted: a large
	// result that is not a page dump is not this rule's business.
	if n := rec.ContentSz; n > SoftFailureThreshold {
		rec.Outcome = OutcomeSoft
		rec.ErrCode = "oversized_result"
		rec.ErrMsg = fmt.Sprintf("read returned %d chars, past the %d soft limit — whole page, not content", n, SoftFailureThreshold)
	}

	// The tab's view of where it is and what the last page looked like is shared
	// state, and the router calls this from every command goroutine at once, so
	// it is all done under m.mu. Doing it unlocked is a data race on the map
	// itself, not just on a field — and a race here would be a crash in the
	// control plane, which is the one thing this feature must never be.
	if command == "snapshot" {
		if digest := digestOf(p.Data); digest != nil {
			rec.Page = digest
			m.mu.Lock()
			ts := m.tabLocked(tab)
			ts.lastDigest = digest
			// A snapshot can be the first thing that reveals where a tab is: the
			// tab was already open when the daemon started, or a click navigated
			// without a landing command. Arm for that one case only. Arming on
			// every snapshot would re-inject the card after every read, because
			// verification clears verifyOn and there would be nothing left to
			// compare against.
			if host != "" && !ts.sawLanding {
				m.armLocked(ts, host)
			}
			m.mu.Unlock()
		}
	} else if isLanding(command) && host != "" {
		// A landing always re-arms, even for a host already shown on this tab:
		// a fresh navigation is a fresh opportunity to be told what we last
		// learned about the place.
		m.mu.Lock()
		ts := m.tabLocked(tab)
		ts.sawLanding = true
		// And it invalidates the page we were looking at. Without this, a
		// cross-site navigation verifies the *new* host's card against the
		// *previous* host's digest — which resolves nothing, trips the majority
		// rule, and records a healthy card as stale. The agent is told to
		// distrust a card that was fine, and a human reading the revision
		// history sees a redesign that never happened.
		ts.lastDigest = nil
		m.armLocked(ts, host)
		m.mu.Unlock()
	}

	if err := m.stream.Append(rec); err != nil {
		m.logf("memory: record result %s: %v", command, err)
	}
	m.wake()
}

// TakeSiteNote returns the text an adapter should append to the result it is
// about to show, or "" for nothing. It is the second half of ADR-0019.
//
// Three rules, and each of them exists because of a way this goes wrong:
//
//   - Only a landing command or a snapshot asks. Any other call leaves the arm
//     alone, so an agent that navigates and reaches for get_text without
//     snapshotting does not silently swallow the card.
//   - A landing shows the card once, unverified — there is no page yet.
//   - The next snapshot verifies it, and stays quiet unless the card has gone
//     stale. That check is free: the snapshot was going to be fetched anyway.
func (m *Manager) TakeSiteNote(command, host string, tabID int) string {
	if m == nil {
		return ""
	}
	if command != "snapshot" && !isLanding(command) {
		return ""
	}
	if host == "" {
		return ""
	}
	m.mu.Lock()
	ts := m.tabs[tabKey(tabID)]
	if ts == nil {
		m.mu.Unlock()
		return ""
	}
	armed, verifyOn, digest := ts.noteArmed, ts.verifyOn, ts.lastDigest
	switch {
	case armed:
		ts.noteArmed = false
	case verifyOn != "" && command == "snapshot":
		// Already shown at the landing; this call is the verification.
		ts.verifyOn = ""
	default:
		m.mu.Unlock()
		return ""
	}
	if command == "snapshot" {
		ts.verifyOn = ""
	}
	m.mu.Unlock()
	card, ok := m.store.Get(host)
	if !ok {
		// A host can have a curated guide before it has earned a card — guides
		// are written at the human's request, cards by traffic (ADR-0034). The
		// pointer is still worth the landing. Not recorded as a card shown: no
		// card was.
		//
		// Announced at the landing and nowhere else, which is the rule every
		// other branch here already follows. This return sits above the `armed`
		// check below, so it used to fire on the verification render too — and
		// a host with a guide but no card has nothing else to say, so the agent
		// heard about the same file on every single read. ADR-0034: "The
		// pointer rides the announcement injection only — the landing, or the
		// snapshot that stood in for one. The verification render stays the
		// bare map, or the agent hears about the same file on every read."
		if !armed {
			return ""
		}
		return GuideNote(m.dataDir, host)
	}

	if !armed {
		// The landing already carried the failures and the working sequences.
		// What it could not carry is a usable handle, because no page had been
		// read yet. This is the call where one can be produced for free, so the
		// map is handed back here with refs from the page in hand.
		_, missing := VerifyCard(card, digest, m.browserID)
		if digest == nil || len(card.Map) == 0 {
			return ""
		}
		// A card is treated as stale when its entries are *mostly* gone. The
		// comparison is written as missing*2 > len rather than missing > len/2:
		// with a single entry, integer division makes len/2 zero, so
		// "missing > 0" would read as "stale" and a healthy one-entry card would
		// be declared out of date on every visit.
		if missing > 0 && missing*2 > len(card.Map) {
			// Record the staleness *before* returning. This branch is the one
			// every ordinary navigate → snapshot pair takes, so a card whose
			// predicates stopped matching was reported to the agent and never
			// reported to the learner: the agent was told to distrust it, the
			// card was not marked for rebuilding, and the rebuild only happened
			// later and by accident, from the traffic the distrust caused.
			m.noteShown(card, digest, host)
			return m.recordShown(host, tabID, RenderStaleNotice(host))
		}
		return m.recordShown(host, tabID, RenderCard(card, RenderOptions{
			MaxTokens: DefaultInjectTokens,
			BrowserID: m.browserID,
			OnlyMap:   true,
			Resolver:  func(p Predicate) (string, bool) { return digest.Resolve(p) },
		}))
	}

	opts := RenderOptions{
		MaxTokens:  DefaultInjectTokens,
		BrowserID:  m.browserID,
		Compressed: true,
		OnlyMap:    command == "snapshot",
	}
	// Resolve only when this call read the page. A landing has nothing behind it,
	// so its card is the failures and the working sequences; a snapshot is the
	// one call where a ref from the page in hand is free, and that is when the
	// map is produced — whether this snapshot armed the card (the tab was
	// already open at startup) or merely verified it.
	if digest != nil && command == "snapshot" {
		opts.Resolver = func(p Predicate) (string, bool) { return digest.Resolve(p) }
	}
	out := RenderCard(card, opts)
	if out == "" {
		return GuideNote(m.dataDir, host)
	}
	// This branch is the announcement — the landing, or the snapshot that
	// stood in for one — so the guide's pointer rides here and only here; the
	// verification render above stays the bare map (ADR-0034).
	out = appendGuideNote(out, m.dataDir, host)
	m.noteShown(card, digest, host)
	return m.recordShown(host, tabID, out)
}

// recordShown notes that a card actually reached an agent, and returns the text
// unchanged so the caller can return it directly.
//
// The write is best-effort and never blocks the response. This runs on the path
// that produces the text an agent is waiting for, and the one thing that must
// not happen is a card costing a command — including a command to learn that
// recording the card failed.
func (m *Manager) recordShown(host string, tabID int, out string) string {
	if out == "" {
		return out
	}
	rec := TraceRecord{
		Kind:    KindCardShown,
		AtMs:    nowMs(),
		Host:    host,
		TabID:   tabID,
		Browser: m.browserID,
		Entries: countCardLines(out),
	}
	if err := m.stream.Append(rec); err != nil {
		m.logf("memory: record card shown %s: %v", host, err)
	}
	return out
}

// countCardLines counts the entries a rendered card carried. It reads the
// rendered text rather than the card, because what matters is what the agent
// was actually given: a card whose every entry failed to resolve renders as a
// stale notice, and that is a card shown with nothing in it — which a
// measurement must be able to tell apart from a card shown with content.
func countCardLines(rendered string) int {
	n := 0
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n++
		}
	}
	return n
}

// noteShown marks the card as used. A card whose entries no longer match the
// page is recorded for the learner to rebuild, which is the self-update half of
// the feature: nothing polls for staleness, it is discovered on the one call
// that was going to check it anyway.
func (m *Manager) noteShown(card *SiteCard, digest *PageDigest, verifiedHost string) {
	if digest == nil || verifiedHost != card.Host {
		return
	}
	_, missing := VerifyCard(card, digest, m.browserID)
	if missing == 0 || len(card.Map) == 0 {
		return
	}
	// Same majority rule as the injection path, and for the same reason: a
	// little churn is normal, a redesign is most of the card.
	if missing*2 <= len(card.Map) {
		return
	}
	// An observation, recorded as one. The card on disk is untouched, so there
	// is nothing to number: the revision this names is the one that was found
	// not to match, which is exactly what a reader of `memory history` needs to
	// know. Writing KindCardRevision with card.Revision+1 instead minted a
	// revision that never existed, and applySegment's next write claimed the
	// same number with different content and a different reason — so the audit
	// trail showed two versions where there was one, plus one.
	obs := CardRevision{
		Host:     card.Host,
		Revision: card.Revision,
		Reason:   "stale",
		Summary:  fmt.Sprintf("%d of %d site-map entries no longer match the page", missing, len(card.Map)),
		AtMs:     nowMs(),
	}
	if err := m.stream.Append(TraceRecord{Kind: KindCardStale, AtMs: obs.AtMs, Revision: &obs}); err != nil {
		m.logf("memory: record staleness %s: %v", card.Host, err)
	}
	m.wake()
}

// tabLocked returns the tab's state, creating it on first sight. The caller
// must hold m.mu — the map is written from every command goroutine.
// takeInflight returns the args recorded for a command, and forgets them.
func (m *Manager) takeInflight(envelopeID string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	in, ok := m.inflight[envelopeID]
	if !ok {
		return nil
	}
	delete(m.inflight, envelopeID)
	return in.args
}

func (m *Manager) tabLocked(tabID int) *tabState {
	key := tabKey(tabID)
	ts, ok := m.tabs[key]
	if !ok {
		ts = &tabState{}
		m.tabs[key] = ts
		m.tabsOrder = append(m.tabsOrder, key)
		for len(m.tabsOrder) > maxTabs {
			oldest := m.tabsOrder[0]
			m.tabsOrder = m.tabsOrder[1:]
			delete(m.tabs, oldest)
		}
	}
	return ts
}

// forgetTabLocked drops a tab's scratch state. The caller must hold m.mu.
//
// Called when a tab closes. Anything still armed for it is discarded, which is
// correct rather than lossy: a card armed for a tab that no longer exists would
// otherwise be verified against whatever page the *next* tab with that id
// showed, since Chrome reuses ids.
func (m *Manager) forgetTabLocked(tabID int) {
	delete(m.tabs, tabKey(tabID))
}

// armLocked schedules this tab's card for the named host and asks the next
// snapshot to verify it. The caller must hold m.mu.
//
// The existence check deliberately does NOT consult the store here: that would
// be a file read and a JSON parse on every landing, taken while holding the lock
// that every other command's tab state needs. Arming is unconditional and the
// real lookup happens in TakeSiteNote, off the lock; a host with no card arms
// and then yields "".
func (m *Manager) armLocked(ts *tabState, host string) {
	ts.noteArmed = true
	ts.verifyOn = host
}

// digestOf reduces a snapshot result to structure (ADR-0018). Returns nil for
// anything that is not a usable snapshot, which is the common case for every
// other command.
func digestOf(data json.RawMessage) *PageDigest {
	if len(data) == 0 {
		return nil
	}
	var res snapshotResult
	if err := json.Unmarshal(data, &res); err != nil || res.Snapshot == "" {
		return nil
	}
	url, title := splitPageLine(res.Snapshot)
	return ParseSnapshot(res.Snapshot, res, url, title)
}

// contentSize measures the characters of content a read returned, and returns 0
// for anything that is not a content read.
//
// Returning 0 rather than a payload length is the whole point. A single
// magnitude cannot serve both questions the trace needs answered: "did this read
// return too much to be content" (a payload question — ResultSz answers it) and
// "is this container on the page worth reading" (a content question). Answering
// the second with the first is not an approximation, it is a category error —
// and it is loudest exactly where it hurts, because a screenshot is the one
// result whose payload is orders of magnitude larger than the text it came from.
//
// The command is checked rather than the payload shape, so a future command that
// returns content under a new key has to be added here deliberately rather than
// starting to rank by accident.
func contentSize(command string, data json.RawMessage) int {
	field := ""
	switch command {
	case "gettext":
		field = "text"
	case "gethtml":
		field = "html"
	default:
		return 0
	}
	var res map[string]json.RawMessage
	if err := json.Unmarshal(data, &res); err != nil {
		return 0
	}
	raw, ok := res[field]
	if !ok {
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	return utf8.RuneCountInString(s)
}

// splitPageLine reads the `Page: <title> | <url>` prefix the snapshot renderer
// puts on the first line (SnapshotMeta in packages/shared/src/snapshot.ts).
// The split is from the right because a title can contain " | ".
//
// Both halves are page-controlled, and the loop below has to be bounded on the
// right as carefully as on the left: it reads rest[i+1] to test for the space
// after a separator, so a separator sitting on the last byte walked off the end
// of the slice. That is not exotic — when a snapshot's body is empty the
// renderer emits `prefix.trimEnd()`, which strips the trailing space and leaves
// the line ending in a bare `|`. This runs on every snapshot, in a WS read-loop
// goroutine, so a page-controlled title could take the whole daemon with it.
func splitPageLine(snapshot string) (url, title string) {
	line := snapshot
	if i := indexByte(snapshot, '\n'); i >= 0 {
		line = snapshot[:i]
	}
	const prefix = "Page: "
	if len(line) <= len(prefix) || line[:len(prefix)] != prefix {
		return "", ""
	}
	rest := line[len(prefix):]
	// The last index a separator can sit at is len(rest)-2, because the
	// candidate is rest[i] and the space that must follow it is rest[i+1].
	for i := len(rest) - 2; i >= 1; i-- {
		if rest[i] == '|' && rest[i-1] == ' ' && rest[i+1] == ' ' {
			return rest[i+2:], rest[:i-1]
		}
	}
	return "", ""
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// wake nudges the learner without blocking: a full signal channel already means
// "there is unread work", so a second nudge is a no-op and no work is lost.
func (m *Manager) wake() {
	select {
	case m.signal <- struct{}{}:
	default:
	}
}

// Start runs the learner loop until ctx is canceled. It is the background job
// ADR-0020 describes: idle, then learn, then idle again.
func (m *Manager) Start(ctx context.Context) {
	go m.loop(ctx)
}

func (m *Manager) loop(ctx context.Context) {
	timer := time.NewTimer(m.idleAfter)
	defer timer.Stop()
	if !timer.Stop() {
		<-timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.signal:
			// Wait for the stream to go quiet so one pass sees a whole
			// session's worth of segments rather than racing the agent.
			if !m.waitQuiet(ctx) {
				return
			}
			if err := m.learn.Run(); err != nil {
				m.logf("memory: learn pass: %v", err)
			}
			m.rotateIfOwned()
			timer.Reset(m.idleAfter)
		case <-timer.C:
			// A pass on a timer as well as on a signal: if the daemon was
			// killed with unread records and the cursor never moved, the next
			// start still makes progress without needing a new signal.
			if err := m.learn.Run(); err != nil {
				m.logf("memory: learn pass: %v", err)
			}
			m.rotateIfOwned()
			timer.Reset(m.idleAfter)
		}
	}
}

// rotateIfOwned rolls the stream over when this Manager owns it.
//
// The cursor has just been advanced to the end of a pass, which is the state
// Rotate requires: every record in the active generation is learned, so moving
// the file aside cannot renumber anything out from under a reader. A no-op for
// a Manager that does not own the stream.
func (m *Manager) rotateIfOwned() {
	if !m.ownsStream {
		return
	}
	rotateOwnedStream(m.stream, m.cursor, m.logf)
}

// waitQuiet blocks until no new record has arrived for idleAfter, reporting
// false if ctx was canceled.
func (m *Manager) waitQuiet(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-m.signal:
			// More work arrived while draining; keep waiting for quiet.
		case <-time.After(m.idleAfter):
			return true
		}
	}
}

// LearnNow runs one pass synchronously. `bridge memory learn` uses it, and it
// is how the first shadow-mode data is produced on demand.
func (m *Manager) LearnNow() error { return m.learn.Run() }

// Close releases the stream handle.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	return m.stream.Close()
}

var _ core.MemoryHook = (*Manager)(nil)
