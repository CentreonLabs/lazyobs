package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/config"
)

const checkTimeout = 20 * time.Second

func newConfigCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage contexts and check the connection",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the configuration file path",
			Args:  cobra.NoArgs,
			Run: func(*cobra.Command, []string) {
				fmt.Fprintln(a.out, a.configPath())
			},
		},
		&cobra.Command{
			Use:   "view",
			Short: "Show the effective configuration and where each value comes from",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return a.viewConfig(cmd.Context()) },
		},
		&cobra.Command{
			Use:     "get-contexts",
			Aliases: []string{"contexts"},
			Short:   "List the contexts",
			Args:    cobra.NoArgs,
			RunE:    func(*cobra.Command, []string) error { return a.getContexts() },
		},
		&cobra.Command{
			Use:   "use-context NAME",
			Short: "Set the current context",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if err := config.UseContext(a.configPath(), args[0]); err != nil {
					return err
				}
				fmt.Fprintf(a.out, "Switched to context %q.\n", args[0])
				return nil
			},
		},
		newSetContextCommand(a),
		&cobra.Command{
			Use:   "check",
			Short: "Verify that the platform is reachable with the current configuration",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return a.check(cmd.Context()) },
		},
	)
	return cmd
}

func newSetContextCommand(a *app) *cobra.Command {
	var url, org, tokenFile, tokenCommand string
	cmd := &cobra.Command{
		Use:   "set-context NAME",
		Short: "Create or update a context",
		Long: `Create or update a context. Only the flags given are changed; an empty
value removes the setting. The first context created becomes the current one.

The token itself is never passed on the command line (it would land in the
shell history): store it in a file readable only by you and use --token-file,
or fetch it from a password manager with --token-command.`,
		Example: `  lazyobs config set-context prod --org my-org --token-file ~/.config/lazyobs/token
  lazyobs config set-context prod --token-command "op read op://vault/lazyobs/token"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("token-file") && cmd.Flags().Changed("token-command") {
				return fmt.Errorf("--token-file and --token-command are mutually exclusive")
			}
			var u config.ContextUpdate
			changed := func(name string, v *string) *string {
				if cmd.Flags().Changed(name) {
					return v
				}
				return nil
			}
			u.URL, u.Org = changed("url", &url), changed("org", &org)
			u.TokenFile, u.TokenCommand = changed("token-file", &tokenFile), changed("token-command", &tokenCommand)
			if err := config.SetContext(a.configPath(), args[0], u); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Context %q saved in %s.\n", args[0], a.configPath())
			return nil
		},
	}
	f := cmd.Flags()
	// These local flags shadow the global --url/--org on purpose: here they edit the context.
	f.StringVar(&url, "url", "", "API base URL")
	f.StringVar(&org, "org", "", "organization code")
	f.StringVar(&tokenFile, "token-file", "", "file containing the API token")
	f.StringVar(&tokenCommand, "token-command", "", "command printing the API token")
	return cmd
}

func (a *app) configPath() string { return config.Path(a.overrides, a.env.Getenv) }

func (a *app) viewConfig(ctx context.Context) error {
	r, err := a.resolve(ctx)
	if r == nil {
		return err
	}
	found := ""
	if !r.ConfigFound {
		found = " (not found)"
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "config file\t%s%s\n", r.ConfigPath, found)
	fmt.Fprintf(w, "context\t%s\n", orNone(r.Context))
	fmt.Fprintf(w, "url\t%s\t(%s)\n", r.URL, r.URLSource)
	fmt.Fprintf(w, "org\t%s\t(%s)\n", orNone(r.Org), orNone(r.OrgSource))
	token := "<none>"
	if !r.Token.IsZero() {
		token = "set" // never print the value, not even partially
	}
	fmt.Fprintf(w, "token\t%s\t(%s)\n", token, orNone(r.TokenSource))
	if err := w.Flush(); err != nil {
		return err
	}
	return err
}

func (a *app) getContexts() error {
	path := a.configPath()
	f, found, err := config.ReadFile(path)
	if err != nil {
		return err
	}
	if !found || len(f.Contexts) == 0 {
		fmt.Fprintf(a.out, "No context defined in %s. Create one with `lazyobs config set-context`.\n", path)
		return nil
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CURRENT\tNAME\tORG\tURL")
	for _, name := range f.Names() {
		c := f.Contexts[name]
		mark := ""
		if name == f.CurrentContext {
			mark = "*"
		}
		url := c.URL
		if url == "" {
			url = config.DefaultURL
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", mark, name, c.Org, url)
	}
	return w.Flush()
}

func (a *app) check(ctx context.Context) error {
	r, err := a.resolve(ctx)
	if err != nil {
		return err
	}
	client, err := api.New(r.URL, r.Org, r.Token)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	start := int64(-60)
	began := time.Now()
	if _, err := client.SearchLogs(ctx, api.LogSearchRequest{Query: "*", Start: &start, MaxHits: 0}); err != nil {
		return fmt.Errorf("cannot query logs of organization %q at %s: %w", r.Org, r.URL, err)
	}
	fmt.Fprintf(a.out, "OK: organization %q reachable at %s (%s)\n", r.Org, r.URL, time.Since(began).Round(time.Millisecond))
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
