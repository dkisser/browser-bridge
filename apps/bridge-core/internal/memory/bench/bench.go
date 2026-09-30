// Package bench measures whether the self-learning store actually changes what
// an agent does on a site it has already used.
//
// It exists because "the card is injected" and "the card helps" are different
// claims, and only the second one is worth anything. The harness is
// deliberately deterministic: the same run produces the same numbers, so a
// change in the trend is a change in the mechanism rather than in the weather.
//
// What this measures, precisely: whether the recall path hands back information
// a *disciplined* consumer can act on. The scripted agent here already follows
// the protocol ADR-0003 sets out — snapshot before reading, address elements by
// the refs that snapshot hands out, guess a selector only when the tree cannot
// answer the question. So the credit the card can earn is not the protocol
// discipline, which the agent has either way; it is the site-specific part,
// namely which of a page's several addressable containers is the one worth
// reading, and which guesses have already been shown to fail here. Without a
// card that knowledge has to be rediscovered on every attempt; with one it is
// handed over, ranked by how much content each container actually returned.
//
// It does NOT measure an LLM's behaviour, and the numbers here must not be
// quoted as if they did. An LLM-driven run against a real site is a different
// harness — it has to supply a driver, a live browser and a real account — and
// its result is the one that answers "does this help an agent". This one
// answers three narrower questions, and all three are worth having:
//
//  1. Does the capture → learn → recall plumbing work end to end on a page
//     built to be ambiguous?
//  2. By how much is there to win, and on which tasks? The Snooze row is the
//     control case and is expected to show nothing; the site map helps locate
//     content, and locating a named control is something the Pseudo-tree
//     already answers.
//  3. What happens when the site is redesigned underneath a card that was
//     learned from the old one. That is the risk, and it is the one a v1-only
//     run cannot see.
package bench

import (
	"fmt"
	"sort"
	"strings"
)

// Task is one thing the agent is asked to do. The name is stable so results
// can be compared across runs.
type Task struct {
	Name string
	// Run performs the task against the site using the given session, and
	// returns whether it succeeded. A session is one attempt with its own
	// fresh budget, so a task that fails does not poison the next repetition.
	Run func(s *Session) bool
}

// Call records one thing the agent did, and whether the browser accepted it.
type Call struct {
	Op     string
	Target string
	OK     bool
}

// Session is one attempt at a task: the scripted agent, the site it is driving,
// and the counters. It is wired to the real memory Manager, so capture, learning
// and recall all run exactly as they do in the daemon — only the browser is
// replaced.
type Session struct {
	Site *Site
	// Note returns the card the store would hand back for this call, or "".
	Note func(command, host string, tabID int) string
	// Record reports an attempted call so the runner can put it in the trace.
	// Without it the trace would contain only the protocol's opening moves and
	// the learner would never see the work that earns a card.
	Record func(command, target string, ok bool, text string)
	// TabID is the tab this attempt drives. It is per-attempt because the two
	// arms must not share one: a tab's state is keyed by id, so sharing it
	// means the card arm's landing reads the no-card arm's page as the digest it
	// is checked against. Zero means 1, which is what the single-attempt tests
	// want and is never a collision because they build one session at a time.
	TabID int

	calls    []Call
	failures int
	taken    int
	// pending is the snapshot text the protocol already paid for, kept so the
	// scripted agent reads the same rendering the store parsed.
	pending string
	// landingNote and snapshotNote are the two cards the protocol's opening
	// moves were given, in order. The agent reads whichever it needs.
	landingNote  string
	snapshotNote string
	// cardSeen records whether a card was offered at all during this attempt,
	// which is what makes "the agent had the information" a checkable fact
	// rather than an assumption.
	cardSeen bool
	// cardUsed records whether a container the card named is the one that
	// answered the question. It is kept apart from cardSeen because a card can
	// be offered and still be useless, and a benchmark that conflated the two
	// would let a regression hide.
	cardUsed bool
	// noteRef is the ref the successful container read was addressed by, kept so
	// a test can assert the card handed out a ref from *this* page.
	noteRef string
	// read is the set of refs already spent on this attempt. The card's
	// shortlist and the page's own containers overlap, and reading the same
	// container twice would charge the agent for the store's advice.
	read map[string]bool
}

