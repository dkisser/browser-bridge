package bench

import (
	"fmt"
	"sort"
	"strings"
)

// Site is a stand-in for a real site, built to have the shape that makes this
// problem hard.
//
// The first version of this fixture was built so a disciplined agent could find
// any message by reading its subject straight out of the Pseudo-tree, and it
// measured nothing: both arms scored zero, because no amount of site knowledge
// helps when the tree already says everything. The second version moved the
// subjects into text nodes, which fixed that, and then measured nothing either
// — for the opposite reason. It had exactly one list and one toolbar, so
// "read the first list" was a correct strategy, and a card had nothing to add.
//
// So the page has to be *ambiguous*, not merely opaque. It carries four named
// containers a read call can address, all of which return text successfully,
// and nothing in the Pseudo-tree says which one holds the mail. That is the
// situation the card exists for, and it is the real one: a folder sidebar, a
// toolbar, a message list and a contacts list all accept a read and only one of
// them is the answer.
//
// The other two properties are borrowed from what actually went wrong:
//
//   - Subjects live in `text` nodes, which the interactive filter drops (see
//     packages/shared/src/snapshot.ts, isInteractiveKept) and which the store's
//     parser drops for the ADR-0018 reason. This is the ADR-0002 incident
//     exactly: a virtualized list whose contents are not addressable. No
//     discipline can locate a message by name from the tree, because the name is
//     not in the tree.
//   - The per-row control and every toolbar button are unnamed, as icon
//     buttons are in a real accessibility tree. What they *do* is only in the
//     text of their container.
//
// It is not a mock in the sense of "returns canned answers": every call resolves
// against the same structure the renderer emits, and a guessed CSS selector
// fails the way the extension's does (ADR-0008's fail-closed), so a bad guess
// costs what a real bad guess costs.
type Site struct {
	Title   string
	Host    string
	Version int // bumped by Redeploy; a redesigned site changes Version

	// Subjects are the messages. Order is stable, which is what makes a run
	// repeatable.
	Subjects []string
	Senders  []string
	Bodies   []string
	Unread   map[int]bool

	// ToolbarLabels are the mailbox toolbar's labels, in order. The buttons
	// carrying them are unnamed, so the labels are only reachable by reading
	// the toolbar.
	ToolbarLabels []string
	// Folders and People are the two decoy containers.
	Folders []string
	People  []string
	// ListName is the message list's accessible name. Redeploy changes it,
	// which is what makes a v1 card stop resolving.
	ListName string
	// RowButton is whether each row has its own unnamed action button.
	RowButton bool
	// opened is the message index a click revealed, or -1. It is a field and
	// not a return value because "the click worked" and "it worked on the
	// message I meant" are different questions, and only the second one decides
	// whether the task succeeded.
	opened int
}

// NewFixture builds the v1 site.
func NewFixture() *Site {
	return &Site{
		Title:         "Inbox",
		Host:          "mail.example.com",
		Version:       1,
		Subjects:      []string{"Quarterly report", "Lunch on Thursday", "Re: invoice 4471", "Welcome aboard", "Standup notes"},
		Senders:       []string{"dana@example.com", "sam@example.com", "ops@example.com", "hr@example.com", "lead@example.com"},
		Bodies:        []string{"Numbers attached.", "Corner table?", "Paid, thanks.", "Your account is ready.", "Moved to 10:30."},
		Unread:        map[int]bool{0: true, 2: true, 4: true},
		ToolbarLabels: []string{"Archive", "Delete", "Mark unread", "Snooze", "Refresh"},
		Folders:       []string{"Inbox", "Starred", "Drafts", "Sent"},
		People:        []string{"Ana", "Ben", "Cleo"},
		ListName:      "Inbox",
		RowButton:     true,
		opened:        -1,
	}
}

