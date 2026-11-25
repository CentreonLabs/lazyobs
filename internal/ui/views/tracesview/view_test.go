package tracesview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type harness struct {
	d        *uitest.Driver
	view     *Model
	logsAsks []string
}

func newHarness(t *testing.T, build func(Options) *Model) *harness {
	t.Helper()
	h := &harness{}
	src := fake.NewTraces(5, func() time.Time { return now })
	th := theme.New(true)
	opts := Options{
		Source: src, Histogram: src, Fetcher: src, Theme: th, Now: func() time.Time { return now },
		OpenLogs: func(query string, rng obs.TimeRange) tea.Cmd {
			h.logsAsks = append(h.logsAsks, query+" @ "+rng.String())
			return nil
		},
	}
	if build == nil {
		build = New
	}
	h.view = build(opts)
	h.d = uitest.New(t, app.New("test", th, app.Tab{Title: "Traces", View: h.view}), 140, 40)
	return h
}

func TestTraceList(t *testing.T) {
	h := newHarness(t, nil)
	s := h.d.Screen()
	for _, want := range []string{"Traces over time", "Traces · newest first", "DURATION", "SERVICE", "OPERATION", "demo-gateway", "traces ·"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen misses %q", want)
		}
	}
	if len(h.view.items) == 0 {
		t.Fatal("no trace listed")
	}
	h.d.Keys("s")
	if h.view.order != obs.OrderSlowest || !strings.Contains(h.d.Screen(), "slowest first") {
		t.Fatal("s must sort by duration")
	}
	for i := 1; i < len(h.view.items); i++ {
		if h.view.items[i].Root.Duration > h.view.items[i-1].Root.Duration {
			t.Fatal("not slowest first")
		}
	}

	h.d.Keys("/")
	h.d.Type("span_status.code:error")
	h.d.Keys("enter")
	if len(h.view.items) == 0 {
		t.Fatal("expected failed traces in the demo")
	}
	for _, tr := range h.view.items {
		if tr.Root.Status != traces.StatusError {
			t.Fatal("filter not applied")
		}
	}
	if !strings.Contains(h.d.Screen(), "●") {
		t.Error("failed traces must be marked")
	}
}

func TestWaterfall(t *testing.T) {
	h := newHarness(t, nil)
	root := h.view.Selected().Root
	h.d.Keys("enter")
	if h.view.trace == nil || h.view.trace.loading || len(h.view.trace.nodes) < 3 {
		t.Fatalf("trace not loaded: %+v", h.view.trace)
	}
	s := h.d.Screen()
	for _, want := range []string{root.Name, "spans", "services", "Spans", "Span", "operation", "duration", "█", root.TraceID} {
		if !strings.Contains(s, want) {
			t.Errorf("waterfall misses %q:\n%s", want, s)
		}
	}
	// Children are indented under their parent.
	nodes := h.view.trace.nodes
	if nodes[0].Depth != 0 || nodes[1].Depth != 1 || !strings.Contains(s, "  "+nodes[1].Service) {
		t.Errorf("tree not indented: %+v", nodes[:2])
	}

	h.d.Keys("j", "j")
	sel := h.view.trace.nodes[h.view.trace.selected]
	if h.view.trace.selected != 2 || !strings.Contains(h.d.Screen(), sel.Name) {
		t.Fatal("j must select the next span")
	}

	h.d.Keys("l")
	if len(h.logsAsks) != 1 || !strings.HasPrefix(h.logsAsks[0], "trace_id:"+root.TraceID) {
		t.Fatalf("logs of the trace: %v", h.logsAsks)
	}

	h.d.Keys("esc")
	if h.view.trace != nil || !strings.Contains(h.d.Screen(), "Traces over time") {
		t.Fatal("esc must go back to the list")
	}
}

func TestForTraceAndState(t *testing.T) {
	src := fake.NewTraces(5, func() time.Time { return now })
	list, err := src.Search(t.Context(), obs.Query{Range: obs.Last(time.Hour), Limit: 1})
	if err != nil || len(list.Items) == 0 {
		t.Fatal("no demo trace")
	}
	root := list.Items[0].Root
	h := newHarness(t, func(o Options) *Model { return ForTrace(o, root.TraceID, root.Start) })
	if h.view.trace == nil || len(h.view.trace.nodes) == 0 || !strings.Contains(h.d.Screen(), root.TraceID) {
		t.Fatalf("trace not opened:\n%s", h.d.Screen())
	}
	if !strings.HasPrefix(h.view.Title(), "⌁ ") {
		t.Errorf("title = %q", h.view.Title())
	}

	s := h.view.Snapshot()
	if s.Trace != root.TraceID || s.Range.IsRelative() {
		t.Fatalf("snapshot = %+v", s)
	}
	r := newHarness(t, func(o Options) *Model { return Restore(o, s) })
	if r.view.trace == nil || r.view.trace.id != root.TraceID || len(r.view.trace.nodes) == 0 {
		t.Fatal("restored tab must reopen its trace")
	}
}

func TestChartZoomAndHover(t *testing.T) {
	h := newHarness(t, nil)
	_ = h.d.Screen()
	hits := h.view.hits
	if hits.chart.PlotW == 0 {
		t.Fatal("no chart")
	}
	y := hits.plotY + 2 + 1 // body row → screen row
	h.d.Send(tea.MouseMotionMsg{X: hits.plotX + 10, Y: y})
	if !strings.Contains(h.d.Screen(), "total") {
		t.Error("hovering must show the tooltip")
	}
	h.d.Click(hits.plotX+5, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseReleaseMsg{X: hits.plotX + 30, Y: y, Button: tea.MouseLeft})
	if h.view.rng.IsRelative() || len(h.view.zoomStack) != 1 {
		t.Fatalf("drag must zoom: %v", h.view.rng)
	}
	h.d.Keys("-")
	if h.view.rng != DefaultRange {
		t.Fatal("- must zoom out")
	}
}
