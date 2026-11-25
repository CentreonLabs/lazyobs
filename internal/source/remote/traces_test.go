package remote_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

// tracesAPI emulates the trace search over root spans (start, duration in
// ms), optionally with the sort direction reversed as observed on the
// platform.
type tracesAPI struct {
	roots    [][2]int64 // start seconds, duration ms
	inverted bool
	requests []api.TraceSearchRequest
}

func (a *tracesAPI) SearchTraces(_ context.Context, req api.TraceSearchRequest) (*api.TraceSearchResponse, error) {
	a.requests = append(a.requests, req)
	hits := append([][2]int64(nil), a.roots...)
	key := 0
	if req.SortField == api.TraceSortDuration {
		key = 1
	}
	desc := req.SortDirection != api.SortAsc
	if a.inverted {
		desc = !desc
	}
	sort.Slice(hits, func(i, j int) bool {
		if desc {
			return hits[i][key] > hits[j][key]
		}
		return hits[i][key] < hits[j][key]
	})
	resp := &api.TraceSearchResponse{NumHits: int64(len(hits))}
	for i := req.StartOffset; i < len(hits) && i < req.StartOffset+req.MaxHits; i++ {
		h := hits[i]
		start := h[0] * 1e9
		resp.Hits = append(resp.Hits, json.RawMessage(fmt.Sprintf(
			`{"trace_id":"t%d","span_id":"s%d","service_name":"gw","span_name":"GET /","span_kind":2,`+
				`"span_start_timestamp_nanos":%d,"span_end_timestamp_nanos":%d,"span_duration_millis":%d,"span_status":{"code":"ok"}}`,
			i, i, start, start+h[1]*1e6, h[1])))
	}
	if req.Aggregation != nil {
		resp.Curves = []api.TraceCurve{{Metric: "trace count", Times: []int64{1000, 2000}, Data: []float64{3, 4}, Attributes: json.RawMessage(`[]`)}}
	}
	return resp, nil
}

func (a *tracesAPI) GetTrace(context.Context, string) (*api.TraceDetailResponse, error) {
	b, err := os.ReadFile("../../api/testdata/trace_detail.json")
	if err != nil {
		return nil, err
	}
	var d api.TraceDetailResponse
	return &d, json.Unmarshal(b, &d)
}

func roots() [][2]int64 {
	end := now.Unix()
	return [][2]int64{{end - 50, 120}, {end - 40, 1677}, {end - 30, 90}, {end - 20, 300}, {end - 10, 150}}
}

func TestTraceSearchWorksAroundReversedSort(t *testing.T) {
	for _, inverted := range []bool{false, true} {
		a := &tracesAPI{roots: roots(), inverted: inverted}
		src := remote.NewTraces(a, func() time.Time { return now })
		for _, order := range []obs.Order{obs.OrderNewest, obs.OrderSlowest} {
			page, err := src.Search(context.Background(), obs.Query{Range: obs.Last(time.Hour), Limit: 10, Order: order})
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i < len(page.Items); i++ {
				prev, cur := page.Items[i-1].Root, page.Items[i].Root
				if order == obs.OrderNewest && cur.Start.After(prev.Start) || order == obs.OrderSlowest && cur.Duration > prev.Duration {
					t.Fatalf("inverted=%v order=%v: not descending at %d", inverted, order, i)
				}
			}
		}
		// Once detected, the right direction is asked first: one request per search.
		n := len(a.requests)
		if _, err := src.Search(context.Background(), obs.Query{Range: obs.Last(time.Hour), Limit: 10}); err != nil {
			t.Fatal(err)
		}
		if len(a.requests) != n+1 {
			t.Errorf("inverted=%v: %d requests for a search after detection", inverted, len(a.requests)-n)
		}
	}
}

func TestTraceSearchMapsSpansAndPages(t *testing.T) {
	a := &tracesAPI{roots: roots()}
	src := remote.NewTraces(a, func() time.Time { return now })
	page, err := src.Search(context.Background(), obs.Query{Text: "service_name:gw", Range: obs.Last(time.Hour), Limit: 2, Order: obs.OrderSlowest})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Items) != 2 || page.Next == nil || page.Next.Offset != 2 {
		t.Fatalf("page = total %d, %d items, next %+v", page.Total, len(page.Items), page.Next)
	}
	r := page.Items[0].Root
	if r.Duration != 1677*time.Millisecond || r.Service != "gw" || r.Kind != traces.KindServer || r.Status != traces.StatusOK || r.ParentID != "" {
		t.Errorf("root span = %+v", r)
	}
	if req := a.requests[0]; req.SortField != api.TraceSortDuration || req.Query != "service_name:gw" {
		t.Errorf("request = %+v", req)
	}
}

func TestTraceHistogramCountsErrors(t *testing.T) {
	a := &tracesAPI{roots: roots()}
	series, err := remote.NewTraces(a, func() time.Time { return now }).
		Histogram(context.Background(), obs.Query{Text: "service_name:gw", Range: obs.Last(time.Hour)}, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 || series[0].Name != "TRACES" || series[1].Name != "ERRORS" || len(series[0].Points) != 2 {
		t.Fatalf("series = %+v", series)
	}
	if q := a.requests[1].Query; q != "(service_name:gw) AND span_status.code:error" {
		t.Errorf("errors query = %q", q)
	}
	if agg := a.requests[0].Aggregation; agg == nil || agg.IntervalSeconds != 60 || agg.TargetResolution != 60 {
		t.Errorf("aggregation = %+v", agg)
	}
}

func TestTraceDetail(t *testing.T) {
	d, err := remote.NewTraces(&tracesAPI{}, nil).Trace(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "POST /v1/orders" || len(d.Spans) != 7 || d.Duration != 131*time.Millisecond {
		t.Fatalf("detail = %+v", d)
	}
	tree := d.Tree()
	if len(tree) != 7 || tree[0].Service != "lazyobs-demo-gateway" || tree[0].Depth != 0 || tree[1].Depth != 1 {
		t.Errorf("tree = %+v", tree)
	}
	// Asynchronous spans may end after the root: the waterfall must cover
	// them, beyond the platform's total duration (the root's).
	start, end := d.Bounds()
	if got := end.Sub(start); got <= d.Duration {
		t.Errorf("bounds span %v, root duration %v", got, d.Duration)
	}
}
