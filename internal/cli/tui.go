package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/api"
	tui "github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
)

// tuiOptions are the flags of the root command.
type tuiOptions struct {
	demo  bool
	query string
	rng   string
}

// logSource is what the logs view needs from a backend.
type logSource interface {
	obs.Searcher[logs.Record]
	obs.Histogrammer
}

func (a *app) runTUI(ctx context.Context, opts tuiOptions) error {
	rng := logsview.DefaultRange
	if opts.rng != "" {
		var err error
		if rng, err = obs.ParseTimeRange(opts.rng, time.Local); err != nil {
			return err
		}
	}

	var (
		src        logSource
		translator obs.QueryTranslator
		label      string
	)
	if opts.demo {
		f := fake.NewLogs(uint64(time.Now().UnixNano()), nil) //nolint:gosec // seed only
		f.Latency = 150 * time.Millisecond
		src, translator, label = f, fake.Translator{}, "demo data"
	} else {
		r, err := a.resolve(ctx)
		if err != nil {
			return err
		}
		client, err := api.New(r.URL, r.Org, r.Token)
		if err != nil {
			return err
		}
		src = remote.NewLogs(client, nil)
		translator = remote.NewTranslator(client, nil)
		label = r.Org
		if r.Context != "" {
			label = fmt.Sprintf("%s · %s", r.Context, r.Org)
		}
	}

	th := theme.New(true) // refined once the terminal reports its background
	logsView := logsview.New(logsview.Options{
		Source: src, Histogram: src, Translator: translator,
		Theme: th, Query: opts.query, Range: rng,
	})
	root := tui.New(label, th,
		tui.Tab{Title: "Logs", View: logsView},
		tui.Tab{Title: "Traces"},
		tui.Tab{Title: "Metrics"},
	)
	if _, err := tea.NewProgram(root, tea.WithContext(ctx)).Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return err
	}
	return nil
}
