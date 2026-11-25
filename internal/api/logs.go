package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Limits enforced by the log search endpoint.
const (
	MaxHits        = 1000
	MaxStartOffset = 10_000
)

// LogSearchRequest is the body of POST /v1/orgs/{org}/logs/search.
//
// Start and End are Unix timestamps in seconds; Start is inclusive and End
// exclusive. A negative Start is relative to End. A nil End means "now".
type LogSearchRequest struct {
	Query       string       `json:"query"`
	Start       *int64       `json:"start,omitempty"`
	End         *int64       `json:"end,omitempty"`
	MaxHits     int          `json:"max_hits"`
	StartOffset int          `json:"start_offset,omitempty"`
	Aggregation *Aggregation `json:"aggregation,omitempty"`
}

// AggregationType selects how hits are bucketed into curves.
type AggregationType string

// Built-in aggregation types. Use CustomAggregation for a field-based one.
const (
	AggregateCount       AggregationType = "count"
	AggregateSeverity    AggregationType = "severity"
	AggregateServiceName AggregationType = "service_name"
)

// Aggregation requests time-series curves alongside the hits.
type Aggregation struct {
	Type AggregationType
	// CustomField groups counts by this field (at most 20 values); it overrides Type.
	CustomField      string
	TargetResolution int // number of points wanted, 10–50000
	IntervalSeconds  int // optional, computed from TargetResolution when 0
}

// MarshalJSON renders the oneOf shape expected by the API.
func (a Aggregation) MarshalJSON() ([]byte, error) {
	var typ any = a.Type
	if a.CustomField != "" {
		typ = map[string]any{"custom": map[string]string{"field": a.CustomField}}
	}
	payload := map[string]any{
		"aggregationType":  typ,
		"targetResolution": a.TargetResolution,
	}
	if a.IntervalSeconds > 0 {
		payload["interval"] = a.IntervalSeconds
	}
	return json.Marshal(payload)
}

// LogSearchResponse is the response of the log search endpoint.
type LogSearchResponse struct {
	// Hits are the raw documents, newest first. Decode them with DecodeLogHit.
	Hits              []json.RawMessage `json:"hits"`
	NumHits           int64             `json:"num_hits"`
	ElapsedTimeMicros int64             `json:"elapsed_time_micros"`
	Errors            []json.RawMessage `json:"errors"`
	Curves            []Curve           `json:"curves"`
}

// Curve is one aggregated time series. Times are Unix milliseconds.
type Curve struct {
	Metric     string      `json:"metric"`
	Times      []int64     `json:"times"`
	Data       []float64   `json:"data"`
	Attributes [][2]string `json:"attributes"`
	Unit       *string     `json:"unit"`
}

// LogHit is a log document as stored by the platform. Only the body and
// timestamp are always present; every other field is optional.
type LogHit struct {
	TimestampNanos         int64           `json:"timestamp_nanos"`
	ObservedTimestampNanos int64           `json:"observed_timestamp_nanos"`
	SeverityText           string          `json:"severity_text"`
	SeverityNumber         int             `json:"severity_number"`
	ServiceName            string          `json:"service_name"`
	Body                   json.RawMessage `json:"body"`
	Attributes             map[string]any  `json:"attributes"`
	ResourceAttributes     map[string]any  `json:"resource_attributes"`
	ScopeName              string          `json:"scope_name"`
	ScopeVersion           string          `json:"scope_version"`
	TraceID                string          `json:"trace_id"`
	SpanID                 string          `json:"span_id"`
	TraceFlags             int             `json:"trace_flags"`
}

// DecodeLogHit decodes one raw hit.
func DecodeLogHit(raw json.RawMessage) (LogHit, error) {
	var h LogHit
	if err := json.Unmarshal(raw, &h); err != nil {
		return LogHit{}, fmt.Errorf("api: decode log hit: %w", err)
	}
	return h, nil
}

// SearchLogs runs a log search, optionally with an aggregation.
func (c *Client) SearchLogs(ctx context.Context, req LogSearchRequest) (*LogSearchResponse, error) {
	if req.MaxHits < 0 || req.MaxHits > MaxHits {
		return nil, fmt.Errorf("api: max_hits must be within [0, %d], got %d", MaxHits, req.MaxHits)
	}
	if req.StartOffset < 0 || req.StartOffset > MaxStartOffset {
		return nil, fmt.Errorf("api: start_offset must be within [0, %d], got %d", MaxStartOffset, req.StartOffset)
	}
	if req.Query == "" {
		req.Query = "*"
	}
	var resp LogSearchResponse
	if err := c.do(ctx, http.MethodPost, c.orgURL("logs", "search"), req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
