package bench

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"browser-bridge/internal/memory"
)

func TestRunAllIsDeterministic(t *testing.T) {
	// The whole reason for a fixture rather than a live site. If two runs
	// differ, a change in the trend is a change in the weather and the numbers
	// mean nothing.
	a, err := RunAll(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunAll(t.TempDir(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("run lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("result %d differs:\n off/on run: %+v\n second:    %+v", i, a[i], b[i])
		}
	}
}

func TestCardSavesCalls(t *testing.T) {
	results, err := RunAll(t.TempDir(), 6)
	if err != nil {
		t.Fatal(err)
	}
	reports := Summarize(results)
	if len(reports) != len(Tasks())*2 {
		t.Fatalf("got %d reports for %d tasks; every task needs both arms", len(reports), len(Tasks()))
	}

	byTask := map[string]map[bool]Report{}
	for _, r := range reports {
		if byTask[r.Task] == nil {
			byTask[r.Task] = map[bool]Report{}
		}
		byTask[r.Task][r.Memory] = r
	}
	for _, task := range Tasks() {
		off, on := byTask[task.Name][false], byTask[task.Name][true]
		if byTask[task.Name][false].Task == "" || byTask[task.Name][true].Task == "" {
			t.Fatalf("task %q is missing an arm from the report", task.Name)
		}
		if off.Success != off.Runs {
			t.Errorf("%s: the no-card arm failed %d/%d; the benchmark is not measuring recall, it is measuring a task the agent cannot do",
				task.Name, off.Runs-off.Success, off.Runs)
		}
		if on.Success != on.Runs {
			t.Errorf("%s: the card arm failed %d/%d; a card must never make the agent worse off",
				task.Name, on.Runs-on.Success, on.Runs)
		}
		if on.Mean >= off.Mean {
			t.Errorf("%s: card costs calls (off %.2f, on %.2f)", task.Name, off.Mean, on.Mean)
		}
		if on.CardUsed == 0 {
			t.Errorf("%s: the card was never used although it was offered %d times", task.Name, on.CardOffered)
		}
		// Only the task that reaches for a selector guess exercises the failure
		// tier. Asserting it everywhere would be asserting that a task which
		// never guesses avoids guessing.
		if task.Name == "open a message and read its body" && on.Rejects >= off.Rejects {
			t.Errorf("the failure tier did nothing (off %.2f rejects/run, on %.2f)", off.Rejects, on.Rejects)
		}
	}

	// The no-card costs are a property of the fixture, not of the recall, and
	// they are asserted exactly: a change here means the fixture changed, and
	// every number in the trend moved with it.
	want := map[string]float64{
		"read the subject of a named message": 6,
		"mark a named message as read":        7,
		"open a message and read its body":    9,
		"find the Snooze control":             5,
	}
	for task, cost := range want {
		if got := byTask[task][false].Mean; got != cost {
			t.Errorf("%s: no-card cost is %.2f, want %.2f — the fixture changed", task, got, cost)
		}
	}
}

func TestControlTaskShowsLessThanTheContentTasks(t *testing.T) {
	// Finding a named control is something the Pseudo-tree already answers, so
	// the site map has little to add there. This is the bound on the claim, and
	// it is asserted so that a future change which makes the card look good at
	// everything has to explain itself.
	results, err := RunAll(t.TempDir(), 6)
	if err != nil {
		t.Fatal(err)
	}
	saved := map[string]float64{}
	for _, r := range Summarize(results) {
		if r.Memory {
			continue
		}
		saved[r.Task] = r.Mean
	}
	for _, r := range Summarize(results) {
		if !r.Memory {
			continue
		}
		got := saved[r.Task] - r.Mean
		if r.Task == "find the Snooze control" {
			if got >= 1.5 {
				t.Errorf("the control task saved %.2f calls; a control is findable from the tree, so a large saving here means the fixture stopped being honest about that", got)
			}
			continue
		}
		if got < 2 {
			t.Errorf("%s: card saved only %.2f calls/run; below two the ranking is not doing its job", r.Task, got)
		}
	}
}

func TestCardHandsOutLiveRefsOnly(t *testing.T) {
	// The core anti-staleness contract: whatever ref the card names must belong
	// to the page in hand, never to the snapshot it was learned from. Run over
	// both site versions so a ref surviving a redesign by coincidence is caught.
	for _, build := range []func() *Site{NewFixture, v2site} {
		for rep := 0; rep < 4; rep++ {
			site := build()
			snap, _, _ := site.Snapshot()
			m, err := memory.New(memory.Options{DataDir: t.TempDir(), BrowserID: "bench"})
			if err != nil {
				t.Fatal(err)
			}
			sess := &Session{
				Site: site,
				Note: func(c, h string, tab int) string { return m.TakeSiteNote(c, h, tab) },
				Record: func(c, tgt string, ok bool, text string) {
					if c == "snapshot" {
						st, _, n := site.Snapshot()
						recordSnapshot(m, "e", 1, site.Host, st, n)
						return
					}
					recordCall(m, "e", 1, site.Host, c, tgt, ok, text)
				},
			}
			sess.landingNote = sess.call("navigate")
			sess.snapshotNote = sess.call("snapshot")
			sess.pending = snap
			sess.solve(goal{Kind: wantRead, Subject: "Quarterly report"})
			if err := m.LearnNow(); err != nil {
				t.Fatal(err)
			}

			if !sess.cardUsed {
				continue
			}
			if sess.noteRef == "" {
				t.Fatal("the card was credited but no ref was recorded")
			}
			// Every ref the card handed over must be one this page can answer,
			// not one the card remembered.
			live := map[string]bool{}
			for _, n := range parseNodes(snap) {
				live["@"+n.ref] = true
			}
			if !live[sess.noteRef] {
				t.Errorf("v%d rep %d: the card pointed at %s, which is not on this page", site.Version, rep, sess.noteRef)
			}
			if ok, _ := site.Resolve(sess.noteRef); !ok {
				t.Errorf("v%d rep %d: the card pointed at %s and the page refused to resolve it", site.Version, rep, sess.noteRef)
			}
			_ = m.Close()
		}
	}
}

func TestRedeployIsSurvivedNotAbsorbed(t *testing.T) {
	// The self-update claim, in the only form that can fail: after a redesign
	// the card's advantage dips, and after the learner has seen the new site it
	// comes back higher than it was. A run where the dip is absent means the
	// redesign was not actually breaking the predicates and this test was
	// measuring nothing.
	results, err := RunPhases(t.TempDir(), Options{Reps: 7, Phases: Phases(3, 1, 3)})
	if err != nil {
		t.Fatal(err)
	}
	phases := Phases(3, 1, 3)
	if len(phases) != 3 {
		t.Fatalf("expected three phases, got %d", len(phases))
	}

	// Recompute cleanly: mean cost per phase per arm, from the raw results.
	// Mean cost per phase per arm, straight from the raw results. The off arm
	// is the same fixture either way, so the comparison is like for like.
	sums := map[string]map[string]float64{}
	counts := map[string]map[string]int{}
	fails := map[string]map[string]int{}
	for _, r := range results {
		if sums[r.Phase] == nil {
			sums[r.Phase] = map[string]float64{}
			counts[r.Phase] = map[string]int{}
			fails[r.Phase] = map[string]int{}
		}
		arm := "off"
		if r.Memory {
			arm = "on"
		}
		sums[r.Phase][arm] += float64(r.Calls)
		counts[r.Phase][arm]++
		if r.Memory && !r.Success {
			fails[r.Phase]["on"]++
		}
	}

	stale, rebuilt := phases[1].Name, phases[2].Name
	for _, phase := range []string{stale, rebuilt} {
		if fails[phase]["on"] > 0 {
			t.Errorf("phase %q: the card arm failed %d times; a stale card must cost calls, not correctness",
				phase, fails[phase]["on"])
		}
	}
	mean := func(phase, arm string) float64 {
		if counts[phase][arm] == 0 {
			return 0
		}
		return sums[phase][arm] / float64(counts[phase][arm])
	}
	staleSaved := mean(stale, "off") - mean(stale, "on")
	rebuiltSaved := mean(rebuilt, "off") - mean(rebuilt, "on")
	if staleSaved < 0 {
		t.Errorf("phase %q: the card cost %.2f calls/run; it must never be worse than no card", stale, -staleSaved)
	}
	if rebuiltSaved <= staleSaved {
		t.Errorf("the card did not recover from the redesign: %.2f calls saved while stale, %.2f after rebuilding",
			staleSaved, rebuiltSaved)
	}
}

func TestScoreChargesFailure(t *testing.T) {
	// Giving up early must never be the cheap option.
	if got := (Result{Success: true, Calls: 2}).Score(); got != 2 {
		t.Errorf("a successful 2-call attempt scores %d", got)
	}
	if got := (Result{Success: false, Calls: 2}).Score(); got != 2+Budget {
		t.Errorf("a failed 2-call attempt scores %d, want %d", got, 2+Budget)
	}
	if (Result{Success: false, Calls: 5}).Score() <= (Result{Success: true, Calls: 5}).Score() {
		t.Error("at equal cost a failed attempt scored no worse than a successful one")
	}
	// Without the penalty, an agent that gives up on the first rejected call
	// would beat one that finishes, and the benchmark would reward surrender.
	if (Result{Success: false, Calls: 1}).Score() <= (Result{Success: true, Calls: 1}).Score() {
		t.Error("quitting after one call is not penalised relative to finishing after one call")
	}
}

func TestPercentileIsNearestRank(t *testing.T) {
	cases := []struct {
		p    int
		want int
	}{{50, 10}, {90, 18}, {100, 20}, {0, 1}}
	sorted := make([]int, 20)
	for i := range sorted {
		sorted[i] = i + 1
	}
	for _, c := range cases {
		if got := percentile(sorted, c.p); got != c.want {
			t.Errorf("p%d of 1..20 is %d, want %d", c.p, got, c.want)
		}
	}
	if got := percentile(nil, 90); got != 0 {
		t.Errorf("percentile of nothing is %d", got)
	}
}

func TestCardParsingHandlesRenderedFormat(t *testing.T) {
	// Parsed against a card the store really produced, not a hand-written
	// sample. The bug this locks down was a byte offset used as a rune offset
	// across a three-byte separator, which dropped every site map line while
	// leaving the card arriving intact — indistinguishable, in the report, from
	// the card not helping.
	m, err := memory.New(memory.Options{DataDir: t.TempDir(), BrowserID: "bench"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	host := NewFixture().Host
	site := NewFixture()
	sess := &Session{
		Site:   site,
		Note:   func(c, h string, tab int) string { return m.TakeSiteNote(c, h, tab) },
		Record: recorderFor(m, site, host),
	}
	for rep := 0; rep < 4; rep++ {
		sess.landingNote = sess.call("navigate")
		sess.snapshotNote = sess.call("snapshot")
		sess.solve(goal{Kind: wantRead, Subject: "Quarterly report"})
		if err := m.LearnNow(); err != nil {
			t.Fatal(err)
		}
	}
	if sess.snapshotNote == "" {
		t.Fatal("no card was injected at the snapshot")
	}
	if !strings.Contains(sess.snapshotNote, "Site map") {
		t.Fatalf("the snapshot note carries no site map:\n%s", sess.snapshotNote)
	}
	entries, ok := parseSiteMap(sess.snapshotNote)
	if !ok {
		t.Fatalf("the site map did not parse:\n%s", sess.snapshotNote)
	}
	if len(entries) == 0 {
		t.Fatal("the site map parsed to nothing")
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.ref, "@") {
			t.Errorf("entry %+v has no usable ref", e)
		}
		if e.purpose == "" || e.role == "" {
			t.Errorf("entry %+v is missing its purpose or role, so an agent cannot tell what it is looking at", e)
		}
		if strings.ContainsAny(e.ref, " \t\"\\") {
			t.Errorf("ref %q carries quoting debris, which is the rune/byte slicing bug", e.ref)
		}
	}
	// The container that actually holds the mail has to be the one ranked
	// first, or the whole mechanism is not doing anything.
	if entries[0].name != NewFixture().ListName {
		t.Errorf("the top-ranked container is [%s], want [%s]; the ranking is not working",
			entries[0].name, NewFixture().ListName)
	}
}

func TestFailureListSuppressesTheGuess(t *testing.T) {
	// One session per repetition, as the runner does, so each attempt's
	// counters are its own. Reusing one session would make "no further rejected
	// calls" indistinguishable from "the counter stopped moving".
	m, err := memory.New(memory.Options{DataDir: t.TempDir(), BrowserID: "bench"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	host := NewFixture().Host
	g := goal{Kind: wantOpen, Subject: "Standup notes"}
	guess := ` [data-message-subject="Standup notes"]`

	for rep := 0; rep < 4; rep++ {
		site := NewFixture()
		sess := &Session{
			Site:   site,
			Note:   func(c, h string, tab int) string { return m.TakeSiteNote(c, h, tab) },
			Record: recorderFor(m, site, host),
		}
		sess.landingNote = sess.call("navigate")
		sess.snapshotNote = sess.call("snapshot")
		sess.pending = mustSnapshot(site)
		sess.solve(g)
		if err := m.LearnNow(); err != nil {
			t.Fatal(err)
		}
		if rep == 0 {
			// With nothing learned yet the guess is all the agent has, and being
			// rejected is the whole point: it is the only thing that creates the
			// failure entry every later repetition is saved by.
			if sess.failures == 0 {
				t.Fatal("the first attempt did not make the guess, so there is nothing for the card to learn")
			}
			if sess.cardSaysFailed(strings.TrimSpace(guess)) {
				t.Fatal("the card claims a failure that has not happened yet")
			}
			continue
		}
		if !sess.cardSaysFailed(strings.TrimSpace(guess)) {
			t.Fatalf("rep %d: the card does not report the guess as failed, so the failure tier is not reaching the agent:\n%s",
				rep, sess.landingNote)
		}
		if sess.failures != 0 {
			t.Errorf("rep %d: the agent made %d rejected calls despite the card naming that exact guess",
				rep, sess.failures)
		}
	}
}

func TestUnquotedSelector(t *testing.T) {
	// The card renders a guess with %q, so a selector with quotes in it comes
	// back escaped. Matching the escaped form would couple the reader of a card
	// to the exact formatting of the one that wrote it.
	line := `  - element_not_found on gettext with "[data-message-subject=\"Standup notes\"]" (4x)`
	want := `[data-message-subject="Standup notes"]`
	if got := unquotedSelector(line); got != want {
		t.Errorf("unquoted %q, want %q", got, want)
	}
	plain := `  - element_not_found on gettext with "div.inbox" (2x)`
	if got := unquotedSelector(plain); got != "div.inbox" {
		t.Errorf("unquoted %q, want div.inbox", got)
	}
	if got := unquotedSelector("  - no selector here (2x)"); got != "" {
		t.Errorf("expected no selector, got %q", got)
	}
}

func TestWriteJSONLAppendsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "trend.jsonl")
	results, err := RunAll(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if werr := WriteJSONL(path, results); werr != nil {
		t.Fatal(werr)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A second run appends rather than truncating: a trend that overwrites
	// itself is not a trend.
	if werr := WriteJSONL(path, results); werr != nil {
		t.Fatal(werr)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size()*2 {
		t.Errorf("second write produced %d bytes, want %d", after.Size(), before.Size()*2)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("line %d is not JSON: %v", lines, err)
		}
		if _, ok := rec["cost"]; !ok {
			t.Errorf("line %d has no cost field", lines)
		}
		if _, ok := rec["task"]; !ok {
			t.Errorf("line %d has no task field", lines)
		}
		lines++
	}
	if lines != len(results)*2 {
		t.Errorf("read %d lines, want %d", lines, len(results)*2)
	}
}

func TestRunRejectsNonsense(t *testing.T) {
	if _, err := RunAll(t.TempDir(), 0); err == nil {
		t.Error("zero repetitions was accepted")
	}
	if _, err := RunPhases(t.TempDir(), Options{Reps: 2, Phases: Phases(3, 3, 3)}); err == nil {
		t.Error("fewer repetitions than phases was accepted; the phases would be silently uneven")
	}
	if _, err := RunPhases(t.TempDir(), Options{Reps: 9}); err == nil {
		t.Error("a run with no phases was accepted")
	}
}

func TestFormatTableMentionsEveryTask(t *testing.T) {
	results, err := RunAll(t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	table := FormatTable(Summarize(results))
	for _, task := range Tasks() {
		if !strings.Contains(table, task.Name) {
			t.Errorf("the report omits %q", task.Name)
		}
	}
	trend := FormatTrend(SummarizeByPhase(results))
	if !strings.Contains(trend, "SAVED") {
		t.Errorf("the trend table has no savings column:\n%s", trend)
	}
}

func recorderFor(m *memory.Manager, site *Site, host string) func(string, string, bool, string) {
	return func(c, tgt string, ok bool, text string) {
		if c == "snapshot" {
			st, _, n := site.Snapshot()
			recordSnapshot(m, "e", 1, host, st, n)
			return
		}
		recordCall(m, "e", 1, host, c, tgt, ok, text)
	}
}

func mustSnapshot(s *Site) string {
	txt, _, _ := s.Snapshot()
	return txt
}
