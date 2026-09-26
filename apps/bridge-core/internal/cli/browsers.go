package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/core"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/ws"
)

// newBrowserListCommand is the browser:list registration in the TS CLI.
func newBrowserListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "browser:list",
		Short: "List connected browser instances",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			browsers, raw, err := listBrowsers(cmd.Context(), g.server)
			if err != nil {
				return fail(cmd, g, "list_failed", err.Error())
			}
			if g.json {
				return printData(cmd, g, raw)
			}
			out := cmd.OutOrStdout()
			if len(browsers) == 0 {
				fmt.Fprintln(out, "No connected browsers.")
				return nil
			}
			fmt.Fprintln(out, "Connected browsers:")
			for _, b := range browsers {
				// TS prints new Date(lastSeen).toLocaleString(); a fixed
				// local-time layout is the portable Go equivalent.
				lastSeen := time.UnixMilli(b.LastSeen).Local().Format("2006-01-02 15:04:05")
				fmt.Fprintf(out, "  - %s (status: %s, lastSeen: %s)\n", b.BrowserID, b.Status, lastSeen)
			}
			return nil
		},
	}
}

// listBrowsers is apps/cli/src/commands/listBrowsers.ts: a list_browsers
// event envelope answered with the registry list. It returns the parsed
// list alongside the raw data so --json output stays byte-faithful.
func listBrowsers(ctx context.Context, server string) ([]core.BrowserConnection, json.RawMessage, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := ws.Dial(dialCtx, server)
	if err != nil {
		//nolint:staticcheck // ST1005: user-facing text mirrors the TS CLI.
		return nil, nil, fmt.Errorf("Could not connect to the bridge server at %s. Is the service running? Start it with: bridge service up", server)
	}
	defer client.Close()

	env, err := client.Request(ctx, core.TypeEvent, map[string]any{"event": "list_browsers"}, "", 10*time.Second)
	if err != nil {
		return nil, nil, err
	}
	var payload core.ResponsePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode response payload %s: %w", env.Payload, err)
	}
	if payload.Status == "error" {
		return nil, nil, errors.New(responseError(payload, "unknown"))
	}

	raw := payload.Data
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("[]")
	}
	browsers := []core.BrowserConnection{}
	if err := json.Unmarshal(raw, &browsers); err != nil {
		return nil, nil, fmt.Errorf("decode browser list %s: %w", raw, err)
	}
	return browsers, raw, nil
}
