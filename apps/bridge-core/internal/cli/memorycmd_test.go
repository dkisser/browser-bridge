package cli

import (
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
		"2 of 3 map entries matched it; 1 did not.",
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
