package alertsview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
)

// 14:05:30: the evaluation of 14:05 covers the incident of 14:00.
var now = time.Date(2026, 11, 25, 14, 5, 30, 0, time.UTC)

type harness struct {
	d        *uitest.Driver
	view     *Model
	src      alerts.Reader
	ticks    []tea.Msg
	logsAsks []string
}

func newHarness(t *testing.T, width int, src alerts.Reader, build func(Options) *Model) *harness {
	t.Helper()
	clock := func() time.Time { return now }
	if src == nil {
		src = fake.NewAlerts(fake.NewLogs(2026, clock), clock)
	}
	h := &harness{src: src}
	th := theme.New(true)
	opts := Options{
		Source: src, Theme: th, Now: clock,
		After: func(_ time.Duration, msg tea.Msg) tea.Cmd { h.ticks = append(h.ticks, msg); return nil },
		OpenLogs: func(query string, rng obs.TimeRange) tea.Cmd {
			h.logsAsks = append(h.logsAsks, query+" @ "+rng.String())
			return nil
		},
	}
	if build == nil {
		build = New
	}
	h.view = build(opts)
	h.view.utc = true
	h.d = uitest.New(t, app.New("test", th, app.Tab{Title: "Alerts", View: h.view}), width, 30)
	return h
}

func (h *harness) contains(t *testing.T, want ...string) {
	t.Helper()
	s := h.d.Screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("screen misses %q:\n%s", w, s)
		}
	}
}

func TestAlertListWorstFirst(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	h.contains(t, "Alert rules", "STATUS", "RULE", "CHECKED", "CRITICAL", "Error burst", "log count", "5m", "14:05:00",
		"⚠ evaluation failed", "◌ downtime Auth migration", "✓ acknowledged by oncall@example.com", "disabled",
		"● 3 critical", "○ 1 disabled", "1 Alerts ●2")
	if first := h.view.items[0]; first.Status() != alerts.StatusCritical {
		t.Errorf("first = %v", first.Status())
	}
	if last := h.view.items[len(h.view.items)-1]; last.Rule.Enabled {
		t.Error("disabled rules come last")
	}
}

func TestAlertDetailsAndLogs(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	h.d.Keys("j", "enter")
	a := h.view.Selected()
	h.contains(t, "status       CRITICAL", "checked 14:05:00", "thresholds", "→ critical", "last count", "notifies", "Runbook", "l: logs of the query")
	h.d.Keys("l")
	want := "severity_text:ERROR @ last 1h"
	if a.Rule.Name != "Error burst" || len(h.logsAsks) != 1 || h.logsAsks[0] != want {
		t.Errorf("rule %q, logs asks = %v, want %q", a.Rule.Name, h.logsAsks, want)
	}
	h.d.Keys("esc")
	if h.view.detailOpen {
		t.Error("esc must hide the details")
	}
}

func TestLogsLoadTheRuleFirst(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	h.d.Keys("j", "l") // details not loaded yet
	if len(h.logsAsks) != 1 || !strings.Contains(h.logsAsks[0], " @ last 1h") {
		t.Errorf("logs asks = %v", h.logsAsks)
	}
}

func TestNarrowDetailsBelow(t *testing.T) {
	h := newHarness(t, 60, nil, nil)
	h.d.Keys("enter")
	if h.view.layout().side {
		t.Error("details must be below the list on a narrow screen")
	}
	// The dividend query is wrapped, its second line aligned with the first.
	h.contains(t, "thresholds", "dividend     service_name:checkout AND", "\n│             severity_text:ERROR")
}

func TestFiltersAndProblems(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	total := len(h.view.items)
	h.d.Keys("p")
	for _, a := range h.view.items {
		if !problem(a) {
			t.Errorf("%q is not a problem", a.Rule.Name)
		}
	}
	if len(h.view.items) == 0 || len(h.view.items) >= total {
		t.Fatalf("problems only = %d of %d", len(h.view.items), total)
	}
	h.contains(t, "problems only")
	h.d.Keys("p", "/")
	h.d.Type("ratio")
	if len(h.view.items) != 2 {
		t.Errorf("filter as you type: %d rules", len(h.view.items))
	}
	h.d.Keys("enter")
	h.contains(t, "“ratio”", "2 of 7")
	h.d.Keys("esc")
	if len(h.view.items) != total {
		t.Error("esc must clear the filter")
	}
}

func TestRefreshKeepsSelectionAndIgnoresStaleTicks(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	h.d.Keys("j", "j")
	selected := h.view.Selected().Rule.ID
	if len(h.ticks) != 1 {
		t.Fatalf("ticks = %d", len(h.ticks))
	}
	stale := h.ticks[0]
	h.d.Keys("r") // a manual refresh plans a new tick
	h.d.Send(stale)
	if len(h.ticks) != 2 {
		t.Errorf("a stale tick must not refresh: %d ticks", len(h.ticks))
	}
	h.d.Send(h.ticks[1])
	if len(h.ticks) != 3 || h.view.Selected().Rule.ID != selected {
		t.Errorf("ticks = %d, selected %q, want %q", len(h.ticks), h.view.Selected().Rule.ID, selected)
	}
}

func TestMouse(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	_ = h.d.Screen()
	x, y, ok := h.d.Find("Payment declines")
	if !ok {
		t.Fatal("rule not shown")
	}
	h.d.Click(x, y, tea.MouseLeft, 0)
	if h.view.Selected().Rule.Name != "Payment declines" || h.view.detailOpen {
		t.Fatalf("selected %q", h.view.Selected().Rule.Name)
	}
	h.d.Click(x, y, tea.MouseLeft, 0)
	if !h.view.detailOpen {
		t.Error("a click on the selected rule opens its details")
	}
	h.d.Wheel(x, y, tea.MouseWheelDown)
	if h.view.Selected().Rule.Name == "Payment declines" {
		t.Error("the wheel moves the selection")
	}
}

func TestSessionState(t *testing.T) {
	h := newHarness(t, 160, nil, nil)
	h.d.Keys("p", "j", "enter")
	st := h.view.Snapshot()
	if !st.ProblemsOnly || !st.Details || st.Rule == "" {
		t.Fatalf("state = %+v", st)
	}
	r := newHarness(t, 160, nil, func(o Options) *Model { return Restore(o, st) })
	if r.view.Selected().Rule.ID != st.Rule || !r.view.detailOpen || !r.view.problemsOnly {
		t.Errorf("restored %+v", r.view.Snapshot())
	}
}

type failingAlerts struct{}

func (failingAlerts) Alerts(context.Context) (alerts.Overview, error) {
	return alerts.Overview{}, errors.New("401 unauthorized")
}

func (failingAlerts) Details(context.Context, alerts.Alert) (alerts.Details, error) {
	return alerts.Details{}, errors.New("nope")
}

type emptyAlerts struct{ warnings []string }

func (e emptyAlerts) Alerts(context.Context) (alerts.Overview, error) {
	return alerts.Overview{Warnings: e.warnings}, nil
}

func (emptyAlerts) Details(context.Context, alerts.Alert) (alerts.Details, error) {
	return alerts.Details{}, nil
}

func TestErrorsAndEmpty(t *testing.T) {
	h := newHarness(t, 160, failingAlerts{}, nil)
	h.contains(t, "Could not read the alerts: 401 unauthorized", "could not read the alerts")

	e := newHarness(t, 160, emptyAlerts{warnings: []string{"could not read the downtimes: 403"}}, nil)
	e.contains(t, "No alert rule in this organization yet", "could not read the downtimes")
}
