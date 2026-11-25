package logsview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

// screenY converts a body row to a screen row (below the header line).
func screenY(bodyY int) int { return bodyY + 1 }

// rendered returns the clickable areas of a freshly rendered frame, as the
// real program renders after every update.
func (h *harness) rendered() hitAreas {
	_ = h.d.Screen()
	return h.view.hits
}

func TestDragToZoomAndZoomOut(t *testing.T) {
	h := newHarness(t, Options{})
	hits := h.rendered()
	if hits.plotW == 0 {
		t.Fatalf("no plot area recorded:\n%s", h.d.Screen())
	}
	y := screenY(hits.plotY + 2)
	x0, x1 := hits.plotX+10, hits.plotX+hits.plotW/2

	h.d.Click(x0, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseMotionMsg{X: x1, Y: y, Button: tea.MouseLeft})
	if h.view.drag == nil || h.view.drag.to-h.view.drag.from != x1-x0 {
		t.Fatalf("drag = %+v", h.view.drag)
	}
	before := h.view.histFrom
	span := h.view.histTo.Sub(h.view.histFrom)
	h.d.Send(tea.MouseReleaseMsg{X: x1, Y: y, Button: tea.MouseLeft})

	rng := h.view.rng
	if rng.IsRelative() || h.view.drag != nil {
		t.Fatalf("drag must zoom on an absolute range, got %v", rng)
	}
	wantFrom := before.Add(time.Duration(float64(span) * 10 / float64(hits.plotW)))
	if d := rng.From.Sub(wantFrom).Abs(); d > 2*time.Second {
		t.Errorf("zoom starts at %v, want about %v", rng.From, wantFrom)
	}
	if rng.To.Sub(rng.From) >= span/2 {
		t.Errorf("zoomed span %v is not narrower than half of %v", rng.To.Sub(rng.From), span)
	}
	if !strings.Contains(h.d.Screen(), "press - to zoom out") {
		t.Errorf("zoom out hint missing:\n%s", h.d.Screen())
	}

	h.d.Keys("-")
	if h.view.rng != DefaultRange || len(h.view.zoomStack) != 0 {
		t.Fatalf("zoom out must restore the previous range, got %v", h.view.rng)
	}
	h.d.Keys("-")
	if h.view.rng != obs.Last(30*time.Minute) {
		t.Fatalf("zoom out without history must double the range, got %v", h.view.rng)
	}
}

func TestClickOnChartDoesNotZoom(t *testing.T) {
	h := newHarness(t, Options{})
	hits := h.rendered()
	x, y := hits.plotX+5, screenY(hits.plotY+1)
	h.d.Click(x, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if h.view.rng != DefaultRange || h.view.drag != nil {
		t.Fatalf("a click must not zoom, range %v", h.view.rng)
	}
}

func TestZoomStopsLive(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("L")
	hits := h.rendered()
	y := screenY(hits.plotY + 1)
	h.d.Click(hits.plotX+2, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseReleaseMsg{X: hits.plotX + 20, Y: y, Button: tea.MouseLeft})
	if h.view.live || h.view.tail != nil {
		t.Fatal("zooming on a past span must stop live mode")
	}
}

func TestAskInPlainWords(t *testing.T) {
	h := newHarness(t, Options{Translator: fake.Translator{}})
	h.d.Keys("a")
	if !strings.Contains(h.d.Screen(), "ask › ") {
		t.Fatalf("ask input missing:\n%s", h.d.Screen())
	}
	h.d.Type("errors from checkout in the last 2 hours")
	h.d.Keys("enter")
	if h.view.query != "severity_text:ERROR AND service_name:checkout" || h.view.rng != obs.Last(2*time.Hour) {
		t.Fatalf("query %q, range %v", h.view.query, h.view.rng)
	}
	s := h.d.Screen()
	if !strings.Contains(s, "translated to: severity_text:ERROR") || !strings.Contains(s, "range last 2h") {
		t.Errorf("translation not shown:\n%s", s)
	}
	for _, r := range h.view.records {
		if r.Service != "checkout" {
			t.Fatalf("search not rerun with the translated query: %s", r.Service)
		}
	}

	// The prompt is kept for the next question; a vague one warns.
	h.d.Keys("a")
	if h.view.input.Value() != "errors from checkout in the last 2 hours" {
		t.Errorf("last prompt not recalled: %q", h.view.input.Value())
	}
	h.d.Keys("esc")
}

func TestAskWithoutTranslator(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("a")
	if h.view.mode != modeNormal || !strings.Contains(h.d.Screen(), "not available") {
		t.Fatalf("missing translator must be reported:\n%s", h.d.Screen())
	}
}

func TestStatusBarShortcutsFollowState(t *testing.T) {
	h := newHarness(t, Options{Translator: fake.Translator{}})
	s := h.d.Screen()
	last := s[strings.LastIndex(s, "\n")+1:]
	for _, want := range []string{"/ query", "a ask", "t range", "L live", "? toggle help"} {
		if !strings.Contains(last, want) {
			t.Errorf("status bar misses %q: %q", want, last)
		}
	}
	if strings.Contains(last, "pause") || strings.Contains(last, "zoom out") {
		t.Errorf("pause and zoom out only apply in live mode / after a zoom: %q", last)
	}
	h.d.Keys("?")
	if !strings.Contains(h.d.Screen(), "ask in plain words") {
		t.Error("the help screen must keep the full description")
	}
	h.d.Keys("?")
	h.d.Keys("L")
	if s := h.d.Screen(); !strings.Contains(s[strings.LastIndex(s, "\n")+1:], "space pause") {
		t.Error("pause must be listed while live")
	}

	plain := newHarness(t, Options{})
	if strings.Contains(plain.d.Screen(), "a ask") {
		t.Error("ask must not be listed without a translator")
	}
}

func TestDragHighlightsTheSelectionAsTheMouseMoves(t *testing.T) {
	h := newHarness(t, Options{})
	hits := h.rendered()
	y := screenY(hits.plotY + 2)
	x0 := hits.plotX + 10
	frame := func() string { return h.d.Model.View().Content }

	before := frame()
	h.d.Click(x0, y, tea.MouseLeft, 0)
	h.d.Send(tea.MouseMotionMsg{X: x0 + 5, Y: y, Button: tea.MouseLeft})
	short := frame()
	h.d.Send(tea.MouseMotionMsg{X: x0 + 25, Y: y, Button: tea.MouseLeft})
	long := frame()

	if short == before || long == short {
		t.Fatal("each mouse move must redraw the highlighted span")
	}
	body := func(s string) string { s = ansi.Strip(s); return s[:strings.LastIndex(s, "\n")] } // without the status bar
	if body(long) != body(before) {
		t.Error("the highlight must only change colors, not the content")
	}
	if len(long) <= len(short) {
		t.Error("a wider selection should style more cells")
	}
	if s := h.d.Screen(); !strings.Contains(s, "→") || !strings.Contains(s, "release to zoom") {
		t.Errorf("the selected span must be shown while dragging:\n%s", s[strings.LastIndex(s, "\n")+1:])
	}

	// Dragging back left of the anchor selects the other side.
	h.d.Send(tea.MouseMotionMsg{X: x0 - 6, Y: y, Button: tea.MouseLeft})
	if d := h.view.drag; d.from != 4 || d.to != 10 || !d.start.Before(d.end) {
		t.Errorf("drag = %+v", d)
	}
	h.d.Send(tea.MouseReleaseMsg{X: x0 - 6, Y: y, Button: tea.MouseLeft})
	if h.view.drag != nil {
		t.Error("release must end the selection")
	}
}
