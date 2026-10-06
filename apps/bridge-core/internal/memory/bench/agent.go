package bench

import (
	"strconv"
	"strings"
)

// The agent here is a script, not a model, and the distinction decides what the
// numbers mean. It is written to be *reasonable*, not to be good: it follows the
// protocol ADR-0003 sets out and is given no site-specific knowledge at all.
//
//  1. Snapshot first. It never reads a selector it has not seen in a snapshot.
//  2. Address elements by the refs a snapshot hands out.
//  3. When the tree cannot answer the question — and on this page it cannot,
//     because the subjects are not in the tree — it does the one thing a real
//     agent does, which is reach for a CSS selector naming what it wants. That
//     guess is the ADR-0002 incident, and it is the single most expensive thing
//     in the whole fixture.
//
// The two arms differ in one thing only: whether the agent reads the note the
// store appended to its results. Everything else — the candidates it considers,
// the order it tries them in, the fallback it reaches for — is the same code.
//
// That is the point of the design, and it is why the numbers can be believed.
// An earlier version of this harness scored only the calls the browser
// *rejected*, and a read of the wrong container — which succeeds, and returns
// the wrong text, and teaches the agent nothing — was free. Both arms came out
// at zero. The score is now the number of calls the attempt cost, because that
// is the thing an agent operator actually feels.

// Tasks are the three shapes the ADR-0002/0003 Gmail incidents took: read a
// specific message, change a specific message's state, and read something only
// the message body holds.
//
// The third is the one that exercises the failure tier. "Open the message
// titled X" is a per-row action, and this page's per-row controls are unnamed
// and its subjects are not in the tree, so an agent that wants to address the
// message directly has nothing to address it with and reaches for a selector.
// The card's failure list is the one part of it that is about what *not* to do.
func Tasks() []Task {
	return []Task{
		{
			Name: "read the subject of a named message",
			Run: func(s *Session) bool {
				return s.solve(goal{Kind: wantRead, Subject: "Quarterly report"})
			},
		},
		{
			Name: "mark a named message as read",
			Run: func(s *Session) bool {
				return s.solve(goal{Kind: wantMarkRead, Subject: "Welcome aboard"})
			},
		},
		{
			Name: "open a message and read its body",
			Run: func(s *Session) bool {
				return s.solve(goal{Kind: wantOpen, Subject: "Standup notes"})
			},
		},
		{
			// The control case, and it is in the table on purpose. Finding a
			// named control is something the Pseudo-tree already answers — the
			// toolbar is right there, with a role that says what it is — so the
			// site map has nothing to add and this row is expected to read
			// "no change". It is the bound on the claim: the card helps locate
			// *content*, not controls, and a benchmark that only ran tasks the
			// card wins would have hidden that.
			Name: "find the Snooze control",
			Run: func(s *Session) bool {
				return s.solve(goal{Kind: wantControl, Label: "Snooze"})
			},
		},
	}
}

type goalKind int

const (
	wantRead goalKind = iota
	wantMarkRead
	wantOpen
	wantControl
)

type goal struct {
	Kind    goalKind
	Subject string
	Label   string
}

