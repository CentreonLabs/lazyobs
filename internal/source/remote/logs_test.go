package remote_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 500_000_000, time.UTC)

type stubAPI struct {
	requests []api.LogSearchRequest
	respond  func(api.LogSearchRequest) (*api.LogSearchResponse, error)
}

func (s *stubAPI) SearchLogs(_ context.Context, req api.LogSearchRequest) (*api.LogSearchResponse, error) {
	s.requests = append(s.requests, req)
	return s.respond(req)
}

func fixtureResponse(t *testing.T, name string) *api.LogSearchResponse {
	t.Helper()
	b, err := os.ReadFile("../../api/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var resp api.LogSearchResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatal(err)
	}
	return &resp
}

func TestSearchMapsRecords(t *testing.T) {
	stub := &stubAPI{respond: func(api.LogSearchRequest) (*api.LogSearchResponse, error) {
		return fixtureResponse(t, "logs_search.json"), nil
	}}
	src := remote.NewLogs(stub, func() time.Time { return now })

	page, err := src.Search(context.Background(), obs.Query{Range: obs.Last(time.Hour), Limit: 3})
	if err != nil {
		t.Fatal(err)
	}

	req := stub.requests[0]
	if req.Query != "*" || req.MaxHits != 3 || req.StartOffset != 0 {
		t.Errorf("unexpected request %+v", req)
	}
	// now = 12:00:00.5 → start floors to 11:00:00, end rounds up to 12:00:01.
	if *req.Start != now.Add(-time.Hour).Unix() || *req.End != now.Unix()+1 {
		t.Errorf("bounds = [%d, %d)", *req.Start, *req.End)
	}

	if page.Total != 1983 || len(page.Items) != 3 {
		t.Fatalf("total=%d items=%d", page.Total, len(page.Items))
	}
	first := page.Items[0]
	if first.Timestamp.UnixNano() != 1793713556297395112 || first.Severity != logs.SeverityWarn ||
		first.Service != "query" || first.Body != "connection pool exhausted, retrying" ||
		first.ResourceAttributes["service.version"] != "0.1.0" || len(first.Raw) == 0 {
		t.Errorf("unexpected first record %+v", first)
	}
	third := page.Items[2]
	if third.Body != "plain string body" || third.Severity != logs.SeverityError ||
		third.TraceID == "" || !third.ObservedTimestamp.IsZero() {
		t.Errorf("unexpected third record %+v", third)
	}
	if page.Next == nil || page.Next.Offset != 3 || page.Next.End.Unix() != now.Unix()+1 {
		t.Errorf("next = %+v, want offset 3 pinned to the queried end", page.Next)
	}
}

// hits builds n hits, one per millisecond, going back from the second sec.
func hits(sec int64, n int) []json.RawMessage {
	out := make([]json.RawMessage, n)
	for i := range n {
		ts := sec*1e9 + int64(999-i%1000)*1e6
		out[i] = json.RawMessage(fmt.Sprintf(`{"timestamp_nanos":%d,"body":{"message":"m%d"}}`, ts, i))
	}
	return out
}

func TestPaginationCursors(t *testing.T) {
	end := now.Unix()
	cases := []struct {
		name      string
		cursor    *obs.Cursor
		returned  int
		total     int64
		oldestSec int64
		want      *obs.Cursor
	}{
		{"short page is the last", nil, 50, 50, end - 1, nil},
		{"total reached", &obs.Cursor{Offset: 900, End: time.Unix(end, 0)}, 100, 1000, end - 1, nil},
		{"offset pagination", &obs.Cursor{Offset: 9900, End: time.Unix(end, 0)}, 100, 50_000, end - 1, &obs.Cursor{Offset: 10_000, End: time.Unix(end, 0)}},
		{"slide window past the offset limit", &obs.Cursor{Offset: 10_000, End: time.Unix(end, 0)}, 100, 50_000, end - 120, &obs.Cursor{Offset: 0, End: time.Unix(end-119, 0)}},
		{"single crowded second is skipped", &obs.Cursor{Offset: 10_000, End: time.Unix(end, 0)}, 100, 50_000, end - 1, &obs.Cursor{Offset: 0, End: time.Unix(end-1, 0)}},
		{"window exhausted", &obs.Cursor{Offset: 10_000, End: time.Unix(end-3599, 0)}, 100, 50_000, end - 3600, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubAPI{respond: func(api.LogSearchRequest) (*api.LogSearchResponse, error) {
				return &api.LogSearchResponse{Hits: hits(tc.oldestSec, tc.returned), NumHits: tc.total}, nil
			}}
			src := remote.NewLogs(stub, func() time.Time { return now })
			page, err := src.Search(context.Background(), obs.Query{
				Range: obs.Between(time.Unix(end-3600, 0), time.Unix(end, 0)), Limit: 100, Cursor: tc.cursor,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if page.Next != nil {
					t.Fatalf("next = %+v, want nil", page.Next)
				}
				return
			}
			if page.Next == nil || page.Next.Offset != tc.want.Offset || !page.Next.End.Equal(tc.want.End) {
				t.Fatalf("next = %+v, want %+v", page.Next, tc.want)
			}
			if tc.cursor != nil && *stub.requests[0].End != tc.cursor.End.Unix() {
				t.Errorf("cursor end not applied: %d", *stub.requests[0].End)
			}
		})
	}
}

