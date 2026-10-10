package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-bridge/internal/memory"
)

// `memory show --resolve` is the only surface that shows a site map's refs
// without a live snapshot, which makes it the one surface that can print a ref
// an agent could not act on. The tests below are mostly about what it must NOT
// print: a stale page's refs, a ref for an entry that did not resolve, and the
// header that claims the check happened against the current page.

func resolvedHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BB_HOME", dir)
	return dir
}

// mailPage is a PageDigest as the trace would have recorded it. The refs are
// deliberately not sequential across the two pages below, so a render that
// picked the older digest would produce a different ref rather than a
// coincidentally equal one.
func mailPage(url string, inboxRef string) *memory.PageDigest {
	return &memory.PageDigest{
		URL: url,
		Nodes: []memory.NodeSig{
			{Role: "list", Name: "Inbox", Ref: inboxRef, Depth: 2},
			{Role: "toolbar", Name: "Message actions", Ref: "@e2", Depth: 2},
		},
	}
}

func mailCard() *memory.SiteCard {
	return &memory.SiteCard{
		Host:        "mail.example.com",
		Revision:    7,
		UpdatedAtMs: 1_700_000_000_000,
		Map: []memory.MapEntry{
			{Purpose: "read mail", Pred: memory.Predicate{Role: "list", Name: "Inbox"}, Value: 88, Uses: 3},
			{Purpose: "read mail", Pred: memory.Predicate{Role: "toolbar", Name: "Message actions"}, Value: 49},
			// Folders is not in either recorded page: the site dropped that
			// sidebar since the entry was learned.
			{Purpose: "read mail", Pred: memory.Predicate{Role: "navigation", Name: "Folders"}, Value: 31},
		},
		Failures: []memory.FailureEntry{
			{Signature: "no such element", Command: "gettext", Sel: "#compose-btn", Count: 2},
		},
		Procedures: []memory.ProcedureEntry{
			{Goal: "read a message", Successes: 3, Steps: []memory.Step{
				{Command: "gettext", On: "list[Inbox]"},
			}},
		},
	}
}

// seed stores a card and a trace with two snapshots for the host and one for a
// different host.
func seed(t *testing.T, home string) {
	t.Helper()
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(mailCard()); err != nil {
		t.Fatal(err)
	}
	stream, err := memory.OpenStream(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()

	recs := []memory.TraceRecord{
		// Older page: Inbox is @e40 here, so a render that reached back for the
		// first digest would print @e40 and the test would catch it.
		{Kind: memory.KindResponse, AtMs: 1_700_000_000_000, Command: "snapshot", Host: "mail.example.com",
			Page: mailPage("https://mail.example.com/older", "@e40")},
		{Kind: memory.KindResponse, AtMs: 1_700_000_100_000, Command: "snapshot", Host: "other.example.com",
			Page: mailPage("https://other.example.com/", "@e77")},
		{Kind: memory.KindResponse, AtMs: 1_700_000_200_000, Command: "snapshot", Host: "mail.example.com",
			Page: mailPage("https://mail.example.com/u/0/#inbox", "@e1")},
	}
	for _, r := range recs {
		if err := stream.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShowResolveResolvesAgainstTheLastRecordedPage(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com", "--resolve")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}

	for _, want := range []string{
		"Resolved offline against the last page",
		"2 of 3 map entries matched that page; 1 did not.",
		formatMs(1_700_000_200_000),           // the newest snapshot, not the older
		"https://mail.example.com/u/0/#inbox", // and its url
		"→ @e1",                               // resolved from that page
		"resolved against the page seen",      // the map section names its evidence
		"no such element",                     // the failure half
		"Sequences that worked here:",         // and the procedure half
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--resolve output is missing %q:\n%s", want, stdout)
		}
	}

	// The header the live injection uses is a claim about the browser's current
	// state. Printing it here would make an offline render indistinguishable
	// from the real thing, which is the one confusion this flag exists to
	// prevent.
	if strings.Contains(stdout, "checked against this page") {
		t.Errorf("--resolve claims to have checked the current page:\n%s", stdout)
	}
	// @e40 is the Inbox ref on the earlier page and @e77 belongs to another
	// host entirely. Neither may appear.
	for _, stale := range []string{"@e40", "@e77", "other.example.com"} {
		if strings.Contains(stdout, stale) {
			t.Errorf("--resolve used a page other than the last one for this host (%s):\n%s", stale, stdout)
		}
	}
	// The entry that did not resolve must be counted, not rendered with a ref.
	if strings.Contains(stdout, "Folders") {
		t.Errorf("--resolve rendered an entry that did not match the page:\n%s", stdout)
	}
}

func TestShowResolveWithoutAPageSaysSo(t *testing.T) {
	home := resolvedHome(t)
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(mailCard()); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com", "--resolve")
	if err == nil {
		t.Fatalf("expected a non-nil error when there is nothing to resolve against:\n%s", stdout)
	}
	if !strings.Contains(stderr, "No page recorded for mail.example.com") {
		t.Errorf("the reason is not stated:\n%s", stderr)
	}
	// The readable halves still print: a card is not withheld because the map
	// could not be resolved.
	if !strings.Contains(stdout, "no such element") {
		t.Errorf("the card itself should still be readable:\n%s", stdout)
	}
	if strings.Contains(stdout, "→ @") {
		t.Errorf("a ref was printed with no page to resolve it against:\n%s", stdout)
	}
}

func TestShowRejectsRawWithResolve(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)

	_, _, err := runCLI(t, "memory", "show", "mail.example.com", "--raw", "--resolve")
	if err == nil {
		t.Fatal("--raw and --resolve are contradictory and must not both apply")
	}
	if !strings.Contains(err.Error(), "pick one") {
		t.Errorf("the conflict is not explained: %v", err)
	}
}

func TestShowWithoutResolveHasNoRefs(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	// The plain rendering has no page behind it, so it must not print a site
	// map at all — this is the behaviour ADR-0024 records, asserted here so the
	// flag's existence cannot quietly become the default.
	if strings.Contains(stdout, "Site map") {
		t.Errorf("plain `memory show` rendered a site map it cannot resolve:\n%s", stdout)
	}
	if !strings.Contains(stdout, "no such element") {
		t.Errorf("plain `memory show` lost the failure list:\n%s", stdout)
	}
}

// The guide is the card's human-curated half (ADR-0028), and `memory show` is
// the pull every recall path already makes, so the guide rides it (ADR-0033).
// The tests below pin when it appears: after the card, never in --raw, and
// even when the card itself is missing — a host can have a guide before it
// has earned a card.

const mailGuide = "# mail.example.com site guide\n\n## Layout\n- compose: button[aria-label=Compose]\n"

func writeMailGuide(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, "data", "guides")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mail.example.com.md"), []byte(mailGuide), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShowAppendsTheCuratedGuide(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)
	writeMailGuide(t, home)

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	for _, want := range []string{
		"no such element", // the card is still there
		"[site guide] mail.example.com",
		"data/guides/mail.example.com.md",
		"aria-label=Compose", // and the guide's own text
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show with a guide is missing %q:\n%s", want, stdout)
		}
	}
	// The guide rides *after* the card: the card is the machine's claim, the
	// guide is commentary on it.
	if strings.Index(stdout, "site guide") < strings.Index(stdout, "no such element") {
		t.Errorf("the guide printed before the card:\n%s", stdout)
	}
}

