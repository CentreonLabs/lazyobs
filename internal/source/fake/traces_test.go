package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

func TestFakeTraces(t *testing.T) {
	src := fake.NewTraces(3, clock(now))
	q := obs.Query{Range: obs.Last(15 * time.Minute), Limit: 50}
	page, err := src.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total < 100 || len(page.Items) != 50 || page.Next == nil {
		t.Fatalf("total %d, %d items", page.Total, len(page.Items))
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i].Root.Start.After(page.Items[i-1].Root.Start) {
			t.Fatal("not newest first")
		}
	}

	slow, _ := src.Search(context.Background(), obs.Query{Range: q.Range, Limit: 50, Order: obs.OrderSlowest})
	for i := 1; i < len(slow.Items); i++ {
		if slow.Items[i].Root.Duration > slow.Items[i-1].Root.Duration {
			t.Fatal("not slowest first")
		}
	}

	// Whole traces: a well-formed tree under the listed root.
	root := page.Items[0].Root
	d, err := src.Trace(context.Background(), root.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	tree := d.Tree()
	if len(tree) < 3 || tree[0].SpanID != root.SpanID || tree[0].Depth != 0 || len(d.Services) < 2 {
		t.Fatalf("trace %s: %d nodes, services %v", root.TraceID, len(tree), d.Services)
	}
	for _, n := range tree[1:] {
		if n.Depth == 0 || n.ParentID == "" || n.Start.Before(root.Start) {
			t.Fatalf("bad span %+v", n.Span)
		}
	}
	if _, err := src.Trace(context.Background(), "nope"); err == nil {
		t.Error("unknown trace must fail")
	}
}

func TestFakeTraceFiltersAndHistogram(t *testing.T) {
	src := fake.NewTraces(3, clock(now))
	rng := obs.Last(time.Hour)
	all, _ := src.Search(context.Background(), obs.Query{Range: rng, Limit: 1})
	errs, err := src.Search(context.Background(), obs.Query{Text: "span_status.code:error", Range: rng, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if errs.Total == 0 || errs.Total >= all.Total {
		t.Fatalf("errors %d of %d", errs.Total, all.Total)
	}
	for _, tr := range errs.Items {
		if tr.Root.Status != traces.StatusError {
			t.Fatal("status filter leaked")
		}
	}
	login, _ := src.Search(context.Background(), obs.Query{Text: "span_attributes.http.route:/login", Range: rng, Limit: 1000})
	for _, tr := range login.Items {
		if tr.Root.Name != "POST /login" {
			t.Fatalf("route filter leaked %s", tr.Root.Name)
		}
	}

	series, err := src.Histogram(context.Background(), obs.Query{Range: rng}, 60)
	if err != nil || len(series) != 2 || series[0].Name != "TRACES" || series[1].Name != "ERRORS" {
		t.Fatalf("series %+v %v", series, err)
	}
	var n, e float64
	for i := range series[0].Points {
		n += series[0].Points[i].Value
		e += series[1].Points[i].Value
	}
	if int64(n) != all.Total || int64(e) != errs.Total {
		t.Errorf("histogram %v/%v, searches %d/%d", n, e, all.Total, errs.Total)
	}

	values, err := src.Values(context.Background(), "span_attributes.http.route", "/v1", now.Add(-time.Hour), now, 10)
	if err != nil || len(values) != 2 {
		t.Errorf("route values = %+v, %v", values, err)
	}
}
