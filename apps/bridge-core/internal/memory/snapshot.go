package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// This file turns a Snapshot response (the pseudo-tree text, ADR-0001) into the
// structure a card is built from. It is the boundary that ADR-0018 draws: the
// parser keeps *addressable structure* and throws away page prose.
//
// What survives: depth, role, heading level, the accessible name of a control,
// the two attributes the extension keeps (href on links, name on form fields,
// per content.ts collectAttrs), and the ref.
//
// What does not survive: every `text` line (that is page prose), every
// `generic` line with no name and no attributes (it carries no information the
// snapshot filter would not have dropped anyway), and — for the page's
// structural hash — names and refs themselves, since both are unstable: refs
// are renumbered on every snapshot, and a heading's wording changes whenever
// marketing does.
//
// The name of a *control* is kept (truncated) because a predicate has nothing
// to match on without it. The prose of the page does not. That is the
// distinction ADR-0018 leaves to the shape of the data rather than to a filter
// written later.

// maxNameLen is the length a name is truncated to before it is stored on disk
// or matched against. The extension's own TIER1_TEXT_MAX_CHARS happens to be 40
// too, but this is not dependent on it: the value is chosen here so that a name
// is short enough to be worth storing, and so that Resolve's prefix match has a
// bounded, predictable window.
const maxNameLen = 40

// maxAttrValLen bounds a stored attribute value, for the same reason.
const maxAttrValLen = 60

// attrValue reduces a digest attribute value to the part that generalises.
//
// The extension collects exactly two kinds of attribute (collectAttrs,
// content.ts): a link's href and a form control's name. href is a URL and gets
// safeURL — the same channel safeURL already closes for the navigate argument
// and the snapshot URL. A link's query string is a session token or a document
// id as readily as it is a search term, and a card file outlives the visit;
// `Predicate.AttrVal` carried the raw value through to `cards/<host>.json`,
// where it was rendered to the agent and, with BRIDGE_MEMORY_API_KEY set,
// posted off-machine by compress.go.
//
// name is kept whole. A form field's identifier is exactly the lesson that
// saves a call next time — "this site calls the box q" — and it is a name the
// extension chose to read, not prose the page was merely displaying.
func attrValue(key, val string) string {
	if key == "href" {
		return safeURL(val)
	}
	return truncate(val, maxAttrValLen)
}

// NodeSig is one addressable node, reduced to what a predicate can match on.
// Ref is the ref *at observation time*; it is evidence, not a handle — a card
// never hands a stored ref back to an agent, because that ref numbered some
// earlier snapshot and would address the wrong element or nothing at all.
type NodeSig struct {
	Role  string            `json:"role"`
	Name  string            `json:"name,omitempty"`
	Ref   string            `json:"ref"`
	Depth int               `json:"d"`
	Level int               `json:"l,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Kids  int               `json:"kids,omitempty"`
}

// pageNode is one parsed line, before projection to a NodeSig.
type pageNode struct {
	role  string
	name  string
	ref   string
	depth int
	level int
	attrs map[string]string
	kids  int
}

// PageDigest is the structural reduction of one snapshot: the nodes worth
// remembering, plus a shape hash that is blind to names and refs.
//
// The page title is deliberately NOT here. It is verbatim page content —
// "Inbox (3) - someone@gmail.com - Gmail" carries an address — it is not used
// for matching anything, and ADR-0018's claim is that no page text reaches
// disk. The URL is kept because it is a handle the agent navigated to, not
// something the page said.
type PageDigest struct {
	URL       string    `json:"url,omitempty"`
	Tier      int       `json:"tier,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Emitted   int       `json:"emitted,omitempty"`
	Total     int       `json:"total,omitempty"`
	Nodes     []NodeSig `json:"nodes,omitempty"`
	Shape     string    `json:"shape,omitempty"`
}

// snapshotResult mirrors SnapshotResult in packages/shared/src/snapshot.ts.
type snapshotResult struct {
	Snapshot     string `json:"snapshot"`
	Truncated    bool   `json:"truncated"`
	NodesTotal   int    `json:"nodes_total"`
	NodesEmitted int    `json:"nodes_emitted"`
	Tier         int    `json:"tier"`
}

