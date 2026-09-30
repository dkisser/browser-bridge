package bench

import (
	"strings"
	"testing"
)

// The fixture is the experiment, so its properties are asserted rather than
// described. Every one of these tests exists because the benchmark was wrong at
// some point and the wrongness was invisible in the numbers.

func TestSnapshotHidesSubjects(t *testing.T) {
	s := NewFixture()
	interactive, _, dropped := s.Snapshot()
	full, _, _ := s.SnapshotFull()

	for _, subject := range s.Subjects {
		if strings.Contains(interactive, subject) {
			t.Errorf("subject %q is visible in the interactive snapshot; the whole difficulty of this fixture is that it is not", subject)
		}
		if !strings.Contains(full, subject) {
			t.Errorf("subject %q is missing from the full snapshot, so it is not on the page at all and the test above proves nothing", subject)
		}
	}
	if dropped == 0 {
		t.Error("the interactive filter dropped nothing, so the two renderings are the same and the test above proves nothing")
	}
}

func TestControlsAreUnnamed(t *testing.T) {
	// A named button can be found by reading the tree, which is the shortcut
	// the fixture exists to close off. This is the ADR-0002 shape: the labels
	// are in the container's text and nowhere else.
	for _, v := range []*Site{NewFixture(), v2site()} {
		snap, _, _ := v.Snapshot()
		for _, n := range parseNodes(snap) {
			if n.role == "button" && n.name != "" {
				t.Errorf("v%d: button %s carries the name %q; it should be an icon button with a tooltip",
					v.Version, n.ref, n.name)
			}
		}
	}
}

func TestEverySnapshotRefResolves(t *testing.T) {
	// The refs a snapshot hands out and the refs a call accepts have to be the
	// same refs. They were not, for one run of this harness, because the page
	// was renumbered in the renderer and Resolve read an unnumbered tree: every
	// call came back "element not found", every task failed in both arms, and
	// the report said the card made no difference.
	for _, s := range []*Site{NewFixture(), v2site()} {
		snap, _, _ := s.Snapshot()
		for _, n := range parseNodes(snap) {
			if ok, _ := s.Resolve("@" + n.ref); !ok {
				t.Errorf("v%d: ref %s (%s [%s]) is in the snapshot but does not resolve", s.Version, n.ref, n.role, n.name)
			}
		}
	}
}

func TestRenderingIsStable(t *testing.T) {
	s := NewFixture()
	first, _, _ := s.Snapshot()
	for i := 0; i < 5; i++ {
		again, _, _ := s.Snapshot()
		if again != first {
			t.Fatalf("snapshot %d differs from the first; a benchmark over a page that renumbers itself measures noise", i)
		}
	}
}

func TestGuessSelectorsNeverResolve(t *testing.T) {
	s := NewFixture()
	for _, sel := range []string{
		`[data-message-subject="Standup notes"]`,
		"div.inbox",
		"[role=list]",
		".gmail-list",
	} {
		if ok, _ := s.Resolve(sel); ok {
			t.Errorf("selector %q resolved; guessed selectors are supposed to be useless here, and if one works the failure tier is not being measured", sel)
		}
	}
}

func TestRedeployBreaksEveryContainerPredicate(t *testing.T) {
	// This is the property the self-update half rests on. If a redesign left
	// the card's predicates resolving, the stale-card phase would measure
	// nothing and "the card goes stale" would be an untested claim.
	v1, v2 := NewFixture(), v2site()
	for _, name := range []string{v1.ListName, v1.folderName(), v1.toolbarName()} {
		if resolveOn(t, v2, "list", name) {
			t.Errorf("v2 still resolves list [%s]; the redesign is supposed to rename every container", name)
		}
		if resolveOn(t, v2, "toolbar", name) {
			t.Errorf("v2 still resolves toolbar [%s]; the redesign is supposed to rename every container", name)
		}
	}
	// The navigation keeps its name, and that is deliberate rather than an
	// oversight: a card that is *partly* valid is the harder case, and the
	// entry that survives must not be the one holding the mail.
	if !resolveOn(t, v2, "navigation", "Mailbox") {
		t.Error("v2 dropped the navigation's name too; a partially-stale card is the case worth testing")
	}
	if _, got := v2.Resolve("@" + firstRefOf(t, v2, "navigation", "Mailbox")); strings.Contains(got, "Quarterly report") {
		t.Error("the surviving navigation entry now returns the mail, so the stale card would still be right by accident")
	}
}

func TestRowClickIsObservable(t *testing.T) {
	s := NewFixture()
	snap, _, _ := s.Snapshot()
	ref := s.RowRef(2)
	if ref == "" {
		t.Fatal("no ref for the third row")
	}
	if ok, _ := s.Resolve(ref); !ok {
		t.Fatalf("row ref %s does not resolve", ref)
	}
	_ = snap
	if !s.Unread[2] {
		t.Fatal("row 2 starts read, so the mark-read task has nothing to do")
	}
	if !s.Click(ref) {
		t.Fatal("clicking the row was rejected")
	}
	if s.Unread[2] {
		t.Error("clicking the row did not mark it read, so the task cannot be observed")
	}
	if s.Opened() != 2 {
		t.Errorf("opened row %d, want 2", s.Opened())
	}
}

func v2site() *Site {
	s := NewFixture()
	s.Redeploy()
	return s
}

func resolveOn(t *testing.T, s *Site, role, name string) bool {
	t.Helper()
	snap, _, _ := s.Snapshot()
	for _, n := range parseNodes(snap) {
		if n.role == role && n.name == name {
			_, got := s.Resolve("@" + n.ref)
			return got != ""
		}
	}
	return false
}

func firstRefOf(t *testing.T, s *Site, role, name string) string {
	t.Helper()
	snap, _, _ := s.Snapshot()
	for _, n := range parseNodes(snap) {
		if n.role == role && n.name == name {
			return n.ref
		}
	}
	t.Fatalf("no %s [%s] on the page", role, name)
	return ""
}