// Redeploy changes the structure the way a real redesign does: the same
// content, a different shape, and — the part that matters — a different
// accessible name for the container that holds the mail.
//
// It exists so the self-update half of the feature can be measured rather than
// assumed. A card written against v1 must stop being trusted once the site is
// v2, the agent must fall back to looking for itself, and the card must then
// rebuild from the traffic v2 produces. A benchmark that only ever runs v1
// cannot tell whether that works, and the risk of *not* working is the whole
// reason a wrong card is dangerous.
func (s *Site) Redeploy() {
	s.Version = 2
	s.Title = "Inbox (updated layout)"
	// Renamed rather than extended: the predicate matcher treats a stored name
	// as a prefix so a label that merely grew ("Snooze" -> "Snooze until
	// later") still resolves to the right control. A redesign that only
	// lengthens labels therefore keeps working, which is the intended
	// behaviour, and the benchmark has to break the prefix to test the other
	// one.
	s.ListName = "Conversations"
	s.ToolbarLabels = []string{"Archive all", "Delete all", "Mark as unread", "Snooze until later", "Reload"}
	s.Folders = []string{"Primary", "Starred", "Drafts", "Sent"}
	s.RowButton = false
}

// --- the page as the extension would render it --------------------------------

// el is one addressable node. The fixture emits the same text format the real
// snapshot does (packages/shared/src/snapshot.ts, formatLine), so the store's
// parser is exercised for real rather than against a convenient shape.
type el struct {
	role   string
	name   string
	attrs  map[string]string
	ref    string
	kids   []*el
	text   string
	isText bool
}

func (e *el) render(depth int, b *strings.Builder, nodes *int, keepText bool) {
	if e.isText {
		if !keepText {
			// The interactive filter drops text runs. This is the whole reason
			// a subject cannot be found by reading the tree.
			return
		}
		b.WriteString(strings.Repeat("  ", depth))
		fmt.Fprintf(b, "text [%s]\n", e.text)
		*nodes++
		return
	}
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(e.role)
	if e.name != "" {
		name := strings.Join(strings.Fields(e.name), " ")
		if len([]rune(name)) > 40 {
			name = string([]rune(name)[:40]) + "…"
		}
		fmt.Fprintf(b, " [%s]", name)
	}
	for _, k := range sortedAttrKeys(e.attrs) {
		v := e.attrs[k]
		if len(v) > 60 {
			v = v[:60] + "…"
		}
		fmt.Fprintf(b, ` %s="%s"`, k, v)
	}
	if e.ref != "" {
		fmt.Fprintf(b, " @%s", e.ref)
	}
	b.WriteString("\n")
	*nodes++
}

func sortedAttrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// page is the one canonical rendering of this site: built, then renumbered.
// Snapshot and Resolve must both go through it, or the refs they hand out and
// the refs they accept will not be the same refs — which is exactly the bug a
// card's predicate resolution would then trip over.
//
// Every container carries a ref, not just the leaves. A real snapshot
// addresses the containers too, and they are the nodes worth reading, so a
// fixture that left them unaddressable measured nothing: the agent had no
// candidate to read and every task failed in both arms.
// page is the one canonical rendering of this site: built, then renumbered, and
// the renumbering happens *here* so that every caller gets the same refs.
// Snapshot and Resolve must agree on numbering, or the refs a snapshot hands out
// and the refs a call will accept are different refs — which is exactly the bug
// a card's predicate resolution would then trip over, and exactly what happened
// when the renumbering was left to the renderer.
func (s *Site) page() *el {
	root := s.build()
	n := 0
	renumber(root, &n)
	return root
}