// solve is the whole agent. Everything the card can change happens in
// candidatesFor; everything after it is the same in both arms.
func (s *Session) solve(g goal) bool {
	snap := s.pendingSnapshot()
	if snap == "" {
		snap, _, _ = s.Site.Snapshot()
	}

	// The guess comes first when the goal is a per-row action, because that is
	// the order a real agent tries: address the thing directly, and fall back
	// to walking the page only when the page refuses. The card's failure list
	// is the only thing that changes this.
	if g.Kind == wantOpen {
		if !s.cardSaysFailed(s.guessSelector(g.Subject)) {
			sel := s.guessSelector(g.Subject)
			ok, out := s.Site.Resolve(sel)
			s.spend("gettext", sel, ok, out)
		}
	}

	// Find the container that answers the question.
	text, ok := s.findContainer(g, snap)
	if !ok {
		return false
	}
	if !strings.Contains(text, g.needle()) {
		return false
	}
	switch g.Kind {
	case wantRead:
		return true
	case wantControl:
		// Locating the control *is* the task. There is no row to act on and
		// nothing left to read.
		return true
	case wantMarkRead, wantOpen:
		idx := s.Site.RowIndexOf(g.Subject)
		if idx < 0 {
			return false
		}
		// The row is found by counting: the subjects are not in the tree, so
		// the index the text gave is matched against the Nth row. That is
		// arithmetic a disciplined agent can do, not knowledge it lacks.
		rowRef := s.rowRef(idx, snap)
		if rowRef == "" {
			return false
		}
		if ok := s.Site.Click(rowRef); !ok {
			s.spend("click", rowRef, false, "")
			return false
		}
		s.spend("click", rowRef, true, "")
		if g.Kind == wantMarkRead {
			return !s.Site.Unread[idx]
		}
		// Opening is not the task; reading what the click revealed is.
		_, body := s.Site.Resolve(rowRef)
		s.spend("gettext", rowRef, true, body)
		return s.Site.Opened() == idx && strings.Contains(body, g.Subject)
	}
	return false
}

func (g goal) needle() string {
	if g.Kind == wantControl {
		return g.Label
	}
	return g.Subject
}

// guessSelector is the fallback a disciplined agent reaches for when the tree
// does not name the thing it wants: a selector describing it. It is
// deterministic, so the store groups the repeats and the card can say "this
// exact guess has failed here N times".
func (s *Session) guessSelector(subject string) string {
	return `[data-message-subject="` + subject + `"]`
}

// findContainer is the one place the arms differ. With a card it reads the
// shortlist the card hands over, in the order the card ranked it; without one it
// walks the page's containers in the order the snapshot lists them.
func (s *Session) findContainer(g goal, snap string) (text string, ok bool) {
	if s.read == nil {
		s.read = map[string]bool{}
	}
	for _, c := range s.candidatesFor(g, snap) {
		if s.exhausted() {
			return "", false
		}
		if s.read[c.ref] {
			continue // already paid for on this attempt
		}
		s.read[c.ref] = true
		found, out := s.Site.Resolve(c.ref)
		if !found {
			s.spend("gettext", c.ref, false, out)
			continue
		}
		s.spend("gettext", c.ref, true, out)
		if strings.Contains(out, g.needle()) {
			s.noteRef = c.ref
			if c.fromCard {
				s.cardUsed = true
			}
			return out, true
		}
	}
	return "", false
}

// candidate is one container worth reading.
type candidate struct {
	ref      string
	role     string
	name     string
	fromCard bool
}

// candidatesFor returns the containers to try, best guess first.
//
// The card's shortlist is *prepended to* the page's own containers, never
// substituted for them, and that is not a detail. Substituting them is what an
// earlier version of this harness did, and it fails in exactly the situation
// this feature exists to survive. The moment a site is redesigned, every
// container the card names stops resolving; the card then comes back holding
// the one entry whose name happened to survive the redesign; and an agent that
// treated the card as the whole list read that entry, did not find what it was
// looking for, and gave up — spending fewer calls than before and *failing*,
// which is strictly worse than having no card at all.
//
// So a card is advice about where to look first, and the page is still there to
// look at afterwards. That is also the only reading consistent with how the card
// is delivered: a labelled note appended to a result the agent was going to get
// anyway. It is a hint, not a sandbox.
func (s *Session) candidatesFor(g goal, snap string) []candidate {
	fromCard := s.cardCandidates(g)
	tree := s.treeCandidates(snap)
	if len(fromCard) == 0 {
		return tree
	}
	seen := make(map[string]bool, len(fromCard))
	out := make([]candidate, 0, len(fromCard)+len(tree))
	for _, c := range fromCard {
		seen[c.ref] = true
		out = append(out, c)
	}
	for _, c := range tree {
		if !seen[c.ref] {
			out = append(out, c)
		}
	}
	return out
}

