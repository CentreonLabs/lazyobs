// Package cli wires the command line: flags, subcommands and the TUI launch.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CentreonLabs/lazyobs/internal/config"
	"github.com/CentreonLabs/lazyobs/internal/version"
)

type app struct {
	env       config.Env
	out, err  io.Writer
	overrides config.Overrides
}

// NewRootCommand builds the lazyobs command tree.
func NewRootCommand(env config.Env, stdout, stderr io.Writer) *cobra.Command {
	a := &app{env: env, out: stdout, err: stderr}
	var tui tuiOptions

	root := &cobra.Command{
		Use:   "lazyobs",
		Short: "Terminal UI for the Centreon observability platform",
		Long: `lazyobs lets you search, filter and follow your observability data
(logs first, traces and metrics next) from the terminal.

Configuration comes from command-line flags, then the LAZYOBS_URL,
LAZYOBS_ORG and LAZYOBS_TOKEN environment variables, then the current
context of the configuration file.`,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		Example: `  lazyobs                                   # reopen the tabs of the last session
  lazyobs --fresh                           # start over: one tab, last 15 minutes
  lazyobs --range 24h --query 'severity_text:ERROR'
  lazyobs --context staging
  lazyobs --demo                            # synthetic data, no account needed`,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runTUI(cmd.Context(), tui) },
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("{{.Version}}\n")

	f := root.PersistentFlags()
	f.StringVar(&a.overrides.ConfigPath, "config", "", "configuration file (default $LAZYOBS_CONFIG or ~/.config/lazyobs/config.yaml)")
	f.StringVar(&a.overrides.Context, "context", "", "context to use (default $LAZYOBS_CONTEXT or current-context)")
	f.StringVar(&a.overrides.Org, "org", "", "organization code (overrides the context)")
	f.StringVar(&a.overrides.URL, "url", "", "API base URL (overrides the context)")

	root.Flags().BoolVar(&tui.demo, "demo", false, "explore synthetic data without connecting to the platform")
	root.Flags().BoolVar(&tui.fresh, "fresh", false, "start a new session instead of reopening the last one's tabs")
	root.Flags().StringVarP(&tui.query, "query", "q", "", "initial query (Quickwit syntax)")
	root.Flags().StringVarP(&tui.rng, "range", "r", "", `initial time range: 15m, 24h, 7d or "2026-11-03 13:00 .. 2026-11-03 14:00"`)

	root.AddCommand(newVersionCommand(a), newConfigCommand(a))
	return root
}

// resolve loads the effective configuration and prints its warnings.
func (a *app) resolve(ctx context.Context) (*config.Resolved, error) {
	r, err := config.Resolve(ctx, a.overrides, a.env)
	if r != nil {
		for _, w := range r.Warnings {
			fmt.Fprintln(a.err, "warning:", w)
		}
	}
	return r, err
}

func newVersionCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Fprintln(a.out, version.String())
		},
	}
}