func (s *Site) build() *el {
	// No ref on the root, and that is the renderer being faithful rather
	// than the fixture being convenient: isInteractiveKept keeps roles the
	// agent can act on, and a bare structural container is not one. Giving it a
	// ref made the whole page addressable, and then reading it returned every
	// subject at once and every task was solved by the one move the site map
	// exists to talk the agent out of.
	root := &el{role: "generic", name: s.Title}
	if s.Version == 1 {
		root.kids = append(root.kids, s.navEl(), s.toolbarEl(), s.listEl(), s.peopleEl(), s.footerEl())
		return root
	}
	// v2: the message list is wrapped in a region and renamed, and the folder
	// list moves inside the navigation. Every container predicate from v1 that
	// named a list or a toolbar is now either unresolvable or, for the ones
	// that still match by role, pointing at a node that no longer means what it
	// meant.
	nav := s.navEl()
	region := &el{role: "region", name: "Conversation", ref: "region"}
	region.kids = append(region.kids, s.listEl())
	root.kids = append(root.kids, nav, s.toolbarEl(), region, s.peopleEl(), s.footerEl())
	return root
}

func (s *Site) navEl() *el {
	nav := &el{role: "navigation", name: "Mailbox", ref: "nav"}
	list := &el{role: "list", name: s.folderName(), ref: "folders"}
	for i, f := range s.Folders {
		list.kids = append(list.kids, &el{role: "listitem", name: f, ref: fmt.Sprintf("f%d", i)})
	}
	nav.kids = append(nav.kids, list)
	return nav
}

func (s *Site) folderName() string {
	if s.Version == 1 {
		return "Folders"
	}
	return "Labels"
}

func (s *Site) toolbarEl() *el {
	bar := &el{role: "toolbar", name: s.toolbarName(), ref: "bar"}
	for i := range s.ToolbarLabels {
		// Unnamed: an icon button with a tooltip, which is what the tree can
		// actually see. The label is only in the toolbar's text.
		bar.kids = append(bar.kids, &el{role: "button", ref: fmt.Sprintf("t%d", i)})
	}
	return bar
}

func (s *Site) toolbarName() string {
	if s.Version == 1 {
		return "Mailbox actions"
	}
	return "Bulk actions"
}

func (s *Site) listEl() *el {
	list := &el{role: "list", name: s.ListName, ref: "list"}
	for i := range s.Subjects {
		row := &el{role: "row", name: s.Senders[i], ref: fmt.Sprintf("m%d", i)}
		// The sender is a label, so it is addressable. The subject is body
		// text, so it is not — which is the whole difficulty of the fixture.
		row.kids = append(row.kids, &el{role: "cell", name: s.Senders[i], ref: fmt.Sprintf("c%d", i)})
		row.kids = append(row.kids, &el{isText: true, text: s.Subjects[i]})
		if s.RowButton {
			row.kids = append(row.kids, &el{role: "button", ref: fmt.Sprintf("b%d", i)})
		}
		list.kids = append(list.kids, row)
	}
	return list
}

func (s *Site) peopleEl() *el {
	side := &el{role: "complementary", name: "People", ref: "side"}
	list := &el{role: "list", name: "Suggestions", ref: "people"}
	for i, p := range s.People {
		list.kids = append(list.kids, &el{role: "listitem", name: p, ref: fmt.Sprintf("p%d", i)})
	}
	side.kids = append(side.kids, list)
	return side
}

func (s *Site) footerEl() *el {
	return &el{role: "contentinfo", name: "Footer", ref: "foot", kids: []*el{
		{role: "link", name: "Privacy", attrs: map[string]string{"href": "/privacy"}, ref: "x1"},
	}}
}

// Snapshot renders the page the way the extension would under the interactive
// filter, including the SnapshotMeta line the store parses the url out of.
// Text runs are dropped, exactly as they are on a real page.
func (s *Site) Snapshot() (text string, nodes, dropped int) {
	return s.render(false)
}

// SnapshotFull is the `filter=full` rendering, where text runs survive. It
// exists so the fixture can be inspected, and so a test can assert that the
// subjects really are in the page and really are absent from the default view.
func (s *Site) SnapshotFull() (text string, nodes, dropped int) {
	return s.render(true)
}

