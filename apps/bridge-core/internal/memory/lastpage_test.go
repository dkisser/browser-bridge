package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// The pull path is mandated on every landing by ADR-0039's skill step, so
// LastPageDigest is no longer a once-when-a-person-looks cost. These tests pin
// that the memoisation added for it cannot serve a stale answer, because the
// whole value of it is that it is allowed to skip work only it would have
// repeated.

func appendTraceLine(t *testing.T, dir, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, streamFileName), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func pageRecord(atMs int64, host, ref string) string {
	return `{"kind":"snapshot","at":` + itoa(atMs) + `,"env":"e1","cmd":"snapshot","tab":1,"browser":"b1","host":"` + host +
		`","out":"ok","page":{"url":"https://` + host + `/","nodes":[{"role":"article","name":"Body","ref":"` + ref + `","d":1}]}}`
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Two pulls of the same host at the same stamp must agree, and the second must
// not depend on the stream still holding what the first saw.
func TestLastPageDigestIsStableAcrossRepeatedPulls(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "browser-1"})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	appendTraceLine(t, dir, pageRecord(1_700_000_000_000, "example.com", "e7"))

	first, firstAt, err := m.LastPageDigest("example.com")
	if err != nil {
		t.Fatalf("first pull: %v", err)
	}
	if first == nil {
		t.Fatal("no digest for a host with a recorded page")
	}
	if len(first.Nodes) != 1 || first.Nodes[0].Ref != "e7" {
		t.Errorf("digest = %+v, want the e7 node", first.Nodes)
	}
	if firstAt != 1_700_000_000_000 {
		t.Errorf("atMs = %d, want 1700000000000", firstAt)
	}

	second, secondAt, err := m.LastPageDigest("example.com")
	if err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if second != first || secondAt != firstAt {
		t.Errorf("the memoised answer differs from the first: %+v/%d vs %+v/%d", second, secondAt, first, firstAt)
	}
}

// A host with no page is a nil answer, not an error, and the nil has to survive
// being cached — otherwise every pull for an unvisited host re-reads the trace.
func TestLastPageDigestCachesTheAbsentCaseToo(t *testing.T) {
	m := newTestManager(t, "browser-1")
	if d, _, err := m.LastPageDigest("never-visited.example"); d != nil || err != nil {
		t.Fatalf("first = %v/%v, want nil/nil", d, err)
	}
	if d, _, err2 := m.LastPageDigest("never-visited.example"); d != nil || err2 != nil {
		t.Fatalf("second = %v/%v, want nil/nil", d, err2)
	}
}

// The one that matters: an appended record must invalidate. Without this the
// cache would answer "no page recorded" for a host that has just been visited,
// which is the same silent-miss shape the pull is supposed to avoid.
func TestLastPageDigestSeesARecordAppendedAfterACachedMiss(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{DataDir: dir, BrowserID: "browser-1"})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	if d, _, perr := m.LastPageDigest("example.com"); d != nil || perr != nil {
		t.Fatalf("precondition: %v/%v, want nil/nil", d, perr)
	}

	appendTraceLine(t, dir, pageRecord(1_700_000_000_000, "example.com", "e9"))

	d, atMs, err := m.LastPageDigest("example.com")
	if err != nil {
		t.Fatalf("post-append pull: %v", err)
	}
	if d == nil {
		t.Fatal("the cached miss survived an appended record — every later pull would be wrong")
	}
	if d.Nodes[0].Ref != "e9" {
		t.Errorf("digest node = %+v, want ref e9", d.Nodes[0])
	}
	if atMs != 1_700_000_000_000 {
		t.Errorf("atMs = %d, want 1700000000000", atMs)
	}
}

// Hosts are cached independently, so alternating between two does not thrash.
func TestLastPageDigestCachesPerHost(t *testing.T) {
	dir := t.TempDir()
	appendTraceLine(t, dir, pageRecord(1_700_000_000_000, "a.example", "e1"))
	appendTraceLine(t, dir, pageRecord(1_700_000_000_001, "b.example", "e2"))

	m, err := New(Options{DataDir: dir, BrowserID: "browser-1"})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	for _, tc := range []struct{ host, ref string }{{"a.example", "e1"}, {"b.example", "e2"}, {"a.example", "e1"}} {
		d, _, err := m.LastPageDigest(tc.host)
		if err != nil {
			t.Fatalf("%s: %v", tc.host, err)
		}
		if d == nil || d.Nodes[0].Ref != tc.ref {
			t.Errorf("%s: got %+v, want ref %s", tc.host, d, tc.ref)
		}
	}
}