func TestShowWithoutACardStillPrintsTheGuide(t *testing.T) {
	home := resolvedHome(t)
	writeMailGuide(t, home)

	stdout, _, err := runCLI(t, "memory", "show", "mail.example.com")
	if err == nil || !strings.Contains(err.Error(), "no card for mail.example.com") {
		t.Fatalf("the missing card is still reported the way it always was: %v", err)
	}
	if !strings.Contains(stdout, "aria-label=Compose") {
		t.Errorf("a host with a guide but no card must still hand back the guide:\n%s", stdout)
	}
}

func TestShowRawOmitsTheGuide(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)
	writeMailGuide(t, home)

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com", "--raw")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	// --raw is the editing surface: it prints the stored card JSON, and mixing
	// prose into it would break the pipe it was asked for.
	if !strings.Contains(stdout, `"rev": 7`) {
		t.Errorf("--raw lost the card JSON:\n%s", stdout)
	}
	if strings.Contains(stdout, "site guide") {
		t.Errorf("--raw printed the guide into what must stay card JSON:\n%s", stdout)
	}
}

func TestShowWithoutAGuidePrintsNoGuideSection(t *testing.T) {
	home := resolvedHome(t)
	seed(t, home)

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.Contains(stdout, "site guide") {
		t.Errorf("a missing guide is the normal state and must print nothing:\n%s", stdout)
	}
}

