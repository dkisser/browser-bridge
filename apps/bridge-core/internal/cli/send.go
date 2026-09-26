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
		TabID:   g.tab,
		Params:  params,
	}, time.Duration(g.timeout)*time.Millisecond)
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

// responseError is the TS `payload.message ?? payload.error ?? fallback`.
func responseError(payload core.ResponsePayload, fallback string) string {
	if payload.Message != "" {
		return payload.Message
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
	return printData(cmd, g, data)
}

// browserCommand is one table-driven browser subcommand: the cobra surface
// plus the params mapping onto the wire command.
type browserCommand struct {
	use     string // first word is the CLI name
	aliases []string
	short   string
	args    cobra.PositionalArgs
	command string // wire CommandType in packages/shared/src/types.ts
	params  func(args []string) (map[string]any, error)
}

func noParams(_ []string) (map[string]any, error) { return map[string]any{}, nil }

// selectorParam maps `bridge <cmd> <selector>` to {selector}.
func selectorParam(args []string) (map[string]any, error) {
	return map[string]any{"selector": args[0]}, nil
}

// intParam parses a positional integer. The TS CLI passed Number() through
// (NaN serialized as null); failing fast keeps the error local and legible.
func intParam(name string) func(args []string) (map[string]any, error) {
	return func(args []string) (map[string]any, error) {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q", name, args[0])
		}
		return map[string]any{name: n}, nil
	}
}

// selectorHint is the shared selector guidance from the TS CLI.
const selectorHint = "The selector must exist on the page — run snapshot first if unsure."

// browserCommands mirrors the straightforward command registrations in
// apps/cli/src/index.ts; snapshot and the wait:* pair take extra flags and
// are built separately. goBack/goForward keep their camelCase wire names,
// with kebab-case as the CLI name and the TS name as an alias.
var browserCommands = []browserCommand{
	{
		use:     "navigate <url>",
		short:   "Navigate to URL in a specific tab",
		args:    cobra.ExactArgs(1),
		command: "navigate",
		params: func(args []string) (map[string]any, error) {
			return map[string]any{"url": args[0]}, nil
		},
	},
	{
		use:     "go-back",
		aliases: []string{"goBack"},
		short:   "Go back in browser history",
		args:    cobra.NoArgs,
		command: "goBack",
		params:  noParams,
	},
	{
		use:     "go-forward",
		aliases: []string{"goForward"},
		short:   "Go forward in browser history",
		args:    cobra.NoArgs,
		command: "goForward",
		params:  noParams,
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
		params: func(args []string) (map[string]any, error) {
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
		params: func(args []string) (map[string]any, error) {
			return map[string]any{"selector": args[0], "text": args[1]}, nil
		},
	},
	{
		use:     "select <selector> <value>",
		short:   "Select an option in a dropdown. " + selectorHint,
		args:    cobra.ExactArgs(2),
		command: "select",
		params: func(args []string) (map[string]any, error) {
			return map[string]any{"selector": args[0], "value": args[1]}, nil
		},
	},
	{
		use:     "scroll <x> <y>",
		short:   "Scroll page by x,y pixels",
		args:    cobra.ExactArgs(2),
		command: "scroll",
		params: func(args []string) (map[string]any, error) {
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
				params, err := bc.params(args)
				if err != nil {
					return fail(cmd, g, "command_failed", err.Error())
				}
				return dispatch(cmd, g, bc.command, params)
			},
		})
	}
}

// snapshotResult mirrors SnapshotResult in packages/shared/src/snapshot.ts
// (only the fields the human-readable output needs).
type snapshotResult struct {
	Snapshot     string `json:"snapshot"`
	NodesEmitted int    `json:"nodes_emitted"`
	NodesTotal   int    `json:"nodes_total"`
	Tier         int    `json:"tier"`
	Truncated    bool   `json:"truncated"`
}

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
