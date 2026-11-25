package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/api"
	tui "github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/session"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
)

// tuiOptions are the flags of the root command.
type tuiOptions struct {
	demo  bool
	fresh bool // ignore the saved session
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
		sessionKey string // what the saved session belongs to
	)
	if opts.demo {
		f := fake.NewLogs(uint64(time.Now().UnixNano()), nil) //nolint:gosec // seed only
		f.Latency = 150 * time.Millisecond
		src, translator, label, sessionKey = f, fake.Translator{}, "demo data", "demo"
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
		sessionKey = "org:" + r.Org + "@" + r.URL
		if r.Context != "" {
			label = fmt.Sprintf("%s · %s", r.Context, r.Org)
			sessionKey = "context:" + r.Context
		}
	}

	th := theme.New(true) // refined once the terminal reports its background
	viewOpts := logsview.Options{Source: src, Histogram: src, Translator: translator, Theme: th}
	store := session.Store{Path: session.DefaultPath(a.env.Getenv)}

	// Reopen the last session unless the command line asks for a new search.
	var (
		tabs        []tabset.Child
		active      int
		notice      string
		noticeLevel = tui.LevelInfo
	)
	if !opts.fresh && opts.query == "" && opts.rng == "" {
		saved, ok, err := store.Load(sessionKey)
		switch {
		case err != nil:
			notice, noticeLevel = fmt.Sprintf("could not restore the last session: %v", err), tui.LevelWarn
		case ok:
			tabs, active = restoreTabs(viewOpts, saved)
			if n := len(tabs); n > 0 {
				notice = fmt.Sprintf("restored %d tab%s from the last session · start with --fresh to begin anew", n, plural(n))
			}
		}
	}
	if len(tabs) == 0 {
		viewOpts.Query, viewOpts.Range = opts.query, rng
		tabs = []tabset.Child{logsview.New(viewOpts)}
	}

	logsTabs := tabset.NewWithTabs("Logs", th, tabs, active)
	root := tui.New(label, th,
		tui.Tab{Title: "Logs", View: logsTabs},
		tui.Tab{Title: "Traces"},
		tui.Tab{Title: "Metrics"},
	)
	if notice != "" {
		root.Notify(noticeLevel, "%s", notice)
	}
	_, runErr := tea.NewProgram(root, tea.WithContext(ctx)).Run()

	// Save even after an interrupt: the state is in memory either way.
	if err := store.Save(sessionKey, snapshotTabs(logsTabs)); err != nil {
		fmt.Fprintln(a.err, "warning: could not save the session:", err)
	}
	if runErr != nil && !errors.Is(runErr, tea.ErrProgramKilled) && !errors.Is(runErr, tea.ErrInterrupted) {
		return runErr
	}
	return nil
}

// restoreTabs rebuilds the saved tabs, skipping any that cannot be read.
func restoreTabs(opts logsview.Options, saved session.Session) (tabs []tabset.Child, active int) {
	active = saved.Active
	for i, raw := range saved.Tabs {
		var s logsview.State
		if err := json.Unmarshal(raw, &s); err != nil {
			if i < saved.Active {
				active--
			}
			continue
		}
		tabs = append(tabs, logsview.Restore(opts, s))
	}
	return tabs, active
}

func snapshotTabs(t *tabset.Model) session.Session {
	s := session.Session{Active: t.Active()}
	for _, tab := range t.Tabs() {
		if v, ok := tab.(*logsview.Model); ok {
			if b, err := json.Marshal(v.Snapshot()); err == nil {
				s.Tabs = append(s.Tabs, b)
			}
		}
	}
	return s
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}
