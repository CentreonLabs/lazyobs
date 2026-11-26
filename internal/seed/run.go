package seed

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

// Exporter receives the export requests; *Sender is the real one.
type Exporter interface {
	Logs(ctx context.Context, req map[string]any) error
	Traces(ctx context.Context, req map[string]any) error
}

// Options describe what to seed.
type Options struct {
	Seed     uint64
	From, To time.Time     // whole seconds in [From, To) are generated
	Batch    time.Duration // span of data per request
	Exporter Exporter
	Progress io.Writer // optional, one line per batch
}

// Totals counts what was sent.
type Totals struct{ Logs, Traces, Spans int }

// Run generates the demo logs and traces of the window, batch by batch, and
// exports them. Logs and traces share the seed, so logs refer to the traces.
func Run(ctx context.Context, o Options) (Totals, error) {
	var tot Totals
	lg, tr := fake.NewLogs(o.Seed, nil), fake.NewTraces(o.Seed, nil)
	batch := max(o.Batch, time.Second)
	for start := o.From.Truncate(time.Second); start.Before(o.To); start = start.Add(batch) {
		end := minTime(start.Add(batch), o.To)
		var recs []logs.Record
		var ds []traces.Detail
		for sec := start.Unix(); sec < end.Unix(); sec++ {
			recs = append(recs, lg.At(sec)...)
			ds = append(ds, tr.At(sec)...)
		}
		if len(recs) > 0 {
			if err := o.Exporter.Logs(ctx, LogsRequest(recs)); err != nil {
				return tot, fmt.Errorf("logs of %s: %w", start.Format(time.RFC3339), err)
			}
		}
		if len(ds) > 0 {
			if err := o.Exporter.Traces(ctx, TracesRequest(ds)); err != nil {
				return tot, fmt.Errorf("traces of %s: %w", start.Format(time.RFC3339), err)
			}
		}
		tot.Logs += len(recs)
		tot.Traces += len(ds)
		for _, d := range ds {
			tot.Spans += len(d.Spans)
		}
		if o.Progress != nil {
			_, _ = fmt.Fprintf(o.Progress, "%s  %5d logs  %4d traces  (total %d logs, %d traces, %d spans)\n",
				start.UTC().Format("2006-01-02 15:04"), len(recs), len(ds), tot.Logs, tot.Traces, tot.Spans)
		}
	}
	return tot, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
