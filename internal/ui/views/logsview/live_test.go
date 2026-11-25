package logsview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// harness wraps the view in a root model so notifications are visible, and
// records scheduled live ticks instead of waiting for them.
type harness struct {
	d     *uitest.Driver
	view  *Model
	clock *clock
	ticks []tea.Msg
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	h := &harness{clock: &clock{time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}}
	src := fake.NewLogs(8, h.clock.now)
	th := theme.New(true)
	opts.Source, opts.Histogram, opts.Theme, opts.Now = src, src, th, h.clock.now
	opts.After = func(_ time.Duration, msg tea.Msg) tea.Cmd {
		h.ticks = append(h.ticks, msg)
		return nil
	}
	h.view = New(opts)
	h.d = uitest.New(t, app.New("test", th, app.Tab{Title: "Logs", View: h.view}), 120, 30)
	return h
}

// tick advances the clock and fires the last scheduled poll.
func (h *harness) tick(t *testing.T, d time.Duration) {
	t.Helper()
	h.clock.t = h.clock.t.Add(d)
	if len(h.ticks) == 0 {
		t.Fatal("no poll scheduled")
	}
	msg := h.ticks[len(h.ticks)-1]
	h.ticks = nil
	h.d.Send(msg)
}

func TestLiveAppendsNewLogsOnTop(t *testing.T) {
	h := newHarness(t, Options{})
	before := len(h.view.records)
	newest := h.view.records[0].Timestamp

	h.d.Keys("L")
	if !strings.Contains(h.d.Screen(), "LIVE ●") || len(h.ticks) != 1 {
		t.Fatalf("live not started (ticks %d):\n%s", len(h.ticks), h.d.Screen())
	}
	h.tick(t, 10*time.Second)
	added := len(h.view.records) - before
	if added == 0 || !h.view.records[0].Timestamp.After(newest) {
		t.Fatalf("no new logs on top (added %d)", added)
	}
	if h.view.cursor != 0 {
		t.Error("selection on the newest log must follow the stream")
	}
	if !strings.Contains(h.d.Screen(), "live") {
		t.Errorf("live counter missing:\n%s", h.d.Screen())
	}

	// A selection further down stays on the same record.
	h.d.Keys("j", "j", "j")
	selected := h.view.records[h.view.cursor].Fingerprint()
	h.tick(t, 10*time.Second)
	if h.view.records[h.view.cursor].Fingerprint() != selected {
		t.Error("selection jumped while new logs arrived")
	}

	h.d.Keys("L")
	if h.view.tail != nil || strings.Contains(h.d.Screen(), "LIVE") {
		t.Fatal("live not stopped")
	}
}

func TestLivePauseResume(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("L")
	h.d.Keys("space")
	count := len(h.view.records)
	h.tick(t, 10*time.Second)
	if len(h.view.records) != count {
		t.Fatal("paused live must not add logs")
	}
	if s := h.d.Screen(); !strings.Contains(s, "PAUSED +") {
		t.Fatalf("paused badge missing:\n%s", s)
	}
	h.d.Keys("space")
	if len(h.view.records) <= count || strings.Contains(h.d.Screen(), "PAUSED") {
		t.Fatal("resume must flush the buffer")
	}
}

func TestLiveOnAbsoluteRangeSwitchesToRelative(t *testing.T) {
	h := newHarness(t, Options{Range: obs.Between(
		time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC), time.Date(2026, 11, 24, 11, 0, 0, 0, time.UTC))})
	h.d.Keys("L")
	if h.view.rng != liveRange || h.view.tail == nil {
		t.Fatalf("range %v, tail %v", h.view.rng, h.view.tail)
	}
	if !strings.Contains(h.d.Screen(), "time range set to last 15m") {
		t.Errorf("user not told about the range change:\n%s", h.d.Screen())
	}
}

func TestStaleResultsAreDropped(t *testing.T) {
	h := newHarness(t, Options{})
	stale := h.view.searchID
	h.d.Keys("r")
	count := len(h.view.records)
	h.d.Send(pageMsg{owner: owner{h.view.id}, id: stale, items: h.view.records[:3]})
	h.d.Send(tailMsg{owner: owner{h.view.id}, gen: h.view.liveGen - 1})
	if len(h.view.records) != count {
		t.Fatal("results of a previous search must be ignored")
	}
}

func TestCopyAndUTC(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("y")
	if !strings.Contains(h.d.Screen(), "log copied") {
		t.Error("copy not confirmed")
	}
	h.d.Keys("z", "enter")
	if s := h.d.Screen(); !strings.Contains(s, "UTC") {
		t.Errorf("UTC toggle not visible:\n%s", s)
	}
}

func TestOutdatedHistogramIsIgnored(t *testing.T) {
	h := newHarness(t, Options{})
	if len(h.view.hist) == 0 || h.view.histCols == 0 {
		t.Fatal("histogram not loaded")
	}
	current := h.view.hist
	h.d.Send(histMsg{owner: owner{h.view.id}, id: h.view.searchID, cols: h.view.histCols + 7, from: h.view.histFrom, to: h.view.histTo, series: nil})
	if len(h.view.hist) != len(current) {
		t.Fatal("a response to an outdated request replaced the timeline")
	}
}
