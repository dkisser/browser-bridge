package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/spf13/cobra"
)

// newPairCommand is the pair registration in the TS CLI.
func newPairCommand() *cobra.Command {
	local := defaultLocal
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Generate a pairing code to connect the browser extension to the local proxy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return pair(cmd, local)
		},
	}
	cmd.Flags().StringVar(&local, "local", defaultLocal, "Local proxy URL")
	return cmd
}

// pair is apps/cli/src/commands/pair.ts: POST /api/pair/start on the local
// proxy and print the code. Errors go to stderr (pair has no --json mode)
// and exit 1 via ErrReported.
func pair(cmd *cobra.Command, local string) error {
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, local+"/api/pair/start", nil)
	if err != nil {
		// A malformed --local URL fails the same way as an unreachable one
		// (fetch throws either way and lands in the same catch).
		return pairUnreachable(cmd, local)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pairUnreachable(cmd, local)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Pairing request failed: HTTP %d\n", resp.StatusCode)
		return ErrReported
	}
	var body struct {
		Success bool `json:"success"`
		Data    *struct {
			Code      string `json:"code"`
			ExpiresIn int    `json:"expiresIn"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Pairing request failed: %v\n", err)
		return ErrReported
	}
	if !body.Success || body.Data == nil {
		msg := body.Error
		if msg == "" {
			msg = "unknown error"
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Pairing request failed: %s\n", msg)
		return ErrReported
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Pairing code:  %s\n", body.Data.Code)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Enter this code in the Browser Bridge side panel to pair ")
	fmt.Fprintf(out, "  The code is valid for %d minutes. Re-running this command generates a new code and invalidates the previous one.\n",
		int(math.Round(float64(body.Data.ExpiresIn)/60000)))
	fmt.Fprintln(out)
	return nil
}

func pairUnreachable(cmd *cobra.Command, local string) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "Could not reach the local proxy at %s.\n", local)
	fmt.Fprintln(cmd.ErrOrStderr(), "Is the service running? Start it with: bridge service up")
	return ErrReported
}
