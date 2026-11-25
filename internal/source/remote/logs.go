// Package remote adapts the platform API client to the domain interfaces of
// internal/obs. It owns everything API-specific: time bounds in seconds,
// the offset limit and the pagination strategy that works around it.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// DefaultLimit is the page size used when the query does not set one.
const DefaultLimit = 100

// LogsAPI is the subset of the API client used by Logs; tests stub it.
type LogsAPI interface {
	SearchLogs(ctx context.Context, req api.LogSearchRequest) (*api.LogSearchResponse, error)
}

// Logs searches logs on the platform.
type Logs struct {
	api LogsAPI
	now func() time.Time
}

var (
	_ obs.Searcher[logs.Record] = (*Logs)(nil)
	_ obs.Histogrammer          = (*Logs)(nil)
)

// NewLogs returns a log source backed by client. A nil now uses time.Now.
func NewLogs(client LogsAPI, now func() time.Time) *Logs {
	if now == nil {
		now = time.Now
	}
	return &Logs{api: client, now: now}
}

// Search returns one page of logs, newest first.
//
// Pages are fetched by offset up to the API limit (10 000). Beyond it, the
// next cursor moves the end of the window to the second of the oldest record
// seen and restarts at offset 0. Records of that boundary second are returned
// again: callers paginating past the limit must deduplicate (see engine.Pager).
func (l *Logs) Search(ctx context.Context, q obs.Query) (obs.Page[logs.Record], error) {
	start, end := l.bounds(q)
	limit := clampLimit(q.Limit)
	offset := 0
	if q.Cursor != nil {
		offset = q.Cursor.Offset
	}

	resp, err := l.api.SearchLogs(ctx, api.LogSearchRequest{
		Query:       queryText(q.Text),
		Start:       &start,
		End:         &end,
		MaxHits:     limit,
		StartOffset: offset,
	})
	if err != nil {
		return obs.Page[logs.Record]{}, err
	}

	page := obs.Page[logs.Record]{Total: resp.NumHits, Warnings: warnings(resp.Errors)}
	page.Items = make([]logs.Record, 0, len(resp.Hits))
	for _, raw := range resp.Hits {
		rec, err := toRecord(raw)
		if err != nil {
			return obs.Page[logs.Record]{}, err
		}
		page.Items = append(page.Items, rec)
	}
	page.Next = nextCursor(page, start, end, offset, limit)
	return page, nil
}

// Histogram returns the number of logs matching q over time.
func (l *Logs) Histogram(ctx context.Context, q obs.Query, buckets int) ([]obs.Series, error) {
	start, end := l.bounds(q)
	resp, err := l.api.SearchLogs(ctx, api.LogSearchRequest{
		Query:   queryText(q.Text),
		Start:   &start,
		End:     &end,
		MaxHits: 0,
		Aggregation: &api.Aggregation{
			Type:             api.AggregateCount,
			TargetResolution: min(max(buckets, 10), 50_000),
		},
	})
	if err != nil {
		return nil, err
	}
	series := make([]obs.Series, 0, len(resp.Curves))
	for _, c := range resp.Curves {
		s := obs.Series{Name: c.Metric, Points: make([]obs.Point, 0, len(c.Times))}
		if c.Unit != nil {
			s.Unit = *c.Unit
		}
		if len(c.Attributes) > 0 {
			s.Labels = make(map[string]string, len(c.Attributes))
			for _, kv := range c.Attributes {
				s.Labels[kv[0]] = kv[1]
			}
		}
		for i, ms := range c.Times {
			if i < len(c.Data) {
				s.Points = append(s.Points, obs.Point{Time: time.UnixMilli(ms), Value: c.Data[i]})
			}
		}
		series = append(series, s)
	}
	return series, nil
}

// bounds converts the query range to API seconds: start inclusive, end exclusive.
func (l *Logs) bounds(q obs.Query) (start, end int64) {
	from, to := q.Range.Resolve(l.now())
	if q.Cursor != nil && !q.Cursor.End.IsZero() {
		to = q.Cursor.End
	}
	start = from.Unix()
	end = to.Unix()
	if to.Nanosecond() > 0 {
		end++ // round up so the last partial second stays included
	}
	return start, end
}

func nextCursor(page obs.Page[logs.Record], start, end int64, offset, limit int) *obs.Cursor {
	fetched := offset + len(page.Items)
	if len(page.Items) < limit || int64(fetched) >= page.Total {
		return nil
	}
	if fetched <= api.MaxStartOffset {
		return &obs.Cursor{Offset: fetched, End: time.Unix(end, 0)}
	}
	// Offset limit reached: slide the window to the oldest second seen.
	oldest := page.Items[len(page.Items)-1].Timestamp.Unix()
	newEnd := oldest + 1
	if newEnd >= end {
		// More than 10 000 records within a single second: skip the rest of it
		// rather than fetching the same records forever.
		newEnd = oldest
	}
	if newEnd <= start {
		return nil
	}
	return &obs.Cursor{Offset: 0, End: time.Unix(newEnd, 0)}
}

func clampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	return min(n, api.MaxHits)
}

func queryText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "*"
	}
	return s
}

func warnings(errs []json.RawMessage) []string {
	if len(errs) == 0 {
		return nil
	}
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		var s string
		if json.Unmarshal(e, &s) != nil {
			s = string(e)
		}
		out = append(out, "partial results: "+s)
	}
	return out
}

func toRecord(raw json.RawMessage) (logs.Record, error) {
	h, err := api.DecodeLogHit(raw)
	if err != nil {
		return logs.Record{}, err
	}
	rec := logs.Record{
		Timestamp:          time.Unix(0, h.TimestampNanos),
		Severity:           logs.ResolveSeverity(h.SeverityNumber, h.SeverityText),
		SeverityText:       h.SeverityText,
		SeverityNumber:     h.SeverityNumber,
		Service:            h.ServiceName,
		Body:               flattenBody(h.Body),
		Attributes:         h.Attributes,
		ResourceAttributes: h.ResourceAttributes,
		ScopeName:          h.ScopeName,
		ScopeVersion:       h.ScopeVersion,
		TraceID:            h.TraceID,
		SpanID:             h.SpanID,
		TraceFlags:         h.TraceFlags,
		Raw:                bytes.Clone(raw),
	}
	if h.ObservedTimestampNanos != 0 {
		rec.ObservedTimestamp = time.Unix(0, h.ObservedTimestampNanos)
	}
	return rec, nil
}

// flattenBody turns the OTel body into a one-line message: a string body as is,
// the "message" key of an object body, or compact JSON otherwise.
func flattenBody(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		if msg, ok := obj["message"]; ok && json.Unmarshal(msg, &s) == nil {
			return s
		}
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String()
	}
	return string(raw)
}
