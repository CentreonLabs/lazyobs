// Package fake provides synthetic, deterministic data sources implementing the
// obs interfaces. They back unit tests, development without platform access,
// and the --demo mode.
//
// Data is a pure function of the seed and the second it belongs to, so any
// time window can be queried in any order with consistent results, and new
// records "arrive" as the injected clock moves forward.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// MaxPerSecond is the maximum number of records generated per second.
const MaxPerSecond = 4

// Logs is a synthetic log source.
type Logs struct {
	seed uint64
	now  func() time.Time
	// Latency, when set, delays every call to mimic the network.
	Latency time.Duration
}

var (
	_ obs.Searcher[logs.Record] = (*Logs)(nil)
	_ obs.Histogrammer          = (*Logs)(nil)
)

// NewLogs returns a source generating logs from seed. A nil now uses time.Now.
func NewLogs(seed uint64, now func() time.Time) *Logs {
	if now == nil {
		now = time.Now
	}
	return &Logs{seed: seed, now: now}
}

// Search returns one page of matching logs, newest first, using offset pagination.
func (l *Logs) Search(ctx context.Context, q obs.Query) (obs.Page[logs.Record], error) {
	if err := l.wait(ctx); err != nil {
		return obs.Page[logs.Record]{}, err
	}
	from, to := l.window(q)
	f, err := parseFilter(q.Text)
	if err != nil {
		return obs.Page[logs.Record]{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if q.Cursor != nil {
		offset = q.Cursor.Offset
	}

	page := obs.Page[logs.Record]{Items: []logs.Record{}}
	matched := 0
	first, last := from.Unix(), ceilSecond(to)-1
	for sec := last; sec >= first; sec-- {
		partial := (sec == first && from.Nanosecond() > 0) || (sec == last && to.Nanosecond() > 0)
		if f.isEmpty() && !partial {
			// Fast path: whole seconds outside the page only need counting.
			if c := l.count(sec); matched+c <= offset || len(page.Items) == limit {
				matched += c
				continue
			}
		}
		for _, r := range l.recordsAt(sec) {
			if r.Timestamp.Before(from) || !r.Timestamp.Before(to) || !f.match(r) {
				continue
			}
			if matched >= offset && len(page.Items) < limit {
				page.Items = append(page.Items, withRaw(r))
			}
			matched++
		}
	}
	page.Total = int64(matched)
	if offset+len(page.Items) < matched {
		page.Next = &obs.Cursor{Offset: offset + len(page.Items), End: to}
	}
	return page, nil
}

// Histogram counts matching logs per bucket over the query range.
func (l *Logs) Histogram(ctx context.Context, q obs.Query, buckets int) ([]obs.Series, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	from, to := l.window(q)
	f, err := parseFilter(q.Text)
	if err != nil {
		return nil, err
	}
	buckets = max(buckets, 1)
	step := max(to.Sub(from)/time.Duration(buckets), time.Second).Truncate(time.Second)
	s := obs.Series{Name: "log count"}
	for t := from.Truncate(time.Second); t.Before(to); t = t.Add(step) {
		var n float64
		for sec := t.Unix(); sec < t.Add(step).Unix() && sec < ceilSecond(to); sec++ {
			if f.isEmpty() {
				n += float64(l.count(sec))
				continue
			}
			for _, r := range l.recordsAt(sec) {
				if f.match(r) {
					n++
				}
			}
		}
		s.Points = append(s.Points, obs.Point{Time: t, Value: n})
	}
	return []obs.Series{s}, nil
}

func (l *Logs) wait(ctx context.Context) error {
	if l.Latency <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(l.Latency):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// window resolves the query bounds, never extending past "now": future logs do not exist yet.
func (l *Logs) window(q obs.Query) (from, to time.Time) {
	now := l.now()
	from, to = q.Range.Resolve(now)
	if q.Cursor != nil && !q.Cursor.End.IsZero() {
		to = q.Cursor.End
	}
	if to.After(now) {
		to = now
	}
	return from, to
}

func (l *Logs) rng(sec int64) *rand.Rand {
	return rand.New(rand.NewPCG(l.seed, uint64(sec))) //nolint:gosec // synthetic data, not security sensitive
}

// count returns the number of records generated for sec without building them.
func (l *Logs) count(sec int64) int { return l.rng(sec).IntN(MaxPerSecond + 1) }

// recordsAt generates the records of one second, newest first.
func (l *Logs) recordsAt(sec int64) []logs.Record {
	r := l.rng(sec)
	n := r.IntN(MaxPerSecond + 1) // must stay the first draw, see count
	out := make([]logs.Record, n)
	for i := range out {
		svc := services[r.IntN(len(services))]
		sev := pickSeverity(r)
		tmpl := messages[sev][r.IntN(len(messages[sev]))]
		rec := logs.Record{
			Timestamp:      time.Unix(sec, r.Int64N(int64(time.Second))),
			Severity:       sev,
			SeverityText:   sev.String(),
			SeverityNumber: int(sev-1)*4 + 1,
			Service:        svc.name,
			Body:           fillTemplate(tmpl, r),
			Attributes: map[string]any{
				"http.method":      methods[r.IntN(len(methods))],
				"http.route":       svc.routes[r.IntN(len(svc.routes))],
				"http.status_code": statusFor(sev, r),
			},
			ResourceAttributes: map[string]any{
				"service.version": svc.version,
				"host.name":       fmt.Sprintf("%s-%02d", svc.name, r.IntN(3)+1),
			},
			ScopeName:    "lazyobs-demo",
			ScopeVersion: "1.0.0",
		}
		if r.IntN(3) > 0 {
			rec.TraceID = fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64())
			rec.SpanID = fmt.Sprintf("%016x", r.Uint64())
			rec.TraceFlags = 1
		}
		rec.ObservedTimestamp = rec.Timestamp.Add(time.Duration(r.IntN(900)+50) * time.Millisecond)
		out[i] = rec
	}
	slices.SortFunc(out, func(a, b logs.Record) int { return b.Timestamp.Compare(a.Timestamp) })
	return out
}

// withRaw attaches a JSON document shaped like the platform's.
func withRaw(r logs.Record) logs.Record {
	doc := map[string]any{
		"timestamp_nanos":          r.Timestamp.UnixNano(),
		"observed_timestamp_nanos": r.ObservedTimestamp.UnixNano(),
		"severity_text":            r.SeverityText,
		"severity_number":          r.SeverityNumber,
		"service_name":             r.Service,
		"body":                     map[string]string{"message": r.Body},
		"attributes":               r.Attributes,
		"resource_attributes":      r.ResourceAttributes,
		"scope_name":               r.ScopeName,
		"scope_version":            r.ScopeVersion,
		"trace_flags":              r.TraceFlags,
	}
	if r.TraceID != "" {
		doc["trace_id"], doc["span_id"] = r.TraceID, r.SpanID
	}
	r.Raw, _ = json.Marshal(doc)
	return r
}

func ceilSecond(t time.Time) int64 {
	if t.Nanosecond() > 0 {
		return t.Unix() + 1
	}
	return t.Unix()
}

// filter is a tiny subset of the Quickwit syntax: whitespace-separated terms,
// all required. "field:value" matches service_name, severity_text or a
// top-level attribute; a bare word matches the body (case-insensitive);
// "*" and the AND keyword are ignored.
type filter struct {
	fields map[string]string
	words  []string
}

func parseFilter(text string) (filter, error) {
	f := filter{fields: map[string]string{}}
	for _, term := range strings.Fields(text) {
		if term == "*" || term == "AND" {
			continue
		}
		if strings.ContainsAny(term, "()") {
			return filter{}, fmt.Errorf("fake: unsupported query syntax %q", term)
		}
		if k, v, ok := strings.Cut(term, ":"); ok {
			f.fields[k] = strings.ToLower(strings.Trim(v, `"`))
			continue
		}
		f.words = append(f.words, strings.ToLower(strings.Trim(term, `"`)))
	}
	return f, nil
}

func (f filter) isEmpty() bool { return len(f.fields) == 0 && len(f.words) == 0 }

func (f filter) match(r logs.Record) bool {
	for k, want := range f.fields {
		var got string
		switch k {
		case "service_name":
			got = r.Service
		case "severity_text":
			got = r.SeverityText
		default:
			v, ok := r.Attributes[strings.TrimPrefix(k, "attributes.")]
			if !ok {
				return false
			}
			got = fmt.Sprint(v)
		}
		if !strings.EqualFold(got, want) {
			return false
		}
	}
	body := strings.ToLower(r.Body)
	for _, w := range f.words {
		if !strings.Contains(body, w) {
			return false
		}
	}
	return true
}
