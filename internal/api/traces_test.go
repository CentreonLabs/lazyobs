package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/CentreonLabs/lazyobs/internal/api"
)

func TestSearchTraces(t *testing.T) {
	var got map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs/my-org_1/traces/search" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write(fixture(t, "traces_search.json"))
	})
	start := int64(-3600)
	resp, err := c.SearchTraces(context.Background(), api.TraceSearchRequest{
		Start: &start, MaxHits: 3, SortField: api.TraceSortDuration, SortDirection: api.SortAsc,
		Aggregation: &api.TraceAggregation{TargetResolution: 10, IntervalSeconds: 360},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, got, map[string]any{
		"query": "*", "start": -3600.0, "max_hits": 3.0, "sort_field": "duration", "sort_direction": "asc",
		"aggregation": map[string]any{"targetResolution": 10.0, "interval": 360.0},
	})
	if resp.NumHits != 10 || len(resp.Hits) != 3 {
		t.Fatalf("num_hits %d, hits %d", resp.NumHits, len(resp.Hits))
	}
	s, err := api.DecodeSpan(resp.Hits[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.TraceID == "" || s.SpanID == "" || s.ParentSpanID != "" || s.ServiceName != "lazyobs-demo-gateway" ||
		s.SpanKind != 2 || s.StartNanos == 0 || s.EndNanos <= s.StartNanos || s.DurationMillis == 0 ||
		s.Status.Code == "" || s.SpanAttributes["http.route"] != "/v1/orders" {
		t.Errorf("root span = %+v", s)
	}
}

func TestGetTrace(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/traces/30877432d1026706d7e805da846a32c3" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write(fixture(t, "trace_detail.json"))
	})
	d, err := c.GetTrace(context.Background(), "30877432d1026706d7e805da846a32c3")
	if err != nil {
		t.Fatal(err)
	}
	if d.TraceName != "POST /v1/orders" || len(d.Services) != 5 || len(d.Spans) != 7 || d.TotalDuration != 131 || d.RootSpanID == "" {
		t.Errorf("detail = %+v", d)
	}
	children := 0
	for _, raw := range d.Spans {
		s, err := api.DecodeSpan(raw)
		if err != nil {
			t.Fatal(err)
		}
		if s.ParentSpanID != "" {
			children++
		}
	}
	if children != 6 {
		t.Errorf("%d child spans, want 6", children)
	}
	if _, err := c.GetTrace(context.Background(), ""); err == nil {
		t.Error("an empty ID must fail before calling the API")
	}
}

func TestTraceTopology(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/traces/topology" || r.URL.Query().Get("start") != "100" || r.URL.Query().Get("end") != "200" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, _ = w.Write(fixture(t, "traces_topology.json"))
	})
	topo, err := c.TraceTopology(context.Background(), 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Nodes) != 5 || len(topo.Edges) != 4 {
		t.Fatalf("topology = %+v", topo)
	}
	for _, n := range topo.Nodes {
		if n.ServiceName == "" || n.SpanCount == 0 || n.P95 == nil {
			t.Errorf("node = %+v", n)
		}
	}
	for _, e := range topo.Edges {
		if e.Source == "" || e.Target == "" || e.CallCount == 0 {
			t.Errorf("edge = %+v", e)
		}
	}
}
