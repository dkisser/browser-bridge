package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// builder writes a plausible call sequence into a stream, the way the router
// would after real traffic.
type builder struct {
	t     *testing.T
	s     *Stream
	clock int64
	n     int
	tab   int
}

func newBuilder(t *testing.T, s *Stream) *builder {
	return &builder{t: t, s: s, clock: 1_700_000_000_000, tab: 1}
}

func (b *builder) tick() int64 {
	b.clock += 1000
	return b.clock
}

func (b *builder) env(prefix string) string {
	b.n++
	return fmt.Sprintf("%s%d", prefix, b.n)
}

// landing writes a navigate that lands on url.
func (b *builder) landing(url string) { //nolint:unparam // varied per test as coverage grows
	env := b.env("nav")
	_ = b.s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: "navigate", TabID: b.tab,
		Args: map[string]any{"url": url}})
	_ = b.s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: "navigate", TabID: b.tab, Outcome: OutcomeOK})
}

// tabSwitch writes a tab_switch that lands the tab on url. It is the commonest
// landing command in the MCP flow and, unlike navigate, names no element work.
func (b *builder) tabSwitch(tab int, url string) {
	b.tab = tab
	env := b.env("tsw")
	_ = b.s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: "tab:switch", TabID: tab,
		Args: map[string]any{"tab_id": tab}})
	// The response record carries the resolved host, which is what the learner
	// reads — a tab:switch names no url in its arguments at all.
	_ = b.s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: "tab:switch", TabID: tab,
		Outcome: OutcomeOK, Host: Host(url)})
}

// snap writes a successful snapshot carrying the given pseudo-tree.
func (b *builder) snap(text string) { //nolint:unparam // varied per test as coverage grows
	env := b.env("snap")
	_ = b.s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: "snapshot", TabID: b.tab})
	_ = b.s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: "snapshot", TabID: b.tab,
		Outcome: OutcomeOK, Page: digestOfText(b.t, text)})
}

// call writes a successful element-addressing call at a ref.
func (b *builder) call(command, sel string) { //nolint:unparam // varied per test as coverage grows
	env := b.env("call")
	_ = b.s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: command, TabID: b.tab,
		Args: map[string]any{"selector": sel}})
	_ = b.s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: command, TabID: b.tab, Outcome: OutcomeOK})
}

// fail writes a failed call — the shape that creates a card on sight.
func (b *builder) fail(command, sel, code, msg string) { //nolint:unparam // varied per test as coverage grows
	env := b.env("fail")
	_ = b.s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: command, TabID: b.tab,
		Args: map[string]any{"selector": sel}})
	_ = b.s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: command, TabID: b.tab,
		Outcome: OutcomeError, ErrCode: code, ErrMsg: msg})
}

func (b *builder) idle(d time.Duration) { b.clock += int64(d / time.Millisecond) }

func newLearner(t *testing.T) (*Learn, *Stream, *Store) { //nolint:unparam // dir is unused by callers today
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStream(dir)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewLearn(s, store, LoadCursor(dir), nil, nil), s, store
}

func TestLearnerWritesNothingForABareNavigate(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hosts := store.Hosts(); len(hosts) != 0 {
		t.Errorf("hosts = %v; merely navigating somewhere must not mint a card", hosts)
	}
}

func TestLearnerCreatesCardOnFailure(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")
	b.fail("gettext", ".entry-content", "selector_not_found", "No element matches .entry-content")

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("a host that produced an error got no card")
	}
	if len(card.Failures) != 1 {
		t.Fatalf("failures = %+v, want one", card.Failures)
	}
	f := card.Failures[0]
	if f.Signature != "selector_not_found" || f.Sel != ".entry-content" || f.Count != 1 {
		t.Errorf("failure = %+v, want the guessed selector kept with its code", f)
	}
	// A failing segment is not evidence of a working procedure.
	if len(card.Procedures) != 0 {
		t.Errorf("procedures = %+v, want none from a failed segment", card.Procedures)
	}
}

