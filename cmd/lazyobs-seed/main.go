// Command lazyobs-seed fills an organization with the demo data of
// `lazyobs --demo` (linked logs and traces) through the OTLP ingestion
// endpoint. It is a development tool: ingested data cannot be deleted and
// stays until the end of the retention period.
//
// The token comes from the lazyobs configuration (current context, or
// --context) or LAZYOBS_TOKEN; the organization is the token's.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/config"
	"github.com/CentreonLabs/lazyobs/internal/seed"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var (
		since    = flag.Duration("since", time.Hour, "seed the data of this period, ending now")
		batch    = flag.Duration("batch", 5*time.Minute, "period of data sent per request")
		seedVal  = flag.Uint64("seed", 2026, "random seed of the demo data")
		endpoint = flag.String("endpoint", seed.DefaultEndpoint, "OTLP/HTTP base URL")
		ctxName  = flag.String("context", "", "lazyobs configuration context holding the token")
		dryRun   = flag.Bool("dry-run", false, "generate and count the data without sending it")
	)
	flag.Parse()

	to := time.Now().Truncate(time.Second)
	opts := seed.Options{Seed: *seedVal, From: to.Add(-*since), To: to, Batch: *batch, Progress: os.Stdout}
	if *dryRun {
		opts.Exporter = discard{}
	} else {
		r, err := config.Resolve(ctx, config.Overrides{Context: *ctxName}, config.OSEnv)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lazyobs-seed:", err)
			return 1
		}
		if r.Token.IsZero() {
			fmt.Fprintln(os.Stderr, "lazyobs-seed: no token configured")
			return 1
		}
		fmt.Printf("seeding %s of demo data for org %q (context %q) into %s\n", *since, r.Org, r.Context, *endpoint)
		opts.Exporter = &seed.Sender{Endpoint: *endpoint, Token: r.Token, Retries: 8}
	}
	tot, err := seed.Run(ctx, opts)
	fmt.Printf("sent %d logs, %d traces, %d spans\n", tot.Logs, tot.Traces, tot.Spans)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazyobs-seed:", err)
		return 1
	}
	return 0
}

type discard struct{}

func (discard) Logs(context.Context, map[string]any) error   { return nil }
func (discard) Traces(context.Context, map[string]any) error { return nil }
