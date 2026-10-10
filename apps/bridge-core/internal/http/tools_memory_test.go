package http

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"browser-bridge/internal/memory"
)

// fakeMemories is a MemoryReader with canned answers, so these tests pin the
// tool's rendering and its honesty about missing things rather than the store's
// behaviour — which internal/memory already covers against real files.
type fakeMemories struct {
	hosts  []string
	cards  map[string]*memory.SiteCard
	guides map[string]string
	digest *memory.PageDigest
	atMs   int64

	readErr  error
	guideErr error
}

func (f *fakeMemories) ListHosts() []string { return f.hosts }

func (f *fakeMemories) ReadCard(host string) (*memory.SiteCard, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.cards[host], nil
}

func (f *fakeMemories) ReadGuide(host string) (string, error) {
	if f.guideErr != nil {
		return "", f.guideErr
	}
	return f.guides[host], nil
}

func (f *fakeMemories) GuidePath(host string) string {
	if f.guides[host] == "" {
		return ""
	}
	return "/data/guides/" + host + ".md"
}

func (f *fakeMemories) LastPageDigest(host string) (*memory.PageDigest, int64, error) {
	if f.digest == nil {
		return nil, 0, nil
	}
	return f.digest, f.atMs, nil
}

func memoryTestServer(router *fakeRouter, reader MemoryReader) *MCPServer {
	return NewMCP(MCPOptions{
		Router:         router,
		Registry:       fakeRegistry{},
		DefaultTimeout: 10 * time.Second,
		Version:        "0.3.2",
		Memories:       reader,
	})
}

// aCard is a card with one map entry that a digest can resolve and one failure,
// so a rendering has all three tiers to trim against.
func aCard() *memory.SiteCard {
	return &memory.SiteCard{
		Host:        "example.com",
		Revision:    3,
		UpdatedAtMs: 1_700_000_000_000,
		Map: []memory.MapEntry{
			{Purpose: "article body", Pred: memory.Predicate{Role: "article", Name: "News"}},
			{Purpose: "sidebar", Pred: memory.Predicate{Role: "navigation", Name: "Sidebar"}},
		},
		Failures: []memory.FailureEntry{
			{Signature: "no such element", Command: "gettext", Sel: "article", Count: 2},
		},
	}
}

// aDigest matches the card's first map entry and not its second, so the
// rendering has both a matched and a missing entry to report.
func aDigest() *memory.PageDigest {
	return &memory.PageDigest{
		URL: "https://example.com/news",
		Nodes: []memory.NodeSig{
			{Role: "article", Name: "News", Ref: "e7"},
			{Role: "banner", Name: "Ad", Ref: "e8"},
		},
	}
}

func TestMemoryListReportsWhatTheStoreHolds(t *testing.T) {
	router := &fakeRouter{}
	srv := memoryTestServer(router, &fakeMemories{
		hosts: []string{"example.com", "news.example.org"},
		cards: map[string]*memory.SiteCard{
			"example.com":      aCard(),
			"news.example.org": {Host: "news.example.org", Revision: 1, UpdatedAtMs: 1_700_000_100_000},
		},
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_list", map[string]any{}))

	var rows []memoryListRow
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		t.Fatalf("memory_list did not return JSON rows: %v\n%s", err, text)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].Host != "example.com" || rows[0].Map != 2 || rows[0].Failures != 1 {
		t.Errorf("row[0] = %+v, want example.com with 2 map / 1 failure", rows[0])
	}
	if rows[0].UpdatedAt == "-" {
		t.Errorf("row[0].UpdatedAt = %q, want a real timestamp", rows[0].UpdatedAt)
	}
}

func TestMemoryListOnAnEmptyStoreIsAValue(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_list", map[string]any{}))
	if !strings.Contains(text, "No site cards yet") {
		t.Errorf("empty store should say so, got:\n%s", text)
	}
}

// The whole point of the pull: the map renders, and it renders resolved.
func TestMemoryShowResolvesTheMapOfflineAndSaysSo(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		digest: aDigest(),
		atMs:   1_700_000_200_000,
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))

	if !strings.Contains(text, "1 of 2 map entries matched that page; 1 did not.") {
		t.Errorf("staleness count missing or wrong:\n%s", text)
	}
	if !strings.Contains(text, "https://example.com/news") {
		t.Errorf("resolved map did not render the matched entry:\n%s", text)
	}
	// ADR-0026: the reader must be able to tell this apart from the live
	// injection, and one statement of provenance is a footer nobody reads.
	if strings.Contains(text, "checked against this page") {
		t.Errorf("offline render used the live injection's phrase:\n%s", text)
	}
	if !strings.Contains(text, "not valid for whatever is in your browser now") {
		t.Errorf("offline render did not warn that the refs are stale:\n%s", text)
	}
}