func (s *Site) render(keepText bool) (string, int, int) {
	root := s.page()
	var b strings.Builder
	fmt.Fprintf(&b, "Page: %s | https://%s/\n", s.Title, s.Host)
	kept := 0
	emitNode(root, 0, &b, &kept, keepText)
	return b.String(), kept, totalRefs(root) - kept
}

// totalRefs counts the addressable nodes, which is what a snapshot reports as
// its node total. Text runs are nodes too, so a rendering that drops them has
// emitted fewer than the page holds.
func totalRefs(e *el) int {
	n := 0
	for _, k := range flatten(e) {
		if k.ref != "" || k.isText {
			n++
		}
	}
	return n
}

func emitNode(e *el, depth int, b *strings.Builder, count *int, keepText bool) {
	e.render(depth, b, count, keepText)
	for _, k := range e.kids {
		emitNode(k, depth+1, b, count, keepText)
	}
}

func renumber(e *el, n *int) {
	if e.ref != "" {
		*n++
		e.ref = fmt.Sprintf("e%d", *n)
	}
	for _, k := range e.kids {
		renumber(k, n)
	}
}

func flatten(e *el) []*el {
	out := []*el{e}
	for _, k := range e.kids {
		out = append(out, flatten(k)...)
	}
	return out
}

// Resolve answers an element-addressing call, the way the extension does.
//
// A ref is resolved against the live page. A CSS selector is resolved only if
// it happens to exist, which for this fixture means never: that is what makes
// guessing selectors useless, and it is the reason a card that hands over a ref
// is worth having.
func (s *Site) Resolve(selector string) (found bool, text string) {
	if !strings.HasPrefix(selector, "@") {
		return false, ""
	}
	ref := strings.TrimPrefix(selector, "@")
	for _, e := range flatten(s.page()) {
		if e.ref != ref {
			continue
		}
		switch e.role {
		case "list":
			return true, s.textOf(e)
		case "toolbar":
			return true, strings.Join(s.ToolbarLabels, " | ")
		case "row":
			// An unopened row exposes its sender. An opened one exposes its
			// body, which is what makes "open a message and read it" a
			// different task from "find its subject".
			//
			// A real page puts the body in a new panel and needs another
			// snapshot to address it. Modelling that would add a snapshot to
			// every open, which measures the cost of the re-snapshot rather
			// than of the recall, and it would be the same cost in both arms.
			// The simplification is confined to this one answer and is noted
			// here so nobody reads the fixture as claiming otherwise.
			if i := s.rowIndexOfSender(e.name); i == s.opened {
				return true, s.Subjects[i] + " — " + s.Bodies[i]
			}
			return true, e.name
		case "cell":
			return true, e.name
		case "listitem":
			return true, e.name
		case "button":
			return true, ""
		case "link":
			return true, e.name
		case "region", "complementary", "navigation", "contentinfo", "generic":
			// A structural node is addressable and reading it returns the text
			// of everything under it, which is how a real read behaves.
			//
			// The root is the one that matters. Reading it is the move every
			// agent reaches for when the tree does not name what it wants, and
			// on a real inbox it returns hundreds of kilobytes of quoted
			// history — which is ADR-0003's 420K get_html, and which the store
			// records as a soft failure rather than as progress. The fixture
			// models it that way rather than making the shortcut work, because
			// letting it work would be modelling a site that does not exist.
			return true, s.textUnder(e)
		}
	}
	return false, ""
}

// textOf returns what reading a list gives back. The message list returns its
// subjects — the one container on this page whose text is the answer — and every
// other list returns its own items. The lengths differ, and that difference is
// what the site map ranks on.
func (s *Site) textOf(e *el) string {
	switch {
	case e.name == s.ListName:
		return strings.Join(s.Subjects, " | ")
	case e.name == s.folderName():
		return strings.Join(s.Folders, " | ")
	case e.name == "Suggestions":
		return strings.Join(s.People, " | ")
	default:
		return e.name
	}
}

