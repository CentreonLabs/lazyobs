package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

// TracesAPI is the subset of the API client used by Traces.
type TracesAPI interface {
	SearchTraces(ctx context.Context, req api.TraceSearchRequest) (*api.TraceSearchResponse, error)
	GetTrace(ctx context.Context, traceID string) (*api.TraceDetailResponse, error)
	TraceTopology(ctx context.Context, start, end int64) (*api.TopologyResponse, error)
}

// ErrorQuery matches traces whose root span failed.
const ErrorQuery = "span_status.code:error"

// Traces searches and loads traces from the platform. The trace search
// matches root spans only, one hit per trace.
type Traces struct {
	api TracesAPI
	now func() time.Time

	// The platform has been seen returning the opposite of the requested
	// sort direction; when the results come back reversed the direction is
	// flipped for good (and flips back if the platform is fixed).
	inverted atomic.Bool
}

var (
	_ obs.Searcher[traces.Trace] = (*Traces)(nil)
	_ obs.Histogrammer           = (*Traces)(nil)
	_ traces.Fetcher             = (*Traces)(nil)
)

// NewTraces returns a trace source backed by client. A nil now uses time.Now.
func NewTraces(client TracesAPI, now func() time.Time) *Traces {
	if now == nil {
		now = time.Now
	}
	return &Traces{api: client, now: now}
}

// Search returns one page of traces, newest or slowest first.
func (t *Traces) Search(ctx context.Context, q obs.Query) (obs.Page[traces.Trace], error) {
	start, end := bounds(q, t.now())
	limit := clampLimit(q.Limit)
	offset := 0
	if q.Cursor != nil {
		offset = q.Cursor.Offset
	}
	field := api.TraceSortTimestamp
	if q.Order == obs.OrderSlowest {
		field = api.TraceSortDuration
	}
	page, err := t.searchSorted(ctx, api.TraceSearchRequest{
		Query: queryText(q.Text), Start: &start, End: &end, MaxHits: limit, StartOffset: offset, SortField: field,
	}, q.Order)
	if err != nil {
		return page, err
	}
	fetched := offset + len(page.Items)
	switch {
	case len(page.Items) < limit || int64(fetched) >= page.Total:
	case fetched <= api.MaxStartOffset:
		page.Next = &obs.Cursor{Offset: fetched, End: time.Unix(end, 0)}
	case q.Order == obs.OrderNewest:
		// Past the offset limit, slide the window to the oldest trace seen.
		oldest := page.Items[len(page.Items)-1].Root.Start.Unix()
		if newEnd := min(oldest+1, end-1); newEnd > start {
			page.Next = &obs.Cursor{End: time.Unix(newEnd, 0)}
		}
	}
	return page, nil
}

// searchSorted runs a search expecting results in descending order,
// detecting and working around a reversed sort direction.
func (t *Traces) searchSorted(ctx context.Context, req api.TraceSearchRequest, order obs.Order) (obs.Page[traces.Trace], error) {
	direction := func() string {
		if t.inverted.Load() {
			return api.SortAsc
		}
		return api.SortDesc
	}
	req.SortDirection = direction()
	page, err := t.search(ctx, req)
	if err != nil || !reversed(page.Items, order) {
		return page, err
	}
	t.inverted.Store(!t.inverted.Load())
	req.SortDirection = direction()
	return t.search(ctx, req)
}

// reversed reports whether items are in ascending order of the sort key.
func reversed(items []traces.Trace, order obs.Order) bool {
	if len(items) < 2 {
		return false
	}
	first, last := items[0].Root, items[len(items)-1].Root
	if order == obs.OrderSlowest {
		return first.Duration < last.Duration
	}
	return first.Start.Before(last.Start)
}

func (t *Traces) search(ctx context.Context, req api.TraceSearchRequest) (obs.Page[traces.Trace], error) {
	resp, err := t.api.SearchTraces(ctx, req)
	if err != nil {
		return obs.Page[traces.Trace]{}, err
	}
	page := obs.Page[traces.Trace]{Total: resp.NumHits, Warnings: warnings(resp.Errors), Items: make([]traces.Trace, 0, len(resp.Hits))}
	for _, raw := range resp.Hits {
		s, err := toSpan(raw)
		if err != nil {
			return obs.Page[traces.Trace]{}, err
		}
		page.Items = append(page.Items, traces.Trace{Root: s})
	}
	return page, nil
}

