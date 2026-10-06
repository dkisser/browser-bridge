package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"browser-bridge/internal/core"
	"browser-bridge/internal/memory"
)

// RunAll is the A-layer benchmark: a deterministic site, a disciplined agent,
// and the two arms differing only in whether the card is read.
//
// The two arms share one store, because that is the honest arrangement: a card
// learned during the "off" repetitions is still there for the "on" ones. Running
// them against separate stores would measure a store that never accumulates,
// which is not the thing that ships.
//
// Repetitions are interleaved (off, on, off, on) rather than run in two blocks.
// Sequential blocks would attribute any drift in the fixture to the arm, and the
// whole point of averaging is to not be fooled by that.
func RunAll(dir string, reps int) ([]Result, error) {
	return run(dir, reps, siteV1)
}

// Options configures a phased run.
type Options struct {
	// Reps is the total number of repetitions, split evenly across Phases.
	Reps int
	// Phases names each stretch of repetitions. A site constructor is looked up
	// per phase, so a run can change the site underneath a card mid-flight.
	Phases []Phase
}

// Phase is one stretch of repetitions against one version of the site.
type Phase struct {
	Name string
	// Build returns the site for an attempt in this phase.
	Build func() *Site
	// NewStore starts from a clean store. Only the first phase may do that;
	// a later phase that does is measuring nothing, since a card cannot be
	// stale if there is no card.
	NewStore bool
}

func siteV1() *Site { return NewFixture() }

func siteV2() *Site {
	s := NewFixture()
	s.Redeploy()
	return s
}

// Phases returns the default schedule: train on the original site, then
// redesign it underneath the card, then stay redesigned long enough for the
// learner to rebuild from the traffic it is now seeing.
//
// The middle phase is the interesting one. A card written against v1 must stop
// being trusted the moment the containers are renamed, and the agent must fall
// back to looking for itself — a wrong card is worse than no card, so "the
// feature notices" is a result in its own right and not a footnote to the
// recovery in the third phase.
func Phases(train, stale, recover int) []Phase {
	var out []Phase
	if train > 0 {
		out = append(out, Phase{Name: "trained on v1", Build: siteV1, NewStore: true})
	}
	if stale > 0 {
		out = append(out, Phase{Name: "v1 card, site redesigned", Build: siteV2})
	}
	if recover > 0 {
		out = append(out, Phase{Name: "card rebuilt on v2", Build: siteV2})
	}
	return out
}

func run(dir string, reps int, build func() *Site) ([]Result, error) {
	if reps <= 0 {
		return nil, fmt.Errorf("bench: repetitions must be positive")
	}
	return runPhases(dir, reps, []Phase{{Name: "v1", Build: build, NewStore: true}})
}

