package logsview

import (
	"strings"
	"testing"
)

// fireSuggestTick delivers the pending debounced value request.
func (h *harness) fireSuggestTick(t *testing.T) {
	t.Helper()
	for i := len(h.ticks) - 1; i >= 0; i-- {
		if tick, ok := h.ticks[i].(suggestTickMsg); ok {
			h.ticks = append(h.ticks[:i], h.ticks[i+1:]...)
			h.d.Send(tick)
			return
		}
	}
	t.Fatal("no value request scheduled")
}

func TestAutocompleteFieldThenValue(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("serv")
	s := h.d.Screen()
	if !h.view.compl.open() || !strings.Contains(s, "service_name") || !strings.Contains(s, "keyword") {
		t.Fatalf("field suggestions missing:\n%s", s)
	}
	if !strings.Contains(s[strings.LastIndex(s, "\n")+1:], "enter complete") {
		t.Errorf("status bar must explain the list: %q", s[strings.LastIndex(s, "\n")+1:])
	}

	h.d.Keys("tab")
	if got := h.view.input.Value(); got != "service_name:" {
		t.Fatalf("after tab: %q", got)
	}
	h.fireSuggestTick(t)
	if !h.view.compl.open() || h.view.compl.tok.kind != tokenValue {
		t.Fatalf("value suggestions missing:\n%s", h.d.Screen())
	}
	second := h.view.compl.items[1].label
	h.d.Keys("down", "tab")
	if got := h.view.input.Value(); got != "service_name:"+second+" " {
		t.Fatalf("after choosing a value: %q", got)
	}
	if h.view.compl.open() {
		t.Error("the list must close after a completed value")
	}

	h.d.Keys("enter")
	if h.view.query != "service_name:"+second {
		t.Fatalf("query = %q", h.view.query)
	}
	for _, r := range h.view.records {
		if r.Service != second {
			t.Fatalf("search not filtered on the completed value: %s", r.Service)
		}
	}
}

func TestAutocompleteValuePrefixAndCache(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("attributes.http.method:P")
	h.fireSuggestTick(t)
	for _, s := range h.view.compl.items {
		if !strings.HasPrefix(s.label, "P") {
			t.Fatalf("prefix not applied: %v", labels(h.view.compl.items))
		}
	}
	h.d.Keys("backspace") // the empty prefix was never fetched: a request is expected
	scheduled := len(h.ticks)
	h.d.Type("P")
	if !h.view.compl.open() {
		t.Error("values typed again must come from the cache")
	}
	if len(h.ticks) != scheduled {
		t.Error("cached values must not be requested again")
	}
}

func TestAutocompleteEscAndNonAggregatable(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("sev")
	h.d.Keys("esc")
	if h.view.mode != modeQuery || h.view.compl.open() {
		t.Fatal("first esc closes the list, keeping the input")
	}
	h.d.Type("e")
	if !h.view.compl.open() {
		t.Fatal("typing reopens the list")
	}
	h.d.Keys("esc", "esc")
	if h.view.mode != modeNormal {
		t.Fatal("second esc cancels the input")
	}

	h.d.Keys("/")
	h.d.Type("body.message:")
	if h.view.compl.open() {
		t.Error("no value list for a field the platform cannot aggregate")
	}
	for _, tick := range h.ticks {
		if _, ok := tick.(suggestTickMsg); ok {
			t.Error("no value request for a non-aggregatable field")
		}
	}
}

func TestStaleValueResponsesAreIgnored(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("service_name:a")
	h.d.Type("u") // a second request supersedes the first one
	var first suggestTickMsg
	for _, tick := range h.ticks {
		if st, ok := tick.(suggestTickMsg); ok {
			first = st
			break
		}
	}
	h.d.Send(first)
	if h.view.compl.open() {
		t.Error("an outdated request must not fire")
	}
}

func TestEnterAcceptsTheHighlightedSuggestion(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("serv")
	h.d.Keys("enter") // completes the field, does not search
	if h.view.mode != modeQuery || h.view.input.Value() != "service_name:" {
		t.Fatalf("enter must complete the field: mode %v, input %q", h.view.mode, h.view.input.Value())
	}
	h.fireSuggestTick(t)
	first := h.view.compl.items[0].label
	h.d.Keys("enter") // completes the value
	if got := h.view.input.Value(); got != "service_name:"+first+" " || h.view.mode != modeQuery {
		t.Fatalf("enter must complete the value: %q", got)
	}
	h.d.Keys("enter") // the list is closed: search
	if h.view.mode != modeNormal || h.view.query != "service_name:"+first {
		t.Fatalf("third enter must search: mode %v, query %q", h.view.mode, h.view.query)
	}
}

func TestExactValueIsNotReplacedByEnter(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("attributes.http.method:P")
	h.fireSuggestTick(t) // POST, PUT
	h.d.Type("UT")       // "PUT" is typed in full and known
	h.fireSuggestTick(t)
	if h.view.compl.selected != -1 {
		t.Fatalf("nothing must be preselected on an exact value, selected %d (%v)", h.view.compl.selected, labels(h.view.compl.items))
	}
	h.d.Keys("enter")
	if h.view.mode != modeNormal || h.view.query != "attributes.http.method:PUT" {
		t.Fatalf("enter must search what was typed: %q", h.view.query)
	}
}

func TestExactFieldWithLongerMatches(t *testing.T) {
	h := newHarness(t, Options{})
	h.d.Keys("/")
	h.d.Type("scope_name") // a known field; nothing longer here, so no list
	h.d.Keys("enter")
	if h.view.mode != modeNormal {
		t.Fatal("enter must search when nothing is highlighted")
	}
}
