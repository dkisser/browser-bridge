package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStream(t *testing.T) (*Stream, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStream(dir)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func TestStreamRoundTripAndCursor(t *testing.T) {
	s, _ := newTestStream(t)
	for i := 0; i < 3; i++ {
		if err := s.Append(TraceRecord{Kind: KindCommand, Envelope: string(rune('a' + i)), Command: "navigate"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	recs, next, skipped, err := s.ReadFrom(0)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(recs) != 3 || skipped != 0 {
		t.Fatalf("got %d records, %d skipped; want 3, 0", len(recs), skipped)
	}
	if recs[1].Envelope != "b" {
		t.Errorf("record 1 envelope = %q, want b", recs[1].Envelope)
	}

	// A second read from the cursor must see only what is new.
	if err := s.Append(TraceRecord{Kind: KindResponse, Envelope: "d"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs, next2, _, rerr := s.ReadFrom(next)
	if rerr != nil {
		t.Fatalf("ReadFrom(next): %v", rerr)
	}
	if len(recs) != 1 || recs[0].Envelope != "d" {
		t.Errorf("incremental read = %+v, want just d", recs)
	}
	if next2 != 4 {
		t.Errorf("next cursor = %d, want 4", next2)
	}
}

func TestStreamSkipsCorruptLineButStopsAtTornTail(t *testing.T) {
	s, dir := newTestStream(t)
	for _, env := range []string{"a", "b"} {
		if err := s.Append(TraceRecord{Kind: KindCommand, Envelope: env}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// A newline-terminated line that cannot parse is corruption: the reader must
	// step over it, or one bad byte stops all learning forever.
	if err := s.Append(TraceRecord{Kind: KindCommand, Envelope: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, streamFileName)
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	// Rewrite the middle record as garbage, then append a torn tail.
	parts := splitLines(string(data))
	var lines []byte
	lines = append(lines, parts[0]...)
	lines = append(lines, []byte("{not json at all}\n")...)
	lines = append(lines, parts[2]...)
	lines = append(lines, []byte(`{"kind":"response","env`)...) // no newline
	if werr := os.WriteFile(path, lines, 0o600); werr != nil {
		t.Fatal(werr)
	}

	recs, next, skipped, rerr2 := s.ReadFrom(0)
	if rerr2 != nil {
		t.Fatalf("ReadFrom: %v", rerr2)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(recs) != 2 {
		t.Errorf("got %d records, want 2 (a and d)", len(recs))
	}
	// The torn tail is not consumed, so the next pass can complete it.
	if next != 3 {
		t.Errorf("next cursor = %d, want 3 (stop before the torn line)", next)
	}
}

func splitLines(s string) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, []byte(s[start:i+1]))
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, []byte(s[start:]))
	}
	return out
}

func TestStoreFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStream(dir)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer func() { _ = s.Close() }()
	if aerr := s.Append(TraceRecord{Kind: KindCommand, Envelope: "a"}); aerr != nil {
		t.Fatal(aerr)
	}
	if cerr := LoadCursor(dir).Set(1); cerr != nil {
		t.Fatalf("cursor Set: %v", cerr)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Put(&SiteCard{Host: "x.test", Revision: 1}); err != nil {
		t.Fatal(err)
	}

	// $BB_HOME/data/ is 0700 because ADR-0017 classified it as sensitive. A
	// world-readable file inside it would undo that for anything a different
	// user could reach, so the mode of each file is asserted rather than assumed.
	for _, f := range []string{
		filepath.Join(dir, streamFileName),
		filepath.Join(dir, cursorFileName),
		filepath.Join(store.Dir(), "x.test.json"),
	} {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %o, want 600", filepath.Base(f), perm)
		}
	}
}

func TestAppendAfterATornTailKeepsTheNewRecord(t *testing.T) {
	s, dir := newTestStream(t)
	if err := s.Append(TraceRecord{Kind: KindCommand, Envelope: "a"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash mid-append: a partial line with no newline.
	path := filepath.Join(dir, streamFileName)
	if err := os.WriteFile(path, []byte(`{"kind":"command","env":"torn`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Reopen so the stream notices the file does not end on a newline.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if aerr := s2.Append(TraceRecord{Kind: KindCommand, Envelope: "b"}); aerr != nil {
		t.Fatal(aerr)
	}

	recs, _, skipped, err := s2.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	// The torn record is lost — it was never durable — but the new one must
	// survive. Concatenating onto the partial line would have destroyed both.
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (the torn line)", skipped)
	}
	var found bool
	for _, r := range recs {
		if r.Envelope == "b" {
			found = true
		}
	}
	if !found {
		t.Errorf("the record appended after a torn tail was destroyed; got %+v", recs)
	}
}

func TestCursorPersists(t *testing.T) {
	dir := t.TempDir()
	c := LoadCursor(dir)
	if c.Get() != 0 {
		t.Errorf("fresh cursor = %d, want 0", c.Get())
	}
	if err := c.Set(42); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := LoadCursor(dir).Get(); got != 42 {
		t.Errorf("reloaded cursor = %d, want 42", got)
	}

	// A corrupt cursor must not be fatal: re-learning from the start is
	// idempotent, so doing more work is the safe failure.
	if werr := os.WriteFile(filepath.Join(dir, cursorFileName), []byte("{garbage"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	if got := LoadCursor(dir).Get(); got != 0 {
		t.Errorf("cursor after corruption = %d, want 0", got)
	}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	card := &SiteCard{Host: "example.com", Revision: 3, Failures: []FailureEntry{{Signature: "nope", Count: 2}}}
	if perr := s.Put(card); perr != nil {
		t.Fatalf("Put: %v", perr)
	}

	got, ok := s.Get("example.com")
	if !ok {
		t.Fatal("Get returned not-ok for a card just written")
	}
	if got.Revision != 3 || len(got.Failures) != 1 {
		t.Errorf("card = %+v, want revision 3 and one failure", got)
	}

	info, err := os.Stat(filepath.Join(s.Dir(), "example.com.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("card mode = %o, want 600 — the data dir is 0700 for a reason", perm)
	}

	// Get must hand back a copy: a caller mutating it must not corrupt the cache.
	got.Revision = 99
	again, _ := s.Get("example.com")
	if again.Revision != 3 {
		t.Error("Get returned a shared pointer; a caller mutated the store's card")
	}

	if err := s.Remove("example.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := s.Get("example.com"); ok {
		t.Error("card still present after Remove")
	}
	if err := s.Remove("example.com"); err != nil {
		t.Errorf("Remove on a missing card = %v, want nil (idempotent)", err)
	}
}

func TestStoreToleratesCorruptCard(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	if err := os.WriteFile(filepath.Join(s.Dir(), "broken.test.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A hand-edited or truncated card must not take the store down: the learner
	// rebuilds it from the stream, which is the truth.
	if _, ok := s.Get("broken.test"); ok {
		t.Error("Get reported a corrupt card as usable")
	}
	// But it must still be *listed*, and Read must say why it is unusable.
	// Hiding it made `memory list` report a healthy set while the one file a
	// person most needs to look at sat in the directory with no name attached.
	hosts := s.Hosts()
	if len(hosts) != 1 || hosts[0] != "broken.test" {
		t.Errorf("Hosts = %v, want [broken.test]: a card that cannot be read is still a card", hosts)
	}
	card, err := s.Read("broken.test")
	if err == nil {
		t.Errorf("Read returned %+v and no error; absent and unparseable must differ", card)
	} else if card != nil {
		t.Errorf("Read returned a card alongside the error: %+v", card)
	}
	// Absent is still absent, and still not an error.
	if card, err := s.Read("absent.test"); err != nil || card != nil {
		t.Errorf("Read(absent) = %+v, %v; want nil, nil", card, err)
	}
}

// `bridge memory rm` runs in its own process, so the daemon's cache has to see
// the deletion. Before this, a deleted card kept being injected at every
// landing until a restart — which is the situation the command documents itself
// as being for.
func TestStoreSeesADeletionMadeByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	host := "shop.example"
	if putErr := s.Put(&SiteCard{Host: host, Revision: 3, Map: []MapEntry{{Purpose: "cart"}}}); putErr != nil {
		t.Fatal(putErr)
	}
	// Warm the cache the way a running daemon does.
	if _, ok := s.Get(host); !ok {
		t.Fatal("the card was not readable straight after Put")
	}

	// Another process deletes it. Nothing tells this one.
	if rmErr := os.Remove(filepath.Join(s.Dir(), "shop.example.json")); rmErr != nil {
		t.Fatal(rmErr)
	}
	if _, ok := s.Get(host); ok {
		t.Error("a deleted card was still served from the cache")
	}
	if hosts := s.Hosts(); len(hosts) != 0 {
		t.Errorf("Hosts = %v, want empty after an out-of-process delete", hosts)
	}

	// And an edit made elsewhere is picked up, not the stale copy.
	if putErr := s.Put(&SiteCard{Host: host, Revision: 1, Map: []MapEntry{{Purpose: "cart"}}}); putErr != nil {
		t.Fatal(putErr)
	}
	if _, ok := s.Get(host); !ok {
		t.Fatal("card missing after re-put")
	}
	rewritten := &SiteCard{Host: host, Revision: 9, Failures: []FailureEntry{{Signature: "no_element", Count: 4}}}
	data, err := json.Marshal(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "shop.example.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(host)
	if !ok {
		t.Fatal("card vanished")
	}
	if got.Revision != 9 || len(got.Failures) != 1 {
		t.Errorf("served a stale card: rev %d, %d failure(s)", got.Revision, len(got.Failures))
	}
}

func TestSafeFileNameCannotEscape(t *testing.T) {
	for _, in := range []string{"../../etc/passwd", "a/b", "..", "."} {
		if got := safeFileName(in); got == "" || filepath.Base(got) != got {
			t.Errorf("safeFileName(%q) = %q, want a single path element", in, got)
		}
	}
	if got := safeFileName("news.example.com"); got != "news.example.com" {
		t.Errorf("safeFileName did not pass a normal host through unchanged: %q", got)
	}
}

// A stream that only grows is the one artefact in this package with no ceiling:
// cards are bounded, failure lists are bounded, and the benchmark log is a
// human's own file — but this one grows with every call forever, and the reader
// scans it from byte 0 on every pass.
func TestStreamRotatesWhenItIsConsumedAndOverTheCeiling(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Fill past the ceiling with one enormous record rather than many, so the
	// test does not have to write 16MB of lines.
	big := TraceRecord{
		Kind: KindCommand, AtMs: 1, Envelope: "e1", Command: "gettext",
		Args: map[string]any{"selector": strings.Repeat("x", streamRotateBytes)},
	}
	if appendErr := s.Append(big); appendErr != nil {
		t.Fatal(appendErr)
	}

	// Not rotated while the learner is behind: those records are not learned yet,
	// and the cursor is a line number, so a rotation now would renumber them.
	rotated, err := s.Rotate(0)
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Fatal("rotated with unlearned records behind the cursor")
	}

	// Rotated once the cursor has caught up.
	if _, next, _, readErr := s.ReadFrom(0); readErr != nil {
		t.Fatal(readErr)
	} else if next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
	rotated, err = s.Rotate(1)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("a consumed, oversized stream was not rotated")
	}

	// The retained generation is still readable, and the new active file is
	// empty and usable.
	recs, _, _, err := s.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Envelope != "e1" {
		t.Fatalf("the rotated generation did not survive: %+v", recs)
	}
	if appendErr := s.Append(TraceRecord{Kind: KindCommand, AtMs: 2, Envelope: "e2", Command: "click"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	recs, _, _, err = s.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("after rotation the reader saw %d records, want 2", len(recs))
	}
	// Line numbers continue across the generations, which is what lets a
	// restarted cursor re-read the retained one without losing its place.
	_, next, _, err := s.ReadFrom(1)
	if err != nil {
		t.Fatal(err)
	}
	if next != 2 {
		t.Errorf("next = %d, want 2", next)
	}
}

// A restarted daemon has to find the generation the previous process rotated
// out of the way.
//
// `rotated` was only ever assigned inside Rotate, which runs from exactly one
// place: Manager.New at startup. So a stream opened by a *later* process never
// learns the previous generation exists. Two things break and neither is loud:
// ReadFrom builds a one-element path list, so the retained generation — the
// audit trail ADR-0020 calls the point of a single stream — is invisible; and
// the persisted cursor is a line number in the *combined* numbering, so after
// the restart it indexes a file that no longer contains those lines, clamps to
// itself, and the learner stops on `next <= from` forever. Every later command
// is recorded and never learned, and the only recovery is deleting
// learner.cursor by hand.
//
// Rotation is a property of the directory, not of whichever process happened to
// be holding the file.
func TestOpenStreamFindsTheGenerationAPriorProcessRotated(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Past the ceiling in one enormous record, so the test does not have to
	// write 16MB of lines to get a rotation.
	if appendErr := first.Append(TraceRecord{
		Kind: KindCommand, AtMs: 1, Envelope: "e1", Command: "gettext",
		Args: map[string]any{"selector": strings.Repeat("x", streamRotateBytes)},
	}); appendErr != nil {
		t.Fatal(appendErr)
	}
	// The learner has to have caught up first: a rotation while records sit
	// behind the cursor would renumber them out from under it.
	if _, next, _, readErr := first.ReadFrom(0); readErr != nil {
		t.Fatal(readErr)
	} else if next != 1 {
		t.Fatalf("ReadFrom(0) next = %d, want 1", next)
	}
	rotated, err := first.Rotate(1)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated {
		t.Fatal("the first process did not rotate")
	}
	if err = first.Append(TraceRecord{Kind: KindCommand, AtMs: 2, Envelope: "e2", Command: "click"}); err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}

	// The restart.
	second, err := OpenStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	recs, next, _, err := second.ReadFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("after the restart ReadFrom(0) returned %d records, want 2 (%+v)", len(recs), recs)
	}
	if recs[0].Envelope != "e1" || recs[1].Envelope != "e2" {
		t.Errorf("records = %q, %q; want e1, e2", recs[0].Envelope, recs[1].Envelope)
	}
	// The combined numbering has to survive the restart, or a cursor persisted
	// by the previous process would point into the wrong file.
	if next != 2 {
		t.Errorf("next = %d, want 2", next)
	}
}
