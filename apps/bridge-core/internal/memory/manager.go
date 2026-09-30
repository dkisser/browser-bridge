package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

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
	// RecordResult. Bounded by the router's own in-flight set, and cleared as
	// each result lands.
	inflight map[string]inflightCmd
	// browserID is the profile cards are attributed to (ADR-0019's
	// per-browser annotation). A card entry remembers the browser it was
	// observed under and is withheld from the others.
	browserID string
}

type inflightCmd struct {
	args map[string]any
}

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
	store, err := OpenStore(opts.DataDir)
	if err != nil {
		return nil, err
	}
	cursor := LoadCursor(opts.DataDir)
	learn := NewLearn(stream, store, cursor, opts.Compressor, logf)
	idle := opts.IdleAfter
	if idle <= 0 {
		idle = defaultIdleAfter
	}
	return &Manager{
		stream:    stream,
		store:     store,
		cursor:    cursor,
		learn:     learn,
		logf:      logf,
		signal:    make(chan struct{}, 1),
		idleAfter: idle,
		tabs:      make(map[int]*tabState),
		inflight:  make(map[string]inflightCmd),
		browserID: opts.BrowserID,
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
		Command:  command,
		TabID:    tabID,
		Browser:  m.browserID,
		Host:     host,
		Args:     redacted,
		AtMs:     nowMs(),
	}
	if err := m.stream.Append(rec); err != nil {
		m.logf("memory: record command %s: %v", command, err)
	}
	m.mu.Lock()
	m.inflight[envelopeID] = inflightCmd{args: redacted}
	m.mu.Unlock()
	m.wake()
}

// RecordResult records the inbound half and updates the tab's view of where it
// is. p may be an error payload the router synthesised rather than one the
// extension sent; both are evidence, and a synthesized failure is often the
// most instructive kind (the agent asked a browser that was not there).
func (m *Manager) RecordResult(envelopeID, command, host string, tabID int, p core.ResponsePayload) {
	if m == nil {
		return
	}
	rec := TraceRecord{
		Kind:     KindResponse,
		AtMs:     nowMs(),
		Envelope: envelopeID,
		Command:  command,
		TabID:    tabID,
		Browser:  m.browserID,
		Host:     host,
		Outcome:  OutcomeOK,
	}
	if p.Error != "" || p.Status == "error" {
		rec.Outcome = OutcomeError
		rec.ErrCode = p.Error
		rec.ErrMsg = p.Message
	}

	// The response record carries the command's host and arguments. Without
	// them only `navigate` can ever teach the learner anything, because a
	// click or a get_text names no site — so the commonest MCP flow (switch to
	// an open tab, then work) would produce no card at all.
	rec.Args = m.takeInflight(envelopeID)

	tab := tabKey(tabID)

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

	// Soft failure: a read that "succeeded" by returning most of a page is the
	// ADR-0003 incident, and no-error-means-success would otherwise record it as
	// a good procedure.
	if n := resultSize(p.Data); n > SoftFailureThreshold {
		rec.Outcome = OutcomeSoft
		rec.ResultSz = n
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
		return ""
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
			return RenderStaleNotice(host)
		}
		return RenderCard(card, RenderOptions{
			MaxTokens: DefaultInjectTokens,
			BrowserID: m.browserID,
			OnlyMap:   true,
			Resolver:  func(p Predicate) (string, bool) { return digest.Resolve(p) },
		})
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
		return ""
	}
	m.noteShown(card, digest, host)
	return out
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
	rev := CardRevision{
		Host:     card.Host,
		Revision: card.Revision + 1,
		Reason:   "stale",
		Summary:  fmt.Sprintf("%d of %d site-map entries no longer match the page", missing, len(card.Map)),
		AtMs:     nowMs(),
	}
	if err := m.stream.Append(TraceRecord{Kind: KindCardRevision, AtMs: rev.AtMs, Revision: &rev}); err != nil {
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
	ts, ok := m.tabs[tabKey(tabID)]
	if !ok {
		ts = &tabState{}
		m.tabs[tabKey(tabID)] = ts
	}
	return ts
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

// resultSize is a cheap UTF-8 length of a JSON payload, used only as a
// magnitude for the soft-failure threshold.
func resultSize(data json.RawMessage) int { return len(data) }

// splitPageLine reads the `Page: <title> | <url>` prefix the snapshot renderer
// puts on the first line (SnapshotMeta in packages/shared/src/snapshot.ts).
// The split is from the right because a title can contain " | ".
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
	for i := len(rest) - 1; i >= 1; i-- {
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
			timer.Reset(m.idleAfter)
		case <-timer.C:
			// A pass on a timer as well as on a signal: if the daemon was
			// killed with unread records and the cursor never moved, the next
			// start still makes progress without needing a new signal.
			if err := m.learn.Run(); err != nil {
				m.logf("memory: learn pass: %v", err)
			}
			timer.Reset(m.idleAfter)
		}
	}
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