// textUnder is what reading a structural container returns: everything under
// it. It is how a region or a sidebar answers a read, and it is why those
// containers are legitimate candidates rather than dead nodes.
func (s *Site) textUnder(e *el) string {
	var parts []string
	for _, n := range flatten(e) {
		if n == e {
			continue
		}
		if n.isText {
			parts = append(parts, n.text)
			continue
		}
		if n.name != "" && n.role != "cell" && n.role != "row" {
			parts = append(parts, n.name)
		}
	}
	if len(parts) == 0 {
		return e.name
	}
	return strings.Join(parts, " | ")
}

// RowIndexOf finds a message by its subject text, the way an agent reads a list
// and matches on what it says. Returns -1 when absent.
func (s *Site) RowIndexOf(subject string) int {
	for i, x := range s.Subjects {
		if x == subject {
			return i
		}
	}
	return -1
}

func (s *Site) rowIndexOfSender(sender string) int {
	for i, x := range s.Senders {
		if x == sender {
			return i
		}
	}
	return -1
}

// MarkRead flips a row's state, so the task has an observable effect rather than
// being a read-only query.
func (s *Site) MarkRead(i int) bool {
	if i < 0 || i >= len(s.Subjects) {
		return false
	}
	s.Unread[i] = false
	return true
}

// RowRef is the ref of the i-th message row, which is what an agent derives by
// counting rows in the Pseudo-tree once it knows which index it wants.
func (s *Site) RowRef(i int) string {
	if i < 0 || i >= len(s.Subjects) {
		return ""
	}
	for _, e := range flatten(s.page()) {
		if e.role == "row" && e.name == s.Senders[i] {
			return "@" + e.ref
		}
	}
	return ""
}

// Opened is the index of the message a click opened, or -1.
func (s *Site) Opened() int { return s.opened }

// Click is the only call that changes state, and it is what the "mark read" and
// "open" tasks are ultimately about. A row opens and marks itself read; a
// per-row icon button does the same, which is why an agent that cannot read the
// button's name has to fall back on the row.
func (s *Site) Click(selector string) bool {
	ref := strings.TrimPrefix(selector, "@")
	for _, e := range flatten(s.page()) {
		if e.ref != ref {
			continue
		}
		switch e.role {
		case "row":
			if e.name == "" {
				return false
			}
			for i, sender := range s.Senders {
				if sender == e.name {
					s.MarkRead(i)
					s.opened = i
					return true
				}
			}
			return false
		case "button":
			// A per-row button opens that row. A toolbar button is a mailbox
			// action with no row, and does nothing observable. The row is found
			// structurally — by walking up to the node that owns this button —
			// rather than by parsing refs back into indices, which is the kind of
			// arithmetic that silently rots the moment the numbering changes.
			if i := s.rowOwning(e); i >= 0 {
				s.MarkRead(i)
				s.opened = i
			}
			return true
		default:
			return true
		}
	}
	return false
}

// rowOwning reports which message row a node belongs to, or -1. It answers by
// walking the rendered tree, so it stays correct across renumbering and across
// the v2 relayout — an earlier version recovered the row by parsing "@e12" back
// into an index, which is the kind of arithmetic that rots silently the moment
// the numbering changes.
func (s *Site) rowOwning(target *el) int {
	return s.findRowOwner(s.page(), target)
}

func (s *Site) findRowOwner(e, target *el) int {
	if e.role == "row" {
		for _, k := range e.kids {
			if k == target {
				return s.rowIndexOfSender(e.name)
			}
		}
	}
	for _, k := range e.kids {
		if i := s.findRowOwner(k, target); i >= 0 {
			return i
		}
	}
	return -1
}

// BodyOf is the text a click on a message reveals — the reason "open a message"
// is a different task from "read its subject".
func (s *Site) BodyOf(i int) string {
	if i < 0 || i >= len(s.Bodies) {
		return ""
	}
	return s.Bodies[i]
}