// treeCandidates is discipline on its own: every addressable container in the
// rendered page, in the order it appears.
func (s *Session) treeCandidates(snap string) []candidate {
	var out []candidate
	for _, n := range parseNodes(snap) {
		switch n.role {
		case "list", "toolbar", "region", "complementary", "navigation", "contentinfo", "generic":
			out = append(out, candidate{ref: "@" + n.ref, role: n.role, name: n.name})
		}
	}
	return out
}

// cardCandidates reads the injected site map.
//
// The card is parsed, not pattern-matched against the goal: the agent cannot
// know which subject it is looking for in a card that only ever describes
// *containers*, so the only honest thing to do with it is to take it as a
// ranked shortlist and read down it. An entry whose label happens to contain the
// goal is preferred, because a card entry that names the answer is worth more
// than one that merely ranks well — but on this page none does, which is the
// honest shape of the problem.
func (s *Session) cardCandidates(g goal) []candidate {
	var out []candidate
	for _, e := range s.cardEntries() {
		if e.purpose != "text container" {
			continue
		}
		out = append(out, candidate{ref: e.ref, role: e.role, name: e.name, fromCard: true})
	}
	if len(out) == 0 {
		return nil
	}
	// Prefer an entry that names the goal, keeping the card's ranking as the
	// tie-break. Stable, so the ranking the store did is the ranking used.
	needle := g.needle()
	preferred := make([]bool, len(out))
	for i, c := range out {
		if strings.Contains(c.name, needle) {
			preferred[i] = true
		}
	}
	ordered := make([]candidate, 0, len(out))
	for i, c := range out {
		if preferred[i] {
			ordered = append(ordered, c)
		}
	}
	for i, c := range out {
		if !preferred[i] {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// cardEntry is one parsed line of the injected site map.
type cardEntry struct {
	purpose string
	role    string
	name    string
	ref     string
}

// cardEntries parses the site map out of the note. Both injection points are
// read, in the order the protocol produced them, and the first section that has
// a site map in it wins.
func (s *Session) cardEntries() []cardEntry {
	for _, note := range []string{s.snapshotNote, s.landingNote} {
		if entries, ok := parseSiteMap(note); ok {
			return entries
		}
	}
	return nil
}

func parseSiteMap(note string) ([]cardEntry, bool) {
	var out []cardEntry
	inMap := false
	for _, line := range strings.Split(note, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Site map"):
			inMap = true
			continue
		case line == "" || (!strings.HasPrefix(line, "- ") && inMap):
			// Any other section header ends the map.
			if inMap {
				inMap = false
			}
			continue
		}
		if !inMap {
			continue
		}
		body := strings.TrimPrefix(line, "- ")
		// Cut, not Index-and-slice. `strings.Index` returns a *byte* offset and
		// the separator is a three-byte arrow, so slicing at index+1 left two
		// stray bytes on the front of the ref, the first field was not the ref,
		// and every site map line was silently discarded. The card was being
		// injected, arriving intact, and parsed into nothing — which looked
		// exactly like the card not helping.
		left, right, found := strings.Cut(body, "→")
		if !found {
			continue
		}
		purpose := strings.TrimSpace(strings.SplitN(left, "·", 2)[0])
		label := strings.TrimSpace(strings.SplitN(left, "·", 2)[1])
		ref := strings.Fields(strings.TrimSpace(right))
		if len(ref) == 0 || !strings.HasPrefix(ref[0], "@") {
			continue
		}
		role, name := splitLabel(label)
		out = append(out, cardEntry{purpose: purpose, role: role, name: name, ref: ref[0]})
	}
	return out, len(out) > 0
}

func splitLabel(label string) (role, name string) {
	if i := strings.Index(label, " ["); i >= 0 && strings.HasSuffix(label, "]") {
		return label[:i], label[i+2 : len(label)-1]
	}
	return label, ""
}

// cardSaysFailed reports whether the card already tells the agent that a guess
// of this *shape* has failed on this site. This is the failure tier, and it is
// the one part of the card that is about what *not* to do — so the only correct
// use of it is to not do the thing.
//
// It matches on shape, not on the literal string, and that is not a loosening —
// it is the reason the card is useful. The card stores `[data-message-subject=…]`
// with the value stripped, because the value is page content and the card file
// outlives the visit. The agent's next guess at that message will be a different
// value of the same attribute, so a string comparison would miss every one of
// them; the shape is what actually transfers.
//
// The card renders a guess with %q, so the quoted run has to be unquoted rather
// than substring-matched — a selector containing quotes comes back escaped
// (`[data-message-subject=…]` with the ellipsis inside), and a naive search for
// the plain string cannot express it at all.
func (s *Session) cardSaysFailed(selector string) bool {
	if selector == "" {
		return false
	}
	shape := guessShape(selector)
	for _, note := range []string{s.landingNote, s.snapshotNote} {
		for _, line := range strings.Split(note, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "- ") || !strings.Contains(line, " on ") {
				continue
			}
			if got := unquotedSelector(line); got != "" && guessShape(got) == shape {
				return true
			}
		}
	}
	return false
}

// guessShape is the part of a guess that generalises: the command it was aimed
// with is carried on the card line, and the selector reduces to tag, attribute
// and class names with the quoted value dropped.
func guessShape(selector string) string {
	if strings.HasPrefix(selector, "@") {
		return selector
	}
	if !strings.ContainsAny(selector, ".#[]():>+~*=, ") {
		return "…"
	}
	var b strings.Builder
	for i := 0; i < len(selector); {
		switch q := selector[i]; q {
		case '"', '\'':
			i++
			for i < len(selector) {
				if selector[i] == '\\' {
					i += 2
					continue
				}
				if selector[i] == q {
					i++
					break
				}
				i++
			}
			// The placeholder, not a deletion: the card renders the shape with
			// the literal replaced rather than removed, so the reader has to
			// produce the same string to compare against. Two different
			// reductions of the same idea is how a card that works stops
			// matching itself.
			b.WriteString("\u2026")
		default:
			b.WriteByte(q)
			i++
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// unquotedSelector pulls the selector out of a rendered failure line, which is
// `<signature> on <command> with "<selector>" (Nx)`. The quoted run is taken
// from the first quote to the last so a selector containing quotes survives.
func unquotedSelector(line string) string {
	first := strings.Index(line, `"`)
	last := strings.LastIndex(line, `"`)
	if first < 0 || last <= first {
		return ""
	}
	out, err := strconv.Unquote(line[first : last+1])
	if err != nil {
		return ""
	}
	return out
}

// rowRef is the i-th row in the rendered page. Counting is arithmetic, not site
// knowledge, so it is done the same way in both arms.
func (s *Session) rowRef(i int, snap string) string {
	n := 0
	for _, node := range parseNodes(snap) {
		if node.role != "row" {
			continue
		}
		if n == i {
			return "@" + node.ref
		}
		n++
	}
	return ""
}

// node is one line of the rendered Pseudo-tree.
type node struct {
	role string
	name string
	ref  string
}

func parseNodes(snap string) []node {
	var out []node
	for _, line := range strings.Split(snap, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		// A ref is the marker that a line is an addressable node. The
		// `Page:` header and the dropped text runs have none.
		if !strings.Contains(trimmed, "@") {
			continue
		}
		role := trimmed
		if i := strings.IndexAny(trimmed, " ["); i >= 0 {
			role = trimmed[:i]
		}
		name := ""
		if i := strings.Index(trimmed, " ["); i >= 0 {
			if j := strings.LastIndex(trimmed, "]"); j > i {
				name = trimmed[i+2 : j]
			}
		}
		ref := ""
		if i := strings.LastIndex(trimmed, " @"); i >= 0 {
			ref = strings.TrimSpace(trimmed[i+2:])
		}
		if ref == "" {
			continue
		}
		out = append(out, node{role: role, name: name, ref: ref})
	}
	return out
}