func TestLearnerBuildsMapFromConfirmedCallsOnly(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")
	b.snap(testSnapshot)
	b.call("gettext", "@e1")   // resolved by the snapshot
	b.call("gettext", ".nope") // a raw selector: not a map entry
	b.call("gettext", "@e99")  // a ref the snapshot never had

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("no card for a host where the agent did verified work")
	}
	if len(card.Map) != 1 {
		t.Fatalf("map = %+v, want exactly the one confirmed entry", card.Map)
	}
	e := card.Map[0]
	if e.Purpose != "text container" || e.Pred.Role != "link" || e.Pred.Name != "World" {
		t.Errorf("entry = %+v, want the link called World used as a text container", e)
	}
	// A card carries no handle (ADR-0023). The ref this entry was observed at
	// lives in the trace, where it is intrinsic; copying it here made the one
	// value in the file that could not be re-resolved, and it was read by
	// nothing. Asserted on the serialised form as well as the struct, because a
	// handle could come back through a custom marshaller just as easily.
	if blob, err := json.Marshal(e); err != nil {
		t.Fatal(err)
	} else if bytes.Contains(blob, []byte(`"ref"`)) || bytes.Contains(blob, []byte("e1")) {
		t.Errorf("a site-map entry carries a handle: %s", blob)
	}
}

func TestProcedureNeedsCorroborationBeforeItIsShown(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)

	// One clean attempt: a candidate is written, but it is not yet trustworthy.
	b.landing("https://news.example.com/")
	b.snap(testSnapshot)
	b.call("gettext", "@e1")
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, _ := store.Get("news.example.com")
	if len(card.Procedures) != 1 || card.Procedures[0].Successes != 1 {
		t.Fatalf("procedures = %+v, want one candidate with Successes 1", card.Procedures)
	}
	if out := RenderCard(card, RenderOptions{MaxTokens: DefaultInjectTokens}); strings.Contains(out, "Sequences that worked") {
		t.Error("an uncorroborated procedure was injected; a single no-error is not evidence")
	}

	// A second, independent attempt with the same shape corroborates it.
	b.idle(3 * time.Minute)
	b.landing("https://news.example.com/")
	b.snap(testSnapshot)
	b.call("gettext", "@e1")
	if err := l.Run(); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	card, _ = store.Get("news.example.com")
	// The count is deliberately not capped at the threshold: it records how often
	// the shape was seen, which is what you would look at before retiring a
	// procedure. What matters here is that it crossed the threshold.
	if card.Procedures[0].Successes < ProcedureCorroboration {
		t.Errorf("Successes = %d, want at least %d", card.Procedures[0].Successes, ProcedureCorroboration)
	}
	out := RenderCard(card, RenderOptions{MaxTokens: DefaultInjectTokens})
	if !strings.Contains(out, "Sequences that worked") {
		t.Errorf("corroborated procedure not injected:\n%s", out)
	}
}

func TestOversizedReadIsASoftFailure(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")

	// ADR-0003's incident, reproduced: a get_html that returned 420K of chrome
	// raised no error, so "no error means success" would have filed it as a good
	// procedure.
	env := b.env("dump")
	_ = s.Append(TraceRecord{Kind: KindCommand, AtMs: b.tick(), Envelope: env, Command: "gethtml", TabID: b.tab,
		Args: map[string]any{"selector": "body"}})
	_ = s.Append(TraceRecord{Kind: KindResponse, AtMs: b.tick(), Envelope: env, Command: "gethtml", TabID: b.tab,
		Outcome: OutcomeSoft, ErrCode: "oversized_result", ResultSz: 420_000, ErrMsg: "read returned 420000 chars"})

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("a whole-page dump produced no card")
	}
	if len(card.Failures) != 1 || card.Failures[0].Signature != "oversized_result" {
		t.Errorf("failures = %+v, want an oversized_result entry", card.Failures)
	}
}

