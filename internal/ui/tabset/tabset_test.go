package tabset_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
)

type harness struct {
	d     *uitest.Driver
	tabs  *tabset.Model
	now   time.Time
	ticks []tea.Msg
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{now: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return h.now }
	src := fake.NewLogs(3, clock)
	th := theme.New(true)
	view := logsview.New(logsview.Options{
		Source: src, Histogram: src, Theme: th, Now: clock,
		After: func(_ time.Duration, msg tea.Msg) tea.Cmd { h.ticks = append(h.ticks, msg); return nil },
	})
	h.tabs = tabset.New("Logs", th, view)
	h.d = uitest.New(t, app.New("test", th, app.Tab{Title: "Logs", View: h.tabs}), 120, 36)
	return h
}

func (h *harness) bar() string { return strings.Split(h.d.Screen(), "\n")[1] }

func TestNewTabStartsFromTheCurrentOne(t *testing.T) {
	h := newHarness(t)
	if !strings.Contains(h.bar(), "1 all logs · 15m") || !strings.Contains(h.bar(), "+") {
		t.Fatalf("tab bar = %q", h.bar())
	}

	h.d.Keys("ctrl+t")
	if h.tabs.Len() != 2 || h.tabs.Active() != 1 || !h.tabs.Capturing() {
		t.Fatalf("len %d, active %d, capturing %v", h.tabs.Len(), h.tabs.Active(), h.tabs.Capturing())
	}
	h.d.Type("service_name:auth")
	h.d.Keys("enter")
	if bar := h.bar(); !strings.Contains(bar, "2 service_name:auth · 15m") || !strings.Contains(bar, "1 all logs") {
		t.Fatalf("tab bar = %q", bar)
	}
	// Each tab shows the results of its own search, even though async
	// results are broadcast to every tab.
	for _, l := range strings.Split(h.d.Screen(), "\n") {
		if strings.Contains(l, "11-25 ") && !strings.Contains(l, " auth ") {
			t.Fatalf("tab 2 shows a log of another search: %q", l)
		}
	}
	h.d.Keys("<")
	if h.tabs.Active() != 0 || !strings.Contains(h.d.Screen(), "query * (press / to search)") {
		t.Fatalf("back on tab 1:\n%s", h.d.Screen())
	}
}

func TestTimeRangesAreLocalToTabs(t *testing.T) {
	h := newHarness(t)
	h.d.Keys("ctrl+t", "esc", "[")
	quarter := 15 * time.Minute / 4
	want := h.now.Add(-15*time.Minute-quarter).Local().Format("15:04") + "→" + h.now.Add(-quarter).Local().Format("15:04")
	if !strings.Contains(h.bar(), "2 all logs · "+want) {
		t.Fatalf("tab 2 should have moved back in time: %q", h.bar())
	}
	if !strings.Contains(h.bar(), "1 all logs · 15m") {
		t.Fatalf("tab 1 must keep its range: %q", h.bar())
	}
	h.d.Keys(">") // wraps around to tab 1
	if h.tabs.Active() != 0 || !strings.Contains(h.d.Screen(), "range last 15m") {
		t.Fatal("tab 1 range changed")
	}
}

func TestLiveKeepsRunningInBackgroundTabs(t *testing.T) {
	h := newHarness(t)
	h.d.Keys("L")
	if !strings.Contains(h.bar(), "1 ● all logs") {
		t.Fatalf("live tab must be marked: %q", h.bar())
	}
	h.d.Keys("ctrl+t", "esc")
	h.d.Type("/")
	h.d.Type("service_name:auth")
	h.d.Keys("enter")
	before := h.d.Screen()

	// Fire the background tab's poll after some time.
	h.now = h.now.Add(10 * time.Second)
	var poll tea.Msg
	for _, m := range h.ticks {
		poll = m
	}
	if poll == nil {
		t.Fatal("no poll scheduled")
	}
	h.d.Send(poll)
	if h.d.Screen() != before {
		t.Error("a background poll must not change the visible tab")
	}
	h.d.Keys("<")
	if !strings.Contains(h.d.Screen(), "live") {
		t.Errorf("tab 1 should have received live logs:\n%s", h.d.Screen())
	}
}

func TestCloseTabs(t *testing.T) {
	h := newHarness(t)
	h.d.Keys("ctrl+w")
	if h.tabs.Len() != 1 || !strings.Contains(h.d.Screen(), "this is the last tab") {
		t.Fatal("the last tab must not close")
	}
	h.d.Keys("ctrl+t", "esc", "ctrl+t", "esc")
	h.d.Keys("<")
	h.d.Keys("ctrl+w")
	if h.tabs.Len() != 2 || h.tabs.Active() != 1 {
		t.Fatalf("len %d active %d", h.tabs.Len(), h.tabs.Active())
	}
}

