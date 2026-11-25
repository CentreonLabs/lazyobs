package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Trace sort fields and directions accepted by the trace search.
const (
	TraceSortTimestamp = "timestamp"
	TraceSortDuration  = "duration"
	SortAsc            = "asc"
	SortDesc           = "desc"
)

// TraceSearchRequest is the body of POST /v1/orgs/{org}/traces/search.
// The search matches root spans only: one hit per trace.
type TraceSearchRequest struct {
	Query         string            `json:"query"`
	Start         *int64            `json:"start,omitempty"`
	End           *int64            `json:"end,omitempty"`
	MaxHits       int               `json:"max_hits"`
	StartOffset   int               `json:"start_offset,omitempty"`
	SortField     string            `json:"sort_field,omitempty"`
	SortDirection string            `json:"sort_direction,omitempty"`
	Aggregation   *TraceAggregation `json:"aggregation,omitempty"`
}

// TraceAggregation asks for the trace count over time.
type TraceAggregation struct {
	TargetResolution int `json:"targetResolution"`
	IntervalSeconds  int `json:"interval"` // required by the trace search
}

// TraceSearchResponse is the response of the trace search.
type TraceSearchResponse struct {
	// Hits are root spans, decoded with DecodeSpan.
	Hits    []json.RawMessage `json:"hits"`
	NumHits int64             `json:"num_hits"`
	Errors  []json.RawMessage `json:"errors"`
	Curves  []TraceCurve      `json:"curves"`
}

// TraceCurve is a trace count series. Times are Unix milliseconds.
type TraceCurve struct {
	Metric string    `json:"metric"`
	Times  []int64   `json:"times"`
	Data   []float64 `json:"data"`
	// Attributes is documented as an object but returned as an array.
	Attributes json.RawMessage `json:"attributes"`
}

// SpanHit is a span as stored by the platform.
type SpanHit struct {
	TraceID            string         `json:"trace_id"`
	SpanID             string         `json:"span_id"`
	ParentSpanID       string         `json:"parent_span_id"`
	ServiceName        string         `json:"service_name"`
	SpanName           string         `json:"span_name"`
	SpanKind           int            `json:"span_kind"`
	StartNanos         int64          `json:"span_start_timestamp_nanos"`
	EndNanos           int64          `json:"span_end_timestamp_nanos"`
	DurationMillis     float64        `json:"span_duration_millis"`
	Status             SpanStatus     `json:"span_status"`
	SpanAttributes     map[string]any `json:"span_attributes"`
	ResourceAttributes map[string]any `json:"resource_attributes"`
}

// SpanStatus is the outcome of a span.
type SpanStatus struct {
	Code    string `json:"code"` // ok, error, unset
	Message string `json:"message"`
}

// DecodeSpan decodes one raw span.
func DecodeSpan(raw json.RawMessage) (SpanHit, error) {
	var s SpanHit
	if err := json.Unmarshal(raw, &s); err != nil {
		return SpanHit{}, fmt.Errorf("api: decode span: %w", err)
	}
	return s, nil
}

// TraceDetailResponse is a whole trace. Spans come in no particular order.
type TraceDetailResponse struct {
	TraceID        string            `json:"traceId"`
	RootSpanID     string            `json:"rootSpanId"`
	RootSpanStatus string            `json:"rootSpanStatus"`
	Services       []string          `json:"services"`
	TraceName      string            `json:"traceName"`
	TotalDuration  float64           `json:"totalDuration"` // milliseconds
	Spans          []json.RawMessage `json:"spans"`
}

// SearchTraces searches traces (root spans). It is a beta feature: other
// organizations get a 404.
func (c *Client) SearchTraces(ctx context.Context, req TraceSearchRequest) (*TraceSearchResponse, error) {
	if req.MaxHits < 0 || req.MaxHits > MaxHits {
		return nil, fmt.Errorf("api: max_hits must be within [0, %d], got %d", MaxHits, req.MaxHits)
	}
	if req.StartOffset < 0 || req.StartOffset > MaxStartOffset {
		return nil, fmt.Errorf("api: start_offset must be within [0, %d], got %d", MaxStartOffset, req.StartOffset)
	}
	if req.Query == "" {
		req.Query = "*"
	}
	var resp TraceSearchResponse
	if err := c.do(ctx, http.MethodPost, c.orgURL("traces", "search"), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTrace fetches a trace with all its spans.
func (c *Client) GetTrace(ctx context.Context, traceID string) (*TraceDetailResponse, error) {
	if traceID == "" {
		return nil, errors.New("api: empty trace ID")
	}
	var resp TraceDetailResponse
	if err := c.do(ctx, http.MethodGet, c.orgURL("traces", traceID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TraceFieldCapabilities lists the span fields seen over [from, to).
func (c *Client) TraceFieldCapabilities(ctx context.Context, from, to time.Time) (map[string]map[string]FieldCapability, error) {
	return c.fieldCapabilities(ctx, "traces", from, to)
}

// TraceFieldTerms returns the most frequent values of a span field.
func (c *Client) TraceFieldTerms(ctx context.Context, field, prefix string, from, to time.Time, maxTerms int) ([]Term, error) {
	return c.fieldTerms(ctx, "traces", field, prefix, from, to, maxTerms)
}