func TestFailuresAccumulateInsteadOfListing(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	for i := 0; i < 3; i++ {
		b.idle(3 * time.Minute)
		b.landing("https://news.example.com/")
		b.fail("gettext", ".entry-content", "selector_not_found", "nope")
	}
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, _ := store.Get("news.example.com")
	if len(card.Failures) != 1 {
		t.Fatalf("failures = %+v, want one grouped entry", card.Failures)
	}
	if card.Failures[0].Count != 3 {
		t.Errorf("Count = %d, want 3", card.Failures[0].Count)
	}
}

func TestLearnerIgnoresCallsOnUnknownHosts(t *testing.T) {
	l, s, store := newLearner(t)
	_ = store
	b := newBuilder(t, s)
	// No landing first: a bare click names no site, so it can teach nothing
	// about one.
	b.fail("click", "#x", "selector_not_found", "nope")
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hosts := store.Hosts(); len(hosts) != 0 {
		t.Errorf("hosts = %v, want none — a call with no known site has no card to write to", hosts)
	}
}

func TestLearnerRecordsRevisionForEveryWrite(t *testing.T) {
	l, s, _ := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")
	b.fail("gettext", ".x", "selector_not_found", "nope")
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	revs, err := s.Revisions("news.example.com", 0)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("revisions = %+v, want one", revs)
	}
	if revs[0].Reason != "new" {
		t.Errorf("reason = %q, want new", revs[0].Reason)
	}
}

func TestLearnPassIsIdempotentAcrossRuns(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.landing("https://news.example.com/")
	b.fail("gettext", ".x", "selector_not_found", "nope")
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	first, _ := store.Get("news.example.com")

	// A second pass with the cursor already advanced must not re-apply the same
	// records, or every idle wake-up would inflate the counts.
	if err := l.Run(); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	second, _ := store.Get("news.example.com")
	if second.Revision != first.Revision {
		t.Errorf("revision moved from %d to %d on a no-op pass", first.Revision, second.Revision)
	}
	if second.Failures[0].Count != first.Failures[0].Count {
		t.Errorf("failure count moved from %d to %d on a no-op pass", first.Failures[0].Count, second.Failures[0].Count)
	}
}

func TestRedactArgsNeverKeepsTypedText(t *testing.T) {
	out := redactArgs("type", map[string]any{"selector": "#q", "text": "my password", "submit": true})
	if _, ok := out["text"]; ok {
		t.Error("redactArgs kept the typed text")
	}
	if got, ok := out["text_len"].(int); !ok || got != len("my password") {
		t.Errorf("text_len = %v, want %d", out["text_len"], len("my password"))
	}
	if out["selector"] != "#q" {
		t.Errorf("selector = %v, want it kept — it is structure, not content", out["selector"])
	}
}

