package seed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/secret"
)

const testToken = "seed-test-token"

func TestLogsRequest(t *testing.T) {
	ts := time.Unix(1795600000, 123)
	req := LogsRequest([]logs.Record{{
		Timestamp: ts, ObservedTimestamp: ts.Add(time.Second), SeverityText: "ERROR", SeverityNumber: 17,
		Service: "checkout", Body: "payment declined", TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef", TraceFlags: 1,
		Attributes:         map[string]any{"http.status_code": 502, "http.route": "/payment"},
		ResourceAttributes: map[string]any{"host.name": "checkout-01"},
	}})
	got := marshal(t, req)
	for _, want := range []string{
		`{"key":"service.name","value":{"stringValue":"demo-checkout"}}`,
		`"timeUnixNano":"1795600000000000123"`,
		`"severityNumber":17`, `"severityText":"ERROR"`,
		`"body":{"stringValue":"payment declined"}`,
		`{"key":"http.status_code","value":{"intValue":"502"}}`,
		`"traceId":"0123456789abcdef0123456789abcdef"`, `"spanId":"0123456789abcdef"`,
		`"scope":{"name":"lazyobs-seed"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("request misses %s:\n%s", want, got)
		}
	}
}

func TestTracesRequest(t *testing.T) {
	start := time.Unix(1795600000, 0)
	req := TracesRequest([]traces.Detail{{Spans: []traces.Span{
		{TraceID: "t", SpanID: "a", Service: "demo-gateway", Name: "GET /", Kind: traces.KindServer, Start: start, Duration: 5 * time.Millisecond, Status: traces.StatusError, StatusMessage: "boom"},
		{TraceID: "t", SpanID: "b", ParentID: "a", Service: "demo-orders", Name: "q", Kind: traces.KindClient, Start: start, Duration: time.Millisecond, Status: traces.StatusOK},
	}}})
	got := marshal(t, req)
	for _, want := range []string{
		`"stringValue":"demo-gateway"`, `"stringValue":"demo-orders"`, `"parentSpanId":"a"`,
		`"kind":2`, `"kind":3`, `"status":{"code":2,"message":"boom"}`, `"status":{"code":1}`,
		`"endTimeUnixNano":"1795600000005000000"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("request misses %s:\n%s", want, got)
		}
	}
	if strings.Count(got, `"scopeSpans"`) != 2 {
		t.Error("spans must be grouped by service")
	}
}

func TestSenderRetriesOnRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Header.Get("X-Api-Key") != testToken || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request %s %v", r.URL.Path, r.Header)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limit exceeded, retry after 2s"}`)
			return
		}
		_, _ = io.WriteString(w, `{"partialSuccess":null}`)
	}))
	defer srv.Close()
	var waits []time.Duration
	s := &Sender{Endpoint: srv.URL, Token: secret.New(testToken), Retries: 3, Sleep: func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}}
	if err := s.Traces(t.Context(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(waits) != 2 || waits[0] != 2*time.Second {
		t.Errorf("calls = %d, waits = %v", calls.Load(), waits)
	}
}

func TestSenderErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"unauthorized"}`)
	}))
	defer srv.Close()
	s := &Sender{Endpoint: srv.URL, Token: secret.New(testToken), Retries: 3}
	err := s.Logs(t.Context(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), testToken) {
		t.Errorf("err = %v", err)
	}
}

type recorder struct{ logs, traces []map[string]any }

func (r *recorder) Logs(_ context.Context, req map[string]any) error {
	r.logs = append(r.logs, req)
	return nil
}

func (r *recorder) Traces(_ context.Context, req map[string]any) error {
	r.traces = append(r.traces, req)
	return nil
}

func TestRunLinksLogsToTraces(t *testing.T) {
	to := time.Unix(1795600000, 0)
	var rec recorder
	tot, err := Run(t.Context(), Options{Seed: 7, From: to.Add(-10 * time.Minute), To: to, Batch: 3 * time.Minute, Exporter: &rec})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.logs) != 4 || len(rec.traces) != 4 {
		t.Fatalf("batches: %d logs, %d traces", len(rec.logs), len(rec.traces))
	}
	if tot.Logs < 600 || tot.Traces < 200 || tot.Spans < 3*tot.Traces {
		t.Errorf("totals = %+v", tot)
	}
	// Some logs must refer to a trace that was sent.
	sent := map[string]bool{}
	for _, req := range rec.traces {
		for _, id := range ids(marshal(t, req), `"traceId":"`) {
			sent[id] = true
		}
	}
	linked := 0
	for _, req := range rec.logs {
		for _, id := range ids(marshal(t, req), `"traceId":"`) {
			if sent[id] {
				linked++
			}
		}
	}
	if linked < tot.Logs/10 {
		t.Errorf("only %d of %d logs refer to a seeded trace", linked, tot.Logs)
	}
}

func ids(doc, prefix string) []string {
	var out []string
	for _, part := range strings.Split(doc, prefix)[1:] {
		out = append(out, part[:strings.IndexByte(part, '"')])
	}
	return out
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
