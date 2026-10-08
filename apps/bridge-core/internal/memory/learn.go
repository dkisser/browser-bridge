package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The learner is ADR-0020's consumer: one pass over the unread suffix of the
// stream, turning call observations into per-host cards. It is pure local file
// analysis — it never touches the browser, which is why it can run whenever the
// machine is idle and why ADR-0018's structural-only record is enough to do the
// job at all.
//
// The three tiers are written by three different rules because the three kinds
// of knowledge are not equally trustworthy:
//
//	map        read off page structure; needs no success signal at all
//	failures   the one signal that cannot be misread — an error is an error
//	procedures inherits every false positive "no error means success" produces,
//	           so a procedure is held back until ProcedureCorroboration
//	           independent successes agree on it
//
// Cold start (ADR-0019's "failure creates a card immediately, two quiet
// segments are needed otherwise") falls out of the same structure: a host with
// a failure is written on sight; a host with only successes is not written
// until a procedure shape repeats.

// segmentIdleGap splits task segments. The bridge has no task concept of its
// own (ADR-0020), so a segment is delimited by the two things it can actually
// observe: a change of host, and a silence long enough that the next command is
// plausibly a new attempt.
const segmentIdleGap = 2 * time.Minute

// maxSegmentsPerPass bounds one pass so a long-idle daemon returning to a large
// backlog does not hold the goroutine indefinitely. The cursor means the rest is
// picked up next pass.
const maxSegmentsPerPass = 64

// maxCardEntries caps each tier. A card is an injected artefact with a token
// budget, not an archive — the stream is the archive, and anything trimmed here
// is still recoverable from it.
const (
	maxMapEntries       = 24
	maxFailureEntries   = 12
	maxProcedureEntries = 8
)

// segment is one attempt at something, reconstructed from the stream.
type segment struct {
	host    string
	browser string
	startMs int64
	endMs   int64

	// digest is the last successful snapshot seen in this segment. It is the
	// page the segment's calls were aimed at.
	digest   *PageDigest
	digestOK bool

	// calls is the successful, element-addressing calls made in this segment,
	// in order, paired with the ref they addressed.
	calls []observedCall

	// failures are the failed calls, in order.
	failures []failedCall

	// idx is this segment's position in the pass that built it. The pass cap
	// discards everything at or past maxSegmentsPerPass, and a response that
	// lands in a discarded segment must leave its command in the carried window
	// — otherwise the rewound cursor skips the command on the next read and the
	// pairing is lost for good.
	idx int

	// shape is the canonical call sequence, used to recognise a repeat. Each
	// entry carries the ref the call addressed, if any, so a procedure step and
	// the observation behind it are the same record rather than two lists that
	// have to be lined up afterwards.
	shape []shapeStep

	// envelope of the first command, as evidence on a written procedure.
	firstEnvelope string
	// startLine is the stream line this segment's first record occupies, used to
	// hold the cursor back when a pass is capped.
	startLine int64
}

type observedCall struct {
	command string
	ref     string
	node    NodeSig
	found   bool
	// value is how many characters of content came back. It is the site map's
	// ranking signal and the only one, which is why it is 0 for anything that is
	// not a content read rather than a payload length (see TraceRecord.ContentSz).
	value int
	atMs  int64
}

// failedCall is one rejected call. It deliberately carries no copy of the
// browser's error message: that message is where a bare-text selector or an
// attribute literal comes back verbatim (see safeSelector), nothing rendered it,
// and a card file outlives the visit. The error *code* is the part that
// generalises — "no such element" and "not allowed" are lessons, "no element
// matches \"her lawyer's note\"" is a copy.
// shapeStep is one call in a segment's shape: what was issued, and — when the
// call addressed an element the page confirmed — what it addressed and whether
// the snapshot knew that element. Ref and fact travel together for the same
// reason a map entry's purpose does: a procedure step is a claim about a
// specific control, and reconstructing which control afterwards is how the claim
// ends up attached to the wrong one.
type shapeStep struct {
	command string
	ref     string
	found   bool
}

type failedCall struct {
	command string
	sig     string
	sel     string
	browser string
	atMs    int64
}

// purposeOf is the site map's vocabulary: what the agent actually *did* with an
// element, not what the element appears to be. A map entry exists because a call
// succeeded on it, so the honest label is the call.
func purposeOf(command string) string {
	switch command {
	case "gettext":
		return "text container"
	case "gethtml":
		return "markup container"
	case "click":
		return "click target"
	case "type":
		return "input field"
	case "select":
		return "select control"
	case "hover":
		return "hover target"
	case "wait:element":
		return "presence check"
	case "scroll":
		return "scroll container"
	case "screenshot":
		return "visual target"
	default:
		return command + " target"
	}
}