func TestHostScope(t *testing.T) {
	cases := map[string]string{
		"https://News.Example.com/path?q=1": "news.example.com",
		"http://example.com:8080/x":         "example.com",
		"https://example.com.":              "example.com",
		"example.com/feed":                  "example.com",
		"about:blank":                       "",
		"":                                  "",
		"not a url":                         "",
	}
	for in, want := range cases {
		if got := Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
	// A registrable domain is deliberately *not* the scope: gmail.com and
	// news.google.com are different operations, so they get different cards.
	if Host("https://mail.google.com") == Host("https://news.google.com") {
		t.Error("gmail and news collapsed into one card")
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens("abcd"); got != 1 {
		t.Errorf("4 ascii chars = %d tokens, want 1", got)
	}
	if got := estimateTokens("站点"); got != 2 {
		t.Errorf("2 CJK chars = %d tokens, want 2", got)
	}
	if estimateTokens(strings.Repeat("x", 4000)) <= 400 {
		t.Error("a 4000-char body was estimated under the 400-token budget")
	}
}

func TestRenderCardRespectsBrowserScope(t *testing.T) {
	card := &SiteCard{
		Host: "example.com",
		Map: []MapEntry{
			{Purpose: "text container", Pred: Predicate{Role: "link", Name: "A"}, Browser: "b-1"},
			{Purpose: "text container", Pred: Predicate{Role: "link", Name: "B"}, Browser: "b-2"},
		},
	}
	digest := digestOfText(t, `Page: X | https://x.test/
link [A] @e1
link [B] @e2
`)
	out := RenderCard(card, RenderOptions{
		MaxTokens: DefaultInjectTokens,
		BrowserID: "b-1",
		Resolver:  func(p Predicate) (string, bool) { return digest.Resolve(p) },
	})
	if !strings.Contains(out, "[A]") {
		t.Errorf("entry for the current browser missing:\n%s", out)
	}
	if strings.Contains(out, "[B]") {
		t.Errorf("an entry observed under a different browser profile was injected:\n%s", out)
	}
}

func TestRenderCardReportsStaleness(t *testing.T) {
	card := &SiteCard{Host: "example.com", Map: []MapEntry{
		{Purpose: "text container", Pred: Predicate{Role: "link", Name: "Gone"}},
	}}
	out := RenderCard(card, RenderOptions{
		MaxTokens: DefaultInjectTokens,
		Resolver:  func(Predicate) (string, bool) { return "", false },
	})
	if !strings.Contains(out, InjectionLabel) {
		t.Errorf("stale render lost its label:\n%s", out)
	}
	if !strings.Contains(out, "no longer match") {
		t.Errorf("stale render did not tell the agent to re-check:\n%s", out)
	}
}

func TestRenderCardEmptyWhenNothingApplies(t *testing.T) {
	if out := RenderCard(nil, RenderOptions{}); out != "" {
		t.Errorf("RenderCard(nil) = %q, want empty", out)
	}
	card := &SiteCard{Host: "example.com"}
	if out := RenderCard(card, RenderOptions{MaxTokens: DefaultInjectTokens}); out != "" {
		t.Errorf("RenderCard of an empty card = %q, want empty", out)
	}
}

func TestRenderCardTrimsProceduresFirst(t *testing.T) {
	var procs []ProcedureEntry
	for i := 0; i < 8; i++ {
		procs = append(procs, ProcedureEntry{
			Goal: fmt.Sprintf("then navigate the listing page and read each row's text container in order, "+
				"using the row ref rather than a class selector, variant %d", i),
			Steps:     []Step{{Command: "gettext", On: "@e1"}, {Command: "gettext", On: "@e2"}},
			Successes: ProcedureCorroboration,
		})
	}
	card := &SiteCard{
		Host: "example.com",
		Map:  []MapEntry{{Purpose: "text container", Pred: Predicate{Role: "link", Name: "World"}}},
		Failures: []FailureEntry{{
			Signature: "selector_not_found", Command: "gettext", Sel: ".entry-content", Count: 4,
		}},
		Procedures: procs,
	}
	digest := digestOfText(t, testSnapshot)
	const budget = 80
	out := RenderCard(card, RenderOptions{
		MaxTokens: budget,
		Resolver:  func(p Predicate) (string, bool) { return digest.Resolve(p) },
	})
	if estimateTokens(out) > budget+16 {
		t.Errorf("render is %d tokens, over the %d budget:\n%s", estimateTokens(out), budget, out)
	}
	// The two trustworthy tiers survive the trim.
	if !strings.Contains(out, "Site map") || !strings.Contains(out, "Observed to fail") {
		t.Errorf("trim dropped a tier that should have survived:\n%s", out)
	}
	if strings.Contains(out, "Sequences that worked") {
		t.Errorf("procedures should have been trimmed first:\n%s", out)
	}
}

func TestRenderCardUsesFreshRefs(t *testing.T) {
	card := &SiteCard{Host: "example.com", Map: []MapEntry{
		{Purpose: "text container", Pred: Predicate{Role: "link", Name: "World"}},
	}}
	digest := digestOfText(t, testSnapshot)
	out := RenderCard(card, RenderOptions{
		MaxTokens: DefaultInjectTokens,
		Resolver:  func(p Predicate) (string, bool) { return digest.Resolve(p) },
	})
	if !strings.Contains(out, "@e1") {
		t.Errorf("render did not hand back a ref from this page:\n%s", out)
	}
	// The stored ref was also e1 here, so make the point explicit with a page
	// where the same control has a different number.
	renamed := digestOfText(t, `Page: Example News | https://news.example.com/
link [World] href="/world" @e77
`)
	out = RenderCard(card, RenderOptions{
		MaxTokens: DefaultInjectTokens,
		Resolver:  func(p Predicate) (string, bool) { return renamed.Resolve(p) },
	})
	if !strings.Contains(out, "@e77") {
		t.Errorf("render reused the stored ref instead of the live one:\n%s", out)
	}
}

func TestCompressIsOptional(t *testing.T) {
	// ADR-0022: with no key there is no compressor, and the feature is whole.
	t.Setenv(envAPIKey, "")
	if c := NewCompressorFromEnv(); c != nil {
		t.Error("NewCompressorFromEnv returned a compressor with no API key set")
	}
}

func TestCompressSendsOpenAIWireFormat(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody = string(body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"- a\n- b"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envAPIKey, "sk-test")
	t.Setenv(envBaseURL, srv.URL)
	c := NewCompressorFromEnv()
	if c == nil {
		t.Fatal("no compressor despite a key being set")
	}
	out, err := c.Compress("example.com", "Site map:\n  - text container → @e1")
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if out != "- a\n- b" {
		t.Errorf("compressed = %q", out)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if body["stream"] != false {
		t.Errorf("stream = %v, want false — a compression call is one shot", body["stream"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want system + user", len(msgs))
	}
}

func TestCompressFailureIsNotFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envAPIKey, "sk-test")
	t.Setenv(envBaseURL, srv.URL)
	c := NewCompressorFromEnv()
	if _, err := c.Compress("example.com", "text"); err == nil {
		t.Error("a 500 did not surface as an error the caller can ignore")
	}
}

// A pass that is capped must not advance the cursor past the segments it
// skipped. The records behind the cursor are never looked at again, so trimming
// the front and advancing anyway (which an earlier version did) destroys those
// lessons permanently — and a daemon idle over a long backlog is exactly when it
// would happen.
func TestCappedPassDefersInsteadOfDropping(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)

	// One failing segment per host, so every segment is card-worthy and the
	// count is driven purely by the cap.
	const hosts = maxSegmentsPerPass + 10
	for i := 0; i < hosts; i++ {
		host := fmt.Sprintf("h%03d.example.com", i)
		b.landing("https://" + host + "/")
		b.fail("gettext", ".x", "selector_not_found", "nope")
	}
	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	firstBatch := len(store.Hosts())
	if firstBatch > maxSegmentsPerPass {
		t.Fatalf("pass wrote %d cards, want at most %d", firstBatch, maxSegmentsPerPass)
	}

	// Drain: keep passing until the backlog is gone.
	for pass := 0; pass < 5; pass++ {
		if err := l.Run(); err != nil {
			t.Fatalf("drain pass %d: %v", pass, err)
		}
	}
	if got := len(store.Hosts()); got != hosts {
		t.Errorf("after draining, %d of %d hosts have cards; %d were dropped rather than deferred",
			got, hosts, hosts-got)
	}
}

// Records carry the command's host and arguments, not only a navigate's. A card
// for the commonest MCP flow — switch to a tab the agent already has open, then
// work in it — is only learnable if a get_text on an inherited tab still names
// a site. Before the response record carried them, that flow produced nothing.
func TestLearnerLearnsAfterATabSwitch(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)

	// tab_switch establishes the host...
	b.tabSwitch(1, "https://news.example.com/mail/inbox")
	// ...and the work afterwards names no site at all.
	b.snap(testSnapshot)
	b.call("gettext", "@e1")

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("work after a tab_switch produced no card")
	}
	if len(card.Map) == 0 {
		t.Error("card has no site map entries from a confirmed call")
	}
}

