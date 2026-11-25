package cli

import (
	"strings"
	"testing"
	"time"

	tui "github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/tracesview"
)

func TestNavigateBetweenLogsAndTraces(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	logSrc, traceSrc := fake.NewLogs(9, clock), fake.NewTraces(9, clock)
	th := theme.New(true)
	logOpts, traceOpts := linkSignals(
		logsview.Options{Source: logSrc, Histogram: logSrc, Theme: th, Now: clock},
		tracesview.Options{Source: traceSrc, Histogram: traceSrc, Fetcher: traceSrc, Theme: th, Now: clock},
	)
	logs := tabset.New(signalLogs, th, logsview.New(logOpts))
	traces := tabset.New(signalTraces, th, tracesview.New(traceOpts))
	root := tui.New("test", th, tui.Tab{Title: signalLogs, View: logs}, tui.Tab{Title: signalTraces, View: traces})
	d := uitest.New(t, root, 140, 40)

	// Find a log pointing at a demo trace and open its trace.
	view := logs.Tab(0).(*logsview.Model)
	for i := 0; view.Selected().TraceID == "" || !strings.HasPrefix(view.Selected().TraceID, "0000"); i++ {
		if i > 200 {
			t.Fatal("no log with a demo trace ID")
		}
		d.Keys("j")
	}
	log := view.Selected()
	d.Keys("T")
	if root.Active() != 1 || traces.Len() != 2 || traces.Active() != 1 {
		t.Fatalf("T must open a traces tab: signal %d, %d tabs", root.Active(), traces.Len())
	}
	tv := traces.Tab(1).(*tracesview.Model)
	if s := tv.Snapshot(); s.Trace != log.TraceID {
		t.Fatalf("opened trace %q, want %q", s.Trace, log.TraceID)
	}
	if !strings.Contains(d.Screen(), log.TraceID) || !strings.Contains(d.Screen(), "Spans") {
		t.Fatalf("waterfall not shown:\n%s", d.Screen())
	}

	// And back: the logs of that trace.
	d.Keys("l")
	if root.Active() != 0 || logs.Len() != 2 {
		t.Fatalf("l must open a logs tab: signal %d, %d tabs", root.Active(), logs.Len())
	}
	lv := logs.Tab(1).(*logsview.Model)
	if q := lv.Snapshot().Query; q != "trace_id:"+log.TraceID {
		t.Fatalf("logs query = %q", q)
	}
	found := false
	for i := 0; i < 50 && !found; i++ {
		found = lv.Selected().Fingerprint() == log.Fingerprint()
		d.Keys("j")
	}
	if !found {
		t.Error("the log we started from must be among the logs of its trace")
	}
}
