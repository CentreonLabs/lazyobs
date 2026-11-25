package logsview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

// clickChart clicks plot column col and returns the bucket under it.
func (h *harness) clickChart(t *testing.T, col int) (start, end time.Time) {
	t.Helper()
	hits := h.rendered()
	x, y := hits.plotX+col, screenY(hits.plotY+2)
	h.d.Click(x, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	colSpan := h.view.histTo.Sub(h.view.histFrom) / time.Duration(hits.plotW)
	at := hits.timeAt(col, h.view.histFrom, h.view.histTo).Add(colSpan / 2)
	start, end, _, _ = chart.BucketAt(h.view.hist, h.view.histFrom, h.view.histTo, at)
	return start, end
}

func TestClickOnChartJumpsToTheBucket(t *testing.T) {
	h := newHarness(t, Options{})
	hits := h.rendered()
	start, end := h.clickChart(t, hits.plotW*9/10)

	sel := h.view.Selected()
	if !sel.Timestamp.Before(end) || sel.Timestamp.Before(start.Add(-time.Minute)) {
		t.Fatalf("selected %v, bucket %v → %v", sel.Timestamp, start, end)
	}
	if h.view.cursor > 0 && h.view.records[h.view.cursor-1].Timestamp.Before(end) {
		t.Error("the newest log of the bucket must be selected, not an older one")
	}
	if h.view.rng != DefaultRange {
		t.Error("a click must not zoom")
	}
	if s := h.d.Screen(); !strings.Contains(s, "bucket ") || !strings.Contains(s, " logs") {
		t.Errorf("notice missing:\n%s", s[strings.LastIndex(s, "\n")+1:])
	}
	// The selected log is in view.
	if h.view.cursor < h.view.top || h.view.cursor >= h.view.top+h.view.listRows() {
		t.Errorf("cursor %d not visible (top %d)", h.view.cursor, h.view.top)
	}
}

func TestClickLoadsPagesUntilTheBucket(t *testing.T) {
	h := newHarness(t, Options{PageSize: 50})
	hits := h.rendered()
	loaded := len(h.view.records)
	_, end := h.clickChart(t, hits.plotW/2)
	if len(h.view.records) <= loaded {
		t.Fatal("pages should have been loaded to reach the bucket")
	}
	if !h.view.Selected().Timestamp.Before(end) || h.view.records[h.view.cursor-1].Timestamp.Before(end) {
		t.Errorf("wrong log selected: %v (bucket end %v)", h.view.Selected().Timestamp, end)
	}
}

func TestClickFarDownTheListRestartsTheListThere(t *testing.T) {
	h := newHarness(t, Options{PageSize: 5})
	histFrom, histTo, hist := h.view.histFrom, h.view.histTo, len(h.view.hist)
	start, end := h.clickChart(t, 2)

	if h.view.listEnd.IsZero() || !h.view.listEnd.Equal(end) {
		t.Fatalf("list should start at the bucket end %v, got %v", end, h.view.listEnd)
	}
	sel := h.view.Selected()
	if h.view.cursor != 0 || !sel.Timestamp.Before(end) || sel.Timestamp.Before(start.Add(-time.Minute)) {
		t.Fatalf("selected #%d at %v, bucket %v → %v", h.view.cursor, sel.Timestamp, start, end)
	}
	if !h.view.histFrom.Equal(histFrom) || !h.view.histTo.Equal(histTo) || len(h.view.hist) != hist || h.view.rng != DefaultRange {
		t.Error("the timeline and the time range must not change")
	}
	if s := h.d.Screen(); !strings.Contains(s, "r for the latest") {
		t.Errorf("the bar must say the list starts later:\n%s", s)
	}
	h.d.Keys("r")
	if !h.view.listEnd.IsZero() || h.view.Selected().Timestamp.Before(end) {
		t.Error("r must go back to the latest logs")
	}
}

func TestClickForwardAfterARestartedList(t *testing.T) {
	h := newHarness(t, Options{PageSize: 5})
	h.clickChart(t, 2) // far back: the list restarts there
	if h.view.listEnd.IsZero() {
		t.Fatal("precondition: the list should have restarted")
	}
	hits := h.rendered()
	start, end := h.clickChart(t, hits.plotW/2) // forward in time
	sel := h.view.Selected()
	if !sel.Timestamp.Before(end) || sel.Timestamp.Before(start.Add(-time.Minute)) {
		t.Fatalf("clicking forward selected %v, bucket %v → %v", sel.Timestamp, start, end)
	}

	_, end = h.clickChart(t, hits.plotW-1) // the latest bucket
	if !h.view.listEnd.IsZero() {
		t.Errorf("the latest bucket must bring the normal list back, list end %v", h.view.listEnd)
	}
	if !h.view.Selected().Timestamp.Before(end) {
		t.Errorf("selected %v after the bucket end %v", h.view.Selected().Timestamp, end)
	}
}