// Learn is one pass of the background learner.
type Learn struct {
	stream *Stream
	store  *Store
	cursor *Cursor
	// compress is optional (ADR-0022). When nil, cards keep their rendered form.
	compress Compressor
	logf     func(format string, args ...any)
	// now is injectable so tests are not at the mercy of the wall clock.
	now func() time.Time
	// pending carries command records whose response has not arrived yet from
	// one pass to the next. See buildSegments for why it is carried rather
	// than re-read. Only Manager.loop and LearnNow call Run, and never
	// concurrently, so it needs no lock.
	pending map[string]TraceRecord
}

// maxPendingCommands bounds the cross-pass command window. A command whose
// response never arrives — a daemon killed between the two records — would
// otherwise sit in the map for the life of the process.
const maxPendingCommands = 512

// Compressor is the optional ADR-0022 model call. It renders a card to a
// shorter form and never decides what is in the card.
type Compressor interface {
	Compress(host string, rendered string) (string, error)
}

// NewLearn builds a learner over the given stream, store and cursor.
func NewLearn(stream *Stream, store *Store, cursor *Cursor, compress Compressor, logf func(string, ...any)) *Learn {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Learn{
		stream:   stream,
		store:    store,
		cursor:   cursor,
		pending:  make(map[string]TraceRecord),
		compress: compress,
		logf:     logf,
		now:      time.Now,
	}
}

// Run performs one pass.
//
// What "safe to re-run" means here is deliberately weaker than idempotent, and
// the difference is worth stating: a pass that is interrupted *before* the
// cursor advances re-reads the same records and produces the same card shape,
// but the counters are incremented, not recomputed. A crash between the card
// write and the cursor write therefore double-counts whatever that segment
// contributed. The window is one card write wide and the effect is a count that
// is too high, which is a worse statistic rather than a wrong instruction — and
// making it exact would mean recomputing every count from the whole stream on
// every pass, which is the thing the cursor exists to avoid.
func (l *Learn) Run() error {
	started := l.now()
	from := l.cursor.Get()
	recs, next, skipped, err := l.stream.ReadFrom(from)
	run := LearnRun{From: from, To: next, Read: len(recs), Skipped: skipped}
	if err != nil {
		// A pass that cannot read must not advance the cursor, or the records
		// it failed to read are gone as far as the learner is concerned.
		return err
	}
	defer func() {
		run.Duration = l.now().Sub(started).Milliseconds()
		if aerr := l.stream.Append(TraceRecord{Kind: KindLearnRun, AtMs: nowMs(), Run: &run}); aerr != nil {
			l.logf("memory: record learn run: %v", aerr)
		}
	}()

	segments, pending := buildSegments(recs, from, l.pending)
	l.pending = pending
	cursorTo := next
	if len(segments) > maxSegmentsPerPass {
		// Process the oldest ones and leave the cursor at the first one we did
		// NOT process, so the next pass picks it up. Dropping the *front* and
		// advancing anyway (which an earlier version did) throws those lessons
		// away permanently: the records are behind the cursor and nothing ever
		// looks at them again. A daemon idle over a long backlog is exactly the
		// case where this would bite.
		//
		// Rewind to the start of the last segment this pass *did* process, not
		// to the first deferred one. A segment's host comes from the navigate
		// or tab:switch that precedes it, and a tab's context is rebuilt from
		// the records read on each pass — so starting the next pass at the
		// deferred segment's first command leaves its navigate behind the
		// cursor, the host unknown, and the segment silently skipped forever.
		// Overlapping by exactly one segment is the smallest rewind that keeps
		// that context, and it costs one re-processed segment per capped pass.
		//
		// The re-processed segment's counts are incremented rather than
		// recomputed, which is the same bounded over-count a crash between the
		// card write and the cursor write produces.
		deferredFrom := segments[maxSegmentsPerPass-1].startLine
		// Counted before the truncation. After it, len(segments) *is* the cap,
		// so the audit line used to read "capped at 64 of 64 segments; the rest
		// are deferred" — the one number in it that could not be trusted was the
		// one telling you how much had been thrown away.
		deferred := len(segments) - maxSegmentsPerPass
		segments = segments[:maxSegmentsPerPass]
		if deferredFrom < from {
			deferredFrom = from
		}
		cursorTo = deferredFrom
		run.Error = fmt.Sprintf("pass capped at %d of %d segments; %d deferred to the next pass",
			maxSegmentsPerPass, maxSegmentsPerPass+deferred, deferred)
	}

	hosts := make(map[string]bool)
	for _, seg := range segments {
		if seg.host == "" {
			continue
		}
		hosts[seg.host] = true
		if err := l.applySegment(seg); err != nil {
			l.logf("memory: learn %s: %v", seg.host, err)
		}
	}
	run.Hosts = len(hosts)

	// The cursor advances only after every card write has landed, so a crash
	// costs a re-read rather than a skipped lesson. It stops at cursorTo, which
	// is short of `next` when the pass was capped.
	if cursorTo > from {
		if err := l.cursor.Set(cursorTo); err != nil {
			return fmt.Errorf("memory: advance cursor: %w", err)
		}
		run.Consumed = int(cursorTo - from)
	}
	return nil
}

