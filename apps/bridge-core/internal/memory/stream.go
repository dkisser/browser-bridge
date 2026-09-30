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

// Stream is the append-only log. It is safe for concurrent use: Appends come
// from the router's command and response paths, and ReadCursor from the
// background learner.
type Stream struct {
	mu   sync.Mutex
	path string
	f    *os.File
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
	path := s.path
	s.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, from, 0, nil
		}
		return nil, from, 0, fmt.Errorf("memory: read stream: %w", err)
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
		if idx >= from {
			var rec TraceRecord
			if uerr := json.Unmarshal(body, &rec); uerr != nil {
				skipped++
			} else {
				recs = append(recs, rec)
			}
		}
		idx++
		next = idx
		if rerr != nil {
			break
		}
	}
	return recs, next, skipped, nil
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
