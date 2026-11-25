package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func key(r logs.Record) uint64 { return r.Fingerprint() }

func drain(t *testing.T, p *engine.Pager[logs.Record]) []logs.Record {
	t.Helper()
	var all []logs.Record
	for calls := 0; !p.Done(); calls++ {
		if calls > 10_000 {
			t.Fatal("pager does not terminate")
		}
		items, _, err := p.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, items...)
	}
	return all
}

func TestPagerWithFakeSource(t *testing.T) {
	src := fake.NewLogs(11, func() time.Time { return now })
	p := engine.NewPager[logs.Record](src, obs.Query{Range: obs.Last(10 * time.Minute), Limit: 250}, now, key)
	if p.Query().Range.IsRelative() {
		t.Fatal("range must be pinned")
	}
	all := drain(t, p)
	if int64(len(all)) != p.Total() || p.Returned() != len(all) || len(all) == 0 {
		t.Fatalf("collected %d, total %d, returned %d", len(all), p.Total(), p.Returned())
	}
	if items, _, _ := p.Next(context.Background()); items != nil {
		t.Error("Next after Done must return nothing")
	}
}

// quickwitAPI emulates the platform's log search over an in-memory dataset,
// including the 10 000 offset limit, second-granularity bounds and the
// newest-first order.
type quickwitAPI struct {
	docs  []int64 // timestamps in nanoseconds, newest first
	calls int
}

func newQuickwitAPI(perSecond, seconds int, last int64) *quickwitAPI {
	q := &quickwitAPI{}
	for s := range seconds {
		for i := range perSecond {
			q.docs = append(q.docs, (last-int64(s))*1e9+int64(i)*1e3)
		}
	}
	sort.Slice(q.docs, func(i, j int) bool { return q.docs[i] > q.docs[j] })
	return q
}

func (q *quickwitAPI) SearchLogs(_ context.Context, req api.LogSearchRequest) (*api.LogSearchResponse, error) {
	q.calls++
	if req.StartOffset > api.MaxStartOffset {
		return nil, errors.New("max value for start_offset is 10_000")
	}
	var match []int64
	for _, ts := range q.docs {
		if ts >= *req.Start*1e9 && ts < *req.End*1e9 {
			match = append(match, ts)
		}
	}
	resp := &api.LogSearchResponse{NumHits: int64(len(match))}
	for i := req.StartOffset; i < len(match) && i < req.StartOffset+req.MaxHits; i++ {
		resp.Hits = append(resp.Hits, json.RawMessage(fmt.Sprintf(`{"timestamp_nanos":%d,"body":"doc"}`, match[i])))
	}
	return resp, nil
}

func TestPagerPastTheOffsetLimit(t *testing.T) {
	last := now.Unix() - 1
	qw := newQuickwitAPI(1500, 20, last) // 30 000 docs, 1 500 per second
	src := remote.NewLogs(qw, func() time.Time { return now })
	p := engine.NewPager[logs.Record](src, obs.Query{Range: obs.Last(time.Hour), Limit: 1000}, now, key)

	all := drain(t, p)
	if len(all) != len(qw.docs) {
		t.Fatalf("collected %d of %d docs (total reported %d)", len(all), len(qw.docs), p.Total())
	}
	seen := map[int64]bool{}
	for i, r := range all {
		ts := r.Timestamp.UnixNano()
		if seen[ts] {
			t.Fatalf("duplicate %d", ts)
		}
		seen[ts] = true
		if i > 0 && r.Timestamp.After(all[i-1].Timestamp) {
			t.Fatal("order broken across the window slide")
		}
	}
}

func TestPagerCrowdedSecondTerminates(t *testing.T) {
	// 12 000 docs within one second cannot all be reached: the pager must stop.
	qw := newQuickwitAPI(12_000, 1, now.Unix()-1)
	src := remote.NewLogs(qw, func() time.Time { return now })
	p := engine.NewPager[logs.Record](src, obs.Query{Range: obs.Last(time.Hour), Limit: 1000}, now, key)
	all := drain(t, p)
	if len(all) < 10_000 || len(all) > 12_000 {
		t.Fatalf("collected %d", len(all))
	}
}

type pagesStub struct {
	pages []obs.Page[logs.Record]
	err   error
	i     int
}

func (s *pagesStub) Search(context.Context, obs.Query) (obs.Page[logs.Record], error) {
	if s.err != nil {
		return obs.Page[logs.Record]{}, s.err
	}
	p := s.pages[s.i]
	s.i++
	return p, nil
}

func rec(sec int64, body string) logs.Record {
	return logs.Record{Timestamp: time.Unix(sec, 0), Body: body}
}

func TestPagerSkipsDuplicatePagesAndKeepsWarnings(t *testing.T) {
	a, b, c := rec(3, "a"), rec(2, "b"), rec(1, "c")
	more := &obs.Cursor{}
	stub := &pagesStub{pages: []obs.Page[logs.Record]{
		{Items: []logs.Record{a, b}, Total: 3, Next: more},
		{Items: []logs.Record{a, b}, Next: more, Warnings: []string{"w"}}, // only duplicates
		{Items: []logs.Record{b, c}},
	}}
	p := engine.NewPager[logs.Record](stub, obs.Query{Range: obs.Last(time.Hour)}, now, key)
	first, _, _ := p.Next(context.Background())
	second, warnings, err := p.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 1 || second[0].Body != "c" || !p.Done() {
		t.Fatalf("first=%v second=%v done=%v", first, second, p.Done())
	}
	if len(warnings) != 1 || p.Total() != 3 {
		t.Errorf("warnings=%v total=%d", warnings, p.Total())
	}
}

func TestPagerError(t *testing.T) {
	boom := errors.New("boom")
	p := engine.NewPager[logs.Record](&pagesStub{err: boom}, obs.Query{}, now, key)
	if _, _, err := p.Next(context.Background()); !errors.Is(err, boom) || p.Done() {
		t.Fatalf("err=%v done=%v", err, p.Done())
	}
}
