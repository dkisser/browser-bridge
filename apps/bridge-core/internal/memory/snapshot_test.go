package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const testSnapshot = `Page: Example News | https://news.example.com/
heading(1) [Top stories]
link [World] href="/world" @e1
link [Next page] href="/p/2" @e2
textbox name="q" @e3
button [Go] @e4
text [some prose that must not be remembered]
`

func digestOfText(t *testing.T, text string) *PageDigest {
	t.Helper()
	url, title := splitPageLine(text)
	return ParseSnapshot(text, snapshotResult{
		Snapshot:     text,
		NodesTotal:   10,
		NodesEmitted: 6,
		Tier:         0,
	}, url, title)
}

func TestParseSnapshotKeepsAddressableStructure(t *testing.T) {
	d := digestOfText(t, testSnapshot)

	if d.URL != "https://news.example.com/" {
		t.Errorf("URL = %q, want the url from the Page: line", d.URL)
	}
	// The page title is never persisted: it is verbatim page content and
	// nothing matches on it (ADR-0018).
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Example News") {
		t.Errorf("the page title reached the serialized digest: %s", raw)
	}
	if len(d.Nodes) != 4 {
		t.Fatalf("got %d addressable nodes, want 4 (the heading carries no ref): %+v", len(d.Nodes), d.Nodes)
	}

	first := d.Nodes[0]
	if first.Role != "link" || first.Name != "World" || first.Ref != "e1" {
		t.Errorf("first node = %+v, want link/World/e1", first)
	}
	if first.Attrs["href"] != "/world" {
		t.Errorf("href = %q, want /world", first.Attrs["href"])
	}

	var box *NodeSig
	for i := range d.Nodes {
		if d.Nodes[i].Role == "textbox" {
			box = &d.Nodes[i]
		}
	}
	if box == nil {
		t.Fatal("no textbox node parsed")
	}
	if box.Attrs["name"] != "q" {
		t.Errorf("textbox name attr = %q, want q", box.Attrs["name"])
	}
}

// A link's href is a URL, and it is the third channel that reaches a card file
// with a token in it — safeURL already closed the navigate argument and the
// snapshot URL, and this is the one Predicate.AttrVal carried raw. It mattered
// twice over: the value is rendered to the agent from the card, and with
// BRIDGE_MEMORY_API_KEY set compress.go posts the rendered card off-machine.
func TestParseSnapshotReducesLinkHref(t *testing.T) {
	const page = `Page: Mail | https://mail.example.com/
link [Inbox] href="/u/0/?token=SECRET123&q=private+meditation" @e1
`
	d := digestOfText(t, page)
	if d == nil || len(d.Nodes) == 0 {
		t.Fatalf("no node parsed from %q", page)
	}
	var link *NodeSig
	for i := range d.Nodes {
		if d.Nodes[i].Role == "link" {
			link = &d.Nodes[i]
		}
	}
	if link == nil {
		t.Fatal("no link node parsed")
	}
	got := link.Attrs["href"]
	if strings.Contains(got, "SECRET123") || strings.Contains(got, "meditation") {
		t.Fatalf("the query string survived into the digest: %q", got)
	}
	if got != "/u/0/" {
		t.Errorf("href = %q, want the query dropped and the path kept", got)
	}
}

// The bound has to survive the reduction. safeURL drops the query, not the
// path, and a card outlives the visit and — with BRIDGE_MEMORY_API_KEY set — is
// sent to a model endpoint, so an unbounded path is a channel whatever it
// happens to contain today.
func TestParseSnapshotBoundsALongHref(t *testing.T) {
	long := "https://x.test/" + strings.Repeat("segment/", 80) + "end"
	d := digestOfText(t, `Page: X | https://x.test/
link [Deep] href="`+long+`" @e1
`)
	var link *NodeSig
	for i := range d.Nodes {
		if d.Nodes[i].Role == "link" {
			link = &d.Nodes[i]
		}
	}
	if link == nil {
		t.Fatal("no link node parsed")
	}
	if n := len(link.Attrs["href"]); n > maxAttrValLen {
		t.Errorf("href is %d chars, want it bounded by %d", n, maxAttrValLen)
	}
}

func TestParseSnapshotDropsProse(t *testing.T) {
	d := digestOfText(t, testSnapshot)
	// ADR-0018: a text line is page content, not structure. Nothing derived from
	// the digest may contain it, or it would be on disk.
	for _, n := range d.Nodes {
		if n.Role == "text" {
			t.Fatalf("a text-role node survived into the digest: %+v", n)
		}
		if strings.Contains(n.Name, "some prose") {
			t.Fatalf("page prose leaked into a node name: %+v", n)
		}
	}
	if strings.Contains(d.Shape, "some prose") {
		t.Fatal("page prose leaked into the shape hash")
	}
}

