package cli

import (
	"encoding/json"
	"fmt"
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
	var raw bool
	cmd := &cobra.Command{
		Use:   "show <host>",
		Short: "Print a card, rendered exactly as an agent would receive it",
		Long: "Print a card the way the control plane injects it — the same 400-token\n" +
			"rendering, so what you read here is what the agent was told.\n\n" +
			"With --raw, prints the stored JSON instead: useful when you intend to edit\n" +
			"or delete entries, since the file is plain and diffable.",
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
			card, ok := store.Get(host)
			if !ok {
				return fmt.Errorf("no card for %s (cards live in %s)", host, store.Dir())
			}
			if raw {
				data, err := json.MarshalIndent(card, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			rendered := memory.RenderCard(card, memory.RenderOptions{MaxTokens: memory.DefaultInjectTokens})
			if rendered == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "card for %s is empty\n", host)
				return ErrReported
			}
			fmt.Fprintln(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Print the stored card JSON instead of the rendered view")
	return cmd
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
			if _, ok := store.Get(host); !ok {
				return fmt.Errorf("no card for %s", host)
			}
			if err := store.Remove(host); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed card for %s\n", host)
			return nil
		},
	}
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