// buildSegments reconstructs attempts from the flat record stream. It tracks a
// per-tab current host, because that is the only way the bridge can know which
// site a later call belongs to: a click carries no URL.
//
// from is the stream line recs[0] corresponds to, so that a segment can report
// where it starts in absolute terms — which is what a capped pass needs in
// order to leave the cursor behind the work it deferred.
//
// The in-progress segment is per tab as well. A single shared one would splice
// two tabs' calls into a sequence no agent ever performed, and mergeProcedure
// would then corroborate that invention.
//
// carried holds commands whose response had not arrived when the previous pass
// ended, and the returned map is that set for the next pass. Without it, a
// response that crossed a pass boundary found no command to pair with and hit
// the `continue` below: no segment, no card, and the evidence behind the cursor
// for good. That is not exotic — Manager.loop runs a pass on a 20s idle timer
// as well as on the wake signal, so any call slower than that splits — and the
// loss is worst exactly where it matters, because a failure is the one signal
// this package says cannot be misread and the cold-start rule is built on.
//
// Carried rather than re-read by rewinding the cursor to the command's line.
// Rewinding would work, but it re-processes the tail of every pass and
// re-increments the counts of segments already applied, and against a 20s
// timer that is most passes. Carrying costs one small map.
//
// The cost of carrying is that a command with no line in this pass anchors its
// segment at the response's line instead (see commandLine). The segment is
// fully processed in this pass either way, so that is the best available
// anchor rather than a lost one, and strictly better than dropping the pair.
func buildSegments(recs []TraceRecord, from int64, carried map[string]TraceRecord) ([]*segment, map[string]TraceRecord) {
	commands := make(map[string]TraceRecord, len(recs)+len(carried))
	for env, cmd := range carried {
		commands[env] = cmd
	}
	// answered records which commands this pass paired, so only the genuinely
	// unanswered ones are carried forward.
	answered := make(map[string]bool, len(recs))
	// commandLines remembers where each command's *command* record sat, not its
	// response. A segment has to start at its command: a deferred pass sets the
	// cursor to the first deferred segment's start, and if that line is the
	// response, the command that produced it falls behind the cursor and the
	// segment is dropped on the next read. Anchoring at the command means the
	// pair is always read together.
	commandLines := make(map[string]int64, len(recs))
	// hostByTab carries the last site a tab was known to be on.
	hostByTab := make(map[int]string)
	curByTab := make(map[int]*segment)

	var out []*segment

	for line, r := range recs {
		switch r.Kind {
		case KindCommand:
			commands[r.Envelope] = r
			commandLines[r.Envelope] = from + int64(line)
			if r.Command == "navigate" {
				// A navigate is the strongest possible segment boundary: the
				// agent has declared it is going somewhere new, so whatever it
				// was doing in this tab is over.
				//
				// Only navigate. The other landings (goBack, refresh,
				// tab:switch, wait:navigation) very often occur *inside* one
				// attempt — clicking a link and then waiting for the page is the
				// single most common flow there is — and cutting on them would
				// split every real task in half so that no shape is ever seen
				// twice.
				curByTab[tabKey(r.TabID)] = nil
			}
		case KindRouterError:
			// The control plane's own error — browser_offline, cannot_buffer,
			// sw_timeout — not the site's. It is paired so the command stops
			// being pending, then dropped: a card is a claim about a host, and
			// "the browser was not connected" is not one. Recorded in the
			// trace for `memory history`, where a human is reading for
			// diagnostics rather than as learned knowledge about a site.
			//
			// sw_timeout is the case that makes this concrete: it times out
			// against the tab's *previous* host, so keeping it put a failure
			// on a card asserting that the wrong site times out.
			if _, ok := commands[r.Envelope]; ok {
				answered[r.Envelope] = true
			}
			continue
		case KindResponse:
			cmd, ok := commands[r.Envelope]
			if !ok {
				continue
			}
			// A response that lands in a segment this pass will discard must not
			// mark its command answered.
			//
			// The cap rewinds the cursor to the start of the last segment the
			// pass *did* process — which is ahead of a carried command's own
			// line — so the command will not be re-read. Marking it answered
			// then drops it from the carried window, and the next pass finds the
			// response with nothing to pair it to and hits the `continue`,
			// losing the evidence permanently. That is the same loss the window
			// exists to prevent, one cap-sized step further along.
			//
			// A landing never creates a segment, so it is answered here; whether
			// the segment a *call* lands in survives the cap is only known once
			// `cur` is resolved, below.
			tab := tabKey(cmd.TabID)
			if isLanding(cmd.Command) {
				answered[r.Envelope] = true
				// The landing command's own response is what tells us the host.
				host := hostFromRecord(r, cmd)
				if host != "" {
					hostByTab[tab] = host
				}
				continue
			}

			host := hostByTab[tab]
			if r.Host != "" {
				host = r.Host
			}
			if host == "" {
				// A call on an unknown site teaches nothing about that site.
				// Commands on chrome:// and about: pages land here and are
				// correctly ignored.
				continue
			}

			cur := curByTab[tab]
			if cur == nil || cur.host != host || startedNewSegment(cur, r) {
				cur = &segment{
					host:          host,
					browser:       r.Browser,
					startMs:       r.AtMs,
					firstEnvelope: cmd.Envelope,
					startLine:     commandLine(cmd, commandLines, from, line),
				}
				cur.idx = len(out)
				curByTab[tab] = cur
				out = append(out, cur)
			}
			// Only a segment this pass will keep counts as answered. A deferred
			// one is discarded by the cap, and the rewound cursor never comes
			// back far enough to re-read its command.
			if cur.idx < maxSegmentsPerPass {
				answered[r.Envelope] = true
			}
			cur.endMs = r.AtMs
			cur.shape = append(cur.shape, shapeStep{command: cmd.Command})
			if r.Browser != "" {
				cur.browser = r.Browser
			}

			if r.Outcome == OutcomeOK {
				consumeSuccess(cur, r, cmd)
			} else {
				cur.failures = append(cur.failures, failureOf(r, cmd))
			}
		}
	}
	return out, unansweredCommands(commands, answered)
}