// A card with no recorded page is a different fact from a card whose map was
// withheld, and RenderCard drops the map section when there is no resolver
// (ADR-0024) — so the tool has to say which happened.
func TestMemoryShowWithoutARecordedPageSaysTheMapIsUnresolved(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards: map[string]*memory.SiteCard{"example.com": aCard()},
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))

	if !strings.Contains(text, "cannot be resolved") {
		t.Errorf("unresolved map was not reported:\n%s", text)
	}
	if !strings.Contains(text, "checked against nothing") {
		t.Errorf("reader was not told the card is unchecked:\n%s", text)
	}
}

// A missing card is the normal state of the world and must not read as a
// failure — the skill tells the agent to carry on, so the tool has to say so.
func TestMemoryShowMissingCardIsNotAnError(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{})
	session := newTestClient(t, srv)

	result := callTool(t, session, "memory_show", map[string]any{"host": "unknown.example"})
	if result.IsError {
		t.Errorf("missing card reported as an error: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if !strings.Contains(text, "No card for unknown.example") {
		t.Errorf("missing card not reported plainly:\n%s", text)
	}
	if !strings.Contains(text, "Carry on without one") {
		t.Errorf("missing card did not tell the agent to continue:\n%s", text)
	}
}

// ADR-0033: guides are written at a human's request while cards are earned by
// traffic, so a guide can legitimately arrive first and must still be served.
func TestMemoryShowServesAGuideWithNoCard(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		guides: map[string]string{"example.com": "# Layout\nThe body is #ContentBody."},
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))

	if !strings.Contains(text, "No card for example.com") {
		t.Errorf("missing card not reported:\n%s", text)
	}
	if !strings.Contains(text, "The body is #ContentBody.") {
		t.Errorf("guide was dropped because no card existed:\n%s", text)
	}
	if !strings.Contains(text, "/data/guides/example.com.md") {
		t.Errorf("guide path missing:\n%s", text)
	}
}

func TestMemoryShowCarriesTheGuideAfterTheCard(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		guides: map[string]string{"example.com": "# Layout\nDismiss the cookie banner first."},
		digest: aDigest(),
		atMs:   1_700_000_200_000,
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))

	if !strings.Contains(text, "[site guide]") {
		t.Errorf("guide section missing:\n%s", text)
	}
	if !strings.Contains(text, "Dismiss the cookie banner first.") {
		t.Errorf("guide prose missing:\n%s", text)
	}
}

