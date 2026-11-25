package logsview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func visible(m *Model) []string {
	var out []string
	for _, n := range m.seriesNames() {
		if !m.hidden[n] {
			out = append(out, n)
		}
	}
	return out
}

func TestLegendClicks(t *testing.T) {
	h := newHarness(t, Options{})
	if len(h.view.seriesNames()) < 3 {
		t.Fatalf("series = %v", h.view.seriesNames())
	}
	x, y, ok := h.d.Find("━━ ERROR")
	if !ok {
		t.Fatalf("legend not found:\n%s", h.d.Screen())
	}

	h.d.Click(x+4, y, tea.MouseLeft, 0)
	if !h.view.hidden["ERROR"] {
		t.Fatal("click must hide ERROR")
	}
	if _, _, ok := h.d.Find("── ERROR"); !ok {
		t.Errorf("hidden level must stay in the legend, dimmed:\n%s", h.d.Screen())
	}
	h.d.Click(x+4, y, tea.MouseLeft, 0)
	if h.view.hidden["ERROR"] {
		t.Fatal("second click must show ERROR again")
	}

	ix, iy, _ := h.d.Find("━━ INFO")
	h.d.Click(ix+4, iy, tea.MouseRight, 0)
	if v := visible(h.view); len(v) != 1 || v[0] != "INFO" {
		t.Fatalf("right-click must show only INFO, visible = %v", v)
	}
	h.d.Click(ix+4, iy, tea.MouseRight, 0)
	if len(h.view.hidden) != 0 {
		t.Fatalf("right-click on the only level must show all, hidden = %v", h.view.hidden)
	}
	h.d.Click(ix+4, iy, tea.MouseLeft, tea.ModShift)
	if v := visible(h.view); len(v) != 1 || v[0] != "INFO" {
		t.Fatalf("shift+click (when the terminal forwards it) must show only INFO, visible = %v", v)
	}

	// Clicking between entries does nothing.
	h.view.hidden = map[string]bool{}
	h.d.Click(x-2, y, tea.MouseLeft, 0)
	if len(h.view.hidden) != 0 {
		t.Error("click outside legend entries must be ignored")
	}
}

func TestListClickAndWheel(t *testing.T) {
	h := newHarness(t, Options{})
	_, header, ok := h.d.Find("TIME ")
	if !ok {
		t.Fatal("list header not found")
	}
	h.d.Click(10, header+4, tea.MouseLeft, 0)
	if h.view.cursor != 3 {
		t.Fatalf("click on the 4th row selected %d", h.view.cursor)
	}
	h.d.Wheel(10, header+2, tea.MouseWheelDown)
	if h.view.cursor != 3+wheelStep {
		t.Fatalf("wheel down moved to %d", h.view.cursor)
	}
	h.d.Wheel(10, header+2, tea.MouseWheelUp)
	h.d.Wheel(10, header+2, tea.MouseWheelUp)
	if h.view.cursor != 0 {
		t.Fatalf("wheel up must stop at the top, cursor %d", h.view.cursor)
	}
}
