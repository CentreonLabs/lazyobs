package logsview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBreakdownPanel(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("b")
	if h.view.mode != modeBreakdown || h.view.input.Value() != "service_name" {
		t.Fatalf("picker: mode %v, value %q", h.view.mode, h.view.input.Value())
	}
	h.d.Keys("enter")
	s := h.d.Screen()
	if h.view.mode != modeNormal || !h.view.breakdownShown() || !strings.Contains(s, "Top service_name") {
		t.Fatalf("panel not shown:\n%s", s)
	}
	rows := h.view.breakdownRows()
	var sum int64
	for _, r := range rows {
		sum += r.count
		if !strings.Contains(s, r.value) {
			t.Errorf("value %q not displayed", r.value)
		}
	}
	if len(rows) != 5 || sum != h.view.bd.data.Total || !strings.Contains(s, "%") || !strings.Contains(s, "█") {
		t.Fatalf("rows %+v (sum %d, total %d)", rows, sum, h.view.bd.data.Total)
	}

	h.d.Keys("tab") // no detail panel: the list, then the top values
	if h.view.focus != focusBreakdown {
		t.Fatalf("focus = %v", h.view.focus)
	}
	h.d.Keys("j")
	second := h.view.breakdownRows()[1].value
	h.d.Keys("+")
	if h.view.query != "service_name:"+second {
		t.Fatalf("query = %q", h.view.query)
	}
	if rows := h.view.breakdownRows(); len(rows) != 1 || rows[0].value != second {
		t.Errorf("the panel must follow the query: %+v", rows)
	}

	h.d.Keys("-") // the same value, excluded: flips the filter
	if h.view.query != "NOT service_name:"+second {
		t.Fatalf("query = %q", h.view.query)
	}
	for _, r := range h.view.breakdownRows() {
		if r.value == second {
			t.Fatal("the excluded value is still counted")
		}
	}

	h.d.Keys("esc")
	if h.view.bd.field != "" || h.view.focus != focusList || strings.Contains(h.d.Screen(), "Top service_name") {
		t.Fatal("esc must close the panel")
	}
}

func TestBreakdownPickerRejectsNonAggregatableFields(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("b")
	h.d.Keys("ctrl+u")
	h.d.Type("body.message") // not aggregatable: no suggestion either
	if h.view.compl.open() {
		t.Fatalf("suggested for a non-aggregatable field: %v", labels(h.view.compl.items))
	}
	h.d.Keys("enter")
	if h.view.mode != modeBreakdown || h.view.bd.field != "" || !strings.Contains(h.d.Screen(), "cannot be broken down") {
		t.Fatalf("mode %v, field %q", h.view.mode, h.view.bd.field)
	}
}

func TestBreakdownPickerSuggestsAggregatableFields(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("b", "ctrl+u")
	h.d.Type("ver")
	for _, s := range h.view.compl.items {
		if f, _ := h.view.field(s.label); !f.Aggregatable {
			t.Errorf("non-aggregatable field suggested: %s", s.label)
		}
	}
	if !h.view.compl.open() {
		t.Fatal("no suggestion for ver")
	}
	h.d.Keys("enter") // completes
	h.d.Keys("enter") // shows
	if h.view.bd.field != "resource_attributes.service.version" {
		t.Fatalf("field = %q", h.view.bd.field)
	}
}

func TestBreakdownClickAndNarrowTerminal(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("b", "enter")
	hits := h.rendered()
	h.d.Click(hits.bdX+2, screenY(hits.bdTop+2), tea.MouseLeft, 0)
	if h.view.focus != focusBreakdown || h.view.bd.selected != 2 {
		t.Fatalf("click: focus %v, selected %d", h.view.focus, h.view.bd.selected)
	}
	h.d.Send(tea.WindowSizeMsg{Width: 80, Height: 30})
	if h.view.breakdownShown() || strings.Contains(h.d.Screen(), "Top service_name") {
		t.Error("no room for the panel on a narrow terminal")
	}
}

func TestBreakdownIsSaved(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("b", "enter")
	s := h.view.Snapshot()
	if s.Breakdown != "service_name" {
		t.Fatalf("snapshot = %+v", s)
	}
	r := newHarnessFrom(t, s)
	if !strings.Contains(r.d.Screen(), "Top service_name") || len(r.view.breakdownRows()) == 0 {
		t.Fatalf("panel not restored:\n%s", r.d.Screen())
	}
}