// unansweredCommands is the window carried into the next pass: every command
// this one still has no response for, bounded so a command whose response never
// comes — a daemon killed between the two records — cannot accumulate for the
// life of the process.
func unansweredCommands(commands map[string]TraceRecord, answered map[string]bool) map[string]TraceRecord {
	candidates := make([]TraceRecord, 0, len(commands))
	for env, cmd := range commands {
		if !answered[env] {
			candidates = append(candidates, cmd)
		}
	}
	if len(candidates) > maxPendingCommands {
		// Keep the newest, so the window tracks current traffic rather than
		// whatever arrived first.
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].AtMs > candidates[j].AtMs })
		candidates = candidates[:maxPendingCommands]
	}
	pending := make(map[string]TraceRecord, len(candidates))
	for _, cmd := range candidates {
		pending[cmd.Envelope] = cmd
	}
	return pending
}

// commandLine is the absolute stream line of a call's command record, falling
// back to the response's own line if the command predates this read (which can
// only happen on the very first segment of a pass).
func commandLine(cmd TraceRecord, lines map[string]int64, from int64, respLine int) int64 {
	if l, ok := lines[cmd.Envelope]; ok {
		return l
	}
	return from + int64(respLine)
}

func startedNewSegment(cur *segment, r TraceRecord) bool {
	if cur.endMs == 0 {
		return false
	}
	return r.AtMs-cur.endMs > int64(segmentIdleGap/time.Millisecond)
}

func tabKey(tabID int) int {
	if tabID < 0 {
		return 0
	}
	return tabID
}

// isLanding reports the commands that establish which site a tab is on.
func isLanding(command string) bool {
	switch command {
	case "navigate", "goBack", "goForward", "refresh", "wait:navigation", "tab:switch", "tab:new":
		return true
	default:
		return false
	}
}

