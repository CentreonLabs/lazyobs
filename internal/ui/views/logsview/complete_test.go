package logsview

import (
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

func TestTokenAt(t *testing.T) {
	cases := []struct {
		query  string
		pos    int
		kind   tokenKind
		field  string
		prefix string
		start  int
	}{
		{"", 0, tokenNone, "", "", 0},
		{"serv", 4, tokenField, "", "serv", 0},
		{"service_name:au", 15, tokenValue, "service_name", "au", 0},
		{"service_name:", 13, tokenValue, "service_name", "", 0},
		{"a:b AND sev", 11, tokenField, "", "sev", 8},
		{"(a:b OR -severity_text:ER)", 25, tokenValue, "severity_text", "ER", 9},
		{`body.message:"conn ti`, 21, tokenValue, "body.message", "conn ti", 0},
		{"a:b ", 4, tokenNone, "", "", 4},
		{"service_name:auth AND x", 3, tokenField, "", "ser", 0},
	}
	for _, tc := range cases {
		got := tokenAt([]rune(tc.query), tc.pos)
		if got.kind != tc.kind || got.field != tc.field || got.prefix != tc.prefix || got.start != tc.start {
			t.Errorf("tokenAt(%q, %d) = %+v", tc.query, tc.pos, got)
		}
	}
}

var testFields = []obs.Field{
	{Name: "attributes.http.method", Type: "keyword", Aggregatable: true},
	{Name: "resource_attributes.service.version", Type: "keyword", Aggregatable: true},
	{Name: "service_name", Type: "keyword", Aggregatable: true},
	{Name: "severity_text", Type: "keyword", Aggregatable: true},
	{Name: "severity_number", Type: "long", Aggregatable: true},
}

func labels(s []suggestion) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.label
	}
	return out
}

func TestFieldSuggestions(t *testing.T) {
	got := labels(fieldSuggestions(testFields, "serv"))
	want := []string{"service_name", "resource_attributes.service.version"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("serv → %v, want prefix match first then segment match", got)
	}
	if got := labels(fieldSuggestions(testFields, "sev")); len(got) != 2 || got[0] != "severity_text" {
		t.Errorf("sev → %v (shortest first)", got)
	}
	if got := labels(fieldSuggestions(testFields, "AN")); len(got) == 0 || got[0] != "AND" {
		t.Errorf("AN → %v, want the AND operator", got)
	}
	if got := labels(fieldSuggestions(testFields, "method")); len(got) != 1 || got[0] != "attributes.http.method" {
		t.Errorf("method → %v", got)
	}
	if got := fieldSuggestions(testFields, "service_name"); len(got) != 0 {
		t.Errorf("a complete name needs no suggestion: %v", labels(got))
	}
}

func TestComplete(t *testing.T) {
	cases := []struct {
		query string
		pos   int
		s     suggestion
		want  string
		cur   int
	}{
		{"serv", 4, suggestion{label: "service_name", kind: tokenField}, "service_name:", 13},
		{"service_name:au", 15, suggestion{label: "auth", kind: tokenValue}, "service_name:auth ", 18},
		{"a:b AND sev x:y", 11, suggestion{label: "severity_text", kind: tokenField}, "a:b AND severity_text: x:y", 22},
		{"x:1 AN", 6, suggestion{label: "AND", kind: tokenField, op: true}, "x:1 AND ", 8},
		{"msg:", 4, suggestion{label: "connection reset", kind: tokenValue}, `msg:"connection reset" `, 23},
		{"service_name:au AND x:y", 15, suggestion{label: "auth", kind: tokenValue}, "service_name:auth AND x:y", 18},
	}
	for _, tc := range cases {
		q := []rune(tc.query)
		got, cur := complete(q, tokenAt(q, tc.pos), tc.s)
		if got != tc.want || cur != tc.cur {
			t.Errorf("complete(%q) = %q @%d, want %q @%d", tc.query, got, cur, tc.want, tc.cur)
		}
	}
}

func TestQuoteValue(t *testing.T) {
	for in, want := range map[string]string{
		"auth": "auth", "/v1/orders": "/v1/orders", "1.2.3": "1.2.3", "a b": `"a b"`, `say "hi"`: `"say \"hi\""`, "": `""`, "a:b": `"a:b"`,
	} {
		if got := quoteValue(in); got != want {
			t.Errorf("quoteValue(%q) = %q, want %q", in, got, want)
		}
	}
}
