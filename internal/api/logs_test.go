package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/secret"
)

const testToken = "tok-0123456789abcdef" // gitleaks:allow (fake test value)

// newTestClient starts a server running handler and returns a client pointing at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := api.New(srv.URL, "my-org_1", secret.New(testToken), api.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ptr[T any](v T) *T { return &v }

func TestSearchLogsRequest(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs/my-org_1/logs/search" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != testToken {
			t.Errorf("X-Api-Key = %q", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "lazyobs/") {
			t.Errorf("User-Agent = %q", ua)
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Errorf("body is not JSON: %s", b)
		}
		_, _ = w.Write(fixture(t, "logs_search.json"))
	})

	_, err := c.SearchLogs(context.Background(), api.LogSearchRequest{
		Query:       "severity_text:ERROR",
		Start:       ptr(int64(1700000000)),
		End:         ptr(int64(1700003600)),
		MaxHits:     100,
		StartOffset: 200,
		Aggregation: &api.Aggregation{Type: api.AggregateCount, TargetResolution: 60},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"query": "severity_text:ERROR", "start": 1700000000.0, "end": 1700003600.0,
		"max_hits": 100.0, "start_offset": 200.0,
		"aggregation": map[string]any{"aggregationType": "count", "targetResolution": 60.0},
	}
	assertJSONEqual(t, gotBody, want)
}

func TestSearchLogsDefaults(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"hits":[],"num_hits":0,"errors":[]}`))
	})
	if _, err := c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 20}); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, gotBody, map[string]any{"query": "*", "max_hits": 20.0})
}

func TestCustomAggregationJSON(t *testing.T) {
	b, err := json.Marshal(api.Aggregation{CustomField: "attributes.http.method", TargetResolution: 10, IntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	assertJSONEqual(t, got, map[string]any{
		"aggregationType":  map[string]any{"custom": map[string]any{"field": "attributes.http.method"}},
		"targetResolution": 10.0,
		"interval":         60.0,
	})
}

func TestSearchLogsResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture(t, "logs_search.json"))
	})
	resp, err := c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 3})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NumHits != 1983 || len(resp.Hits) != 3 || len(resp.Errors) != 0 {
		t.Fatalf("unexpected response: num_hits=%d hits=%d errors=%d", resp.NumHits, len(resp.Hits), len(resp.Errors))
	}

	first, err := api.DecodeLogHit(resp.Hits[0])
	if err != nil {
		t.Fatal(err)
	}
	// Nanosecond timestamps exceed float64 precision: they must decode exactly.
	if first.TimestampNanos != 1793713556297395112 || first.ObservedTimestampNanos != 1793713557606075335 {
		t.Errorf("timestamps lost precision: %d / %d", first.TimestampNanos, first.ObservedTimestampNanos)
	}
	if first.ServiceName != "query" || first.SeverityNumber != 13 || first.SeverityText != "WARN" {
		t.Errorf("unexpected fields: %+v", first)
	}
	if string(first.Body) != `{ "message": "connection pool exhausted, retrying" }` {
		t.Errorf("body = %s", first.Body)
	}

	third, err := api.DecodeLogHit(resp.Hits[2])
	if err != nil {
		t.Fatal(err)
	}
	if third.TraceID != "5b8efff798038103d269b633813fc60c" || third.Attributes["http.method"] != "GET" {
		t.Errorf("optional fields not decoded: %+v", third)
	}
}

func TestSearchLogsAggregationResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture(t, "logs_search_aggregation.json"))
	})
	resp, err := c.SearchLogs(context.Background(), api.LogSearchRequest{
		Aggregation: &api.Aggregation{Type: api.AggregateSeverity, TargetResolution: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Curves) != 2 {
		t.Fatalf("curves = %d", len(resp.Curves))
	}
	cv := resp.Curves[0]
	if cv.Times[0] != 1793016000000 || cv.Data[2] != 25.5 || cv.Attributes[0] != [2]string{"severity_number", "13-17"} || cv.Unit != nil {
		t.Errorf("unexpected curve: %+v", cv)
	}
}

func TestSearchLogsErrors(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		wantMsg  string
		wantHint string
	}{
		{http.StatusBadRequest, `{"message":"failed to parse query: ` + "`foo:(`" + `"}`, "failed to parse query", ""},
		{http.StatusUnauthorized, `{"message":"Unauthorized"}`, "Unauthorized", "token is missing, invalid or expired"},
		{http.StatusPaymentRequired, ``, "Payment Required", "not enabled"},
		{http.StatusInternalServerError, `upstream exploded`, "upstream exploded", ""},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 1})
			var apiErr *api.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("want *api.Error, got %v", err)
			}
			if api.StatusCode(err) != tc.status {
				t.Errorf("StatusCode = %d", api.StatusCode(err))
			}
			if !strings.Contains(err.Error(), tc.wantMsg) || !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("error %q should contain %q and %q", err, tc.wantMsg, tc.wantHint)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %q", err)
			}
		})
	}
}

func TestSearchLogsTransportErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused
	c, err := api.New(srv.URL, "org", secret.New(testToken))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 1})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %q", err)
	}
}

func TestSearchLogsValidation(t *testing.T) {
	c := newTestClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("invalid requests must not reach the server")
	})
	for _, req := range []api.LogSearchRequest{
		{MaxHits: api.MaxHits + 1},
		{MaxHits: -1},
		{MaxHits: 10, StartOffset: api.MaxStartOffset + 1},
	} {
		if _, err := c.SearchLogs(context.Background(), req); err == nil {
			t.Errorf("expected validation error for %+v", req)
		}
	}
}

func TestNewValidation(t *testing.T) {
	tok := secret.New(testToken)
	cases := []struct {
		name, url, org string
		token          secret.String
	}{
		{"bad org chars", "", "my org", tok},
		{"org too long", "", strings.Repeat("a", 51), tok},
		{"empty org", "", "", tok},
		{"relative url", "api.example.com", "org", tok},
		{"bad scheme", "ftp://api.example.com", "org", tok},
		{"missing token", "", "org", secret.String{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := api.New(tc.url, tc.org, tc.token); err == nil {
				t.Error("expected an error")
			} else if strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %q", err)
			}
		})
	}

	c, err := api.New("", "mbe-org", tok)
	if err != nil || c.Org() != "mbe-org" {
		t.Fatalf("default base URL should be accepted: %v", err)
	}
}

func assertJSONEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", g, w)
	}
}

func TestRateLimitedRequestsAreRetried(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			if calls == 1 {
				w.Header().Set("Retry-After", "0.1")
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limit exceeded, retry after 100ms"}`))
			return
		}
		_, _ = w.Write([]byte(`{"hits":[],"num_hits":7,"errors":[]}`))
	})
	resp, err := c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 1})
	if err != nil || resp.NumHits != 7 || calls != 3 {
		t.Fatalf("calls %d, err %v", calls, err)
	}
}

func TestRateLimitGivesUp(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limit exceeded"}`))
	})
	_, err := c.SearchLogs(context.Background(), api.LogSearchRequest{MaxHits: 1})
	if api.StatusCode(err) != http.StatusTooManyRequests || calls != 3 || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("calls %d, err %v", calls, err)
	}
}