// LastDigestFor picks the newest, with ties going to the later record.
func TestLastDigestForPicksTheNewestPage(t *testing.T) {
	recs := []TraceRecord{
		{Host: "example.com", AtMs: 100, Page: &PageDigest{URL: "https://example.com/old"}},
		{Host: "other.example", AtMs: 500, Page: &PageDigest{URL: "https://other.example/"}},
		{Host: "example.com", AtMs: 300, Page: &PageDigest{URL: "https://example.com/new"}},
	}
	d, atMs := LastDigestFor(recs, "example.com")
	if d == nil || d.URL != "https://example.com/new" || atMs != 300 {
		t.Errorf("got %+v at %d, want the 300ms record", d, atMs)
	}
	if d, _ := LastDigestFor(recs, "missing.example"); d != nil {
		t.Errorf("an unvisited host got %+v, want nil", d)
	}
}

// The stamp has to cover the rotation, because a rotation does not change the
// active file's size — it renames it and creates a new one. A stamp built from
// the active file alone would keep serving a digest from the generation that
// was just moved aside.
func TestStreamStampMovesWhenTheStreamGrows(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStream(dir)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	before := s.Stamp()
	if err := s.Append(TraceRecord{Kind: "snapshot", AtMs: 1, Host: "example.com"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if s.Stamp() == before {
		t.Error("the stamp did not move after an append, so a cached pull would never be invalidated")
	}
}

// RenderResolvedOffline is the one copy of the offline rendering that the CLI's
// --resolve and the MCP memory_show both go through (ADR-0026's provenance has
// to change in both places at once, which is what a copy cannot promise).
func TestRenderResolvedOfflineCountsTheCardAndNamesThePage(t *testing.T) {
	card := &SiteCard{
		Host: "example.com",
		Map: []MapEntry{
			{Purpose: "body", Pred: Predicate{Role: "article", Name: "Body"}},
			{Purpose: "sidebar", Pred: Predicate{Role: "navigation", Name: "Side"}},
		},
	}
	digest := &PageDigest{URL: "https://example.com/", Nodes: []NodeSig{{Role: "article", Name: "Body", Ref: "e7"}}}

	got := RenderResolvedOffline(card, digest, 1_700_000_000_000)
	if got.Matched != 1 || got.Total != 2 || got.Missing != 1 {
		t.Errorf("counts = %d/%d/%d, want 1 of 2 matched, 1 missing", got.Matched, got.Total, got.Missing)
	}
	if got.PageURL != "https://example.com/" || got.SeenAtMs != 1_700_000_000_000 {
		t.Errorf("provenance = %q at %d", got.PageURL, got.SeenAtMs)
	}
	// The provenance has to reach the map section's own title, not just the
	// counts — a footer nobody reads is what ADR-0026 rules out.
	if !contains(got.Text, "the page seen") {
		t.Errorf("the rendering does not name the page it resolved against:\n%s", got.Text)
	}
	// And it must never claim the live injection's phrase, which is true of the
	// page in hand and false of anything read out of a file.
	if contains(got.Text, "checked against this page") {
		t.Errorf("the offline render used the live injection's phrase:\n%s", got.Text)
	}
}

// No digest: the readable halves still render, and the counts report every map
// entry as unresolved rather than silently dropping the section.
func TestRenderResolvedOfflineWithoutADigest(t *testing.T) {
	card := &SiteCard{
		Host:       "example.com",
		Failures:   []FailureEntry{{Signature: "no such element", Command: "gettext", Sel: "article", Count: 2}},
		Map:        []MapEntry{{Purpose: "body", Pred: Predicate{Role: "article", Name: "Body"}}},
		Procedures: []ProcedureEntry{},
	}
	got := RenderResolvedOffline(card, nil, 0)
	if got.Matched != 0 || got.Total != 1 || got.Missing != 1 {
		t.Errorf("counts = %d/%d/%d, want 0 of 1 matched", got.Matched, got.Total, got.Missing)
	}
	if !contains(got.Text, "Observed to fail here") {
		t.Errorf("the failures tier did not render without a resolver:\n%s", got.Text)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