// A failure's selector is what the agent will otherwise guess again, so it has
// to survive on the response record — not only on the command.
func TestLearnerKeepsTheSelectorOnTheFailure(t *testing.T) {
	l, s, store := newLearner(t)
	b := newBuilder(t, s)
	b.tabSwitch(1, "https://news.example.com/")
	b.fail("gettext", ".entry-content", "selector_not_found", "No element matches")

	if err := l.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	card, ok := store.Get("news.example.com")
	if !ok {
		t.Fatal("no card")
	}
	if len(card.Failures) != 1 || card.Failures[0].Sel != ".entry-content" {
		t.Errorf("failures = %+v, want the guessed selector preserved", card.Failures)
	}
}

// stepsOf used to zip two lists with different filters — the shape has one
// entry per response, seg.calls only the successful ref-addressed ones — and
// pair them by index. Any segment containing a failure diverged, and the result
// was a card claiming a call that *errored* succeeded on a specific control.
// This is the tier that is supposed to be the trustworthy one, so a false
// positive here is worse than having no procedure tier.
func TestProcedureStepsKeepTheirOwnRef(t *testing.T) {
	digest := &PageDigest{Nodes: []NodeSig{
		{Role: "link", Name: "A", Ref: "e1"},
		{Role: "link", Name: "B", Ref: "e2"},
		{Role: "link", Name: "C", Ref: "e3"},
	}}
	seg := &segment{digest: digest, shape: []shapeStep{
		{command: "snapshot"},
		// This one failed. It must contribute a step with no ref and no note.
		{command: "gettext"},
		{command: "gettext", ref: "e2", found: true},
		{command: "gettext", ref: "e3", found: true},
	}}
	// A call that was ref-addressed but which this digest does not know: the ref
	// is kept (it was aimed at something) but nothing claims the page confirmed it.
	seg.shape = append(seg.shape, shapeStep{command: "click", ref: "e99"})

	steps := stepsOf(seg)
	want := []Step{
		{Command: "snapshot"},
		{Command: "gettext"},
		{Command: "gettext", On: "@e2", Note: "text container"},
		{Command: "gettext", On: "@e3", Note: "text container"},
		{Command: "click", On: "@e99"},
	}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, steps[i], want[i])
		}
	}
	// The failure is the load-bearing case: a step with a note means the card is
	// telling the agent "this worked here", on a call that returned an error.
	for i, s := range steps {
		if i == 1 && s.Note != "" {
			t.Errorf("the failed call was recorded as %q, a successful read", s.Note)
		}
	}
}

func TestProcedureStepsIgnoreParallelCallList(t *testing.T) {
	// seg.calls is now only used for the site map. Populating it differently
	// from the shape must not be able to move a ref, because that is precisely
	// the coupling that produced the mis-attribution.
	seg := &segment{
		digest: &PageDigest{Nodes: []NodeSig{{Role: "link", Name: "A", Ref: "e1"}}},
		shape: []shapeStep{
			{command: "gettext", ref: "e1", found: true},
			{command: "click", ref: "e1", found: true},
		},
		calls: []observedCall{
			{command: "gettext", ref: "e7", found: true},
		},
	}
	steps := stepsOf(seg)
	if steps[0].On != "@e1" || steps[1].On != "@e1" {
		t.Errorf("steps took their refs from seg.calls: %+v", steps)
	}
}
