package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"browser-bridge/internal/memory"
)

// `bridge memory` is the human surface of the self-learning store (ADRs
// 0018-0022). It exists because a store that rewrites itself needs a way to be
// argued with: an automatic update you cannot inspect, revert or delete is not
// reviewable, it is just drift.
//
// Every subcommand here works on files under $BB_HOME/data and needs no running
// service — which is the point. You must be able to see and remove a bad card
// when the daemon is the thing that is stuck.
func newMemoryCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage the learned site cards (self-learning store)",
		Long: "Inspect and manage what the control plane has learned about driving sites.\n\n" +
			"Everything here reads or writes files under $BB_HOME/data and works with the\n" +
			"service stopped. A card is a claim about a site that may be out of date, so\n" +
			"reading one before trusting it is the intended use.",
	}
	cmd.AddCommand(
		newMemoryListCommand(g),
		newMemoryShowCommand(),
		newMemoryLearnCommand(),
		newMemoryHistoryCommand(),
		newMemoryRemoveCommand(),
		newMemoryBenchCommand(),
	)
	return cmd
}

// openStore resolves the card store. It is deliberately not fatal when the
// directory does not exist yet: an install that has never run a browser command
// has no cards, and that is not an error.
func openStore() (*memory.Store, error) {
	env, err := EnvFromOSEnv()
	if err != nil {
		return nil, err
	}
	store, err := memory.OpenStore(env.DataDir())
	if err != nil {
		return nil, err
	}
	return store, nil
}

func newMemoryListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every host with a learned card",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			hosts := store.Hosts()
			if g != nil && g.json {
				type row struct {
					Host       string `json:"host"`
					Revision   int    `json:"revision"`
					UpdatedAt  string `json:"updatedAt"`
					Map        int    `json:"map"`
					Failures   int    `json:"failures"`
					Procedures int    `json:"procedures"`
				}
				rows := make([]row, 0, len(hosts))
				for _, h := range hosts {
					card, ok := store.Get(h)
					if !ok {
						continue
					}
					rows = append(rows, row{
						Host:       card.Host,
						Revision:   card.Revision,
						UpdatedAt:  formatMs(card.UpdatedAtMs),
						Map:        len(card.Map),
						Failures:   len(card.Failures),
						Procedures: len(card.Procedures),
					})
				}
				return printJSONRows(cmd, g, rows)
			}
			if len(hosts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No site cards yet. Drive a site, then run `bridge memory learn`.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tREV\tUPDATED\tMAP\tFAILS\tSTEPS")
			for _, h := range hosts {
				card, ok := store.Get(h)
				if !ok {
					continue
				}
				fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%d\n", card.Host, card.Revision,
					formatMs(card.UpdatedAtMs), len(card.Map), len(card.Failures), len(card.Procedures))
			}
			return w.Flush()
		},
	}
}

func newMemoryShowCommand() *cobra.Command {
	var raw, resolve bool
	cmd := &cobra.Command{
		Use:   "show <host>",
		Short: "Print a card, rendered exactly as an agent would receive it",
		Long: "Print a card the way the control plane injects it — the same 400-token\n" +
			"rendering, so what you read here is what the agent was told.\n\n" +
			"With --raw, prints the stored JSON instead: useful when you intend to edit\n" +
			"or delete entries, since the file is plain and diffable.\n\n" +
			"With --resolve, additionally prints the site map with refs resolved against\n" +
			"the last page the control plane recorded for this host, read out of the\n" +
			"trace. That is a page from the past, not the one in your browser, and the\n" +
			"output says so — it is how you see what the map resolves to without a live\n" +
			"snapshot to check it against (ADR-0026).\n\n" +
			"When a curated site guide exists (data/guides/<host>.md) it is printed after\n" +
			"the card — except with --raw, which stays the card alone (ADR-0033).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if raw && resolve {
				return fmt.Errorf("--raw prints the stored card and --resolve adds a rendering of it; pick one")
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			host := memory.Host(args[0])
			if host == "" {
				return fmt.Errorf("%q is not a host name", args[0])
			}
			env, err := EnvFromOSEnv()
			if err != nil {
				return err
			}
			card, err := readCard(store, host)
			if err != nil {
				// A host can have a curated guide before it has a card — guides
				// are written at the human's request, cards are earned by
				// traffic. Print what there is; the missing card is still
				// reported the way it always was.
				printGuide(cmd, env.DataDir(), host)
				return err
			}
			if raw {
				data, err := json.MarshalIndent(card, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			if resolve {
				err := showResolved(cmd, host, card)
				printGuide(cmd, env.DataDir(), host)
				return err
			}
			rendered := memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens})
			if rendered == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "card for %s is empty\n", host)
				printGuide(cmd, env.DataDir(), host)
				return ErrReported
			}
			fmt.Fprintln(cmd.OutOrStdout(), rendered)
			printGuide(cmd, env.DataDir(), host)
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Print the stored card JSON instead of the rendered view")
	cmd.Flags().BoolVar(&resolve, "resolve", false, "Also resolve the site map against the last recorded page for this host")
	return cmd
}

