package fake_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func clock(t time.Time) func() time.Time { return func() time.Time { return t } }

func search(t *testing.T, src *fake.Logs, q obs.Query) obs.Page[logs.Record] {
	t.Helper()
	p, err := src.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeterministicAndOrdered(t *testing.T) {
	q := obs.Query{Range: obs.Last(10 * time.Minute), Limit: 50}
	a := search(t, fake.NewLogs(42, clock(now)), q)
	b := search(t, fake.NewLogs(42, clock(now)), q)
	if a.Total == 0 || len(a.Items) != 50 || a.Total != b.Total {
		t.Fatalf("total=%d/%d items=%d", a.Total, b.Total, len(a.Items))
	}
	for i := range a.Items {
		if a.Items[i].Fingerprint() != b.Items[i].Fingerprint() {
			t.Fatal("same seed must give the same records")
		}
		if i > 0 && a.Items[i].Timestamp.After(a.Items[i-1].Timestamp) {
			t.Fatal("records must be sorted newest first")
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(a.Items[0].Raw, &doc); err != nil || doc["service_name"] != a.Items[0].Service {
		t.Errorf("raw document invalid: %s", a.Items[0].Raw)
	}
}

func TestPaginationCoversEverythingOnce(t *testing.T) {
	src := fake.NewLogs(7, clock(now))
	q := obs.Query{Range: obs.Last(5 * time.Minute), Limit: 37}
	seen := map[uint64]bool{}
	var total int64
	for pages := 0; ; pages++ {
		p := search(t, src, q)
		total = p.Total
		for _, r := range p.Items {
			if seen[r.Fingerprint()] {
				t.Fatalf("duplicate record on page %d", pages)
			}
			seen[r.Fingerprint()] = true
		}
		if p.Next == nil {
			break
		}
		q.Cursor = p.Next
	}
	if int64(len(seen)) != total {
		t.Fatalf("collected %d records, total says %d", len(seen), total)
	}
}

func TestFilters(t *testing.T) {
	src := fake.NewLogs(1, clock(now))
	all := search(t, src, obs.Query{Range: obs.Last(30 * time.Minute), Limit: 1000})
	errs := search(t, src, obs.Query{Text: "severity_text:ERROR", Range: obs.Last(30 * time.Minute), Limit: 1000})
	if errs.Total == 0 || errs.Total >= all.Total {
		t.Fatalf("error filter total=%d of %d", errs.Total, all.Total)
	}
	for _, r := range errs.Items {
		if r.Severity != logs.SeverityError {
			t.Fatalf("filter leaked %v", r.Severity)
		}
	}
	words := search(t, src, obs.Query{Text: "service_name:checkout AND timeout", Range: obs.Last(30 * time.Minute), Limit: 1000})
	for _, r := range words.Items {
		if r.Service != "checkout" || !strings.Contains(r.Body, "timeout") {
			t.Fatalf("unexpected match %s %q", r.Service, r.Body)
		}
	}
	if _, err := src.Search(context.Background(), obs.Query{Text: "foo:(", Range: obs.Last(time.Minute)}); err == nil {
		t.Error("unsupported syntax must fail like the real API")
	}
}

func TestNoFutureAndLiveArrival(t *testing.T) {
	t0 := now
	src := fake.NewLogs(3, func() time.Time { return t0 })
	q := obs.Query{Range: obs.Between(now.Add(-time.Minute), now.Add(time.Hour)), Limit: 1000}
	before := search(t, src, q)
	for _, r := range before.Items {
		if !r.Timestamp.Before(now) {
			t.Fatal("records from the future")
		}
	}
	t0 = now.Add(30 * time.Second)
	after := search(t, src, q)
	if after.Total <= before.Total {
		t.Fatalf("new records should arrive as time passes: %d → %d", before.Total, after.Total)
	}
}

func TestHistogramMatchesTotal(t *testing.T) {
	src := fake.NewLogs(5, clock(now))
	q := obs.Query{Range: obs.Last(time.Hour)}
	series, err := src.Histogram(context.Background(), q, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) < 4 {
		t.Fatalf("expected one series per severity, got %d", len(series))
	}
	var sum float64
	for _, s := range series {
		if len(s.Points) != 60 || logs.ParseSeverity(s.Name) == logs.SeverityUnknown {
			t.Fatalf("series %q has %d points", s.Name, len(s.Points))
		}
		for _, p := range s.Points {
			sum += p.Value
		}
	}
	if total := search(t, src, obs.Query{Range: q.Range, Limit: 1}).Total; int64(sum) != total {
		t.Fatalf("histogram sum %v != total %d", sum, total)
	}

	// Long ranges are sampled but stay fast.
	began := time.Now()
	if _, err := src.Histogram(context.Background(), obs.Query{Range: obs.Last(30 * 24 * time.Hour)}, 200); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(began); d > 3*time.Second {
		t.Errorf("30-day histogram took %v", d)
	}
}

func TestLatencyRespectsContext(t *testing.T) {
	src := fake.NewLogs(1, clock(now))
	src.Latency = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Search(ctx, obs.Query{Range: obs.Last(time.Minute)}); err == nil {
		t.Fatal("expected context error")
	}
}

func TestFilterSyntax(t *testing.T) {
	src := fake.NewLogs(1, clock(now))
	q := func(text string) obs.Page[logs.Record] {
		t.Helper()
		return search(t, src, obs.Query{Text: text, Range: obs.Last(30 * time.Minute), Limit: 1000})
	}
	all := q("*")
	notAuth := q("NOT service_name:auth")
	dashAuth := q("-service_name:auth")
	auth := q("service_name:auth")
	if notAuth.Total+auth.Total != all.Total || dashAuth.Total != notAuth.Total || auth.Total == 0 {
		t.Fatalf("all %d, auth %d, NOT auth %d, -auth %d", all.Total, auth.Total, notAuth.Total, dashAuth.Total)
	}
	for _, r := range notAuth.Items {
		if r.Service == "auth" {
			t.Fatal("NOT not applied")
		}
	}
	phrase := q(`body.message:"health check"`)
	if phrase.Total == 0 {
		t.Fatal("quoted phrase on body.message found nothing")
	}
	for _, r := range phrase.Items {
		if !strings.Contains(r.Body, "health check") {
			t.Fatalf("phrase leaked %q", r.Body)
		}
	}
	if _, err := src.Search(context.Background(), obs.Query{Text: "a:1 OR b:2", Range: obs.Last(time.Minute)}); err == nil {
		t.Error("OR is not supported by the demo source and must say so")
	}
}
