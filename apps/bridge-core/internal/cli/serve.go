package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/app"
)

// serveCommandName names the hidden control-plane subcommand; spawnCore
// execs the bridge binary with it and daemonIdentity matches on it.
const serveCommandName = "serve"

// newServeCommand builds `bridge serve`: the control plane, formerly the
// standalone bridge-core binary (ADR-0013). It is hidden — the public command
// set is unchanged — and is how the supervisor (spawnCore) runs the daemon.
// Configuration comes from the BRIDGE_* environment exactly as bridge-core
// read it; the process blocks until SIGINT/SIGTERM cancels the command
// context (wired in Execute).
func newServeCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:    serveCommandName,
		Short:  "Run the control plane in the foreground (internal; spawned by 'bridge service up')",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.ConfigFromEnv()
			if err != nil {
				return err
			}
			cfg.Version = version
			cfg.Logger = log.Default()
			if err := app.Run(cmd.Context(), cfg); err != nil {
				return fmt.Errorf("failed to start bridge-core: %w", err)
			}
			return nil
		},
	}
}
