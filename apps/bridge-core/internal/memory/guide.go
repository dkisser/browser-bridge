package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxGuideBytes bounds how much of a curated guide `bridge memory show` will
// print. A guide rides the card's recall path into context on every visit
// (ADR-0033), so its size is bounded for the same reason the card's rendering
// is. The skill that writes guides already keeps them short; the cap is for
// the guide that grew anyway.
const MaxGuideBytes = 8192

// GuidePath returns where the curated guide for a host lives. Guides share the
// cards' host-keyed, filesystem-safe naming, so `bridge memory show <host>`
// finds the same host's guide without a second mapping to keep in sync.
func GuidePath(dir, host string) string {
	return filepath.Join(dir, "guides", safeFileName(host)+".md")
}

// GuideNote is the one-line pointer the landing paths carry when a host has a
// curated guide: the path, never the prose (ADR-0034). The prose stays behind
// the pull (`bridge memory show`) because it is unbounded; a fixed-shape
// pointer line is not. "" when the host has no guide.
func GuideNote(dir, host string) string {
	path := GuidePath(dir, host)
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		return ""
	}
	return "[site guide] this host has a curated guide: " + path +
		" — read it when you need the why; where it disagrees with the live page, the page wins."
}

// appendGuideNote adds the pointer to a rendered card, keeping the pointer
// last: the card is the machine's claim, the guide is commentary on it.
func appendGuideNote(card, dir, host string) string {
	note := GuideNote(dir, host)
	if note == "" {
		return card
	}
	return card + "\n" + note
}

// ReadGuide returns the curated guide for a host, or "" when none exists — a
// host without a guide is the normal state of the world, not an error. Content
// past MaxGuideBytes is cut at a line boundary and marked, so the reader can
// tell the guide was longer and where it stopped.
func ReadGuide(dir, host string) (string, error) {
	data, err := os.ReadFile(GuidePath(dir, host))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	text := string(data)
	if len(text) <= MaxGuideBytes {
		return text, nil
	}
	cut := strings.LastIndex(text[:MaxGuideBytes], "\n")
	if cut < 0 {
		cut = MaxGuideBytes
	}
	return fmt.Sprintf("%s\n(truncated — guide is %d bytes, cap is %d)",
		text[:cut], len(text), MaxGuideBytes), nil
}
