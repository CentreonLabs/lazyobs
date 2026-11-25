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
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// MaxPerSecond is the maximum number of records generated per second.
const MaxPerSecond = 8

// Traffic shape: the rate follows a 30-minute wave, and every 20 minutes an
// incident lasting 2 minutes raises the volume and the share of errors.
const (
	wavePeriod     = 30 * 60
	incidentEvery  = 20 * 60
	incidentLength = 2 * 60
)

func inIncident(sec int64) bool { return sec%incidentEvery < incidentLength }

// maxAt is the maximum number of records generated during sec.
func maxAt(sec int64) int {
	wave := 1 + 0.6*math.Sin(2*math.Pi*float64(sec%wavePeriod)/wavePeriod)
	n := int(2.5*wave + 0.5) // 1 to 4
	if inIncident(sec) {
		n *= 2
	}
	return min(n, MaxPerSecond)
}

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

// maxSampledSeconds bounds the seconds generated per histogram bucket; larger
// buckets are estimated from evenly spread samples to stay fast on long ranges.
const maxSampledSeconds = 120

// Histogram counts matching logs per bucket over the query range, one series
// per severity (named after it), like the platform's severity aggregation.
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

	counts := map[logs.Severity][]float64{}
	var times []time.Time
	for t := from.Truncate(time.Second); t.Before(to); t = t.Add(step) {
		i := len(times)
		times = append(times, t)
		first, last := t.Unix(), min(t.Add(step).Unix(), ceilSecond(to))
		n := last - first
		stride := max(n/maxSampledSeconds, 1)
		for sec := first; sec < last; sec += stride {
			for _, r := range l.recordsAt(sec) {
				if !f.match(r) {
					continue
				}
				if counts[r.Severity] == nil {
					counts[r.Severity] = make([]float64, 0, buckets+1)
				}
				for len(counts[r.Severity]) <= i {
					counts[r.Severity] = append(counts[r.Severity], 0)
				}
				counts[r.Severity][i] += float64(stride)
			}
		}
	}

	var series []obs.Series
	for sev := logs.SeverityTrace; sev <= logs.SeverityFatal; sev++ {
		values, ok := counts[sev]
		if !ok {
			continue
		}
		s := obs.Series{Name: sev.String(), Labels: map[string]string{"severity": sev.String()}}
		for i, t := range times {
			var v float64
			if i < len(values) {
				v = values[i]
			}
			s.Points = append(s.Points, obs.Point{Time: t, Value: v})
		}
		series = append(series, s)
	}
	return series, nil
}

func (l *Logs) wait(ctx context.Context) error { return wait(ctx, l.Latency) }

// wait mimics network latency, honoring cancellation.
func wait(ctx context.Context, latency time.Duration) error {
	if latency <= 0 {
		return ctx.Err()
	}
	select {
	case <-time.After(latency):
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
func (l *Logs) count(sec int64) int { return l.rng(sec).IntN(maxAt(sec) + 1) }

// recordsAt generates the records of one second, newest first.
func (l *Logs) recordsAt(sec int64) []logs.Record {
	r := l.rng(sec)
	n := r.IntN(maxAt(sec) + 1) // must stay the first draw, see count
	out := make([]logs.Record, n)
	for i := range out {
		svc := services[r.IntN(len(services))]
		sev := pickSeverity(r, inIncident(sec))
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

// filter is a tiny subset of the Quickwit syntax: terms separated by
// spaces, all required (AND is implied); NOT or a leading "-" excludes the
// next term or parenthesized group (groups are not nested). "field:value"
// matches a field (body and body.message by substring, others exactly,
// case-insensitively); a bare word matches the body. Values may be quoted.
// "*" and the AND keyword are ignored; OR is rejected.
type filter struct{ groups []filterGroup }

// filterGroup is a term, or several in parentheses, that must all match.
type filterGroup struct {
	terms   []filterTerm
	negated bool
}

type filterTerm struct {
	field, value string // field is empty for a word of the body
	negated      bool
}

func parseFilter(text string) (filter, error) {
	var f filter
	var group *filterGroup // open parenthesized group
	negate := false
	for _, tok := range splitQuery(text) {
		switch {
		case tok == "*" || tok == "AND":
			continue
		case tok == "NOT":
			negate = true
			continue
		case tok == "OR" || strings.Count(tok, "(") > 1 || (group != nil && strings.HasPrefix(tok, "(")) ||
			strings.ContainsAny(strings.TrimSuffix(strings.TrimPrefix(tok, "("), ")"), "()"):
			return filter{}, fmt.Errorf("fake: unsupported query syntax %q", tok)
		}
		if rest, ok := strings.CutPrefix(tok, "("); ok {
			group = &filterGroup{negated: negate}
			negate, tok = false, rest
		}
		closing := false
		if rest, ok := strings.CutSuffix(tok, ")"); ok && !strings.HasSuffix(tok, `\")`) {
			if group == nil {
				return filter{}, fmt.Errorf("fake: unbalanced parenthesis in %q", text)
			}
			closing, tok = true, rest
		}
		t := filterTerm{negated: negate}
		negate = false
		if rest, ok := strings.CutPrefix(tok, "-"); ok {
			t.negated, tok = !t.negated, rest
		}
		if k, v, ok := strings.Cut(tok, ":"); ok && k != "" && !strings.HasPrefix(k, `"`) {
			t.field, t.value = k, unquote(v)
		} else {
			t.value = unquote(tok)
		}
		if group != nil {
			group.terms = append(group.terms, t)
		} else {
			f.groups = append(f.groups, filterGroup{terms: []filterTerm{t}})
		}
		if closing {
			f.groups = append(f.groups, *group)
			group = nil
		}
	}
	if group != nil {
		return filter{}, fmt.Errorf("fake: unbalanced parenthesis in %q", text)
	}
	return f, nil
}

// splitQuery splits on spaces outside double quotes.
func splitQuery(text string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for i, r := range text {
		switch {
		case r == '"' && (i == 0 || text[i-1] != '\\'):
			quoted = !quoted
			cur.WriteRune(r)
		case r == ' ' && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func unquote(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		v = strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
	}
	return v
}

func (f filter) isEmpty() bool { return len(f.groups) == 0 }

// document is what a filter sees of an item: its text (matched by bare
// words and the text fields) and a lookup of its other fields.
type document struct {
	text       string
	textFields []string // fields matched by substring against text
	field      func(name string) (string, bool)
}

func (f filter) match(r logs.Record) bool {
	return f.matchDoc(document{
		text:       r.Body,
		textFields: []string{"body", "body.message"},
		field: func(name string) (string, bool) {
			if v, ok := fieldValue(r, name); ok {
				return v, true
			}
			v, ok := r.Attributes[name] // bare attribute name
			return fmt.Sprint(v), ok
		},
	})
}

func (f filter) matchDoc(d document) bool {
	for _, g := range f.groups {
		all := true
		for _, t := range g.terms {
			if t.matches(d) == t.negated {
				all = false
				break
			}
		}
		if all == g.negated {
			return false
		}
	}
	return true
}

func (t filterTerm) matches(d document) bool {
	want := strings.ToLower(t.value)
	if t.field == "" || slices.Contains(d.textFields, t.field) {
		return strings.Contains(strings.ToLower(d.text), want)
	}
	got, ok := d.field(t.field)
	return ok && strings.EqualFold(got, want)
}