// hostFromRecord pulls the host out of a landing response's payload, which the
// command record already carries in its `url` arg for navigate and in the
// response body for the rest.
func hostFromRecord(r TraceRecord, cmd TraceRecord) string {
	if h := HostFromURL(stringArg(cmd.Args, "url")); h != "" {
		return h
	}
	if h := HostFromURL(stringArg(r.Args, "url")); h != "" {
		return h
	}
	return r.Host
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

// consumeSuccess folds a successful call into the segment: a snapshot replaces
// the page the segment is reasoning about, an element-addressing call records
// what it was aimed at.
// consumeSuccess folds a successful call into the segment: a snapshot replaces
// the page the segment is reasoning about, an element-addressing call records
// what it was aimed at — on the shape entry for this very call, so the two can
// never drift apart.
func consumeSuccess(seg *segment, r TraceRecord, cmd TraceRecord) {
	// The shape entry for this response was appended by the caller moments ago.
	last := len(seg.shape) - 1
	attachRef := func(ref string, found bool) {
		if last < 0 {
			return
		}
		seg.shape[last].ref = ref
		seg.shape[last].found = found
	}
	switch cmd.Command {
	case "snapshot":
		seg.digest = r.Page
		seg.digestOK = r.Page != nil
		return
	}
	sel := selectorOf(cmd.Args)
	if sel == "" || !strings.HasPrefix(sel, "@") {
		// A raw selector is not a ref. It is still evidence that somebody
		// guessed one, but a card entry keyed on a guess is the disease this
		// feature exists to remove, so map entries come only from ref-addressed
		// calls that a snapshot confirmed.
		return
	}
	ref := strings.TrimPrefix(sel, "@")
	call := observedCall{command: cmd.Command, ref: ref, atMs: r.AtMs, value: r.ContentSz}
	if seg.digest != nil {
		for _, n := range seg.digest.Nodes {
			if n.Ref == ref {
				call.node = n
				call.found = true
				break
			}
		}
	}
	seg.calls = append(seg.calls, call)
	attachRef(call.ref, call.found)
}

func failureOf(r TraceRecord, cmd TraceRecord) failedCall {
	// The signature is a code from a closed set, never the message. Nothing is
	// stored here that could quote the page back — see errCode. "unknown" is a
	// real answer, not a missing one: an unrecognised failure still happened,
	// and a failure is the one signal this package trusts most.
	sig := r.ErrCode
	if sig == "" {
		sig = "unknown"
	}
	return failedCall{
		command: cmd.Command,
		sig:     sig,
		sel:     truncate(safeSelector(selectorOf(cmd.Args)), 120),
		browser: r.Browser,
		atMs:    r.AtMs,
	}
}

// applySegment folds one reconstructed attempt into its host's card.
func (l *Learn) applySegment(seg *segment) error {
	// Cold start. A card is created when a segment carries *evidence about the
	// site*, not merely because the agent went there:
	//
	//   - a failure (an error is the one signal that cannot be misread), or
	//   - a confirmed map entry, i.e. a snapshot resolved a ref and an
	//     element-addressing call succeeded on it.
	//
	// A bare navigate produces neither, so wandering through twenty sites
	// leaves twenty no cards. This replaces an earlier version that required two
	// quiet segments, which could not work: the count lived only in this pass,
	// so a host with no failures never crossed the threshold at all.
	evidence := len(seg.failures) > 0 || hasConfirmedCall(seg)
	if !evidence {
		return nil
	}

	card, hadCard := l.store.Get(seg.host)
	created := !hadCard
	if created {
		card = &SiteCard{Host: seg.host, CreatedAtMs: nowMs()}
	}
	changed := mergeFailures(card, seg.failures)
	if mergeMap(card, seg) {
		changed = true
	}
	if mergeProcedure(card, seg) {
		changed = true
	}

	if !changed {
		return nil
	}
	card.Revision++
	card.UpdatedAtMs = nowMs()
	if err := l.store.Put(card); err != nil {
		return err
	}

	summary := fmt.Sprintf("%d failure(s), %d map entr(ies), %d procedure(s)",
		len(card.Failures), len(card.Map), len(card.Procedures))
	revRec := CardRevision{
		Host:     seg.host,
		Revision: card.Revision,
		Reason:   reasonFor(created, seg),
		Summary:  summary,
		AtMs:     card.UpdatedAtMs,
	}
	if seg.firstEnvelope != "" {
		revRec.Evidence = []string{seg.firstEnvelope}
	}
	if err := l.stream.Append(TraceRecord{Kind: KindCardRevision, AtMs: revRec.AtMs, Revision: &revRec}); err != nil {
		return err
	}

	if l.compress != nil {
		l.refreshCompressed(card)
	}
	return nil
}

// hasConfirmedCall reports whether the segment did real, verified work on the
// page: a snapshot that resolved a ref the agent then used successfully.
func hasConfirmedCall(seg *segment) bool {
	for _, c := range seg.calls {
		if c.found {
			return true
		}
	}
	return false
}

func reasonFor(created bool, seg *segment) string {
	switch {
	case created:
		return "new"
	case len(seg.failures) > 0:
		// Not "stale". That word already means the other thing in this stream —
		// noteShown's "the site map no longer matches the page" — and
		// `bridge memory history` is the surface a human reads to decide whether
		// an automatic update was right. One word meaning two things fills that
		// history with false alarms, which is how a review surface stops being
		// consulted.
		return "after_failures"
	default:
		return "rebuilt"
	}
}

// refreshCompressed is the optional ADR-0022 step. A failure here is logged and
// forgotten: the card is already durable, and the compression is a view.
func (l *Learn) refreshCompressed(card *SiteCard) {
	// Compressed: false, deliberately. RenderCard's compressed branch returns
	// card.Compressed when asked for it, so compressing with the default options
	// posted the *previous model output* back at the model from the second pass
	// onward — the card's own fields never reached the endpoint at all, and the
	// failure tier silently vanished from every later compression. The input
	// has to be the card itself or the compression is a feedback loop.
	rendered := RenderCard(card, RenderOptions{MaxTokens: DefaultInjectTokens})
	out, err := l.compress.Compress(card.Host, rendered)
	if err != nil || out == "" {
		l.logf("memory: compress %s: %v", card.Host, err)
		return
	}
	card.Compressed = truncate(out, 2000)
	card.CompressedAtMs = nowMs()
	card.CompressedRev = card.Revision
	if err := l.store.Put(card); err != nil {
		l.logf("memory: store compressed %s: %v", card.Host, err)
	}
}

// mergeFailures folds this segment's failures into the card, grouped by
// signature so a repeated mistake accumulates a count instead of a list.
func mergeFailures(card *SiteCard, failures []failedCall) bool {
	if len(failures) == 0 {
		return false
	}
	before := beforeKeys(card.Failures, failureKey)
	updated := false
	added := false
	for _, f := range failures {
		found := -1
		for i := range card.Failures {
			e := &card.Failures[i]
			if e.Signature == f.sig && e.Sel == f.sel && e.Command == f.command {
				found = i
				break
			}
		}
		if found < 0 {
			card.Failures = append(card.Failures, FailureEntry{
				Signature: f.sig,
				Command:   f.command,
				Sel:       f.sel,
				Count:     1,
				Browser:   f.browser,
				LastAtMs:  f.atMs,
			})
			added = true
			continue
		}
		// A repeat still changes the card, and the count is the whole point of
		// grouping: this failure keeps happening. Not marking it dirty here
		// silently dropped every repeat after the first, which is the one thing
		// the failure tier is for.
		card.Failures[found].Count++
		card.Failures[found].LastAtMs = f.atMs
		updated = true
	}
	sort.SliceStable(card.Failures, func(i, j int) bool {
		return card.Failures[i].Count > card.Failures[j].Count
	})
	if len(card.Failures) > maxFailureEntries {
		card.Failures = card.Failures[:maxFailureEntries]
	}
	// A new entry lands at Count: 1 and the sort is by descending Count, so at
	// the cap it is the first thing cut. Reporting a change anyway told the
	// ledger the failure tier had grown when it had not — and the failure tier
	// is the one signal this package calls unmissable.
	return updated || (added && additionSurvived(card.Failures, failureKey, before))
}

// failureKey is the identity a failure entry is grouped by, and the key
// beforeKeys / additionSurvived compare on.
func failureKey(e FailureEntry) string {
	return e.Signature + "\x00" + e.Sel + "\x00" + e.Command
}

// beforeKeys snapshots which keys a card already holds, so a merge can tell an
// entry it added from one it only updated — and, after the cap has run, tell
// whether the addition survived at all.
func beforeKeys[T any](entries []T, key func(T) string) map[string]bool {
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[key(e)] = true
	}
	return out
}

