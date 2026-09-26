// Package cli implements the bridge command line — the Go port of the
// deleted apps/cli (commander.js), per ADR-0012 phase 2b, plus the
// `bridge service` lifecycle tree ported from install/bridge.sh.tmpl
// (phase 3) and the hidden `bridge serve` control-plane subcommand
// (ADR-0013). Command names, flag semantics, output formats, error texts,
// and the exit-1-on-failure contract follow their predecessors.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

// New builds the bridge command tree. version is the binary version
// (link-time -X main.version=..., "dev" for local builds) reported by
// `bridge --version`.
func New(version string) *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:   "bridge",
		Short: "Browser Bridge CLI",
		// The bash router printed "bridge <version>"; keep that shape.
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// The TS CLI has no completion command.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.SetVersionTemplate("bridge {{.Version}}\n")
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
	root.AddCommand(newServiceCommand(g))
	root.AddCommand(newAutostartCommand(g))
	root.AddCommand(newServeCommand(version))
	registerMovedVerbs(root, g)
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

// Execute runs the CLI and returns the process exit code. SIGINT/SIGTERM
// cancel the command context so `service up --foreground` and `service logs`
// shut down cleanly (the bash supervisor's trap).
func Execute(version string) int {
	root := New(version)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		switch {
		case errors.Is(err, ErrReported):
			// Command failures are already formatted by fail().
		case errors.Is(err, ErrSilent):
			// status/doctor findings were the output; exit 1 silently.
		default:
			// Cobra builds the message as "unknown command %q for %q" for
			// typos and uninstalled verbs. Map that to the BB-E101 code
			// the bash router emitted so release scripts, install docs,
			// and install tests that grep for BB-E### stay consistent.
			// JSON mode is detected from the raw args because Cobra does
			// not parse flags before bailing on an unknown subcommand.
			jsonMode := argsHaveJSON(os.Args[1:])
			formatted := TranslateError(root, err, jsonMode)
			if jsonMode {
				fmt.Fprintln(root.OutOrStdout(), formatted)
			} else {
				fmt.Fprintln(root.ErrOrStderr(), "Error:", formatted)
			}
		}
		return 1
	}
	return 0
}

// translateError re-formats Cobra's built-in error texts into the
// BB-E###-style messages the bash router used, so existing release scripts
// and install tests that grep for the codes keep working. The
// unknown-command path used to print Cobra's bare
//
//	Error: unknown command "foo" for "bridge"
//
// which broke those consumers. Today the only re-format we need is the
// unknown-command case; flag-parse errors are passed through verbatim.
//
// Exported so tests can drive the formatting without going through
// Execute, which builds its own root command and so cannot share the
// caller-supplied stderr capture.
//
// jsonMode is passed in explicitly because Cobra's flag.Changed is false
// on the unknown-command path — Cobra bails before parsing flags when
// args[0] looks like a verb that doesn't match a registered subcommand.
// Scanning args for --json / --json=true is the only reliable way to
// pick the JSON-mode branch.
func TranslateError(root *cobra.Command, err error, jsonMode bool) string {
	const unknownPrefix = "unknown command "
	if msg := err.Error(); strings.HasPrefix(msg, unknownPrefix) {
		name := extractUnknownCmdName(msg)
		if name == "" {
			return err.Error()
		}
		if jsonMode {
			raw, _ := json.Marshal(errorObject{Status: "error", Error: "BB-E101", Message: fmt.Sprintf("unknown command '%s'. Run 'bridge --help' for help.", name)})
			return string(raw)
		}
		return fmt.Sprintf("BB-E101: unknown command '%s'. Run 'bridge --help' for help.", name)
	}
	return err.Error()
}

// argsHaveJSON scans argv for --json or --json=true|false. Used by the
// unknown-command error path because Cobra's flag.Changed is not yet set
// when the unknown-command branch is taken.
func argsHaveJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
		if strings.HasPrefix(a, "--json=") {
			v := strings.TrimPrefix(a, "--json=")
			return v == "true" || v == "1"
		}
	}
	return false
}

// extractUnknownCmdName pulls "foo" out of `unknown command "foo" for "bridge"`.
// Returns "" when the message does not match the expected shape, in which
// case the caller falls back to the raw error text.
func extractUnknownCmdName(msg string) string {
	rest := strings.TrimPrefix(msg, "unknown command ")
	end := strings.Index(rest, " for ")
	if end < 0 {
		return ""
	}
	name := rest[:end]
	name = strings.Trim(name, `"`)
	return name
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
