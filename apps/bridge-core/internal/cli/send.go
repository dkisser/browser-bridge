package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"browser-bridge/internal/core"
	"browser-bridge/internal/memory"
	"browser-bridge/internal/ws"
)

// sendCommand is apps/cli/src/commands/sendCommand.ts: dial, send one
// command envelope, unwrap the response payload.
func sendCommand(ctx context.Context, g *globals, command string, params map[string]any) (json.RawMessage, error) {
	if g.browser == "" {
		//nolint:staticcheck // ST1005: user-facing text mirrors the TS CLI.
		return nil, errors.New("Required: --browser <id>")
	}
	if params == nil {
		params = map[string]any{}
	}

	// TS: waitForOpen(5000) bounds the connect.
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := ws.Dial(dialCtx, g.server)
	if err != nil {
		// TS discards the underlying error; the message tells the user how
		// to fix it.
		//nolint:staticcheck // ST1005: user-facing text mirrors the TS CLI.
		return nil, fmt.Errorf("Could not connect to the bridge server at %s. Is the service running? Start it with: bridge service up", g.server)
	}
	defer client.Close()

	env, err := client.SendCommand(ctx, g.browser, core.CommandPayload{
		Command: command,
		TabID:   addressedTab(g, params),
		Params:  params,
	}, cliTransportTimeout(g, params))
	if err != nil {
		return nil, err
	}

	var payload core.ResponsePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode response payload %s: %w", env.Payload, err)
	}
	if payload.Status == "error" {
		return nil, errors.New(responseError(payload, "Unknown error"))
	}
	if len(payload.Data) == 0 || string(payload.Data) == "null" {
		return json.RawMessage(`{"status":"ok"}`), nil
	}
	return payload.Data, nil
}

// addressedTab is the tab a command is addressed to.
//
// For the tab-scoped commands the target is the positional argument —
// `tab:close 5`, `tab:switch 5` — which the parameter function puts in
// params.tabId, while the envelope's top-level TabID carried the global --tab
// and defaulted to 0. The router reads only the top-level field, so
// `bridge tab:close 5` pruned tab 0: the tab it deleted was not the tab it
// closed, and tab 5's host and digest survived to be inherited by whatever tab
// Chrome later reused id 5 for. Only those two commands set params.tabId, so
// preferring it cannot move a command that means the global.
func addressedTab(g *globals, params map[string]any) int {
	if v, ok := params["tabId"].(int); ok {
		return v
	}
	return g.tab
}

// cliTransportTimeout is how long the CLI waits for a command's response.
//
// Normally that is just --timeout. When the command carries an in-page
// timeout in its params, the extension's own wait is bounded by that, so the
// transport must outlast it — otherwise the one diagnostic worth seeing
// arrives after the caller has already stopped listening. Whichever deadline
// is longer wins, so a deliberately small --timeout is still honoured.
//
// The in-page budget is identified by the presence of `timeout` in params
// rather than declared per command. The MCP layer declares it explicitly
// (commandSpec.waitBudget), which is the better idiom, but the CLI's wait
// commands are built by their own constructors rather than the
// browserCommands table, so a table flag would cover navigate and silently
// skip both wait commands — the exact drift this constant was deduplicated
// to prevent. Every CLI command that sets params.timeout (navigate,
// wait:element, wait:navigation) is bounding the extension's own wait; keep
// it that way, or give this an explicit per-command field instead.
func cliTransportTimeout(g *globals, params map[string]any) time.Duration {
	deadline := time.Duration(g.timeout) * time.Millisecond
	inPage, ok := params["timeout"].(int)
	if !ok {
		return deadline
	}
	if extended := time.Duration(inPage)*time.Millisecond + core.InPageTimeoutSlack; extended > deadline {
		return extended
	}
	return deadline
}