func runPhases(dir string, total int, phases []Phase) ([]Result, error) {
	if total <= 0 {
		return nil, fmt.Errorf("bench: repetitions must be positive")
	}
	if len(phases) == 0 {
		return nil, fmt.Errorf("bench: no phases")
	}
	per := total / len(phases)
	if per == 0 {
		return nil, fmt.Errorf("bench: %d repetitions is fewer than the %d phases", total, len(phases))
	}
	// Anything left over goes to the first phase rather than being dropped, so
	// the caller gets the repetitions it asked for.
	counts := make([]int, len(phases))
	for i := range counts {
		counts[i] = per
	}
	for i := 0; i < total-per*len(phases); i++ {
		counts[i%len(phases)]++
	}

	m, err := memory.New(memory.Options{DataDir: dir, BrowserID: "bench"})
	if err != nil {
		return nil, err
	}
	defer func() { _ = m.Close() }()

	tasks := Tasks()
	var out []Result
	for pi, phase := range phases {
		for r := 0; r < counts[pi]; r++ {
			for _, on := range []bool{false, true} {
				for _, task := range tasks {
					site := phase.Build()
					res := attempt(m, site, task, on, pi, r)
					res.Version = site.Version
					res.Phase = phase.Name
					out = append(out, res)
				}
			}
			// Let the learner run between repetitions, as it does on idle.
			if err := m.LearnNow(); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// RunPhases is the A-layer benchmark with a site that changes underneath the
// card partway through. See Phases.
func RunPhases(dir string, opts Options) ([]Result, error) {
	return runPhases(dir, opts.Reps, opts.Phases)
}

// attempt is one repetition of one task in one arm.
//
// The envelope prefix carries the arm and the tab id is per-attempt. Both are
// load-bearing rather than cosmetic. Sharing an envelope id across arms made the
// two attempts indistinguishable in the stream, so a procedure's evidence listed
// the same envelope twice and its `Successes` — the counter that is supposed to
// mean "two independent attempts agreed on this" — was incremented by the other
// arm of the *same* attempt. Sharing a tab did the same thing to the digest: the
// card arm's landing was checked against the no-card arm's page, which happens to
// be the same synthetic page and would not be on a real one.
//
// A trace the benchmark produces has to look like a session someone could have
// had, because "it runs the real learner over the real records" is the entire
// claim. Colliding identifiers are a trace no session could produce.
func attempt(m *memory.Manager, site *Site, task Task, on bool, phase, rep int) Result {
	env := fmt.Sprintf("p%d-r%d-a%d-%s", phase, rep, boolToArm(on), task.Name)
	tabID := phase*1_000_000 + rep*1_000 + boolToArm(on)*100
	sess := &Session{
		Site:  site,
		TabID: tabID,
		Note: func(command, h string, tab int) string {
			if on {
				return m.TakeSiteNote(command, h, tab)
			}
			return ""
		},
		Record: func(command, target string, ok bool, text string) {
			// A snapshot's result *is* the page, and the page is the only thing
			// a card can be written from. Recording it as a generic text result
			// would leave the learner with no structure and no card would ever
			// exist.
			if command == "snapshot" {
				snapText, _, nodes := site.Snapshot()
				recordSnapshot(m, env, tabID, site.Host, snapText, nodes)
				return
			}
			recordCall(m, env, tabID, site.Host, command, target, ok, text)
		},
	}

	// The protocol's opening moves. The store is driven through the same Record
	// path as everything else, so the trace is exactly what the router would
	// have written — including the calls the task makes, which are the only
	// ones that can earn a card.
	note := sess.call("navigate")
	sess.landingNote = note
	snap, _, _ := site.Snapshot()
	snapNote := sess.call("snapshot")
	sess.snapshotNote = snapNote
	sess.pending = snap

	return Result{
		Task:     task.Name,
		Memory:   on,
		Success:  task.Run(sess),
		Calls:    len(sess.calls),
		Failures: sess.failures,
		CardSeen: sess.cardSeen,
		CardUsed: sess.cardUsed,
	}
}

// call records an operation and asks the store for whatever card it hands back.
//
// The order is the point. The real path is: the router records the result, and
// only then does the MCP adapter ask for the note to append to the text it is
// about to return. Asking first asks a store that has not seen the call yet —
// so a landing has not armed itself, `TakeSiteNote` finds nothing, and the
// landing's half of the card (the failures, the working sequences) is silently
// empty. The site map still arrives, because it is produced at the snapshot,
// which is armed by the *previous* call. That is a convincing-looking failure:
// most of the card arrives and the tier that never arrives is the one the
// failure-list measurement depends on.
// call records one of the protocol's opening moves and takes whatever card the
// store hands back. Only navigate and snapshot go through here, and neither
// addresses an element, so neither can be rejected — which is why there is no
// target and no success flag. Element-addressing calls are recorded by spend,
// where the outcome is known.
func (s *Session) call(op string) string {
	s.spend(op, "", true, "")
	note := ""
	if s.Note != nil {
		tab := s.TabID
		if tab == 0 {
			tab = 1
		}
		note = s.Note(op, s.Site.Host, tab)
	}
	if note != "" {
		s.cardSeen = true
	}
	return note
}

// pendingSnapshot is the snapshot the protocol already paid for, kept so the
// scripted agent reads the same text the store parsed. Parsing it twice would
// be cheaper to write and would be measuring a different thing: the store only
// ever sees one rendering, and so must the agent.
func (s *Session) pendingSnapshot() string { return s.pending }

func recordSnapshot(m *memory.Manager, prefix string, tabID int, host, snap string, nodes int) {
	env := prefix + "-snap"
	m.RecordCommand(env, "snapshot", host, tabID, nil)
	m.RecordResult(env, "snapshot", host, tabID, core.ResponsePayload{
		Status: "ok",
		Data: mustJSON(map[string]any{
			"snapshot": snap, "truncated": false,
			"nodes_total": nodes, "nodes_emitted": nodes, "tier": 0,
		}),
	})
}

// recordCall writes one element-addressing call to the trace. The command names
// are the *wire* names, not the MCP tool names: the router records what the
// extension said, and an earlier version of this harness recorded "get_text",
// which no extension has ever sent. The store switched on the wire name when it
// decided what a call was for, so every entry came out labelled "get_text
// target", the card never matched anything the agent looked for, and both arms
// scored identically at zero.
func recordCall(m *memory.Manager, prefix string, tabID int, host, command, target string, ok bool, text string) {
	env := fmt.Sprintf("%s-%s", prefix, command)
	params := map[string]any{}
	if target != "" {
		params["selector"] = target
	}
	m.RecordCommand(env, command, host, tabID, params)
	if ok {
		m.RecordResult(env, command, host, tabID, core.ResponsePayload{
			Status: "ok",
			Data:   mustJSON(map[string]any{"text": text}),
		})
		return
	}
	m.RecordResult(env, command, host, tabID, core.ResponsePayload{
		Status: "error", Error: "element_not_found",
		Message: "No element matches " + target + ".",
	})
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// SummarizeByPhase is the same aggregation, split by phase. A run with one
// phase is indistinguishable from Summarize; a run with three is the shape that
// shows a redesign being survived.
func SummarizeByPhase(results []Result) []Report {
	phases := []string{}
	seen := map[string]bool{}
	grouped := map[string]map[string]map[bool][]Result{}
	for _, r := range results {
		if !seen[r.Phase] {
			seen[r.Phase] = true
			phases = append(phases, r.Phase)
		}
		if grouped[r.Task] == nil {
			grouped[r.Task] = map[string]map[bool][]Result{}
		}
		if grouped[r.Task][r.Phase] == nil {
			grouped[r.Task][r.Phase] = map[bool][]Result{}
		}
		grouped[r.Task][r.Phase][r.Memory] = append(grouped[r.Task][r.Phase][r.Memory], r)
	}
	var out []Report
	for _, task := range taskOrder(results) {
		for _, phase := range phases {
			byArm, ok := grouped[task][phase]
			if !ok {
				continue
			}
			for _, on := range []bool{false, true} {
				if rs, ok := byArm[on]; ok {
					out = append(out, summarize(task, on, rs))
				}
			}
		}
	}
	return out
}

func taskOrder(results []Result) []string {
	var order []string
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.Task] {
			seen[r.Task] = true
			order = append(order, r.Task)
		}
	}
	return order
}

// WriteJSONL appends the raw results so a trend can be recomputed later without
// re-running anything. This is the difference between a measurement and a claim.
func WriteJSONL(path string, results []Result) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, r := range results {
		rec := map[string]any{
			"at":          time.Now().UnixMilli(),
			"mode":        "fixture",
			"task":        r.Task,
			"memory":      r.Memory,
			"ok":          r.Success,
			"calls":       r.Calls,
			"failures":    r.Failures,
			"cardSeen":    r.CardSeen,
			"cardUsed":    r.CardUsed,
			"cost":        r.Score(),
			"siteVersion": r.Version,
			"phase":       r.Phase,
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// Summarize groups results by task and arm.
func Summarize(results []Result) []Report {
	grouped := map[string]map[bool][]Result{}
	order := []string{}
	for _, r := range results {
		if grouped[r.Task] == nil {
			grouped[r.Task] = map[bool][]Result{}
			order = append(order, r.Task)
		}
		grouped[r.Task][r.Memory] = append(grouped[r.Task][r.Memory], r)
	}
	out := make([]Report, 0, len(order))
	for _, task := range order {
		for _, on := range []bool{false, true} {
			if rs, ok := grouped[task][on]; ok {
				out = append(out, summarize(task, on, rs))
			}
		}
	}
	return out
}

// boolToArm gives each arm a stable index for the envelope prefix and the tab
// id, so a trace from this harness is one a real session could have produced.
func boolToArm(on bool) int {
	if on {
		return 1
	}
	return 0
}
