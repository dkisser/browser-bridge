package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// The stream is ADR-0020: one typed append-only file that the Trace, the card
// revision log and the learner's own bookkeeping are all written to, so a card
// and the evidence behind it can never drift apart — they are the same file,
// read at different offsets.
//
// Two properties are load-bearing and both come from the "single file" choice:
//
//   - Appends are single O_APPEND writes of a whole line. A crash mid-write
//     leaves a torn final line, which the reader skips; it cannot leave a hole,
//     because nothing else writes this file and nothing rewrites it.
//   - The reader tolerates that torn line rather than failing. A learner that
//     refuses to start because the daemon was killed while recording is a
//     learner that stops learning exactly when something went wrong.

const streamFileName = "stream.jsonl"

// rotatedStreamName is the one previous generation. Two would be tidier and
// cost more than they are worth: the audit surface a human reads is the recent
// one, and the learner rebuilds any card it needs from the current file alone.
const rotatedStreamName = "stream.jsonl.1"

// streamRotateBytes is when the active file is rolled over. Without a ceiling
// the stream is the one artefact in this package that grows forever: cards are
// bounded, failures are bounded, the benchmark log is a human's own file — but
// this one grows with every call forever, and ReadFrom scans it from byte 0 on
// every pass, so both the disk and the idle CPU cost of the control plane are
// a function of how long it has been running.
const streamRotateBytes = 16 << 20

// Stream is the append-only log. It is safe for concurrent use: Appends come
// from the router's command and response paths, and ReadCursor from the
// background learner.
type Stream struct {
	mu   sync.Mutex
	path string
	f    *os.File
	// rotated is the previous generation, kept so `memory history` and
	// Revisions still see the records the rotation moved out of the way.
	rotated string
	// lastByte records whether the previous append ended the file on a newline,
	// so a torn tail left by a crash is closed off before the next record is
	// added. Without it, the next append concatenates onto the partial line and
	// both records are destroyed instead of one.
	lastByte byte
}

// OpenStream opens (creating if needed) the stream under dir. The parent dir
// must already exist; the caller owns the 0700 data dir (ADR-0017).
func OpenStream(dir string) (*Stream, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("memory: create stream dir: %w", err)
	}
	path := filepath.Join(dir, streamFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("memory: open stream: %w", err)
	}
	s := &Stream{path: path, f: f, lastByte: '\n'}
	// A stream that already exists may have been left torn by a crash. Only
	// then does lastByte become something other than a newline; the default
	// must be '\n' so the first append to a new file is not preceded by a
	// spurious blank line.
	if info, serr := f.Stat(); serr == nil && info.Size() > 0 {
		if last, lerr := readLastByte(path); lerr == nil {
			s.lastByte = last
		}
	}
	// Adopt a generation a *previous* process rotated out of the way.
	//
	// Rotation is a property of the directory, not of whichever process
	// happened to be holding the file, and Rotate only ever ran from the
	// daemon's own startup path. So a stream opened by a later process saw
	// neither the retained generation nor the combined line numbering the
	// cursor is written in: ReadFrom built a one-element path list, and the
	// persisted cursor indexed a file that no longer held those lines, clamped
	// to itself, and froze the learner for the life of the process. Discovering
	// the sibling here is what makes a restart pick up where the last one left
	// off.
	s.rotated = existingRotation(dir)
	return s, nil
}

// existingRotation reports the retained generation in dir, or "" if there is
// none. A stat error of any kind reads as "no rotation": the active stream is
// still perfectly usable without it, so a failure to look must not be a
// failure to open.
func existingRotation(dir string) string {
	rotated := filepath.Join(dir, rotatedStreamName)
	if info, err := os.Stat(rotated); err != nil || info.IsDir() {
		return ""
	}
	return rotated
}

func readLastByte(path string) (byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return '\n', nil
	}
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, info.Size()-1); err != nil {
		return 0, err
	}
	return buf[0], nil
}

