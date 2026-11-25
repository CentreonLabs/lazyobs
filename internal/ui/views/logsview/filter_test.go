package logsview

import (
	"strings"
	"testing"
)

func TestAddTerm(t *testing.T) {
	cases := []struct {
		query, term, want string
		added             bool
	}{
		{"", "service_name:auth", "service_name:auth", true},
		{"*", "service_name:auth", "service_name:auth", true},
		{"severity_text:ERROR", "service_name:auth", "severity_text:ERROR AND service_name:auth", true},
		{"a:1 OR b:2", "c:3", "(a:1 OR b:2) AND c:3", true},
		{"(a:1 OR b:2) AND d:4", "c:3", "(a:1 OR b:2) AND d:4 AND c:3", true},
		{`msg:"x OR y"`, "c:3", `msg:"x OR y" AND c:3`, true},
		{"service_name:auth AND x:1", "service_name:auth", "service_name:auth AND x:1", false},
		{"service_name:authz", "service_name:auth", "service_name:authz AND service_name:auth", true},
		{"NOT service_name:auth", "service_name:auth", "service_name:auth", true},
		{"x:1 AND service_name:auth AND y:2", "NOT service_name:auth", "x:1 AND NOT service_name:auth AND y:2", true},
		{"x:1 AND NOT service_name:auth", "service_name:auth", "x:1 AND service_name:auth", true},
		{"KNOT service_name:auth", "service_name:auth", "KNOT service_name:auth", false}, // KNOT is a word, not NOT
		{"x:1 AND NOT service_name:auth", "NOT service_name:auth", "x:1 AND NOT service_name:auth", false},
	}
	for _, tc := range cases {
		got, added := addTerm(tc.query, tc.term)
		if got != tc.want || added != tc.added {
			t.Errorf("addTerm(%q, %q) = %q, %v; want %q, %v", tc.query, tc.term, got, added, tc.want, tc.added)
		}
	}
}

// selectField moves the detail selection to the entry of field.
func (h *harness) selectField(t *testing.T, field string) filterEntry {
	t.Helper()
	for i := 0; i < 50; i++ {
		e := h.view.detailEntries[h.view.detailField]
		if e.field == field {
			return e
		}
		h.d.Keys("j")
	}
	t.Fatalf("no %s field in the detail panel", field)
	return filterEntry{}
}

func TestFilterFromDetail(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("enter", "tab")
	if !h.view.detailFocused() || len(h.view.detailEntries) < 5 {
		t.Fatalf("detail focus %v, entries %d", h.view.detailFocused(), len(h.view.detailEntries))
	}
	s := h.d.Screen()
	if last := s[strings.LastIndex(s, "\n")+1:]; !strings.Contains(last, "+ filter on field") || !strings.Contains(last, "- exclude") {
		t.Errorf("status bar must explain the filters: %q", last)
	}

	svc := h.selectField(t, "service_name")
	h.d.Keys("+")
	if h.view.query != "service_name:"+svc.value || len(h.view.records) == 0 {
		t.Fatalf("query = %q, %d logs", h.view.query, len(h.view.records))
	}
	for _, r := range h.view.records {
		if r.Service != svc.value {
			t.Fatalf("filter not applied: %s", r.Service)
		}
	}

	if !h.view.detailFocused() {
		t.Fatal("the detail panel keeps the focus to chain filters")
	}
	method := h.selectField(t, "attributes.http.method")
	h.d.Keys("-")
	want := "service_name:" + svc.value + " AND NOT attributes.http.method:" + method.value
	if h.view.query != want || len(h.view.records) == 0 {
		t.Fatalf("query = %q (%d logs), want %q", h.view.query, len(h.view.records), want)
	}
	for _, r := range h.view.records {
		if r.Attributes["http.method"] == method.value {
			t.Fatalf("excluded value still listed: %v", r.Attributes)
		}
	}

	h.selectField(t, "service_name")
	h.d.Keys("+")
	if !strings.Contains(h.d.Screen(), "the query already has") || h.view.query != want {
		t.Errorf("a filter already in the query must not be added twice: %q", h.view.query)
	}
}

func TestMinusStillZoomsOutFromTheList(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("enter") // detail shown, list focused
	h.d.Keys("-")
	if h.view.query != "" || h.view.rng.Last != 2*DefaultRange.Last {
		t.Fatalf("- from the list must zoom out: query %q, range %v", h.view.query, h.view.rng)
	}
}

func TestMessageField(t *testing.T) {
	h := newHarness(t, Options{})
	r := h.view.records[0]
	if f := messageField(r); f != "body.message" {
		t.Errorf("structured body: %q", f)
	}
	r.Raw = []byte(`{"body":"plain"}`)
	if f := messageField(r); f != "body" {
		t.Errorf("plain body: %q", f)
	}
	r.Raw = []byte(`{"body":{"level":3}}`)
	if f := messageField(r); f != "" {
		t.Errorf("unknown body shape: %q", f)
	}
}