// responseError renders a command failure: the extension's human-readable
// message when it sent one, then the bare error code, then the caller's
// fallback — the TS `payload.message ?? payload.error ?? fallback`.
func responseError(payload core.ResponsePayload, fallback string) string {
	if payload.Message != "" {
		return payload.Message
	}
	// Identifies what was refused, for the case where a producer sends the
	// structured denial without a human-readable message. The extension does
	// not currently — it always fills Message via humanDenialMessage — so
	// this does not fire today.
	//
	// Deliberately *identifying* rather than advisory. humanDenialMessage in
	// packages/shared/src/policy.ts is the canonical renderer, and some of its
	// reasons carry security-relevant guidance — the origin_not_approved
	// message explicitly tells the reader not to work around the gate with
	// other tools. A second, thinner renderer in Go would drift from that and
	// quietly drop the guidance; naming the refusal is all this can honestly
	// do, so the comment points at the real one instead of imitating it.
	if d := payload.Denied; d != nil {
		subject := d.Origin
		if subject == "" {
			subject = d.Command
		}
		if subject == "" {
			subject = "policy"
		}
		if d.Capability != "" {
			return fmt.Sprintf("Refused: %s (%s, capability %s).", subject, d.Reason, d.Capability)
		}
		return fmt.Sprintf("Refused: %s (%s).", subject, d.Reason)
	}
	if payload.Error != "" {
		return payload.Error
	}
	return fallback
}

// dispatch runs one browser command and prints the result, the TS
// dispatchCommand.
func dispatch(cmd *cobra.Command, g *globals, command string, params map[string]any) error {
	data, err := sendCommand(cmd.Context(), g, command, params)
	if err != nil {
		return fail(cmd, g, "command_failed", err.Error())
	}
	if err := printData(cmd, g, data); err != nil {
		return err
	}
	printGuideHint(cmd, g, command, params, data)
	return nil
}

// printGuideHint adds the guide's pointer after a landing command's own
// output (ADR-0034). The CLI gets no in-band injection — the note is not on
// the wire — so the landing itself announces the one curated file worth
// reading, the same pointer the MCP landing carries. Human mode only:
// --json output is a structured contract and a prose line would break it.
func printGuideHint(cmd *cobra.Command, g *globals, command string, params map[string]any, data json.RawMessage) {
	if g.json || (command != "navigate" && command != "tab:new") {
		return
	}
	url := guideHintURL(params, data)
	host := memory.HostFromURL(url)
	if host == "" {
		return
	}
	env, err := EnvFromOSEnv()
	if err != nil {
		return
	}
	if note := memory.GuideNote(env.DataDir(), host); note != "" {
		fmt.Fprintln(cmd.OutOrStdout(), note)
	}
}

// guideHintURL picks the URL a landing ended on: the response's final URL
// when it carries one (navigate follows redirects), else the URL that was
// asked for.
func guideHintURL(params map[string]any, data json.RawMessage) string {
	url, _ := params["url"].(string)
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &result); err == nil && result.URL != "" {
		url = result.URL
	}
	return url
}

// browserCommand is one table-driven browser subcommand: the cobra surface
// plus the params mapping onto the wire command.
type browserCommand struct {
	use     string // first word is the CLI name
	aliases []string
	short   string
	args    cobra.PositionalArgs
	command string // wire CommandType in packages/shared/src/types.ts
	params  func(g *globals, args []string) (map[string]any, error)
}

func noParams(_ *globals, _ []string) (map[string]any, error) { return map[string]any{}, nil }

// selectorParam maps `bridge <cmd> <selector>` to {selector}.
func selectorParam(_ *globals, args []string) (map[string]any, error) {
	return map[string]any{"selector": args[0]}, nil
}

// intParam parses a positional integer. The TS CLI passed Number() through
// (NaN serialized as null); failing fast keeps the error local and legible.
// navSettleParams is the params every command that waits for a navigation to
// settle must carry.
//
// The extension bounds its own wait with this budget and falls back to a
// hardcoded 30s when it is absent, so a command that omits it makes the two
// entry points disagree: the CLI's default --timeout is 10s, and the extension
// would sit on a listener for 20s after the CLI had already given up — for a
// navigation that then succeeds, the agent is told it timed out and the router
// never records the landing, leaving the tab attributed to the site it just
// left.
//
// navigate had this reasoning in its own params function; goBack and goForward
// did too much the same thing to reach the settle and did not. One helper, so
// the next command that waits inherits the reasoning rather than the bug.
func navSettleParams(g *globals, _ []string) (map[string]any, error) {
	return map[string]any{"timeout": g.timeout}, nil
}

