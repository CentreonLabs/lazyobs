// Command lazyobs is a terminal UI for the Centreon observability platform.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/CentreonLabs/lazyobs/internal/cli"
	"github.com/CentreonLabs/lazyobs/internal/config"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli.NewRootCommand(config.OSEnv, os.Stdout, os.Stderr).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "lazyobs:", err)
		return 1
	}
	return 0
}
