package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"browser-bridge/internal/memory"
	"browser-bridge/internal/memory/bench"
)

// `bridge memory bench` exists because "the card is injected" and "the card
// helps" are different claims, and only the second one is worth anything.
//
// It has two halves, and they are deliberately different in kind.
//
// The fixture half is a deterministic synthetic site driven through the real
// Manager — the same capture, the same learner, the same renderer, with only
// the browser replaced. It answers whether the plumbing works and roughly how
// much there is to win, and it answers it identically every time, so a change in
// the trend is a change in the mechanism. What it cannot answer is whether this
// helps *your* agent, because its agent is a script.
//
// The live half is that other half. You run the task in a real agent against a
// real site, then report one line back. Everything expensive — how many calls
// it took, how many the browser refused, whether a card was even on offer — is
// read out of the trace the daemon already wrote, not taken on trust. The only
// thing you supply is the part the trace cannot know: whether the task
// succeeded, and whether the agent actually used the card it was given.
//
// That split is the whole design. Asking a human to count calls is how a
// baseline ends up being a guess, and a guessed baseline cannot show a
// regression.

// benchLogName is where results accumulate. It is a plain JSONL file under the
// data dir, so a trend is recomputable without re-running anything, and so a
// run can be read with nothing but `tail`.
const benchLogName = "bench.jsonl"

func newMemoryBenchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Measure whether learned site cards actually change what an agent does",
		Long: "Measure whether the learned site card changes what an agent does on a site it\n" +
			"has already used.\n\n" +
			"Two halves, and they answer different questions:\n\n" +
			"  (default)          a deterministic fixture site driven through the real\n" +
			"                     learner. Same numbers every run, so a change in the trend\n" +
			"                     is a change in the mechanism. Measures the plumbing, not\n" +
			"                     an LLM.\n" +
			"  record / report    you run a task in a real agent and report one line back;\n" +
			"                     the call and failure counts are read out of the trace the\n" +
			"                     daemon already wrote. This is the half that measures a real\n" +
			"                     agent.\n\n" +
			"Results accumulate in " + benchLogName + " under the data dir, append-only.",
	}
	cmd.AddCommand(
		newBenchRunCommand(),
		newBenchRecordCommand(),
		newBenchReportCommand(),
	)
	return cmd
}

func benchLogPath() (string, error) {
	env, err := EnvFromOSEnv()
	if err != nil {
		return "", err
	}
	return filepath.Join(env.DataDir(), benchLogName), nil
}

// --- fixture half --------------------------------------------------------------

func newBenchRunCommand() *cobra.Command {
	var (
		train   int
		stale   int
		rebuild int
		out     string
		keep    bool
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the deterministic fixture benchmark (no browser, no LLM, no network)",
		Long: "Run the deterministic fixture benchmark.\n\n" +
			"A synthetic mail page is built with the difficulty the real one had — the\n" +
			"message subjects are not in the Pseudo-tree, the per-row controls are\n" +
			"unnamed, and four different containers all accept a read — and it is driven\n" +
			"through the real Manager, so capture, learning and recall all run exactly as\n" +
			"they do in the daemon. Only the browser is replaced.\n\n" +
			"By default the site is redesigned underneath the card partway through, which\n" +
			"is the only way to see whether a stale card costs calls or costs\n" +
			"correctness. Pass --stale 0 --rebuild 0 for the single-phase run.\n\n" +
			"The benchmark's own store is a throwaway directory. It never touches the real\n" +
			"cards, so this cannot corrupt what the daemon has learned.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if train+stale+rebuild == 0 {
				return fmt.Errorf("bench: nothing to run — set --train, --stale or --rebuild")
			}
			dir, err := os.MkdirTemp("", "bridge-bench-")
			if err != nil {
				return err
			}
			if !keep {
				defer func() { _ = os.RemoveAll(dir) }()
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Store kept at %s\n", dir)
			}

			reps := train + stale + rebuild
			phases := bench.Phases(train, stale, rebuild)
			results, err := bench.RunPhases(dir, bench.Options{Reps: reps, Phases: phases})
			if err != nil {
				return err
			}
			outw := cmd.OutOrStdout()
			fmt.Fprintf(outw, "%d repetitions x %d tasks x 2 arms, across %d phase(s)\n\n",
				reps, len(bench.Tasks()), len(phases))
			fmt.Fprint(outw, bench.FormatTable(bench.Summarize(results)))
			if len(phases) > 1 {
				fmt.Fprint(outw, "\nBy phase — the shape that matters is whether the saving dips when\nthe site is redesigned and comes back after the learner has seen the new one:\n\n")
				fmt.Fprint(outw, bench.FormatTrend(bench.SummarizeByPhase(results)))
			}

			path := out
			if path == "" {
				path, err = benchLogPath()
				if err != nil {
					return err
				}
			}
			if err := bench.WriteJSONL(path, results); err != nil {
				return err
			}
			fmt.Fprintf(outw, "\nAppended %d results to %s\n", len(results), path)
			return nil
		},
	}
	cmd.Flags().IntVar(&train, "train", 3, "repetitions against the original site, to build a card")
	cmd.Flags().IntVar(&stale, "stale", 1, "repetitions with the site redesigned underneath the card")
	cmd.Flags().IntVar(&rebuild, "rebuild", 3, "further repetitions on the new site, to rebuild the card")
	cmd.Flags().StringVar(&out, "out", "", "append results here instead of the data dir (default "+benchLogName+")")
	cmd.Flags().BoolVar(&keep, "keep-store", false, "keep the throwaway store so its cards can be read with `bridge memory show`")
	return cmd
}

