package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// logTailBytes caps the initial back-read when locating the last lines.
const logTailBytes = 64 << 10

// logTailLines is how many existing lines `logs` prints before following
// (tail's default).
const logTailLines = 10

// Logs is `bridge service logs`: the bash python3 wrapper around `tail -f`,
// prefixing each line with a "YYYY-MM-DD HH:MM:SS " timestamp. It follows
// until ctx is canceled (SIGINT/SIGTERM), which exits 0 like the bash trap.
func Logs(ctx context.Context, e *Env, w io.Writer) error {
	f, err := os.Open(e.LogFile())
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", e.LogFile(), err)
	}
	defer func() { _ = f.Close() }()

	offset, err := printLastLines(f, w)
	if err != nil {
		return err
	}

	// Follow: poll for growth (and truncation/rotation) like tail -f does.
	// Reuse a single ticker instead of `time.After` per iteration — the
	// latter allocates a fresh *time.Timer on every cycle and leaves the
	// previous one alive until it fires, growing the runtime timer-set
	// under sustained idle.
	pending := ""
	buf := make([]byte, 32<<10)
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		st, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat %s: %w", e.LogFile(), err)
		}
		if st.Size() < offset {
			// Truncated or rotated: restart from the top of the new file.
			if _, serr := f.Seek(0, io.SeekStart); serr != nil {
				return fmt.Errorf("seek %s: %w", e.LogFile(), serr)
			}
			offset = 0
		}
		if st.Size() == offset {
			continue
		}
		if _, serr := f.Seek(offset, io.SeekStart); serr != nil {
			return fmt.Errorf("seek %s: %w", e.LogFile(), serr)
		}
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read %s: %w", e.LogFile(), err)
		}
		offset += int64(n)
		pending += string(buf[:n])
		for {
			line, rest, found := strings.Cut(pending, "\n")
			if !found {
				break
			}
			printLogLine(w, line)
			pending = rest
		}
	}
}

// printLastLines prints the last logTailLines complete lines with timestamp
// prefixes and returns the current end offset.
func printLastLines(f *os.File, w io.Writer) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", f.Name(), err)
	}
	size := st.Size()
	start := int64(0)
	if size > logTailBytes {
		start = size - logTailBytes
	}
	if _, serr := f.Seek(start, io.SeekStart); serr != nil {
		return 0, fmt.Errorf("seek %s: %w", f.Name(), serr)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", f.Name(), err)
	}
	lines := strings.Split(string(raw), "\n")
	// Drop the possibly-partial first line when we started mid-file, and the
	// empty tail element when the file ends with a newline.
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	for _, line := range lines {
		printLogLine(w, line)
	}
	return size, nil
}

func printLogLine(w io.Writer, line string) {
	fmt.Fprintf(w, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}