// writeTestGuide writes a curated guide the way a human would, so a test can
// assert the JSON carries it.
func writeTestGuide(t *testing.T, dataDir, host, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "guides"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memory.GuidePath(dataDir, host), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// `bridge memory rm` is the escape hatch ADR-0031 §3 is built around: "a card
// has to be removable". It checked for the file by re-deriving the store's
// naming rule as host+".json", while the store escapes any host that needs it
// — ::1 is %3A%3A1, an underscore hostname is %5F. So the existence check
// looked for a file that was never written, and the command reported "no card
// for ::1" with the real card sitting right there.
//
// That is exactly the host set ADR-0032 newly admitted, so the two ADRs
// disagreed in practice: one said a card has to be removable, the other
// admitted hosts the escape hatch could not reach.
func TestMemoryRmRemovesACardWhoseHostNeededEscaping(t *testing.T) {
	home := resolvedHome(t)
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{"::1", "a_b.example.com", "mail.example.com"}
	for _, host := range hosts {
		if err = store.Put(&memory.SiteCard{Host: host}); err != nil {
			t.Fatal(err)
		}
		// The card is on disk, at the path the store names for it.
		if _, statErr := os.Stat(store.Path(host)); statErr != nil {
			t.Fatalf("seed %q: %v", host, statErr)
		}
	}

	for _, host := range hosts {
		if _, stderr, runErr := runCLI(t, "memory", "rm", host); runErr != nil {
			t.Errorf("bridge memory rm %s = %v\n%s", host, runErr, stderr)
		}
		if _, ok := store.Get(host); ok {
			t.Errorf("bridge memory rm %s left the card in place", host)
		}
	}
}

// The hint readCard prints has to name a file that exists. It used to be
// built as filepath.Join(store.Dir(), host+".json"), which is not where the
// store writes a host that needed escaping — so the advice "fix it with an
// editor" pointed at nothing.
func TestUnparseableCardHintNamesTheFileThatExists(t *testing.T) {
	home := resolvedHome(t)
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	host := "a_b.example.com"
	if err = os.WriteFile(store.Path(host), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, runErr := runCLI(t, "memory", "show", host)
	if runErr == nil {
		t.Fatal("memory show on an unparseable card succeeded")
	}
	if !strings.Contains(stderr, store.Path(host)) {
		t.Errorf("the parse-failure hint does not name the file that exists:\n%s", stderr)
	}
}

// `--json` is a persistent root flag, so a subcommand that accepts it and
// ignores it is worse than one that lacks it: the caller has no way to tell.
//
// Both skills name `bridge memory show <host> --json` as *the* way an agent
// pulls a card (ADR-0027 — at coding time nothing has been injected, so the
// pull is the only way to get it), and an agent piping that command into a JSON
// parser was getting the human rendering. `memory learn`, `history` and `rm`
// had the same gap.
func TestEveryMemorySubcommandHonoursJSON(t *testing.T) {
	home := resolvedHome(t)
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(mailCard()); err != nil {
		t.Fatal(err)
	}
	writeTestGuide(t, home+"/data", "mail.example.com", "# mail\n\nLayout notes.\n")

	for _, args := range [][]string{
		{"memory", "show", "mail.example.com", "--json"},
		{"memory", "list", "--json"},
		{"memory", "history", "mail.example.com", "--json"},
		{"memory", "learn", "--json"},
		{"memory", "rm", "mail.example.com", "--json"},
	} {
		t.Run(args[1], func(t *testing.T) {
			stdout, stderr, err := runCLI(t, args...)
			_ = stderr
			// rm removes the card, so it is the last case to run; the others
			// tolerate its absence, and every one is asserted on shape only.
			if err != nil && args[1] != "rm" {
				t.Fatalf("%v: %v\n%s", args, err, stderr)
			}
			var out any
			if uerr := json.Unmarshal([]byte(stdout), &out); uerr != nil {
				t.Errorf("%v printed something that is not JSON: %v\n%s", args, uerr, stdout)
			}
		})
	}
}

// The pull an agent actually makes has to carry the content, not just be
// parseable. `card` is the stored card and `guide` the human-curated prose, so
// a single command gives the agent both halves.
func TestMemoryShowJSONCarriesTheCardAndTheGuide(t *testing.T) {
	home := resolvedHome(t)
	store, err := memory.OpenStore(home + "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(mailCard()); err != nil {
		t.Fatal(err)
	}
	writeTestGuide(t, home+"/data", "mail.example.com", "Layout notes.\n")

	stdout, stderr, err := runCLI(t, "memory", "show", "mail.example.com", "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	var out struct {
		Host      string           `json:"host"`
		Card      *memory.SiteCard `json:"card"`
		Rendered  string           `json:"rendered"`
		Guide     *string          `json:"guide"`
		GuidePath string           `json:"guidePath"`
	}
	if err = json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if out.Host != "mail.example.com" {
		t.Errorf("host = %q", out.Host)
	}
	if out.Card == nil {
		t.Fatal("card is null; the agent gets nothing to select from")
	}
	if len(out.Card.Map) == 0 {
		t.Error("the card came back with an empty site map")
	}
	if out.Guide == nil || *out.Guide != "Layout notes.\n" {
		t.Errorf("guide = %v, want the curated prose", out.Guide)
	}
	if out.GuidePath == "" {
		t.Error("guidePath is empty, so the agent cannot open the file itself")
	}
}

// "A `no card for <host>` answer is normal — continue without it." The skill
// says so, so in JSON mode a missing card is a value and the command succeeds.
func TestMemoryShowJSONTreatsAMissingCardAsAValue(t *testing.T) {
	resolvedHome(t)
	stdout, stderr, err := runCLI(t, "memory", "show", "nothing.example.com", "--json")
	if err != nil {
		t.Fatalf("a missing card failed in JSON mode, but the skill says to continue: %v\n%s", err, stderr)
	}
	var out struct {
		Card *memory.SiteCard `json:"card"`
	}
	if err = json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if out.Card != nil {
		t.Errorf("card = %+v, want null", out.Card)
	}
}