// structural roles: everything that identifies a thing a card can talk about.
// `text` is absent on purpose (ADR-0018) and `generic` only appears when it
// carries a name, which is how the extension itself decides a generic is worth
// emitting (snapshot.ts isInteractiveKept).
func structuralRole(role string) bool {
	switch role {
	case "text", "":
		return false
	case "generic":
		return true // caller additionally requires a name
	default:
		return true
	}
}

// maxDigestNodes bounds how many nodes one snapshot contributes to the Trace.
//
// The MCP schema lets an agent ask for up to 100,000 characters of pseudo-tree
// (schemas.go, snapshot's max_chars), which on a dense page is thousands of
// nodes. Persisting all of them would make every snapshot a large record in a
// log that is never rotated (ADR-0020), so the count is capped.
//
// What the cap costs is bounded and known: a map entry can only be learned for
// a ref that is in the digest, so a call aimed past the cap simply is not
// learned. That degrades to "we did not learn that one" rather than to a wrong
// entry, which is the right way round. The cap is not a security control — the
// extension already bounds the snapshot it will produce — it is a growth-rate
// decision.
const maxDigestNodes = 256

// ParseSnapshot reduces snapshot text to a PageDigest. url comes from the
// snapshot's own Page: line, since the extension puts it there rather than in
// the result object. The title on that line is read and dropped.
//
// The URL is reduced to the part that identifies a site, for the same reason
// redactArgs reduces a command's url argument: a query string is whatever the
// site puts in one, and this digest is written to a card file that outlives the
// visit.
func ParseSnapshot(text string, res snapshotResult, url, _ string) *PageDigest {
	nodes := parseSnapshotLines(text)
	d := &PageDigest{
		URL:       safeURL(url),
		Tier:      res.Tier,
		Truncated: res.Truncated,
		Emitted:   res.NodesEmitted,
		Total:     res.NodesTotal,
	}
	shape := sha256.New()
	for _, n := range nodes {
		// The shape hash must survive ref renumbering, so the ref is not an
		// input. Names are excluded too: they change when copy changes, and a
		// copy change is not a structural change worth invalidating a card
		// over. Attribute *keys* are included — a link that stops carrying an
		// href is a real structural change.
		shape.Write([]byte(strconv.Itoa(n.depth)))
		shape.Write([]byte("|"))
		shape.Write([]byte(n.role))
		if n.level > 0 {
			shape.Write([]byte("("))
			shape.Write([]byte(strconv.Itoa(n.level)))
			shape.Write([]byte(")"))
		}
		for _, k := range sortedKeys(n.attrs) {
			shape.Write([]byte(" "))
			shape.Write([]byte(k))
		}
		shape.Write([]byte("\n"))

		if n.ref == "" || len(d.Nodes) >= maxDigestNodes {
			continue
		}
		sig := NodeSig{
			Role:  n.role,
			Name:  n.name,
			Ref:   n.ref,
			Depth: n.depth,
			Level: n.level,
			Kids:  n.kids,
		}
		if len(n.attrs) > 0 {
			sig.Attrs = n.attrs
		}
		d.Nodes = append(d.Nodes, sig)
	}
	d.Shape = hex.EncodeToString(shape.Sum(nil))[:16]
	return d
}

// parseSnapshotLines reads the rendered pseudo-tree back.
//
// The format is fixed by formatLine in packages/shared/src/snapshot.ts:
//
//	<indent><role>[(<level>)] [<name>] <k>="<v>" ... @<ref>
//
// It is parsed right-to-left rather than with one big pattern, because the
// tail is unambiguous (a ref, then attribute pairs) while the name is
// bracketed and may itself contain spaces, '@' or '"' — anchoring from the end
// is the only way to stay correct on a control labelled `Report "urgent"`.
func parseSnapshotLines(text string) []pageNode {
	var out []pageNode
	for _, line := range strings.Split(text, "\n") {
		n, ok := parseSnapshotLine(line)
		if ok {
			out = append(out, n)
		}
	}
	return out
}

