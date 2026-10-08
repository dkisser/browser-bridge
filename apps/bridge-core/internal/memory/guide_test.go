package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGuide(t *testing.T, dir, host, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "guides"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GuidePath(dir, host), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGuideMissingIsEmptyNotError(t *testing.T) {
	guide, err := ReadGuide(t.TempDir(), "mail.example.com")
	if err != nil {
		t.Fatalf("a host without a guide is the normal state, not an error: %v", err)
	}
	if guide != "" {
		t.Errorf("expected no guide, got %q", guide)
	}
}

func TestReadGuideReturnsTheFile(t *testing.T) {
	dir := t.TempDir()
	writeGuide(t, dir, "mail.example.com", "# mail.example.com\n\nLayout notes.\n")
	guide, err := ReadGuide(dir, "mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if guide != "# mail.example.com\n\nLayout notes.\n" {
		t.Errorf("guide came back changed: %q", guide)
	}
}

func TestGuidePathSharesTheCardNaming(t *testing.T) {
	// A host that needs escaping must land on the same basename its card
	// would, or `memory show <host>` and the file on disk disagree.
	got := GuidePath("/data", "localhost:8080")
	want := filepath.Join("/data", "guides", "localhost%3A8080.md")
	if got != want {
		t.Errorf("GuidePath = %q, want %q", got, want)
	}
}

func TestReadGuideCapsAtALineBoundary(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("x", 100) + "\n"
	var b strings.Builder
	for len(b.String()) < MaxGuideBytes+100 {
		b.WriteString(line)
	}
	writeGuide(t, dir, "big.example.com", b.String())

	guide, err := ReadGuide(dir, "big.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(guide, "(truncated — guide is ") {
		t.Errorf("an over-cap guide must say it was truncated:\n%.120s", guide)
	}
	// The cut is at a newline: body plus that newline is a prefix of the file.
	body, _, _ := strings.Cut(guide, "\n(truncated")
	if !strings.HasPrefix(b.String(), body+"\n") {
		t.Errorf("the cut did not land on a line boundary")
	}
	if len(body) > MaxGuideBytes {
		t.Errorf("body past the cap: %d bytes", len(body))
	}
}

func TestGuideNoteAbsentIsEmpty(t *testing.T) {
	if note := GuideNote(t.TempDir(), "mail.example.com"); note != "" {
		t.Errorf("a host without a guide must not be announced: %q", note)
	}
}

func TestGuideNotePointsAtTheFileNeverTheProse(t *testing.T) {
	dir := t.TempDir()
	writeGuide(t, dir, "mail.example.com", "# the why lives here\n")
	note := GuideNote(dir, "mail.example.com")
	for _, want := range []string{"[site guide]", GuidePath(dir, "mail.example.com")} {
		if !strings.Contains(note, want) {
			t.Errorf("the pointer is missing %q: %q", want, note)
		}
	}
	if strings.Contains(note, "the why lives here") {
		t.Errorf("the pointer must carry the path, never the prose: %q", note)
	}
}

func TestGuideNoteEmptyFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	writeGuide(t, dir, "mail.example.com", "")
	if note := GuideNote(dir, "mail.example.com"); note != "" {
		t.Errorf("an empty guide says nothing, so its pointer must too: %q", note)
	}
}

// The pointer rides the announcement injection — the landing, or the snapshot
// that stood in for one — and only there: the verification render stays the
// bare map, or the agent hears about the same file on every read.

// newGuideTestManager builds the Manager directly rather than through
// newTestManager, because these tests must write the guide into the same data
// directory the manager reads — and Store().Dir() is the cards/ subdirectory,
// not the data directory.
func newGuideTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "test"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, dir
}

func TestLandingCarriesTheGuidePointer(t *testing.T) {
	m, dir := newGuideTestManager(t)
	host := "example.test"
	seedCard(t, m, host)
	writeGuide(t, dir, host, "# example.test\n")

	land(t, m, host, "e1")
	note := m.TakeSiteNote("navigate", host, 1)
	if !strings.Contains(note, "site guide") {
		t.Errorf("the landing did not announce the guide:\n%s", note)
	}
	if !strings.Contains(note, "selector_not_found") {
		t.Errorf("the pointer must ride after the card, not replace it:\n%s", note)
	}

	snapshot(t, m, host, "e2", "link [World] @e1")
	verify := m.TakeSiteNote("snapshot", host, 1)
	if strings.Contains(verify, "site guide") {
		t.Errorf("the verification render must stay the bare map:\n%s", verify)
	}
}

func TestLandingWithGuideButNoCardStillAnnounces(t *testing.T) {
	m, dir := newGuideTestManager(t)
	host := "example.test"
	writeGuide(t, dir, host, "# example.test\n")

	land(t, m, host, "e1")
	note := m.TakeSiteNote("navigate", host, 1)
	if !strings.Contains(note, "site guide") {
		t.Fatalf("a guide that arrived before the card is still worth the landing: %q", note)
	}
	if n := countKind(t, m, KindCardShown); n != 0 {
		t.Errorf("%d card_shown records, but no card was shown — the guide is not a card", n)
	}
}

func TestLandingWithoutGuideHasNoPointer(t *testing.T) {
	m, _ := newGuideTestManager(t)
	host := "example.org"
	seedCard(t, m, host)

	land(t, m, host, "e1")
	if note := m.TakeSiteNote("navigate", host, 1); strings.Contains(note, "site guide") {
		t.Errorf("a host without a guide must not be announced:\n%s", note)
	}
}
