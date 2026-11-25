package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

func TestFakeTranslator(t *testing.T) {
	cases := []struct {
		prompt, query string
		last          time.Duration
	}{
		{"errors from checkout in the last 2 hours", "severity_text:ERROR AND service_name:checkout", 2 * time.Hour},
		{"warnings of the auth service over the past day", "severity_text:WARN AND service_name:auth", 24 * time.Hour},
		{`logs containing "timeout"`, `"timeout"`, 0},
		{"something vague", "*", 0},
	}
	for _, tc := range cases {
		got, err := fake.Translator{}.Translate(context.Background(), tc.prompt)
		if err != nil {
			t.Fatal(err)
		}
		if got.Query != tc.query {
			t.Errorf("%q → %q, want %q", tc.prompt, got.Query, tc.query)
		}
		switch {
		case tc.last == 0 && got.Range != nil:
			t.Errorf("%q: unexpected range %v", tc.prompt, got.Range)
		case tc.last != 0 && (got.Range == nil || got.Range.Last != tc.last):
			t.Errorf("%q: range %v, want last %v", tc.prompt, got.Range, tc.last)
		}
	}
	if _, err := (fake.Translator{}).Translate(context.Background(), " "); err == nil {
		t.Error("empty prompt must fail")
	}
}