// Budget is how many calls an attempt may make before it is declared hopeless.
// A real agent does not have a hard limit, but the benchmark needs one or a
// hopeless strategy runs forever.
//
// It is enforced, in solve and findContainer, and it is also the penalty a failed
// attempt is charged in Result.Score — so the two uses cannot disagree about what
// "hopeless" means. It used to be only the penalty, with a `taken` counter that
// nothing read: a documented cap that does not exist is worse than none, because
// the numbers still look bounded.
const Budget = 12

// spend records an attempt. text is what the call came back with, and it is
// not optional bookkeeping: it is the only thing the site map has to rank
// containers by, so a harness that recorded calls without their results would
// leave the store's ranking signal permanently zero — which is exactly what
// happened when this took a hardcoded "".
// exhausted reports whether this attempt has spent its budget. Every loop that
// can run more than once checks it, so a strategy that would otherwise keep
// trying stops the way a real agent's caller eventually stops it.
func (s *Session) exhausted() bool { return s.taken >= Budget }

func (s *Session) spend(op, target string, ok bool, text string) {
	s.calls = append(s.calls, Call{Op: op, Target: target, OK: ok})
	if s.Record != nil {
		s.Record(op, target, ok, text)
	}
	if !ok {
		s.failures++
	}
	s.taken++
}