// additionSurvived reports whether any entry whose key was absent before the
// merge is still present after the cap.
//
// The three merges all append, then rank or sort, then truncate — and an entry
// that was just added is the one most likely to fall off. They returned true
// regardless, so applySegment bumped Revision and appended a card_revision
// record describing an update that never landed. Worse for mergeFailures, where
// the new entry carries Count: 1 and the sort is by descending Count, so a
// genuinely new failure mode on a card with twelve existing entries was
// silently lost while the revision record claimed the failure tier had grown.
//
// `memory history` is the review surface ADR-0019 points a human at before
// accepting an automatic update. An update that did not happen is worse there
// than a missing one, because the reader cannot tell which is which.
func additionSurvived[T any](entries []T, key func(T) string, before map[string]bool) bool {
	for _, e := range entries {
		if !before[key(e)] {
			return true
		}
	}
	return false
}

// mergeMap folds the ref-addressed calls this segment made into the site map.
// Only calls a snapshot confirmed become entries, so the map never contains a
// guess — which is the whole point of the feature.
//
// The map is then ranked, and the cap is applied to the ranking rather than to
// arrival order. Both exist for the same reason. Twenty-four entries in the
// order they were first seen is a log; the agent reading it learns only that
// this page has been poked at. Ranked by how much content each element actually
// returned, it becomes the shortlist an agent wants: on a mail page that is the
// message list first, and the folder sidebar and the toolbar — which also
// "succeeded", and which is why a flat list was useless — far below it.
//
// The rank is content, then frequency, then recency. Content first because it
// is the thing that distinguishes a container worth reading from one that merely
// accepted a call; frequency because a container read on every visit is more
// likely to be the site's main working surface; recency last because it is the
// weakest of the three and is only there to break a genuine tie.
func mergeMap(card *SiteCard, seg *segment) bool {
	if seg.digest == nil {
		return false
	}
	before := beforeKeys(card.Map, func(e MapEntry) string { return e.Pred.String() })
	updated := false
	added := false
	for _, call := range seg.calls {
		if !call.found {
			continue
		}
		pred := PredicateFor(call.node)
		// A node with neither a name nor an attribute cannot be pinned down.
		// Resolve returns the *first* node whose role matches, so after a
		// redesign a bare `button` predicate would land on whatever button now
		// comes first, report itself valid, and hand the agent a ref to the
		// wrong control with the card's own authority behind it. That is worse
		// than the entry being absent: an absent entry makes the agent look,
		// and a confidently wrong one makes it not.
		//
		// This costs the map every unnamed control, which on a real mail page
		// is the per-row "mark read" button — the one control the incident was
		// about. The loss is real and is recorded in ADR-0018's gaps rather
		// than worked around with an ordinal, which would be a different
		// fragile thing wearing the same clothes.
		if pred.Name == "" && pred.AttrKey == "" {
			continue
		}
		key := pred.String()
		found := -1
		for i := range card.Map {
			if card.Map[i].Pred.String() == key {
				found = i
				break
			}
		}
		if found < 0 {
			card.Map = append(card.Map, MapEntry{
				Purpose:      purposeOf(call.command),
				Pred:         pred,
				Browser:      seg.browser,
				ObservedAtMs: call.atMs,
				Uses:         1,
				Value:        call.value,
			})
			added = true
			continue
		}
		// A repeat still changes the card. Not marking it dirty here loses the
		// update on any segment that also carried a failure, because
		// mergeProcedure declines those — the same bug that silently dropped
		// repeated failure counts, in the tier next door.
		card.Map[found].Uses++
		card.Map[found].ObservedAtMs = call.atMs
		updated = true
		// Keep the largest reading, not the latest. A container that returned
		// the whole list once and a title bar the next time is the list, and
		// taking the last value would rank it as a title bar.
		if call.value > card.Map[found].Value {
			card.Map[found].Value = call.value
		}
		if card.Map[found].Browser == "" {
			card.Map[found].Browser = seg.browser
		}
	}
	rankMap(card.Map)
	if len(card.Map) > maxMapEntries {
		card.Map = card.Map[:maxMapEntries]
	}
	// The cap is applied to the ranking, so a new entry only falls off when it
	// ranked below everything already there. That is the system working, not
	// an update — so it must not be reported as one.
	return updated || (added && additionSurvived(card.Map, func(e MapEntry) string { return e.Pred.String() }, before))
}

