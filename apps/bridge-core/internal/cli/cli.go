// Package cli implements the bridge command line — the Go port of the
// deleted apps/cli (commander.js), per ADR-0012 phase 2b. Command names,
// flag semantics, output formats, error texts, and the exit-1-on-failure
// contract follow apps/cli/src/index.ts.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// Default flag values: the ports come from packages/shared/src/constants.ts
// (WEBSOCKET_PORT / LOCAL_WS_PORT), the 10s timeout from the TS CLI.
const (
	defaultServer  = "ws://localhost:3001"
	defaultLocal   = "http://localhost:3002"
	defaultTimeout = 10000
)

// ErrReported marks errors the command already printed in the TS format
// (human or --json); Execute must exit 1 without printing them again.
var ErrReported = errors.New("error already reported")

// globals carries the persistent flags, mirroring GlobalOptions in
// apps/cli/src/index.ts.
type globals struct {
	server  string
	browser string
	tab     int
	json    bool
	timeout int
}

// New builds the bridge command tree.
func New() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:   "bridge",
		Short: "Browser Bridge CLI",
		// The TS CLI reports 0.0.1 (commander .version(), never bumped).
		Version:       "0.0.1",
		SilenceUsage:  true,
		SilenceErrors: true,
		// The TS CLI has no completion command.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.PersistentFlags().StringVar(&g.server, "server", defaultServer, "WS Server URL")
	root.PersistentFlags().StringVar(&g.browser, "browser", "", "Target browser instance")
	root.PersistentFlags().IntVar(&g.tab, "tab", 0, "Target tab id")
	root.PersistentFlags().BoolVar(&g.json, "json", false, "Structured JSON output")
	root.PersistentFlags().IntVar(&g.timeout, "timeout", defaultTimeout, "Command timeout")

	registerBrowserCommands(root, g)
	root.AddCommand(newSnapshotCommand(g))
	root.AddCommand(newWaitElementCommand(g))
	root.AddCommand(newWaitNavigationCommand(g))
	root.AddCommand(newBrowserListCommand(g))
	root.AddCommand(newPairCommand())
	// Reserved for future distributed-mode support; the TS stub errors out
	// the same way.
	root.AddCommand(&cobra.Command{
		Use:   "bridge-host",
		Short: "Configure CLI to point at a remote Browser Bridge server (not yet implemented)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "bridge-host: not yet implemented. See docs/superpowers/specs/2026-06-15-distribution-design.md")
			return ErrReported
		},
	})
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := New()
	if err := root.Execute(); err != nil {
		if !errors.Is(err, ErrReported) {
			// Flag-parse and unknown-command errors land here; command
			// failures are already formatted by fail().
			fmt.Fprintln(root.ErrOrStderr(), "Error:", err)
		}
		return 1
	}
	return 0
}

// printData is output() in the TS CLI: --json pretty-prints with a 2-space
// indent (JSON.stringify(data, null, 2)); human mode prints strings raw and
// everything else as compact JSON (console.log prints objects with Node's
// inspect format, which compact JSON approximates).
func printData(cmd *cobra.Command, g *globals, data json.RawMessage) error {
	if g.json {
		var buf bytes.Buffer
		if err := json.Indent(&buf, data, "", "  "); err != nil {
			return fail(cmd, g, "command_failed", err.Error())
		}
		fmt.Fprintln(cmd.OutOrStdout(), buf.String())
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), s)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// fail is outputError() in the TS CLI: --json prints a compact
// {status,error,message} object to stdout; human mode prints "Error: msg"
// to stderr. The returned ErrReported makes the process exit 1.
func fail(cmd *cobra.Command, g *globals, errorKind, message string) error {
	if g.json {
		// Marshal cannot fail on three strings.
		raw, _ := json.Marshal(errorObject{Status: "error", Error: errorKind, Message: message})
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", message)
	}
	return ErrReported
}

// errorObject keeps the {status, error, message} key order of the TS
// outputError JSON.
type errorObject struct {
	Status  string `json:"status"`
	Error   string `json:"error"`
	Message string `json:"message"`
}
