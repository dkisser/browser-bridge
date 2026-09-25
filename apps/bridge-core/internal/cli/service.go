package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/service"
)

// serviceHelpText is the bash print_service_help() output, kept verbatim.
const serviceHelpText = `Usage: bridge service <command> [args]

Commands:
  up [--foreground]   Start bridge-core (launchd-supervised on macOS)
  down                Stop bridge-core
  restart             Restart bridge-core
  status              Service state + login auto-start state
  logs                Tail logs (bridge-core)
  enable              Start bridge-core at login (macOS LaunchAgent)
  disable             Do not start bridge-core at login
  update [version]    Upgrade to a release (default: latest)
  doctor              Diagnose the install
  version             Print installed + latest version
  uninstall           Remove ~/.browser-bridge/ (use --yes to skip prompt)
`

// newServiceCommand builds the `bridge service` tree: the Go port of
// install/bridge.sh.tmpl's cmd_service.
func newServiceCommand(g *globals) *cobra.Command {
	svc := &cobra.Command{
		Use:   "service",
		Short: "Manage bridge services (up, down, status, enable, ...)",
		// Runnable with arbitrary args so an unknown subcommand reaches RunE
		// and produces the bash BB-E306 instead of cobra's suggestion text.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprint(cmd.OutOrStdout(), serviceHelpText)
				return nil
			}
			return fail(cmd, g, "BB-E306", fmt.Sprintf("BB-E306: unknown service command '%s'. Run 'bridge service' for the list.", args[0]))
		},
	}

	var foreground bool
	up := &cobra.Command{
		Use:   "up",
		Short: "Start bridge-core (launchd-supervised on macOS)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Up(cmd.Context(), e, r, foreground, cmd.OutOrStdout())
			})
		},
	}
	up.Flags().BoolVar(&foreground, "foreground", false, "Run the supervisor in the foreground (used by the LaunchAgent)")
	svc.AddCommand(up)

	svc.AddCommand(&cobra.Command{
		Use:   "down",
		Short: "Stop bridge-core",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Down(cmd.Context(), e, r, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "restart",
		Short: "Restart bridge-core",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Restart(cmd.Context(), e, r, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Service state + login auto-start state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				running, err := service.Status(cmd.Context(), e, r, cmd.OutOrStdout())
				if err != nil {
					return err
				}
				if !running {
					return service.ErrSilent
				}
				return nil
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Tail logs (bridge-core)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Logs(cmd.Context(), e, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Start bridge-core at login (macOS LaunchAgent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Enable(cmd.Context(), e, r, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Do not start bridge-core at login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Disable(cmd.Context(), e, r, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "update [version]",
		Short: "Upgrade to a release (default: latest)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				target := "latest"
				if len(args) == 1 {
					target = args[0]
				}
				url := service.UpdateScriptURL(e.UpdateOrg, e.UpdateRepo, target)
				return service.Update(cmd.Context(), e, r, target, url, cmd.OutOrStdout())
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				ok, err := service.Doctor(cmd.Context(), e, r, cmd.OutOrStdout())
				if err != nil {
					return err
				}
				if !ok {
					return service.ErrSilent
				}
				return nil
			})
		},
	})

	svc.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print installed + latest version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Version(e, cmd.OutOrStdout())
			})
		},
	})

	var uninstallYes bool
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove ~/.browser-bridge/ (use --yes to skip prompt)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				return service.Uninstall(cmd.Context(), e, r, uninstallYes, cmd.InOrStdin(), cmd.OutOrStdout())
			})
		},
	}
	uninstall.Flags().BoolVar(&uninstallYes, "yes", false, "Skip the confirmation prompt")
	svc.AddCommand(uninstall)

	return svc
}

// newAutostartCommand is the deprecated `bridge autostart on|off|status`
// alias, kept for one release like the bash version.
func newAutostartCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "autostart [on|off|status]",
		Short: "Deprecated alias for bridge service enable|disable|status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runService(cmd, g, func(e *service.Env, r service.Runner) error {
				action := "status"
				if len(args) == 1 {
					action = args[0]
				}
				return service.Autostart(cmd.Context(), e, r, action, cmd.OutOrStdout(), cmd.ErrOrStderr())
			})
		},
	}
}

// movedVerbs are the pre-service-namespace top-level lifecycle commands.
// The bash router rejected them with BB-E305; the Go CLI does the same.
var movedVerbs = []string{"up", "down", "restart", "status", "logs", "update", "doctor", "uninstall", "version"}

// registerMovedVerbs adds the BB-E305 stubs for top-level lifecycle verbs.
func registerMovedVerbs(root *cobra.Command, g *globals) {
	for _, verb := range movedVerbs {
		root.AddCommand(&cobra.Command{
			Use:   verb,
			Short: fmt.Sprintf("(moved to 'bridge service %s')", verb),
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fail(cmd, g, "BB-E305", fmt.Sprintf("BB-E305: '%s' has moved: lifecycle commands live under 'bridge service' — try 'bridge service %s'.", verb, verb))
			},
		})
	}
}

// runService resolves the environment and runner, runs fn, and maps the
// error onto the TS-style failure output: --json gets the BB-E### code as
// the error kind, human mode gets "Error: BB-E###: ..." on stderr.
func runService(cmd *cobra.Command, g *globals, fn func(e *service.Env, r service.Runner) error) error {
	e, err := service.EnvFromOSEnv()
	if err != nil {
		return fail(cmd, g, "service_error", err.Error())
	}
	if err := fn(e, service.ExecRunner{}); err != nil {
		if errors.Is(err, service.ErrSilent) {
			return service.ErrSilent
		}
		kind := "service_error"
		var ce *service.CodedError
		if errors.As(err, &ce) {
			kind = ce.Code
		}
		return fail(cmd, g, kind, err.Error())
	}
	return nil
}