func parseSnapshotLine(line string) (pageNode, bool) {
	rest := line
	depth := 0
	for strings.HasPrefix(rest, "  ") {
		depth++
		rest = rest[2:]
	}
	if rest == "" {
		return pageNode{}, false
	}

	n := pageNode{depth: depth}

	// role, plus an optional (level) — a heading's level is structural, so it
	// is kept; it does not churn like wording does.
	i := 0
	for i < len(rest) && (rest[i] == '_' || rest[i] == '-' ||
		(rest[i] >= 'a' && rest[i] <= 'z') || (rest[i] >= '0' && rest[i] <= '9')) {
		i++
	}
	n.role = rest[:i]
	if n.role == "" {
		return pageNode{}, false
	}
	rest = rest[i:]

	if strings.HasPrefix(rest, "(") {
		j := strings.IndexByte(rest, ')')
		if j < 0 {
			return pageNode{}, false
		}
		if lvl, err := strconv.Atoi(rest[1:j]); err == nil {
			n.level = lvl
		}
		rest = rest[j+1:]
	}

	// Strip trailing '@ref'.
	if idx := strings.LastIndexByte(rest, ' '); idx >= 0 && strings.HasPrefix(rest[idx+1:], "@") {
		n.ref = rest[idx+2:]
		rest = rest[:idx]
	}

	// Strip trailing key="value" pairs, right to left.
	for {
		trimmed := strings.TrimRight(rest, " ")
		sp := strings.LastIndexByte(trimmed, ' ')
		if sp < 0 {
			break
		}
		tail := trimmed[sp+1:]
		eq := strings.IndexByte(tail, '=')
		if eq <= 0 {
			break
		}
		key := tail[:eq]
		val := tail[eq+1:]
		if !validAttrKey(key) || len(val) < 2 || val[0] != '"' || val[len(val)-1] != '"' {
			break
		}
		if n.attrs == nil {
			n.attrs = make(map[string]string, 2)
		}
		n.attrs[key] = attrValue(key, val[1:len(val)-1])
		rest = trimmed[:sp]
	}

	// Whatever is left, if bracketed, is the accessible name.
	if len(rest) >= 2 && strings.HasPrefix(rest, " [") && strings.HasSuffix(rest, "]") {
		n.name = truncate(normalizeSpace(rest[2:len(rest)-1]), maxNameLen)
	}

	if !structuralRole(n.role) {
		return pageNode{}, false
	}
	if n.role == "generic" && n.name == "" && len(n.attrs) == 0 {
		return pageNode{}, false
	}
	return n, true
}

func validAttrKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Small maps; an insertion sort keeps the hash input deterministic without
	// pulling in sort for four lines.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// Resolve finds the ref of the first node in the digest matching pred. This is
// the whole of ADR-0019's staleness check: a card entry *is* a predicate plus
// an expected match, so "has this card gone stale" and "can I still find the
// thing it names" are the same question asked once.
func (d *PageDigest) Resolve(pred Predicate) (string, bool) {
	if d == nil {
		return "", false
	}
	for _, n := range d.Nodes {
		if pred.matches(n) {
			return n.Ref, true
		}
	}
	return "", false
}

// Predicate identifies a node by role, name and optionally one attribute. It is
// deliberately weaker than a selector and stronger than nothing: a CSS
// selector is a guess about markup that ADR-0003 already showed to be wrong
// often, while (role, name) is a statement about what the control *is*.
type Predicate struct {
	Role    string `json:"role,omitempty"`
	Name    string `json:"name,omitempty"`
	AttrKey string `json:"ak,omitempty"`
	AttrVal string `json:"av,omitempty"`
}

func (p Predicate) matches(n NodeSig) bool {
	if p.Role != "" && p.Role != n.Role {
		return false
	}
	if p.Name != "" {
		// Prefix match: a snapshot truncates a long name at 40 chars, so an
		// exact comparison would fail on exactly the long labels that are
		// worth remembering.
		if !strings.HasPrefix(n.Name, p.Name) {
			return false
		}
	}
	if p.AttrKey != "" {
		v, ok := n.Attrs[p.AttrKey]
		if !ok || (p.AttrVal != "" && v != p.AttrVal) {
			return false
		}
	}
	return p.Role != "" || p.Name != ""
}

func (p Predicate) String() string {
	var b strings.Builder
	b.WriteString(p.Role)
	if p.Name != "" {
		b.WriteString(" [")
		b.WriteString(p.Name)
		b.WriteString("]")
	}
	if p.AttrKey != "" {
		b.WriteString(" ")
		b.WriteString(p.AttrKey)
		if p.AttrVal != "" {
			b.WriteString("=")
			b.WriteString(p.AttrVal)
		}
	}
	return b.String()
}