func TestTabBarMouse(t *testing.T) {
	h := newHarness(t)
	x, y, _ := h.d.Find("+")
	h.d.Click(x, y, tea.MouseLeft, 0)
	h.d.Keys("esc")
	if h.tabs.Len() != 2 || h.tabs.Active() != 1 {
		t.Fatalf("click on + must open a tab, len %d", h.tabs.Len())
	}
	x, y, _ = h.d.Find("1 all logs")
	h.d.Click(x+2, y, tea.MouseLeft, 0)
	if h.tabs.Active() != 0 {
		t.Fatal("click on a tab must select it")
	}
	x, y, _ = h.d.Find("×")
	h.d.Click(x, y, tea.MouseLeft, 0)
	if h.tabs.Len() != 1 {
		t.Fatal("click on × must close the tab")
	}

	// Clicks below the bar still reach the view, shifted by the bar.
	_, header, _ := h.d.Find("TIME ")
	h.d.Click(10, header+3, tea.MouseLeft, 0)
	if !strings.Contains(h.d.Screen(), "range last 15m") {
		t.Fatal("view broken after clicks")
	}
}

func TestMaxTabs(t *testing.T) {
	h := newHarness(t)
	for range tabset.MaxTabs + 1 {
		h.d.Keys("ctrl+t", "esc")
	}
	if h.tabs.Len() != tabset.MaxTabs || !strings.Contains(h.d.Screen(), "at most") {
		t.Fatalf("len %d", h.tabs.Len())
	}
}

func TestTitlesWinOverTheHint(t *testing.T) {
	h := newHarness(t)
	h.d.Keys("ctrl+t")
	h.d.Type("service_name:auth")
	h.d.Keys("enter", "[")
	if bar := h.bar(); strings.Contains(bar, "…") {
		t.Errorf("titles must not be cut while the hint takes room: %q", bar)
	}
	if bar := h.bar(); !strings.Contains(bar, "2 service_name:auth · ") {
		t.Errorf("tab bar = %q", bar)
	}
}

func TestRestoredTabs(t *testing.T) {
	th := theme.New(true)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	src := fake.NewLogs(3, func() time.Time { return now })
	opts := logsview.Options{Source: src, Histogram: src, Theme: th, Now: func() time.Time { return now }}
	tabs := []tabset.Child{
		logsview.Restore(opts, logsview.State{Range: logsview.DefaultRange}),
		logsview.Restore(opts, logsview.State{Query: "service_name:auth", Range: logsview.DefaultRange}),
	}
	set := tabset.NewWithTabs("Logs", th, tabs, 1)
	d := uitest.New(t, app.New("test", th, app.Tab{Title: "Logs", View: set}), 120, 36)
	if set.Active() != 1 || !strings.Contains(d.Screen(), "query service_name:auth") {
		t.Fatalf("active tab not restored:\n%s", d.Screen())
	}
	// Every restored tab has searched, not only the visible one.
	d.Keys("<")
	if !strings.Contains(d.Screen(), " logs · ") {
		t.Errorf("background tab did not search:\n%s", d.Screen())
	}
}

func TestOpenLogsAroundOne(t *testing.T) {
	h := newHarness(t)
	h.d.Keys("j", "j", "j")
	origin := h.tabs.Tab(0).(*logsview.Model).Selected()

	h.d.Keys("c")
	if h.tabs.Len() != 2 || h.tabs.Active() != 1 {
		t.Fatalf("len %d, active %d", h.tabs.Len(), h.tabs.Active())
	}
	ctx := h.tabs.Tab(1).(*logsview.Model)
	s := ctx.Snapshot()
	if s.Query != "service_name:"+origin.Service {
		t.Errorf("query = %q, want the service of the log", s.Query)
	}
	if s.Range.IsRelative() || s.Range.From.After(origin.Timestamp.Add(-29*time.Second)) || s.Range.To.Before(origin.Timestamp.Add(30*time.Second)) {
		t.Errorf("range %v is not ±30s around %v", s.Range, origin.Timestamp)
	}
	if got := ctx.Selected(); got.Fingerprint() != origin.Fingerprint() {
		t.Fatalf("the original log must be selected in the new tab, got %v", got.Timestamp)
	}
	screen := h.d.Screen()
	if !strings.Contains(screen, "◆") || !strings.Contains(screen, "Details") || !strings.Contains(screen, "logs around") {
		t.Errorf("anchor mark, detail panel or notice missing:\n%s", screen)
	}

	h.d.Keys("<")
	if h.tabs.Tab(0).(*logsview.Model).Selected().Fingerprint() != origin.Fingerprint() {
		t.Error("the first tab must keep its selection")
	}
}