func intParam(name string) func(_ *globals, args []string) (map[string]any, error) {
	return func(_ *globals, args []string) (map[string]any, error) {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q", name, args[0])
		}
		return map[string]any{name: n}, nil
	}
}

// selectorHint is the shared selector guidance from the TS CLI.
const selectorHint = "The selector must exist on the page — run snapshot first if unsure."

// browserCommands mirrors the straightforward command registrations that
// were in apps/cli/src/index.ts (deleted with the TS CLI); snapshot and the
// wait:* pair take extra flags and
// are built separately. goBack/goForward keep their camelCase wire names,
// with kebab-case as the CLI name and the TS name as an alias.
var browserCommands = []browserCommand{
	{
		use:     "navigate <url>",
		short:   "Navigate to URL in a specific tab",
		args:    cobra.ExactArgs(1),
		command: "navigate",
		params: func(g *globals, args []string) (map[string]any, error) {
			params, err := navSettleParams(g, args)
			if err != nil {
				return nil, err
			}
			params["url"] = args[0]
			return params, nil
		},
	},
	{
		use:     "go-back",
		aliases: []string{"goBack"},
		short:   "Go back in browser history",
		args:    cobra.NoArgs,
		command: "goBack",
		params:  navSettleParams,
	},
	{
		use:     "go-forward",
		aliases: []string{"goForward"},
		short:   "Go forward in browser history",
		args:    cobra.NoArgs,
		command: "goForward",
		params:  navSettleParams,
	},
	{
		use:     "refresh",
		short:   "Refresh current page",
		args:    cobra.NoArgs,
		command: "refresh",
		params:  noParams,
	},
	{
		use:     "tab:list",
		short:   "List all open tabs",
		args:    cobra.NoArgs,
		command: "tab:list",
		params:  noParams,
	},
	{
		use:     "tab:new [url]",
		short:   "Open a new tab",
		args:    cobra.MaximumNArgs(1),
		command: "tab:new",
		params: func(g *globals, args []string) (map[string]any, error) {
			params := map[string]any{}
			if len(args) == 1 {
				params["url"] = args[0]
			}
			return params, nil
		},
	},
	{
		use:     "tab:close <tabId>",
		short:   "Close a tab by ID",
		args:    cobra.ExactArgs(1),
		command: "tab:close",
		params:  intParam("tabId"),
	},
	{
		use:     "tab:switch <tabId>",
		short:   "Switch to a tab by ID",
		args:    cobra.ExactArgs(1),
		command: "tab:switch",
		params:  intParam("tabId"),
	},
	{
		use:     "click <selector>",
		short:   "Click an element. " + selectorHint,
		args:    cobra.ExactArgs(1),
		command: "click",
		params:  selectorParam,
	},
	{
		use:     "type <selector> <text>",
		short:   "Type text into an element. " + selectorHint,
		args:    cobra.ExactArgs(2),
		command: "type",
		params: func(g *globals, args []string) (map[string]any, error) {
			return map[string]any{"selector": args[0], "text": args[1]}, nil
		},
	},
	{
		use:     "select <selector> <value>",
		short:   "Select an option in a dropdown. " + selectorHint,
		args:    cobra.ExactArgs(2),
		command: "select",
		params: func(g *globals, args []string) (map[string]any, error) {
			return map[string]any{"selector": args[0], "value": args[1]}, nil
		},
	},
	{
		use:     "scroll <x> <y>",
		short:   "Scroll page by x,y pixels",
		args:    cobra.ExactArgs(2),
		command: "scroll",
		params: func(g *globals, args []string) (map[string]any, error) {
			params := map[string]any{"selector": "page"}
			for i, key := range []string{"x", "y"} {
				n, err := strconv.ParseFloat(args[i], 64)
				if err != nil {
					return nil, fmt.Errorf("invalid %s %q", key, args[i])
				}
				params[key] = n
			}
			return params, nil
		},
	},
	{
		use:     "hover <selector>",
		short:   "Hover over an element. " + selectorHint,
		args:    cobra.ExactArgs(1),
		command: "hover",
		params:  selectorParam,
	},
	{
		use:     "gettext <selector>",
		short:   "Get text content of an element. " + selectorHint,
		args:    cobra.ExactArgs(1),
		command: "gettext",
		params:  selectorParam,
	},
	{
		use:     "gethtml <selector>",
		short:   "Get inner HTML of an element. " + selectorHint,
		args:    cobra.ExactArgs(1),
		command: "gethtml",
		params:  selectorParam,
	},
	{
		use:     "screenshot",
		short:   "Take a screenshot",
		args:    cobra.NoArgs,
		command: "screenshot",
		params:  noParams,
	},
	{
		use:     "pageinfo",
		short:   "Get current page info",
		args:    cobra.NoArgs,
		command: "pageinfo",
		params:  noParams,
	},
}