func TestSearchErrorsAndWarnings(t *testing.T) {
	boom := errors.New("boom")
	src := remote.NewLogs(&stubAPI{respond: func(api.LogSearchRequest) (*api.LogSearchResponse, error) {
		return nil, boom
	}}, nil)
	if _, err := src.Search(context.Background(), obs.Query{Range: obs.Last(time.Minute)}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}

	src = remote.NewLogs(&stubAPI{respond: func(req api.LogSearchRequest) (*api.LogSearchResponse, error) {
		if req.MaxHits != remote.DefaultLimit {
			t.Errorf("default limit not applied: %d", req.MaxHits)
		}
		return &api.LogSearchResponse{Errors: []json.RawMessage{json.RawMessage(`"split 3 timed out"`)}}, nil
	}}, nil)
	page, err := src.Search(context.Background(), obs.Query{Range: obs.Last(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Warnings) != 1 || page.Warnings[0] != "partial results: split 3 timed out" {
		t.Errorf("warnings = %v", page.Warnings)
	}
}

func TestBodyFlattening(t *testing.T) {
	cases := map[string]string{
		`"text"`:                        "text",
		`{"message":"hello","extra":1}`: "hello",
		`{"level":"x", "n": 2}`:         `{"level":"x","n":2}`,
		`null`:                          "",
		`42`:                            "42",
	}
	for body, want := range cases {
		stub := &stubAPI{respond: func(api.LogSearchRequest) (*api.LogSearchResponse, error) {
			return &api.LogSearchResponse{Hits: []json.RawMessage{json.RawMessage(`{"timestamp_nanos":1,"body":` + body + `}`)}, NumHits: 1}, nil
		}}
		page, err := remote.NewLogs(stub, nil).Search(context.Background(), obs.Query{Range: obs.Last(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if got := page.Items[0].Body; got != want {
			t.Errorf("body %s → %q, want %q", body, got, want)
		}
	}
}

func TestHistogram(t *testing.T) {
	stub := &stubAPI{respond: func(api.LogSearchRequest) (*api.LogSearchResponse, error) {
		return fixtureResponse(t, "logs_search_aggregation.json"), nil
	}}
	series, err := remote.NewLogs(stub, func() time.Time { return now }).
		Histogram(context.Background(), obs.Query{Text: "service_name:api", Range: obs.Last(time.Hour)}, 3)
	if err != nil {
		t.Fatal(err)
	}
	req := stub.requests[0]
	if req.MaxHits != 0 || req.Aggregation == nil || req.Aggregation.Type != api.AggregateSeverity ||
		req.Aggregation.TargetResolution != 10 || req.Query != "service_name:api" {
		t.Errorf("unexpected request %+v (aggregation %+v)", req, req.Aggregation)
	}
	if len(series) != 2 || len(series[0].Points) != 3 {
		t.Fatalf("series = %+v", series)
	}
	p := series[0].Points[2]
	if !p.Time.Equal(time.UnixMilli(1793023200000)) || p.Value != 25.5 || series[0].Labels["severity_number"] != "13-17" ||
		series[0].Name != "WARN" || series[1].Name != "ERROR" {
		t.Errorf("unexpected point %+v labels %v", p, series[0].Labels)
	}
}