// Result is one attempt's numbers.
type Result struct {
	Task   string `json:"task"`
	Memory bool   `json:"memory"`
	// Phase names the stretch of repetitions this attempt belonged to, and
	// SiteVersion the site it ran against. Together they are what makes a
	// redesign measurable: the interesting curve is not "better over time" but
	// "good, then gone, then good again", and that shape only exists if each
	// attempt remembers which world it was in.
	Phase    string `json:"phase,omitempty"`
	Version  int    `json:"siteVersion"`
	Success  bool   `json:"success"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
	CardSeen bool   `json:"cardSeen"`
	CardUsed bool   `json:"cardUsed"`
}

// Score is the number the trend is drawn from: what the attempt cost.
//
// It is the call count, with a failed attempt charged the budget on top, and
// that is a deliberate change from scoring only the calls the browser rejected.
// A rejected call is obviously wasted, but a read of the *wrong* container is
// not rejected — it succeeds, returns the wrong text, and leaves the agent
// exactly where it started. Scoring only rejections made that free, both arms
// came out at zero, and the benchmark reported no difference on a page built
// specifically to have one. Calls are the unit an agent operator feels, and
// every wasted call is a call.
//
// The failure penalty is there so that giving up early is never the cheap
// option: an attempt that spends two calls and fails must not look better than
// one that spends two and succeeds.
func (r Result) Score() int {
	if r.Success {
		return r.Calls
	}
	return r.Calls + Budget
}

// Report is the aggregate over repetitions.
type Report struct {
	Task    string  `json:"task"`
	Memory  bool    `json:"memory"`
	Runs    int     `json:"runs"`
	Success int     `json:"success"`
	Mean    float64 `json:"meanCost"`
	Median  float64 `json:"medianCost"`
	Min     int     `json:"minCost"`
	Max     int     `json:"maxCost"`
	// p90 is reported because the mean alone is not a trend: a distribution
	// with one bad tail and a good median looks the same as a uniformly
	// improved one.
	P90 int `json:"p90Cost"`
	// Rejects is the mean number of calls the browser refused. It is reported
	// next to the cost rather than folded into it because it is the part the
	// failure tier is responsible for, and a cost improvement with no
	// movement here says the ranking did the work and the failure list did
	// not.
	Rejects float64 `json:"meanRejects"`
	// CardOffered is how many of the runs were given a card at all. A
	// regression that silently stops injecting shows up here as a drop, which
	// is a different failure from "the card stopped helping".
	CardOffered int `json:"cardOffered"`
	CardUsed    int `json:"cardUsed"`
	// Phase is carried through from the results so a trend table can be built
	// without re-joining against the raw run.
	Phase string `json:"phase,omitempty"`
}

func summarize(task string, memory bool, results []Result) Report {
	rep := Report{Task: task, Memory: memory, Runs: len(results), Phase: results[0].Phase}
	scores := make([]int, 0, len(results))
	rejects := 0
	for _, r := range results {
		if r.Success {
			rep.Success++
		}
		rejects += r.Failures
		rep.CardOffered += boolToInt(r.CardSeen)
		rep.CardUsed += boolToInt(r.CardUsed)
		scores = append(scores, r.Score())
	}
	if len(scores) == 0 {
		return rep
	}
	sort.Ints(scores)
	sum := 0
	for _, s := range scores {
		sum += s
	}
	rep.Mean = float64(sum) / float64(len(scores))
	rep.Median = float64(percentile(scores, 50))
	rep.Min = scores[0]
	rep.Max = scores[len(scores)-1]
	rep.P90 = percentile(scores, 90)
	rep.Rejects = float64(rejects) / float64(len(scores))
	return rep
}

// percentile uses the nearest-rank method: with 20 samples, p90 is the 18th
// sorted value. Interpolation would imply a precision the sample size does not
// have.
func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FormatTable renders the reports as something you can read at a glance, with
// the per-task delta between the two arms. The delta is the actual finding; the
// two columns on their own are just bookkeeping.
func FormatTable(reports []Report) string {
	byTask := map[string]map[bool]Report{}
	order := []string{}
	for _, r := range reports {
		if byTask[r.Task] == nil {
			byTask[r.Task] = map[bool]Report{}
			order = append(order, r.Task)
		}
		byTask[r.Task][r.Memory] = r
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-34s %-4s %4s %4s %6s %7s %4s %6s %9s\n",
		"TASK", "MEM", "RUNS", "OK", "COST", "MEDIAN", "P90", "REJECT", "CARD USED")
	for _, task := range order {
		for _, on := range []bool{false, true} {
			r, ok := byTask[task][on]
			if !ok {
				continue
			}
			label := "off"
			if on {
				label = "on"
			}
			fmt.Fprintf(&b, "%-34s %-4s %4d %4d %6.2f %7.0f %4d %6.2f %6d/%-3d\n",
				task, label, r.Runs, r.Success, r.Mean, r.Median, r.P90, r.Rejects,
				r.CardUsed, r.CardOffered)
		}
		off, hasOff := byTask[task][false]
		on, hasOn := byTask[task][true]
		if hasOff && hasOn {
			d := off.Mean - on.Mean
			verdict := "no change"
			switch {
			case d > 0.5:
				verdict = fmt.Sprintf("card saves %.2f calls/run", d)
			case d < -0.5:
				verdict = fmt.Sprintf("card COSTS %.2f calls/run", -d)
			}
			fmt.Fprintf(&b, "%-34s   -> %s\n\n", "", verdict)
		}
	}
	return b.String()
}

// FormatTrend renders one row per task per phase, which is the shape that
// answers "does this survive the site changing".
//
// FormatTable is right for a single stable site: two arms, one number each. It
// cannot show the thing a phased run exists to show, because averaging across a
// phase where the card was correct, a phase where it was stale, and a phase
// where it had been rebuilt produces one number that describes none of them.
func FormatTrend(reports []Report) string {
	type key struct{ task, phase string }
	pairs := map[key][2]Report{}
	var order []key
	for _, r := range reports {
		k := key{r.Task, r.Phase}
		if _, ok := pairs[k]; !ok {
			pairs[k] = [2]Report{}
			order = append(order, k)
		}
		slot := pairs[k]
		if r.Memory {
			slot[1] = r
		} else {
			slot[0] = r
		}
		pairs[k] = slot
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-34s %-26s %7s %7s %8s %7s\n",
		"TASK", "PHASE", "OFF", "ON", "SAVED", "ON OK")
	for _, k := range order {
		off, on := pairs[k][0], pairs[k][1]
		saved := off.Mean - on.Mean
		okRatio := "-"
		if on.Runs > 0 {
			okRatio = fmt.Sprintf("%d/%d", on.Success, on.Runs)
		}
		fmt.Fprintf(&b, "%-34s %-26s %7.2f %7.2f %8.2f %7s\n",
			k.task, k.phase, off.Mean, on.Mean, saved, okRatio)
	}
	return b.String()
}