func registerBrowserCommands(root *cobra.Command, g *globals) {
	for _, bc := range browserCommands {
		root.AddCommand(&cobra.Command{
			Use:     bc.use,
			Aliases: bc.aliases,
			Short:   bc.short,
			Args:    bc.args,
			RunE: func(cmd *cobra.Command, args []string) error {
				params, err := bc.params(g, args)
				if err != nil {
					return fail(cmd, g, "command_failed", err.Error())
				}
				return dispatch(cmd, g, bc.command, params)
			},
		})
	}
}

// snapshotResult is the shared wire shape (core.SnapshotResult mirrors
// SnapshotResult in packages/shared/src/snapshot.ts); the CLI only prints a
// subset of the fields, but decoding the rest is free and keeping one
// declaration avoids a second copy drifting from the MCP layer's.
type snapshotResult = core.SnapshotResult

// newSnapshotCommand is the snapshot registration in the TS CLI, including
// its bespoke human output (snapshot text + stats line).
func newSnapshotCommand(g *globals) *cobra.Command {
	var selector, filter string
	var maxChars int
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Take a compact snapshot of the page (default: interactive elements + headings; use --filter full for the complete tree)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := map[string]any{}
			if selector != "" {
				params["selector"] = selector
			}
			if filter == "full" {
				params["filter"] = "full"
			}
			if cmd.Flags().Changed("max-chars") {
				params["max_chars"] = maxChars
			}
			data, err := sendCommand(cmd.Context(), g, "snapshot", params)
			if err != nil {
				return fail(cmd, g, "command_failed", err.Error())
			}
			if g.json {
				return printData(cmd, g, data)
			}
			var result snapshotResult
			if err := json.Unmarshal(data, &result); err != nil {
				return fail(cmd, g, "command_failed", fmt.Sprintf("decode snapshot result: %v", err))
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Snapshot)
			fmt.Fprintf(cmd.OutOrStdout(), "[nodes: %d/%d | tier: %d | truncated: %t]\n",
				result.NodesEmitted, result.NodesTotal, result.Tier, result.Truncated)
			return nil
		},
	}
	cmd.Flags().StringVar(&selector, "selector", "", "Limit snapshot to this selector (default: whole page)")
	cmd.Flags().StringVar(&filter, "filter", "", "interactive (default) or full — full includes text runs, images and structural containers")
	cmd.Flags().IntVar(&maxChars, "max-chars", 0, "Maximum snapshot size in characters (default: 8000 interactive, 3000 full)")
	return cmd
}

// newWaitElementCommand / newWaitNavigationCommand mirror the TS wait:*
// commands: their local --timeout goes into the command params while the
// global --timeout keeps bounding the request itself. The local flag
// shadows the persistent one for these two commands only.
func newWaitElementCommand(g *globals) *cobra.Command {
	var timeout int
	cmd := &cobra.Command{
		Use:   "wait:element <selector>",
		Short: "Wait for an element to appear. Pick the selector from a snapshot of this page — a guessed selector may never match.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return dispatch(cmd, g, "wait:element", map[string]any{
				"selector": args[0],
				"timeout":  timeout,
			})
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", defaultTimeout, "Timeout in ms")
	return cmd
}

func newWaitNavigationCommand(g *globals) *cobra.Command {
	var timeout int
	cmd := &cobra.Command{
		Use:   "wait:navigation",
		Short: "Wait for page navigation to complete",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return dispatch(cmd, g, "wait:navigation", map[string]any{"timeout": timeout})
		},
	}
	cmd.Flags().IntVar(&timeout, "timeout", defaultTimeout, "Timeout in ms")
	return cmd
}