func TestShapeSurvivesRefRenumbering(t *testing.T) {
	// The extension renumbers refs on every snapshot, so a shape that included
	// them would report every page as changed.
	a := digestOfText(t, `Page: X | https://x.test/
link [A] href="/a" @e1
link [B] href="/b" @e2
`)
	b := digestOfText(t, `Page: X | https://x.test/
link [A] href="/a" @e7
link [B] href="/b" @e9
`)
	if a.Shape != b.Shape {
		t.Errorf("shape changed under ref renumbering: %s vs %s", a.Shape, b.Shape)
	}
}

func TestShapeChangesWithStructure(t *testing.T) {
	a := digestOfText(t, `Page: X | https://x.test/
link [A] href="/a" @e1
`)
	b := digestOfText(t, `Page: X | https://x.test/
heading(1) [New]
link [A] href="/a" @e1
`)
	if a.Shape == b.Shape {
		t.Error("shape did not change when the page gained a heading")
	}
}

func TestParseHandlesAwkwardNames(t *testing.T) {
	// formatName does not escape anything, so a label carrying a quote, a
	// bracket or an at-sign must not derail the tail parse.
	d := digestOfText(t, `Page: X | https://x.test/
link [Report "urgent" @ noon] href="/r" @e1
link [a=b] @e2
`)
	if len(d.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(d.Nodes), d.Nodes)
	}
	if d.Nodes[0].Attrs["href"] != "/r" {
		t.Errorf("href = %q; the name containing \" and @ confused the attribute parse", d.Nodes[0].Attrs["href"])
	}
	if d.Nodes[0].Ref != "e1" {
		t.Errorf("ref = %q, want e1", d.Nodes[0].Ref)
	}
}

func TestResolveMatchesByRoleAndNamePrefix(t *testing.T) {
	d := digestOfText(t, testSnapshot)

	ref, ok := d.Resolve(Predicate{Role: "link", Name: "World"})
	if !ok || ref != "e1" {
		t.Errorf("Resolve(link, World) = %q, %v; want e1, true", ref, ok)
	}

	// A snapshot truncates a long name at 40 chars, so an exact match against
	// the visible prefix has to work.
	ref, ok = d.Resolve(Predicate{Role: "link", Name: "Next"})
	if !ok || ref != "e2" {
		t.Errorf("Resolve(link, Next) = %q, %v; want e2, true (prefix match)", ref, ok)
	}

	if _, ok := d.Resolve(Predicate{Role: "link", Name: "Missing"}); ok {
		t.Error("Resolve matched a name that is not on the page")
	}
	if _, ok := d.Resolve(Predicate{Role: "button", Name: "World"}); ok {
		t.Error("Resolve matched on name alone, ignoring the role")
	}
	if _, ok := d.Resolve(Predicate{Role: "link", Name: "World", AttrKey: "href", AttrVal: "/other"}); ok {
		t.Error("Resolve matched with the wrong attribute value")
	}
}

func TestSplitPageLine(t *testing.T) {
	// A title can contain the separator, so the split has to come from the right.
	url, title := splitPageLine("Page: News | Live | https://n.test/\nheading @e1")
	if url != "https://n.test/" || title != "News | Live" {
		t.Errorf("splitPageLine = (%q, %q), want (https://n.test/, News | Live)", url, title)
	}
	if url, _ := splitPageLine("heading [X] @e1"); url != "" {
		t.Errorf("splitPageLine on a snapshot with no Page line = %q, want empty", url)
	}
}

// The MCP schema allows a 100,000-character pseudo-tree, which is thousands of
// nodes. Every snapshot is written to a log that is never rotated, so the digest
// is capped. Without this a busy session writes hundreds of KB per snapshot.
func TestDigestCapsPersistedNodes(t *testing.T) {
	var b strings.Builder
	b.WriteString("Page: Big | https://big.test/\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "link [Item %d] href=\"/i/%d\" @e%d\n", i, i, i)
	}
	d := digestOfText(t, b.String())
	if len(d.Nodes) != maxDigestNodes {
		t.Errorf("digest kept %d nodes, want the cap of %d", len(d.Nodes), maxDigestNodes)
	}
	// The shape is computed over the whole tree regardless of the cap, so the
	// fingerprint does not weaken just because the node list was trimmed.
	if d.Shape == "" {
		t.Error("shape hash was lost when nodes were capped")
	}
	// A call aimed past the cap degrades to "not learned", never to a wrong
	// entry: the ref is simply absent from the digest.
	if _, ok := d.Resolve(Predicate{Role: "link", Name: "Item 2999"}); ok {
		t.Error("a node beyond the cap resolved; the cap is not being applied")
	}
}
