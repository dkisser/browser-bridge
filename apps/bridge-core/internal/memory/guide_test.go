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
