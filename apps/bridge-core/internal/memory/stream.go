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
	return s, nil
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
// fully consumed at the moment it turns over, the cursor restles at 0, and the
// next pass re-reads the retained generation from the start — which is
// idempotent, because rebuilding a card from the same records produces the same
// card.
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
	lines, ok := s.lineCount()
	if !ok || cursorLine < lines {
		return false, nil
	}

	rotated := filepath.Join(filepath.Dir(s.path), rotatedStreamName)
	_ = os.Remove(rotated)
	if cerr := s.f.Close(); cerr != nil {
		return false, fmt.Errorf("memory: close stream for rotation: %w", cerr)
	}
	if rerr := os.Rename(s.path, rotated); rerr != nil {
		return false, fmt.Errorf("memory: rotate stream: %w", rerr)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, fmt.Errorf("memory: reopen stream: %w", err)
	}
	s.f = f
	s.rotated = rotated
	s.lastByte = '\n'
	return true, nil
}

// maxStreamLineBytes bounds one record's scanner buffer during a rotation
// sweep. It clears the rotation ceiling on purpose: a legitimate record can be
// large, and a sweep that gave up early would report a line count too low and
// rotate with records still unlearned.
const maxStreamLineBytes = 2 * streamRotateBytes

// lineCount counts the lines in the active generation, or reports that it
// could not.
func (s *Stream) lineCount() (int64, bool) {
	f, err := os.Open(s.path)
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

func (c *Cursor) Get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Line
}

// Set advances the cursor. Writing it before the derived cards are durable
// would lose work on a crash between the two, so the learner calls this only
// after its writes have landed.
func (c *Cursor) Set(line int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