// Histogram returns the number of traces over time ("TRACES") and of
// traces in error ("ERRORS").
func (t *Traces) Histogram(ctx context.Context, q obs.Query, buckets int) ([]obs.Series, error) {
	start, end := bounds(q, t.now())
	buckets = min(max(buckets, 10), 50_000)
	interval := int(max((end-start)/int64(buckets), 1))
	series := make([]obs.Series, 0, 2)
	for _, s := range []struct{ name, query string }{
		{"TRACES", queryText(q.Text)},
		{"ERRORS", andQuery(q.Text, ErrorQuery)},
	} {
		resp, err := t.api.SearchTraces(ctx, api.TraceSearchRequest{
			Query: s.query, Start: &start, End: &end, MaxHits: 0,
			Aggregation: &api.TraceAggregation{TargetResolution: buckets, IntervalSeconds: interval},
		})
		if err != nil {
			return nil, err
		}
		out := obs.Series{Name: s.name}
		for _, c := range resp.Curves {
			for i, ms := range c.Times {
				if i < len(c.Data) {
					out.Points = append(out.Points, obs.Point{Time: time.UnixMilli(ms), Value: c.Data[i]})
				}
			}
		}
		series = append(series, out)
	}
	return series, nil
}

// Trace implements traces.Fetcher.
func (t *Traces) Trace(ctx context.Context, traceID string) (traces.Detail, error) {
	resp, err := t.api.GetTrace(ctx, traceID)
	if err != nil {
		return traces.Detail{}, err
	}
	d := traces.Detail{
		TraceID:  resp.TraceID,
		Name:     resp.TraceName,
		Services: resp.Services,
		Duration: time.Duration(resp.TotalDuration * float64(time.Millisecond)),
		Spans:    make([]traces.Span, 0, len(resp.Spans)),
	}
	for _, raw := range resp.Spans {
		s, err := toSpan(raw)
		if err != nil {
			return traces.Detail{}, err
		}
		d.Spans = append(d.Spans, s)
	}
	return d, nil
}

func toSpan(raw json.RawMessage) (traces.Span, error) {
	h, err := api.DecodeSpan(raw)
	if err != nil {
		return traces.Span{}, err
	}
	s := traces.Span{
		TraceID: h.TraceID, SpanID: h.SpanID, ParentID: h.ParentSpanID,
		Service: h.ServiceName, Name: h.SpanName, Kind: traces.Kind(h.SpanKind),
		Start:         time.Unix(0, h.StartNanos),
		Status:        traces.ParseStatus(h.Status.Code),
		StatusMessage: h.Status.Message,
		Attributes:    h.SpanAttributes, ResourceAttributes: h.ResourceAttributes,
		Raw: append([]byte(nil), raw...),
	}
	switch {
	case h.EndNanos > h.StartNanos:
		s.Duration = time.Duration(h.EndNanos - h.StartNanos)
	default:
		s.Duration = time.Duration(h.DurationMillis * float64(time.Millisecond))
	}
	return s, nil
}

// bounds converts a query range to API seconds: start inclusive, end exclusive.
func bounds(q obs.Query, now time.Time) (start, end int64) {
	from, to := q.Range.Resolve(now)
	if q.Cursor != nil && !q.Cursor.End.IsZero() {
		to = q.Cursor.End
	}
	start, end = from.Unix(), to.Unix()
	if to.Nanosecond() > 0 {
		end++
	}
	return start, end
}

// andQuery requires both a user query (possibly empty) and a term.
func andQuery(q, term string) string {
	if t := queryText(q); t != "*" {
		return fmt.Sprintf("(%s) AND %s", t, term)
	}
	return term
}

var _ traces.TopologyReader = (*Traces)(nil)

// Topology implements traces.TopologyReader.
func (t *Traces) Topology(ctx context.Context, from, to time.Time) (traces.Topology, error) {
	end := to.Unix()
	if to.Nanosecond() > 0 {
		end++
	}
	resp, err := t.api.TraceTopology(ctx, from.Unix(), end)
	if err != nil {
		return traces.Topology{}, err
	}
	ms := func(v *float64) time.Duration {
		if v == nil {
			return 0
		}
		return time.Duration(*v * float64(time.Millisecond))
	}
	var topo traces.Topology
	for _, n := range resp.Nodes {
		topo.Services = append(topo.Services, traces.ServiceStats{
			Name: n.ServiceName, Spans: n.SpanCount, Errors: n.ErrorCount, P50: ms(n.P50), P95: ms(n.P95), P99: ms(n.P99),
		})
	}
	for _, e := range resp.Edges {
		topo.Calls = append(topo.Calls, traces.Call{From: e.Source, To: e.Target, Count: e.CallCount})
	}
	return topo, nil
}