// printGuide appends the human-curated site guide when one exists. The guide
// is the card's missing half — the semantics structural learning cannot hold
// (ADR-0028) — and show is the pull every recall path already makes, so the
// guide rides it (ADR-0033). A missing guide is the normal state and prints
// nothing; an unreadable one is a warning rather than a failure, because the
// card — the thing show exists for — was already printed.
func printGuide(cmd *cobra.Command, dir, host string) {
	guide, err := memory.ReadGuide(dir, host)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: the guide for %s exists but will not read: %v\n", host, err)
		return
	}
	if guide == "" {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n[site guide] %s — %s (human-curated; where it disagrees with the live page, the page wins)\n",
		host, memory.GuidePath(dir, host))
	fmt.Fprintln(cmd.OutOrStdout(), guide)
}

// showResolved renders a card with its site map resolved, and says where the
// page came from.
//
// The honesty here is the whole point. The map's refs are only meaningful
// against the page they were resolved from, and the page in the user's browser
// is not that page (ADR-0024). A renderer that quietly printed them would look
// exactly like the live injection and be wrong in the one case where a wrong ref
// costs something. So the provenance is stated twice — in the header, and in
// the map section's own title, which is why RenderOptions carries a PageNote
// instead of this fixing up the string afterwards.
func showResolved(cmd *cobra.Command, host string, card *memory.SiteCard) error {
	out := cmd.OutOrStdout()

	env, err := EnvFromOSEnv()
	if err != nil {
		return err
	}
	digest, atMs, err := lastPageFor(env.DataDir(), host)
	if err != nil {
		return err
	}
	if digest == nil {
		// Not a failure of the store: there is a card, it just has nothing to
		// resolve against. The readable halves still print, so this is a warning
		// rather than an error — but it is ErrReported, because a script that
		// asked to resolve and got an unresolved map has not been answered.
		fmt.Fprintln(out, memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens}))
		fmt.Fprintf(cmd.ErrOrStderr(),
			"No page recorded for %s yet, so the site map cannot be resolved. Visit the site and run a\nsnapshot, then try again — the digest is kept in the trace, so nothing else is needed.\n", host)
		return ErrReported
	}

	_, missing := memory.VerifyCard(card, digest, "")
	note := "the page seen " + formatMs(atMs)
	fmt.Fprintf(out, "Resolved offline against the last page the control plane recorded for %s.\n", host)
	fmt.Fprintf(out, "  seen %s", formatMs(atMs))
	if digest.URL != "" {
		fmt.Fprintf(out, "  %s", digest.URL)
	}
	if len(card.Map) > 0 {
		fmt.Fprintf(out, "\n  %d of %d map entries matched it; %d did not.\n", len(card.Map)-missing, len(card.Map), missing)
	}
	fmt.Fprintf(out, "  These refs are not valid for whatever is in your browser now.\n\n")

	// Both halves, because that is what an agent receives across a landing and
	// the snapshot after it — reassembled here from a stored digest instead of a
	// live one. Same options the injection uses, plus the provenance note.
	fmt.Fprintln(out, memory.RenderCard(card, memory.RenderOptions{
		MaxTokens: memory.DefaultInjectTokens,
		Resolver:  func(p memory.Predicate) (string, bool) { return digest.Resolve(p) },
		PageNote:  note,
	}))
	return nil
}

// lastPageFor returns the most recent page digest recorded for a host, and when
// it was seen. The digest rides on the trace record, so this works with the
// service stopped — which is the property the whole memory command group is
// built around.
//
// One read of the whole stream, not a scan back from the end: the file is
// append-only and never rotated, so there is no tail to seek to, and a
// diagnostic that only runs when a person is looking at a card does not need a
// reader that streams.
func lastPageFor(dir string, host string) (*memory.PageDigest, int64, error) {
	stream, err := memory.OpenStream(dir)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = stream.Close() }()
	recs, _, _, err := stream.ReadFrom(0)
	if err != nil {
		return nil, 0, err
	}
	var (
		best  *memory.PageDigest
		bestA int64
	)
	for _, r := range recs {
		if r.Page == nil || r.Host != host {
			continue
		}
		// >= so a later record wins ties, which is what "most recent" means when
		// two snapshots land in the same millisecond.
		if best == nil || r.AtMs >= bestA {
			best, bestA = r.Page, r.AtMs
		}
	}
	return best, bestA, nil
}

func newMemoryLearnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "learn",
		Short: "Run one learner pass over the recorded trace, now",
		Long: "Run the background learner once, synchronously.\n\n" +
			"The daemon does this on its own when the machine goes quiet; this is for\n" +
			"seeing the result immediately, and for the first run — a card written by\n" +
			"`bridge memory show` cannot be checked against a baseline that does not\n" +
			"exist yet.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := EnvFromOSEnv()
			if err != nil {
				return err
			}
			m, err := memory.New(memory.Options{DataDir: env.DataDir()})
			if err != nil {
				return err
			}
			defer func() { _ = m.Close() }()
			if err := m.LearnNow(); err != nil {
				return err
			}
			hosts := m.Store().Hosts()
			if len(hosts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing learned yet — no recorded calls on a host that has failed or repeated.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Learned %d host(s):\n", len(hosts))
			for _, h := range hosts {
				card, ok := m.Store().Get(h)
				if !ok {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  rev %d  map %d  failures %d  steps %d\n",
					card.Host, card.Revision, len(card.Map), len(card.Failures), len(card.Procedures))
			}
			return nil
		},
	}
}

func newMemoryHistoryCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history <host>",
		Short: "Show the revision history of a card, newest first",
		Long: "Show why a card says what it says.\n\n" +
			"Every automatic update appends a card_revision record to the same stream the\n" +
			"observations live in, so this is the audit trail for the store itself: what\n" +
			"changed, when, and on what evidence.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := EnvFromOSEnv()
			if err != nil {
				return err
			}
			host := memory.Host(args[0])
			if host == "" {
				return fmt.Errorf("%q is not a host name", args[0])
			}
			stream, err := memory.OpenStream(env.DataDir())
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()
			revs, err := stream.Revisions(host, limit)
			if err != nil {
				return err
			}
			if len(revs) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No revisions recorded for %s\n", host)
				return nil
			}
			for _, r := range revs {
				fmt.Fprintf(cmd.OutOrStdout(), "rev %-4d %s  %-8s %s\n",
					r.Revision, formatMs(r.AtMs), r.Reason, r.Summary)
				if len(r.Evidence) > 0 {
					sort.Strings(r.Evidence)
					fmt.Fprintf(cmd.OutOrStdout(), "        evidence: %v\n", r.Evidence)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum revisions to print")
	return cmd
}

func newMemoryRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <host>",
		Short: "Delete a card so the agent stops being told about that site",
		Long: "Delete a card. The recorded trace is left alone, so a later learner pass may\n" +
			"rebuild the card from those observations — delete is not a ban. To keep it\n" +
			"gone, remove the trace as well or stop the service while you do it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			host := memory.Host(args[0])
			if host == "" {
				return fmt.Errorf("%q is not a host name", args[0])
			}
			// Deliberately not readCard: a card that will not parse is
			// precisely the one worth deleting, and asking it to parse first
			// made the delete unavailable for the files that needed it.
			if _, err := os.Stat(filepath.Join(store.Dir(), host+".json")); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no card for %s", host)
				}
				return err
			}
			if err := store.Remove(host); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed card for %s\n", host)
			return nil
		},
	}
}

// readCard loads a card and turns "there is nothing here" and "there is
// something here and it will not parse" into two different answers.
//
// The second case used to be indistinguishable from the first, which left a
// person with a truncated or schema-shifted card file no way to inspect or
// delete it through the tool that exists for exactly that — while `memory list`
// reported the directory as healthy.
func readCard(store *memory.Store, host string) (*memory.SiteCard, error) {
	card, err := store.Read(host)
	if err != nil {
		return nil, fmt.Errorf("the card for %s exists but will not parse (%v)\n  %s\n  "+
			"Fix it with an editor, or delete it with `bridge memory rm %s`",
			host, err, filepath.Join(store.Dir(), host+".json"), host)
	}
	if card == nil {
		return nil, fmt.Errorf("no card for %s (cards live in %s)", host, store.Dir())
	}
	return card, nil
}

// printJSONRows follows printData's --json convention: indented JSON on stdout,
// with the same error shape on failure.
func printJSONRows(cmd *cobra.Command, g *globals, rows any) error {
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fail(cmd, g, "command_failed", err.Error())
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func formatMs(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}