// Append writes one record. The whole line is written in a single call so that
// concurrent appenders interleave at line granularity, not byte granularity.
func (s *Stream) Append(rec TraceRecord) error {
	if rec.AtMs == 0 {
		rec.AtMs = nowMs()
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("memory: marshal record: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return errors.New("memory: append to a closed stream")
	}
	// Close off a torn line from a previous crash before adding to it. The
	// partial record is lost — it was never durable — but the record being
	// written now survives, which is the whole point.
	if s.lastByte != '\n' {
		if _, werr := s.f.Write([]byte{'\n'}); werr != nil {
			return fmt.Errorf("memory: close torn stream line: %w", werr)
		}
	}
	n, err := s.f.Write(data)
	if err != nil {
		return fmt.Errorf("memory: append record: %w", err)
	}
	if n > 0 {
		s.lastByte = data[n-1]
	}
	return nil
}

// StreamStamp is a cheap fingerprint of exactly the bytes ReadFrom would read:
// the active file and the one retained generation. Two stamps being equal means
// a full re-read cannot produce a different answer, which is what makes it a
// cache key (ADR-0039).
//
// Rotation is covered because it does not change the active file's size — it
// renames it and creates a fresh one — so a stamp built from the active file
// alone would go stale the moment a rotation happened and keep serving a digest
// from a generation that had just been moved aside.
type StreamStamp struct {
	ActiveSize int64
	// Nanoseconds, and named for it. A field called *Ms holding UnixNano is a
	// trap: TraceRecord.AtMs is milliseconds, so a reader comparing the two —
	// which is the obvious thing to do when both say "millisecond timestamp" —
	// gets a value 10^6 too large and concludes the cache never invalidates.
	ActiveModNs  int64
	RotatedSize  int64
	RotatedModNs int64
}

// Stamp reports the current fingerprint. It stats two files and does not read
// them, which is the entire point: the alternative is the full decode that
// Stamp exists to avoid.
func (s *Stream) Stamp() StreamStamp {
	s.mu.Lock()
	path, rotated := s.path, s.rotated
	s.mu.Unlock()

	var st StreamStamp
	if fi, err := os.Stat(path); err == nil {
		st.ActiveSize, st.ActiveModNs = fi.Size(), fi.ModTime().UnixNano()
	}
	if rotated != "" {
		if fi, err := os.Stat(rotated); err == nil {
			st.RotatedSize, st.RotatedModNs = fi.Size(), fi.ModTime().UnixNano()
		}
	}
	return st
}

// ReadFrom returns every record from line index `from` (0-based) to the end of
// the file, and the index one past the last line it could account for.
//
// The reader distinguishes two kinds of unreadable line, and the difference
// matters:
//
//   - A final line with no terminating newline is a *torn tail*: the daemon died
//     mid-append. The next append will complete it, so the cursor stops *before*
//     it and the next pass picks it up.
//   - A newline-terminated line that will not parse is *corruption*. Skipping it
//     and moving on is the only way the learner keeps making progress; stalling
//     on it would mean one bad byte stopped all learning forever.
func (s *Stream) ReadFrom(from int64) (recs []TraceRecord, next int64, skipped int, err error) {
	// Deliberately NOT holding the write mutex. This scans the whole file, and
	// the appenders are the router's command and response paths — holding the
	// lock here would make every browser call wait on a full-file read of a log
	// that is never rotated. Safety does not need it: the file is append-only,
	// so a concurrent Append either lands before our EOF (we read it, and the
	// card write that follows still happens before the cursor moves) or after
	// (we pick it up next pass). The torn-tail check below is what makes a
	// half-written trailing line safe to read concurrently.
	s.mu.Lock()
	paths := []string{s.path}
	if s.rotated != "" {
		paths = append([]string{s.rotated}, paths...)
	}
	s.mu.Unlock()

	var (
		idx      int64
		skippedN int
	)
	for _, path := range paths {
		got, upTo, miss, rerr := s.readFile(path, from, idx)
		if rerr != nil {
			return nil, from, skipped, rerr
		}
		recs = append(recs, got...)
		skippedN += miss
		if upTo > idx {
			idx = upTo
		}
	}
	// An empty stream, or one with nothing at or after `from`, has to hand back
	// exactly what was asked for: the learner stops on `next <= from`, and a
	// next below from would look like it had consumed records it never read.
	next = idx
	if next < from {
		next = from
	}
	return recs, next, skipped + skippedN, nil
}

// readFile reads one generation, numbering its lines from base. `idx` is the
// absolute line number of this file's first line.
func (s *Stream) readFile(path string, from, base int64) (recs []TraceRecord, next int64, skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, base, 0, nil
		}
		return nil, base, 0, fmt.Errorf("memory: read stream: %w", err)
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReaderSize(f, 64*1024)
	var idx int64
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) == 0 && rerr != nil {
			// Clean EOF.
			break
		}
		complete := rerr == nil // ReadBytes only errors with data when there is no newline
		if !complete {
			// Torn tail: stop without consuming it.
			break
		}
		body := bytes.TrimRight(line, "\n")
		// Absolute line number, so a caller whose cursor counted through the
		// previous generation still lands in the right place.
		if base+idx >= from {
			var rec TraceRecord
			if uerr := json.Unmarshal(body, &rec); uerr != nil {
				skipped++
			} else {
				recs = append(recs, rec)
			}
		}
		idx++
		next = base + idx
		if rerr != nil {
			break
		}
	}
	return recs, next, skipped, nil
}