// rankMap orders the site map best-first. Stable, so entries of equal rank keep
// the order they were first seen in and the card does not reshuffle itself
// between passes.
func rankMap(entries []MapEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		if a.Uses != b.Uses {
			return a.Uses > b.Uses
		}
		return a.ObservedAtMs > b.ObservedAtMs
	})
}

// PredicateFor turns an observed node into the predicate a later snapshot is
// matched against. One attribute is kept when there is exactly one — the
// extension only emits href (links) and name (form fields), so "exactly one" is
// the normal case and picking one of several would be arbitrary.
func PredicateFor(n NodeSig) Predicate {
	p := Predicate{Role: n.Role, Name: truncate(n.Name, 40)}
	keys := sortedKeys(n.Attrs)
	if len(keys) == 1 {
		p.AttrKey = keys[0]
		p.AttrVal = n.Attrs[keys[0]]
	}
	return p
}

// mergeProcedure records this segment's call shape as a candidate and counts
// repeats.
//
// Writing on the first sighting and *showing* only at the threshold is the whole
// design here, and the split is what makes the gate durable. An earlier version
// refused to write a procedure until it had been seen twice — which needed the
// sighting to be counted somewhere, and the only durable place is the card
// itself. So the candidate is always written, and RenderCard is what refuses to
// show one below ProcedureCorroboration.
func mergeProcedure(card *SiteCard, seg *segment) bool {
	if len(seg.failures) > 0 || len(seg.shape) == 0 {
		return false
	}
	steps := stepsOf(seg)
	goal := goalOf(seg)

	before := beforeKeys(card.Procedures, procedureKey)
	for i := range card.Procedures {
		p := &card.Procedures[i]
		if p.Goal != goal || !sameSteps(p.Steps, steps) {
			continue
		}
		p.Successes++
		p.LastAtMs = seg.endMs
		if seg.browser != "" {
			p.Browser = seg.browser
		}
		if seg.firstEnvelope != "" && len(p.Evidence) < 4 {
			p.Evidence = append(p.Evidence, seg.firstEnvelope)
		}
		return true
	}
	card.Procedures = append(card.Procedures, ProcedureEntry{
		Goal:      goal,
		Steps:     steps,
		Successes: 1,
		Browser:   seg.browser,
		Evidence:  []string{seg.firstEnvelope},
		LastAtMs:  seg.endMs,
	})
	if len(card.Procedures) > maxProcedureEntries {
		card.Procedures = card.Procedures[:maxProcedureEntries]
	}
	// The new entry was appended last and the cap keeps the first N, so at the
	// cap it is always the one dropped. The `return true` that used to sit
	// here said the card had learned a new working sequence when it had not,
	// and the learner wrote a revision record to match.
	return additionSurvived(card.Procedures, procedureKey, before)
}

