// Package cli wires the command line: flags, subcommands and the TUI launch.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CentreonLabs/lazyobs/internal/config"
	"github.com/CentreonLabs/lazyobs/internal/version"
)

// errTUINotReady is returned until the interface lands.
var errTUINotReady = errors.New("the interface is not implemented yet; see `lazyobs config --help`")

type app struct {
	env       config.Env
	out, err  io.Writer
	overrides config.Overrides
}

// NewRootCommand builds the lazyobs command tree.
func NewRootCommand(env config.Env, stdout, stderr io.Writer) *cobra.Command {
	a := &app{env: env, out: stdout, err: stderr}

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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := a.resolve(cmd.Context()); err != nil {
				return err
			}
			return errTUINotReady
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetVersionTemplate("{{.Version}}\n")

	f := root.PersistentFlags()
	f.StringVar(&a.overrides.ConfigPath, "config", "", "configuration file (default $LAZYOBS_CONFIG or ~/.config/lazyobs/config.yaml)")
	f.StringVar(&a.overrides.Context, "context", "", "context to use (default $LAZYOBS_CONTEXT or current-context)")
	f.StringVar(&a.overrides.Org, "org", "", "organization code (overrides the context)")
	f.StringVar(&a.overrides.URL, "url", "", "API base URL (overrides the context)")

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