// Rotate rolls the active file over to the single retained generation and
// starts a fresh one. It is a no-op unless the stream is over the ceiling AND
// the learner has consumed everything in it.
//
// That second condition is what makes this safe. The cursor is a line number,
// so a rotation renumbers every line after the cut; rotating with unlearned
// records behind the cursor would either lose them or need an offset the cursor
// has nowhere to keep. Waiting for the learner to catch up means the file is
// fully consumed at the moment it turns over.
//
// The cursor is then left exactly where it is, and it needs no adjustment: it
// holds the combined line count of both generations, and after the rename the
// retained generation is the old active file while the new active file is
// empty — the same total, numbered the same way. It lands on the first line of
// the new active file.
//
// This comment used to say the cursor "restles at 0" and that re-reading the
// retained generation was "idempotent, because rebuilding a card from the same
// records produces the same card". Both were wrong, and Run's own doc says so at
// length: applySegment increments Uses, Count and Revision rather than
// recomputing them, so a re-read double-counts and mints a phantom revision.
// Nothing caught it because the condition above was unreachable — the cursor is
// permanently one learn_run record short of the end, so the old `cursorLine <
// lines` test could never pass and this function had never run.
func (s *Stream) Rotate(cursorLine int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fi, err := s.f.Stat()
	if err != nil || fi.Size() < streamRotateBytes {
		return false, nil
	}
	// How many lines the active file holds, to compare against the cursor.
	// A line can be large — a single command's recorded args, in principle —
	// so the buffer has to clear the ceiling rather than assume small records.
	// If the count cannot be established, do not rotate: not knowing is a
	// reason to leave the file alone, and the next start will try again.
	// The safety condition is "nothing unlearned would be renumbered", and it
	// used to be written as `cursorLine >= lines`. That is stricter than the
	// condition, and strictly unreachable: Run appends its own learn_run record
	// *after* it advances the cursor, so the cursor is permanently one line
	// short of the end and the test could never pass. The 16MB ceiling ADR-0032
	// introduced therefore rotated nothing, ever — not merely "only at
	// startup" — which is why the stream simply grew. See
	// unconsumedIsBookkeeping for what is safe to leave behind.
	if !s.unconsumedIsBookkeeping(cursorLine) {
		return false, nil
	}

	rotated := filepath.Join(filepath.Dir(s.path), rotatedStreamName)
	// The rename comes first, and the old handle stays open across it.
	//
	// This used to close s.f, then rename, then reopen — and the reopen is
	// fallible (a read-only remount, ENOSPC, EMFILE). On that path the function
	// returned with s.f a closed, non-nil *os.File: Append's guard is
	// `if s.f == nil`, so it passed, every later write returned "file already
	// closed", and commands, results and card_shown records stopped being
	// written for the rest of the process's life with one log line per command
	// and no recovery. Rotate's own s.f.Stat() failed from then on too.
	//
	// Renaming under the open handle is safe on the platforms this runs on, and
	// os.Rename replaces the destination atomically, so the unlink the old code
	// did first — which destroyed the retained generation before the rename
	// could fail — is gone as well.
	if rerr := os.Rename(s.path, rotated); rerr != nil {
		return false, fmt.Errorf("memory: rotate stream: %w", rerr)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		// Put it back, so a failed rotation leaves the stream exactly as it
		// was rather than half-moved with no active file.
		if backErr := os.Rename(rotated, s.path); backErr != nil {
			return false, fmt.Errorf("memory: reopen stream: %w (and rolling the rename back failed: %v)", err, backErr)
		}
		return false, fmt.Errorf("memory: reopen stream: %w", err)
	}
	// Only now is the old descriptor retired. A close error here is reported
	// but not fatal: the new handle is already in place and the stream works.
	cerr := s.f.Close()
	s.f = f
	s.rotated = rotated
	s.lastByte = '\n'
	if cerr != nil {
		return true, fmt.Errorf("memory: close rotated stream: %w", cerr)
	}
	return true, nil
}

