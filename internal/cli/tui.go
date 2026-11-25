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
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/session"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/tracesview"
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
	obs.Breakdowner
}

// traceSource is what the traces view needs from a backend.
type traceSource interface {
	obs.Searcher[traces.Trace]
	obs.Histogrammer
	traces.Fetcher
}

// Signal tab titles, also used to route between them.
const (
	signalLogs   = "Logs"
	signalTraces = "Traces"
)

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
		tsrc       traceSource
		fields     obs.FieldSuggester
		translator obs.QueryTranslator
		label      string
		sessionKey string // what the saved session belongs to
	)
	if opts.demo {
		seed := uint64(time.Now().UnixNano()) //nolint:gosec // seed only
		f, ft := fake.NewLogs(seed, nil), fake.NewTraces(seed, nil)
		f.Latency, ft.Latency = 150*time.Millisecond, 150*time.Millisecond
		src, tsrc, fields, translator, label, sessionKey = f, ft, f, fake.Translator{}, "demo data", "demo"
	} else {
		r, err := a.resolve(ctx)
		if err != nil {
			return err
		}
		client, err := api.New(r.URL, r.Org, r.Token)
		if err != nil {
			return err
		}
		src, tsrc = remote.NewLogs(client, nil), remote.NewTraces(client, nil)
		translator = remote.NewTranslator(client, nil)
		fields = remote.NewFields(client)
		label = r.Org
		sessionKey = "org:" + r.Org + "@" + r.URL
		if r.Context != "" {
			label = fmt.Sprintf("%s · %s", r.Context, r.Org)
			sessionKey = "context:" + r.Context
		}
	}

	th := theme.New(true) // refined once the terminal reports its background
	logOpts, traceOpts := linkSignals(
		logsview.Options{Source: src, Histogram: src, Breakdown: src, Fields: fields, Translator: translator, Theme: th},
		tracesview.Options{Source: tsrc, Histogram: tsrc, Fetcher: tsrc, Theme: th},
	)
	store := session.Store{Path: session.DefaultPath(a.env.Getenv)}
	tracesKey := sessionKey + "#traces"

	// Reopen the last session unless the command line asks for a new search.
	var (
		logTabs, traceTabs     []tabset.Child
		logActive, traceActive int
		signal                 int
		notice                 string
		noticeLevel            = tui.LevelInfo
	)
	if !opts.fresh && opts.query == "" && opts.rng == "" {
		saved, ok, err := store.Load(sessionKey)
		switch {
		case err != nil:
			notice, noticeLevel = fmt.Sprintf("could not restore the last session: %v", err), tui.LevelWarn
		case ok:
			logTabs, logActive = restoreTabs(saved, func(s logsview.State) tabset.Child { return logsview.Restore(logOpts, s) })
			signal = saved.Signal
			if savedTraces, ok, _ := store.Load(tracesKey); ok {
				traceTabs, traceActive = restoreTabs(savedTraces, func(s tracesview.State) tabset.Child { return tracesview.Restore(traceOpts, s) })
			}
			if n := len(logTabs) + len(traceTabs); n > 0 {
				notice = fmt.Sprintf("restored %d tab%s from the last session · start with --fresh to begin anew", n, plural(n))
			}
		}
	}
	if len(logTabs) == 0 {
		logOpts.Query, logOpts.Range = opts.query, rng
		logTabs = []tabset.Child{logsview.New(logOpts)}
	}
	if len(traceTabs) == 0 {
		traceTabs = []tabset.Child{tracesview.New(traceOpts)}
	}

	logsSet := tabset.NewWithTabs(signalLogs, th, logTabs, logActive)
	tracesSet := tabset.NewWithTabs(signalTraces, th, traceTabs, traceActive)
	root := tui.New(label, th,
		tui.Tab{Title: signalLogs, View: logsSet},
		tui.Tab{Title: signalTraces, View: tracesSet},
		tui.Tab{Title: "Metrics"},
	)
	root.SetActive(signal)
	if notice != "" {
		root.Notify(noticeLevel, "%s", notice)
	}
	_, runErr := tea.NewProgram(root, tea.WithContext(ctx)).Run()

	// Save even after an interrupt: the state is in memory either way.
	logState := snapshotTabs(logsSet, (*logsview.Model).Snapshot)
	logState.Signal = root.Active()
	for _, err := range []error{
		store.Save(sessionKey, logState),
		store.Save(tracesKey, snapshotTabs(tracesSet, (*tracesview.Model).Snapshot)),
	} {
		if err != nil {
			fmt.Fprintln(a.err, "warning: could not save the session:", err)
			break
		}
	}
	if runErr != nil && !errors.Is(runErr, tea.ErrProgramKilled) && !errors.Is(runErr, tea.ErrInterrupted) {
		return runErr
	}
	return nil
}

// linkSignals lets each view open the other signal: a log's trace in a new
// traces tab, a trace's logs in a new logs tab.
func linkSignals(l logsview.Options, t tracesview.Options) (logsview.Options, tracesview.Options) {
	l.OpenTrace = func(traceID string, at time.Time) tea.Cmd {
		return tui.Route(signalTraces, tabset.NewTabMsg{Build: func(cur tabset.Child) tabset.Child {
			return cur.(*tracesview.Model).TraceTab(traceID, at)
		}})
	}
	t.OpenLogs = func(query string, rng obs.TimeRange) tea.Cmd {
		return tui.Route(signalLogs, tabset.NewTabMsg{Build: func(cur tabset.Child) tabset.Child {
			return cur.(*logsview.Model).NewSearch(query, rng)
		}})
	}
	return l, t
}

// restoreTabs rebuilds the saved tabs of a signal, skipping any that cannot be read.
func restoreTabs[S any](saved session.Session, build func(S) tabset.Child) (tabs []tabset.Child, active int) {
	active = saved.Active
	for i, raw := range saved.Tabs {
		var s S
		if err := json.Unmarshal(raw, &s); err != nil {
			if i < saved.Active {
				active--
			}
			continue
		}
		tabs = append(tabs, build(s))
	}
	return tabs, active
}

// snapshotTabs saves the tabs of a signal whose views are of type V.
func snapshotTabs[V tabset.Child, S any](t *tabset.Model, snapshot func(V) S) session.Session {
	s := session.Session{Active: t.Active()}
	for _, tab := range t.Tabs() {
		if v, ok := tab.(V); ok {
			if b, err := json.Marshal(snapshot(v)); err == nil {
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