// --- live half ----------------------------------------------------------------

func newBenchRecordCommand() *cobra.Command {
	var (
		host     string
		task     string
		ok       bool
		card     string
		since    time.Duration
		calls    int
		failures int
		note     string
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Report one real task back, after running it in your agent",
		Long: "Report one real task back, after running it in an agent against a live site.\n\n" +
			"You supply only what the trace cannot know — whether the task succeeded and\n" +
			"whether the agent used the card it was shown. The call and failure counts are\n" +
			"read out of the recorded trace for the last --since window, so a baseline is\n" +
			"not a number you counted by hand.\n\n" +
			"Pass --calls or --failures as well and the two are cross-checked: a mismatch\n" +
			"means the window covered the wrong stretch of session, which is the most\n" +
			"common way a hand-rolled baseline quietly becomes wrong.\n\n" +
			"Example:\n" +
			"  bridge memory bench record --host news.google.com \\\n" +
			"    --task 'find todays top story' --ok --card used",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if task == "" {
				return fmt.Errorf("bench: --task is required; the name has to be stable or the trend cannot group it")
			}
			if card != "" && card != "used" && card != "ignored" && card != "none" {
				return fmt.Errorf("bench: --card must be one of used, ignored, none")
			}
			if since <= 0 {
				return fmt.Errorf("bench: --since must be positive (e.g. --since 10m)")
			}
			env, err := EnvFromOSEnv()
			if err != nil {
				return err
			}
			m, err := memory.New(memory.Options{DataDir: env.DataDir()})
			if err != nil {
				return err
			}
			defer func() { _ = m.Close() }()

			window, err := traceWindow(m, host, since)
			if err != nil {
				return err
			}
			outw := cmd.OutOrStdout()
			fmt.Fprintf(outw, "Trace over the last %s: %d call(s), %d rejected.\n",
				since, window.Calls, window.Failures)
			if card != "none" {
				fmt.Fprintf(outw, "Card was %s; the trace shows it was handed over %d time(s), %d entries in total.\n",
					card, window.WithCard, window.CardLines)
			}
			rec := map[string]any{
				"at":          time.Now().UnixMilli(),
				"mode":        "live",
				"task":        task,
				"host":        host,
				"ok":          ok,
				"memory":      card == "used",
				"cardSeen":    window.WithCard > 0,
				"cardsShown":  window.WithCard,
				"cardLines":   window.CardLines,
				"cardUsed":    card == "used",
				"calls":       window.Calls,
				"failures":    window.Failures,
				"cost":        window.Calls,
				"window":      since.String(),
				"callsReport": calls,
			}
			if note != "" {
				rec["note"] = note
			}
			var mismatches []string
			if calls > 0 && calls != window.Calls {
				mismatches = append(mismatches, fmt.Sprintf("you reported %d calls, the trace shows %d", calls, window.Calls))
			}
			if failures > 0 && failures != window.Failures {
				mismatches = append(mismatches, fmt.Sprintf("you reported %d failures, the trace shows %d", failures, window.Failures))
			}
			if card == "used" && window.WithCard == 0 {
				mismatches = append(mismatches, "you said the card was used but no card was injected in this window")
			}
			if card == "none" && window.WithCard > 0 {
				mismatches = append(mismatches, "you said no card was offered but the trace window contains injected cards")
			}
			rec["crossCheck"] = len(mismatches) == 0
			if len(mismatches) > 0 {
				rec["mismatches"] = mismatches
			}

			path, err := benchLogPath()
			if err != nil {
				return err
			}
			if err := appendJSONL(path, rec); err != nil {
				return err
			}
			for _, msg := range mismatches {
				fmt.Fprintf(outw, "  ! %s\n", msg)
			}
			if len(mismatches) > 0 {
				fmt.Fprintf(outw, "\nRecorded, with the disagreement noted. A trend built on this window\nwould be measuring the wrong stretch of session — re-record it with a --since\nthat covers exactly the task.\n")
			} else {
				fmt.Fprintf(outw, "\nRecorded to %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "restrict the trace window to this host (default: all)")
	cmd.Flags().StringVar(&task, "task", "", "stable name for the task, so results group into a trend")
	cmd.Flags().BoolVar(&ok, "ok", false, "the task succeeded; omit for a failure")
	cmd.Flags().StringVar(&card, "card", "", "what the agent did with the card: used, ignored, or none")
	cmd.Flags().DurationVar(&since, "since", 10*time.Minute, "how far back to read the trace for")
	cmd.Flags().IntVar(&calls, "calls", 0, "cross-check: the call count you counted yourself")
	cmd.Flags().IntVar(&failures, "failures", 0, "cross-check: the failure count you counted yourself")
	cmd.Flags().StringVar(&note, "note", "", "free text kept with the record")
	return cmd
}

// traceCounts is what the recorded trace says happened in a window.
type traceCounts struct {
	Calls    int
	Failures int
	// WithCard is how many times a card was actually handed to an agent, and
	// CardLines how many entries those cards carried between them.
	WithCard  int
	CardLines int
	Commands  int
	ByCommand map[string]int
}

// traceWindow reads the whole stream and counts the calls in the window. The
// stream is read from the start rather than from the learner's cursor: the
// cursor is the *learner's* position and using it here would mean counting
// nothing on a machine where the learner has already caught up.
//
// CardShown is counted from the stream rather than asked of the agent, and that
// is the one number an agent genuinely cannot report about itself. An agent that
// did not notice the card, or that ignored it, will happily say the card was
// never there — and a baseline built on that says recall is broken when the
// recall was fine and the agent was not.
func traceWindow(m *memory.Manager, host string, since time.Duration) (traceCounts, error) {
	out := traceCounts{ByCommand: map[string]int{}}
	cutoff := time.Now().Add(-since).UnixMilli()
	from := int64(0)
	for {
		recs, next, _, err := m.Stream().ReadFrom(from)
		if err != nil {
			return out, err
		}
		for _, r := range recs {
			if r.AtMs < cutoff {
				continue
			}
			if host != "" && r.Host != host {
				continue
			}
			switch r.Kind {
			case memory.KindCardShown:
				out.WithCard++
				out.CardLines += r.Entries
			case memory.KindResponse:
				out.Commands++
				if r.Outcome == memory.OutcomeOK {
					out.Calls++
					out.ByCommand[r.Command]++
					continue
				}
				out.Failures++
			}
		}
		if next <= from || len(recs) == 0 {
			break
		}
		from = next
	}
	return out, nil
}

func newBenchReportCommand() *cobra.Command {
	var (
		mode string
		min  int
	)
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Aggregate what has been recorded so far",
		Long: "Aggregate the recorded results into a trend.\n\n" +
			"Groups by mode, task and arm, and reports the mean call cost, the success\n" +
			"rate, and how often a card was offered at all. The last of those matters\n" +
			"most: a cost improvement alongside a collapse in cards offered is a\n" +
			"different regression from the card simply not helping.\n\n" +
			"Mode is part of the group key and never a column averaged across. A fixture\n" +
			"run and a live run of the same task are two different experiments — a\n" +
			"script on a synthetic page, and an agent on a real site — and one number\n" +
			"for both describes neither.\n\n" +
			"A live record whose window held no calls is left out and counted\n" +
			"separately: the window missed the task, which is what the cross-check is\n" +
			"for, and averaging its zero in would drag a real mean toward a number no\n" +
			"agent ever spent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if mode != "all" && mode != "fixture" && mode != "live" {
				return fmt.Errorf("bench: --mode must be one of all, fixture, live")
			}
			path, err := benchLogPath()
			if err != nil {
				return err
			}
			recs, err := readJSONL(path)
			if err != nil {
				return err
			}
			outw := cmd.OutOrStdout()
			if len(recs) == 0 {
				fmt.Fprintf(outw, "Nothing recorded yet (%s does not exist).\n\nRun `bridge memory bench run` for the fixture baseline, or\n`bridge memory bench record` after a task in a live agent.\n", path)
				return nil
			}

			type group struct {
				task     string
				mode     string
				memory   bool
				runs     int
				ok       int
				calls    int
				withCard int
				bad      int
			}
			groups := map[string]*group{}
			var order []string
			skipped := 0
			for _, r := range recs {
				m, _ := r["mode"].(string)
				if mode != "all" && m != mode {
					continue
				}
				task, _ := r["task"].(string)
				if task == "" {
					continue
				}
				withCard, _ := r["memory"].(bool)
				// A live record whose window contained no calls is not a
				// measurement of anything — the window missed the task, which is
				// exactly what the cross-check exists to catch. Averaging its
				// zero in would drag a real mean toward a number no agent ever
				// spent, so it is counted and left out.
				if m == "live" {
					if n, isNum := r["calls"].(float64); !isNum || n == 0 {
						skipped++
						continue
					}
				}
				// Mode is part of the key, never a column to average across. A
				// fixture run and a live run of the same task are two different
				// experiments — a script on a synthetic page, and an agent on a
				// real site — and their mean describes neither. The first
				// version of this report grouped on (task, arm) and blended them
				// into one number, which is the whole mistake the two halves of
				// this command exist to keep apart.
				key := m + "\x00" + task + "\x00" + strconv.FormatBool(withCard)
				g := groups[key]
				if g == nil {
					g = &group{task: task, mode: m, memory: withCard}
					groups[key] = g
					order = append(order, key)
				}
				g.runs++
				if b, _ := r["ok"].(bool); b {
					g.ok++
				}
				if n, ok := r["calls"].(float64); ok {
					g.calls += int(n)
				}
				if b, _ := r["cardSeen"].(bool); b {
					g.withCard++
				}
				if mms, ok := r["mismatches"].([]any); ok && len(mms) > 0 {
					g.bad++
				}
			}
			sort.Strings(order)
			kept := 0
			for _, key := range order {
				g := groups[key]
				if g.runs < min {
					continue
				}
				kept++
				arm := "no card"
				if g.memory {
					arm = "card  "
				}
				mean := float64(g.calls) / float64(g.runs)
				note := ""
				if g.bad > 0 {
					note = fmt.Sprintf("  ! %d record(s) failed cross-check", g.bad)
				}
				fmt.Fprintf(outw, "%-8s %-34s %-8s runs %-4d ok %-8s mean calls %6.2f  card offered %d/%d%s\n",
					g.mode, g.task, arm, g.runs, fmt.Sprintf("%d/%d", g.ok, g.runs), mean, g.withCard, g.runs, note)
			}
			if kept == 0 {
				fmt.Fprintf(outw, "No records matched --mode %s (with at least %d run(s) per group).\n", mode, min)
			}
			if skipped > 0 {
				fmt.Fprintf(outw, "\n%d live record(s) left out: the trace window held no calls, so the\nnumbers describe a stretch of session that was not the task. Re-record those\nwith a --since that covers it.\n", skipped)
			}
			fmt.Fprintf(outw, "\nFrom %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "all", "which records to include: all, fixture, live")
	cmd.Flags().IntVar(&min, "min-runs", 1, "hide groups with fewer runs than this")
	return cmd
}

// --- jsonl helpers -------------------------------------------------------------

func appendJSONL(path string, rec map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func readJSONL(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	dec := json.NewDecoder(f)
	for {
		var rec map[string]any
		err := dec.Decode(&rec)
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				return out, nil
			}
			return nil, err
		}
		out = append(out, rec)
	}
}