// maxStreamLineBytes bounds one record's scanner buffer during a rotation
// sweep. It clears the rotation ceiling on purpose: a legitimate record can be
// large, and a sweep that gave up early would report a line count too low and
// rotate with records still unlearned.
const maxStreamLineBytes = 2 * streamRotateBytes

// unconsumedIsBookkeeping reports whether every record after the first `from`
// lines of the active generation is one the learner wrote about itself.
//
// Rotate's whole precondition is that moving the file aside cannot renumber a
// record out from under a reader, which means no *learning* record may be
// unread. A learn_run record — a description of a pass, written by the pass
// itself after the cursor moved — carries nothing to learn, so leaving one
// behind is safe and re-reading it is free. Requiring the cursor to be past it
// instead made the condition unsatisfiable.
//
// A line that will not parse counts as unconsumed: a torn tail is a record
// somebody may still be able to read.
func (s *Stream) unconsumedIsBookkeeping(from int64) bool {
	if from < 0 {
		return false
	}
	// `from` is an index into the *combined* numbering ReadFrom produces — the
	// retained generation first, the active file after it — so it has to be
	// rebased before it can index the active file's own lines.
	//
	// Without this the comparison below was between a combined index and a
	// per-file line number, so with any retained generation present every line
	// of the active file was skipped, nothing was parsed, and the function
	// reported success for a stream it had not looked at. Rotate then moved the
	// active file over a retained generation the learner had never read — the
	// one thing ADR-0032 and ADR-0036 both say the precondition exists to
	// prevent.
	base := int64(0)
	if s.rotated != "" {
		n, ok := countLines(s.rotated)
		if !ok {
			// Not knowing how the generations line up is a reason to leave the
			// file alone, which is what Rotate's other unknown does.
			return false
		}
		base = n
	}
	if from < base {
		// The cursor is still inside the retained generation, so every line of
		// the active file is unread.
		return false
	}
	skip := from - base

	f, err := os.Open(s.path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxStreamLineBytes)
	var line int64
	for sc.Scan() {
		if line < skip {
			line++
			continue
		}
		line++
		var rec TraceRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return false
		}
		if rec.Kind != KindLearnRun {
			return false
		}
	}
	return sc.Err() == nil
}