// raw is the machine view; the guide's prose stays out of it (ADR-0033's
// budget argument) but its path travels so the reader knows to go and get it.
func TestMemoryShowRawIsTheStructuredCard(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		guides: map[string]string{"example.com": "# Layout\nSome prose."},
		digest: aDigest(),
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com", "raw": true}))

	var out struct {
		Host      string           `json:"host"`
		Card      *memory.SiteCard `json:"card"`
		GuidePath string           `json:"guidePath"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("raw is not JSON: %v\n%s", err, text)
	}
	if out.Host != "example.com" || out.Card == nil || out.Card.Revision != 3 {
		t.Errorf("raw payload = %+v, want the host and the card", out)
	}
	if out.GuidePath != "/data/guides/example.com.md" {
		t.Errorf("raw guidePath = %q, want the path", out.GuidePath)
	}
	if strings.Contains(text, "Some prose.") {
		t.Errorf("raw carried the guide prose, which the budget argument excludes:\n%s", text)
	}
}

// A card that will not parse is reported with the reason, not hidden behind an
// empty answer — an agent must be able to tell "empty" from "broken".
func TestMemoryShowReportsAnUnreadableCard(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{readErr: errors.New("unexpected EOF")})
	session := newTestClient(t, srv)

	result := callTool(t, session, "memory_show", map[string]any{"host": "example.com"})
	if !result.IsError {
		t.Errorf("unreadable card was not an error: %s", resultText(t, result))
	}
	if !strings.Contains(resultText(t, result), "unexpected EOF") {
		t.Errorf("the reason was swallowed: %s", resultText(t, result))
	}
}

// Self-learning is optional; app.go comes up without a store when it will not
// open, and the tools must say that rather than fail obscurely.
func TestMemoryToolsReportDisabledLearning(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, nil)
	session := newTestClient(t, srv)

	for name, args := range map[string]map[string]any{
		"memory_list": {},
		"memory_show": {"host": "example.com"},
	} {
		result := callTool(t, session, name, args)
		if !result.IsError {
			t.Errorf("%s: disabled learning was not an error", name)
		}
		if !strings.Contains(resultText(t, result), "self-learning is disabled") {
			t.Errorf("%s: %s", name, resultText(t, result))
		}
	}
}

// dispatch calls TakeSiteNote, which yields a card at most once per landing.
// A memory tool that went through dispatch would spend the landing's card and
// leave the browsing tools with nothing, so neither tool may reach the router.
func TestMemoryToolsNeverReachTheBrowser(t *testing.T) {
	router := &fakeRouter{}
	srv := memoryTestServer(router, &fakeMemories{
		hosts:  []string{"example.com"},
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		digest: aDigest(),
	})
	session := newTestClient(t, srv)

	callTool(t, session, "memory_list", map[string]any{})
	callTool(t, session, "memory_show", map[string]any{"host": "example.com"})

	if got := router.captured(); len(got) != 0 {
		t.Errorf("memory tools sent %d commands to the browser: %+v", len(got), got)
	}
}

// The schema is closed and these are the only tools with no tab_id or
// timeout_ms — the signal that they read the store and not the browser.
func TestMemorySchemasTakeNoTabOrTimeout(t *testing.T) {
	listSchema := toolInputSchemas["memory_list"]
	if listSchema.AdditionalProperties == nil || len(listSchema.Properties) != 0 {
		t.Errorf("memory_list should be a closed empty object, got %+v", listSchema.Properties)
	}
	show := toolInputSchemas["memory_show"]
	if show.AdditionalProperties == nil {
		t.Error("memory_show must stay closed (additionalProperties: false)")
	}
	for _, forbidden := range []string{"tab_id", "timeout_ms"} {
		if _, ok := show.Properties[forbidden]; ok {
			t.Errorf("memory_show must not accept %s — it does not drive the browser", forbidden)
		}
	}
	if _, ok := show.Properties["raw"]; !ok {
		t.Error("memory_show is missing the raw flag the skill's crystallize flow needs")
	}
}

// Guard the tool contract itself: a typo in a handler name would otherwise only
// show up as a missing tool at runtime.
func TestMemoryToolsAreRegistered(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{})
	session := newTestClient(t, srv)

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	found := map[string]bool{}
	for _, tool := range list.Tools {
		found[tool.Name] = true
	}
	for _, name := range []string{"memory_list", "memory_show"} {
		if !found[name] {
			t.Errorf("%s is not in tools/list", name)
		}
	}
}

// #1: the tool is documented as the MCP equivalent of `bridge memory show`, so
// it has to normalise the host the way that command does. An agent holding a
// URL from pageinfo is the ordinary case, and an empty answer there reads as
// "never learned" while a card sits on disk.
//
// The bar is agreement with memory.Host, not perfection: a bare
// "example.com:8443" keeps its port there too, because url.Parse reads
// "example.com:" as a scheme. That is a pre-existing property of the
// normaliser both sides share, and changing it here would make the two
// interfaces disagree about the same host — which is the thing #1 is about.
func TestMemoryShowNormalisesTheHostLikeTheCLI(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		digest: aDigest(),
		atMs:   1_700_000_200_000,
	})
	session := newTestClient(t, srv)

	for _, host := range []string{
		"example.com",
		"EXAMPLE.com",
		"https://example.com/news",
		"https://example.com:8443/news",
		"  example.com  ",
		"example.com/news",
	} {
		text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": host}))
		if strings.Contains(text, "No card for") {
			t.Errorf("host %q missed a card that example.com has:\n%s", host, text)
		}
		if !strings.Contains(text, "Resolved offline") {
			t.Errorf("host %q did not resolve to the example.com card:\n%s", host, text)
		}
	}
}

// The degenerate case of the same thing: whitespace satisfies minLength:1, and
// "" is what normalises to, which safeFileName maps onto a "_" file.
func TestMemoryShowRejectsAHostThatNormalisesToNothing(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{})
	session := newTestClient(t, srv)

	result := callTool(t, session, "memory_show", map[string]any{"host": "   "})
	if !result.IsError {
		t.Errorf("blank host was not rejected: %s", resultText(t, result))
	}
	if !strings.Contains(resultText(t, result), "not a host name") {
		t.Errorf("blank host error is unclear: %s", resultText(t, result))
	}
}

// #3: a guide the human wrote failing to read must not cost the card, which is
// what this tool exists for. The CLI's printGuard — printGuide — said so first.
func TestMemoryShowKeepsTheCardWhenTheGuideWillNotRead(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:    map[string]*memory.SiteCard{"example.com": aCard()},
		digest:   aDigest(),
		guideErr: errors.New("permission denied"),
	})
	session := newTestClient(t, srv)

	result := callTool(t, session, "memory_show", map[string]any{"host": "example.com"})
	if result.IsError {
		t.Fatalf("an unreadable guide became a total failure: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if !strings.Contains(text, "Observed to fail here") {
		t.Errorf("the card was dropped along with the guide:\n%s", text)
	}
	if !strings.Contains(text, "Warning:") || !strings.Contains(text, "permission denied") {
		t.Errorf("the guide failure was swallowed rather than warned about:\n%s", text)
	}
}

// #4: a store whose only card will not parse is not an empty store. Saying
// "No site cards yet" would send an agent to re-learn a site that is on disk.
func TestMemoryListDistinguishesABrokenStoreFromAnEmptyOne(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		hosts:   []string{"broken.example"},
		readErr: errors.New("unexpected EOF"),
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_list", map[string]any{}))
	if strings.Contains(text, "No site cards yet") {
		t.Errorf("a broken store was reported as an empty one:\n%s", text)
	}
	if !strings.Contains(text, "broken.example") || !strings.Contains(text, "will not parse") {
		t.Errorf("the broken host was not named:\n%s", text)
	}
}

// The same, with good rows alongside: the readable ones are the answer, the
// broken one is a caveat on them.
func TestMemoryListKeepsGoodRowsAndFlagsTheBrokenOne(t *testing.T) {
	r := &partialMemories{fakeMemories: fakeMemories{
		hosts:  []string{"good.example", "broken.example"},
		cards:  map[string]*memory.SiteCard{"good.example": {Host: "good.example", Revision: 2}},
		digest: aDigest(),
	}}
	r.broken = map[string]bool{"broken.example": true}
	srv := memoryTestServer(&fakeRouter{}, r)
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_list", map[string]any{}))
	if !strings.Contains(text, "good.example") {
		t.Errorf("the readable row was dropped:\n%s", text)
	}
	if !strings.Contains(text, "omitted because they will not parse") {
		t.Errorf("the broken row was not flagged as a caveat:\n%s", text)
	}
}

// #5: an empty card is a real state, and rendering it as a header followed by
// nothing makes it indistinguishable from a card the budget loop emptied.
func TestMemoryShowSaysWhenACardHasNothingInIt(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"empty.example": {Host: "empty.example", Revision: 3}},
		digest: aDigest(),
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "empty.example"}))
	if !strings.Contains(text, "no entries") {
		t.Errorf("an empty card rendered as nothing at all:\n%s", text)
	}
}

// #2: RenderCard emits [learned site patterns] itself. A second bracketed label
// above it would put two of them in one payload and make the reader pick.
func TestMemoryShowDoesNotImpersonateTheInjectionLabel(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		digest: aDigest(),
		atMs:   1_700_000_200_000,
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))
	if n := strings.Count(text, "["+memory.InjectionLabel+"]"); n != 1 {
		t.Errorf("the injection label appears %d times, want exactly 1 (RenderCard's own):\n%s", n, text)
	}
	if strings.Contains(text, "[learned site memory") {
		t.Errorf("the pull added a second bracketed label:\n%s", text)
	}
}

// #6: the count describes the card, and says the cap applies, because
// RenderCard trims without marking what it dropped.
func TestMemoryShowStatesThatTheCapMayShowFewerEntries(t *testing.T) {
	srv := memoryTestServer(&fakeRouter{}, &fakeMemories{
		cards:  map[string]*memory.SiteCard{"example.com": aCard()},
		digest: aDigest(),
		atMs:   1_700_000_200_000,
	})
	session := newTestClient(t, srv)

	text := resultText(t, callTool(t, session, "memory_show", map[string]any{"host": "example.com"}))
	if !strings.Contains(text, "capped at") {
		t.Errorf("the count did not say the rendering is capped, so it promises lines that may be trimmed:\n%s", text)
	}
}

// partialMemories fails ReadCard for a named subset, which is the one case
// memory_list has to distinguish and fakeMemories cannot express.
type partialMemories struct {
	fakeMemories
	broken map[string]bool
}

func (p *partialMemories) ReadCard(host string) (*memory.SiteCard, error) {
	if p.broken[host] {
		return nil, errors.New("unexpected EOF")
	}
	return p.fakeMemories.ReadCard(host)
}