// procedureKey is the identity a procedure entry is matched on — the same
// test the loop above does by hand, in a form beforeKeys can use.
func procedureKey(e ProcedureEntry) string {
	return e.Goal + "\x00" + strings.Join(stepKeys(e.Steps), "\x01")
}

func stepKeys(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Command+"\x00"+s.On)
	}
	return out
}

func sameSteps(a, b []Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Command != b[i].Command || a[i].On != b[i].On {
			return false
		}
	}
	return true
}

// stepsOf renders the segment's shape as procedure steps.
//
// It reads the shape and nothing else. An earlier version zipped the shape —
// one entry per *response* — against `seg.calls`, which holds only the
// *successful, ref-addressed* ones, pairing them by index. The two lists have
// different filters, so any segment containing a failure or a snapshot diverged
// and every ref after the divergence point shifted: a six-call attempt came out
// with its first ref dropped and its last one lost, and — worse, because this is
// the tier that claims to be the trustworthy one — the one *failed* call in a
// segment was recorded as a successful read on a control. A card that says "this
// worked" about a call that errored is precisely the false positive
// ProcedureCorroboration exists to hold back, arriving through the side door.
func stepsOf(seg *segment) []Step {
	steps := make([]Step, 0, len(seg.shape))
	for _, sh := range seg.shape {
		step := Step{Command: sh.command}
		if sh.ref != "" {
			step.On = "@" + sh.ref
		}
		if sh.found {
			step.Note = purposeOf(sh.command)
		}
		steps = append(steps, step)
	}
	return steps
}

// goalOf is a readable name for a call shape, derived deterministically. A model
// call could phrase this better (ADR-0022), but it must never be the only way to
// tell one procedure from another, so the name is a function of the shape.
func goalOf(seg *segment) string {
	parts := make([]string, 0, len(seg.shape))
	for _, sh := range seg.shape {
		parts = append(parts, sh.command)
	}
	return "then " + strings.Join(parts, " → ")
}