// countLines reports how many complete lines a file holds, or that it could not
// be established. A line can be large — a single command's recorded args, in
// principle — so the scanner buffer clears the rotation ceiling rather than
// assuming small records.
func countLines(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxStreamLineBytes)
	var lines int64
	for sc.Scan() {
		lines++
	}
	if sc.Err() != nil {
		return 0, false
	}
	return lines, true
}

// Close releases the append handle.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// Cursor is the learner's position in the stream: a line index, persisted so a
// restarted daemon does not re-learn everything and does not skip anything.
type Cursor struct {
	Line int64 `json:"line"`
	mu   sync.Mutex
	path string
}

const cursorFileName = "learner.cursor"

// LoadCursor reads the cursor, defaulting to 0 for a first run or an unreadable
// file. A corrupt cursor is not fatal: re-learning from the start is
// idempotent, so the safe failure is to do more work, not less.
func LoadCursor(dir string) *Cursor {
	c := &Cursor{path: filepath.Join(dir, cursorFileName)}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return c
	}
	var onDisk struct {
		Line int64 `json:"line"`
	}
	if err := json.Unmarshal(data, &onDisk); err == nil && onDisk.Line >= 0 {
		c.Line = onDisk.Line
	}
	return c
}

// Get reports the learner's position, re-reading the file first.
//
// The file is the shared truth because the cursor is shared: `bridge memory
// learn` and `bench record` open a second Manager over the live data directory
// while the daemon holds its own (they are safe to open precisely because they
// do not own the rotation). Each holds an in-memory copy, and a pass that reads
// a stale one re-consumes records the other process already consumed — which
// applySegment then *increments*, so the card's counts double.
func (c *Cursor) Get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if onDisk, ok := c.readOnDisk(); ok && onDisk > c.Line {
		c.Line = onDisk
	}
	return c.Line
}

// readOnDisk reports the persisted position, and whether it could be read. A
// missing or unparseable file reads as "no opinion" rather than as zero, so a
// corrupt cursor is left alone instead of rewinding the learner to the start.
func (c *Cursor) readOnDisk() (int64, bool) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return 0, false
	}
	var onDisk struct {
		Line int64 `json:"line"`
	}
	if err := json.Unmarshal(data, &onDisk); err != nil || onDisk.Line < 0 {
		return 0, false
	}
	return onDisk.Line, true
}

// Set advances the cursor. Writing it before the derived cards are durable
// would lose work on a crash between the two, so the learner calls this only
// after its writes have landed.
// Monotonic: a Set that would move the cursor backwards is ignored.
//
// Two processes can hold a Cursor over the same file, and the whole file is
// rewritten by whoever writes last. Without this a short-lived CLI pass could
// finish after a long daemon pass and put the shared cursor back to its own
// older, in-memory value — after which the daemon re-reads the records the CLI
// just consumed and double-counts them. Refusing the write costs a redundant
// read; allowing it costs a corrupted card.
//
// The invariant this depends on is that the cursor never legitimately moves
// backwards. It held before this check and it holds now that a rotation does
// not reset it: after a rename the combined line count is unchanged, so the
// position is still the right one.
func (c *Cursor) Set(line int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if onDisk, ok := c.readOnDisk(); ok && line < onDisk {
		c.Line = onDisk
		return nil
	}
	data, err := json.Marshal(struct {
		Line int64 `json:"line"`
	}{Line: line})
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("memory: write cursor: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("memory: replace cursor: %w", err)
	}
	// Only now, with the file actually replaced, is the in-memory value
	// allowed to move. Updating it first would let a failed write leave the
	// learner believing it had consumed records it still has to read.
	c.Line = line
	return nil
}

var _ io.Closer = (*Stream)(nil)
