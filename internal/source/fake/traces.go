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
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

// Traces is a synthetic trace source, deterministic like Logs: the traces
// of a second are a pure function of the seed and the second. Its incidents
// match the demo logs': more errors and slower payments.
type Traces struct {
	seed uint64
	now  func() time.Time
	// Latency, when set, delays every call to mimic the network.
	Latency time.Duration
}

var (
	_ obs.Searcher[traces.Trace] = (*Traces)(nil)
	_ obs.Histogrammer           = (*Traces)(nil)
	_ traces.Fetcher             = (*Traces)(nil)
	_ obs.FieldSuggester         = (*Traces)(nil)
)

// NewTraces returns a source generating traces from seed. A nil now uses time.Now.
func NewTraces(seed uint64, now func() time.Time) *Traces {
	if now == nil {
		now = time.Now
	}
	return &Traces{seed: seed, now: now}
}

// maxTracesPerSecond bounds the traces started each second.
const maxTracesPerSecond = 2

type route struct {
	method, path string
	steps        []step
}

// step is a span under the entry service: service, operation, kind, children.
type step struct {
	service, name string
	kind          traces.Kind
	attrs         map[string]any
	children      []step
}

var routes = []route{
	{"POST", "/v1/orders", []step{{"orders", "CreateOrder", traces.KindServer, map[string]any{"rpc.method": "CreateOrder"}, []step{
		{"orders", "INSERT orders", traces.KindClient, map[string]any{"db.system": "postgresql", "db.statement": "INSERT INTO orders VALUES ($1, $2)"}, nil},
		{"inventory", "ReserveItems", traces.KindServer, map[string]any{"rpc.method": "ReserveItems"}, []step{
			{"inventory", "redis GET stock", traces.KindClient, map[string]any{"db.system": "redis"}, nil},
		}},
		{"payments", "Charge", traces.KindServer, map[string]any{"rpc.method": "Charge", "payment.provider": "acme"}, nil},
		{"notifier", "SendConfirmation", traces.KindProducer, map[string]any{"messaging.system": "kafka", "messaging.destination": "order-events"}, nil},
	}}}},
	{"GET", "/v1/orders/{id}", []step{{"orders", "GetOrder", traces.KindServer, map[string]any{"rpc.method": "GetOrder"}, []step{
		{"orders", "SELECT orders", traces.KindClient, map[string]any{"db.system": "postgresql", "db.statement": "SELECT * FROM orders WHERE id = $1"}, nil},
	}}}},
	{"POST", "/login", []step{{"auth", "Authenticate", traces.KindServer, map[string]any{"rpc.method": "Authenticate"}, []step{
		{"auth", "SELECT users", traces.KindClient, map[string]any{"db.system": "postgresql", "db.statement": "SELECT * FROM users WHERE login = $1"}, nil},
		{"auth", "redis SET session", traces.KindClient, map[string]any{"db.system": "redis"}, nil},
	}}}},
	{"GET", "/items", []step{{"inventory", "ListItems", traces.KindServer, map[string]any{"rpc.method": "ListItems"}, []step{
		{"inventory", "redis GET items", traces.KindClient, map[string]any{"db.system": "redis"}, nil},
	}}}},
}

func (t *Traces) rng(sec int64) *rand.Rand {
	return rand.New(rand.NewPCG(t.seed^0x7472616365, uint64(sec))) //nolint:gosec // synthetic data
}

// count is the number of traces started during sec, without building them.
func (t *Traces) count(sec int64) int {
	n := t.rng(sec).IntN(maxTracesPerSecond + 1)
	if !inIncident(sec) && n == maxTracesPerSecond {
		n-- // busier during incidents
	}
	return n
}

func traceID(sec int64, i int) string { return fmt.Sprintf("%016x%016x", uint64(sec), uint64(i)) } //nolint:gosec // bit pattern only

func parseTraceID(id string) (sec int64, i int, ok bool) {
	var s, n uint64
	if len(id) != 32 {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(id, "%016x%016x", &s, &n); err != nil {
		return 0, 0, false
	}
	return int64(s), int(n), true //nolint:gosec // values written by traceID
}

// tracesAt generates the traces of one second, each with all its spans.
func (t *Traces) tracesAt(sec int64) []traces.Detail {
	n := t.count(sec)
	r := t.rng(sec)
	out := make([]traces.Detail, 0, n)
	for i := range n {
		out = append(out, t.build(sec, i, r))
	}
	return out
}

func (t *Traces) build(sec int64, i int, r *rand.Rand) traces.Detail {
	incident := inIncident(sec)
	rt := routes[r.IntN(len(routes))]
	fail := r.IntN(100) < 3 || (incident && rt.path == "/v1/orders" && r.IntN(3) == 0)
	id := traceID(sec, i)
	start := time.Unix(sec, r.Int64N(int64(time.Second)))
	d := traces.Detail{TraceID: id, Name: rt.method + " " + rt.path}

	spanN := 0
	newID := func() string { spanN++; return fmt.Sprintf("%08x%08x", uint32(sec), uint32(i<<8|spanN)) } //nolint:gosec // bit pattern only
	services := map[string]bool{}
	var add func(s step, parent string, at time.Time) time.Duration
	add = func(s step, parent string, at time.Time) time.Duration {
		sp := traces.Span{
			TraceID: id, SpanID: newID(), ParentID: parent, Service: "demo-" + s.service, Name: s.name, Kind: s.kind,
			Start: at, Attributes: map[string]any{}, ResourceAttributes: map[string]any{"service.version": "1.4.2", "deployment.environment": "demo"},
		}
		for k, v := range s.attrs {
			sp.Attributes[k] = v
		}
		own := time.Duration(r.IntN(8)+1) * time.Millisecond
		if s.service == "payments" && incident {
			own += time.Duration(r.IntN(900)+300) * time.Millisecond
		}
		cursor := at.Add(own)
		var childEnd time.Duration
		idx := len(d.Spans)
		d.Spans = append(d.Spans, sp)
		for _, c := range s.children {
			dur := add(c, sp.SpanID, cursor)
			if c.kind != traces.KindProducer { // async sends do not hold the parent
				cursor = cursor.Add(dur)
			}
			childEnd = max(childEnd, cursor.Sub(at))
		}
		total := max(childEnd, own) + time.Duration(r.IntN(4))*time.Millisecond
		d.Spans[idx].Duration = total
		if fail && s.service == "payments" {
			d.Spans[idx].Status, d.Spans[idx].StatusMessage = traces.StatusError, "card declined"
		} else {
			d.Spans[idx].Status = traces.StatusOK
		}
		services["demo-"+s.service] = true
		return total
	}

	rootID := newID()
	root := traces.Span{
		TraceID: id, SpanID: rootID, Service: "demo-gateway", Name: rt.method + " " + rt.path, Kind: traces.KindServer, Start: start,
		Attributes:         map[string]any{"http.method": rt.method, "http.route": rt.path, "http.status_code": 200},
		ResourceAttributes: map[string]any{"service.version": "3.4.1", "deployment.environment": "demo"},
		Status:             traces.StatusOK,
	}
	d.Spans = append(d.Spans, root)
	cursor := start.Add(time.Duration(r.IntN(3)+1) * time.Millisecond)
	for _, s := range rt.steps {
		cursor = cursor.Add(add(s, rootID, cursor))
	}
	d.Spans[0].Duration = cursor.Sub(start) + time.Millisecond
	if fail {
		d.Spans[0].Status, d.Spans[0].StatusMessage = traces.StatusError, "upstream returned 502"
		d.Spans[0].Attributes["http.status_code"] = 502
	}
	services["demo-gateway"] = true
	for s := range services {
		d.Services = append(d.Services, s)
	}
	slices.Sort(d.Services)
	d.Duration = d.Spans[0].Duration
	for k := range d.Spans {
		d.Spans[k].Raw = spanJSON(d.Spans[k])
	}
	return d
}

func spanJSON(s traces.Span) []byte {
	doc := map[string]any{
		"trace_id": s.TraceID, "span_id": s.SpanID, "service_name": s.Service, "span_name": s.Name, "span_kind": int(s.Kind),
		"span_start_timestamp_nanos": s.Start.UnixNano(), "span_end_timestamp_nanos": s.End().UnixNano(),
		"span_duration_millis": s.Duration.Milliseconds(), "span_status": map[string]string{"code": s.Status.String(), "message": s.StatusMessage},
		"span_attributes": s.Attributes, "resource_attributes": s.ResourceAttributes,
	}
	if s.ParentID != "" {
		doc["parent_span_id"] = s.ParentID
	}
	b, _ := json.Marshal(doc)
	return b
}

// spanDoc exposes a root span to the query filter, with the platform's field names.
func spanDoc(s traces.Span) document {
	return document{
		text:       s.Name,
		textFields: []string{"span_name"},
		field: func(name string) (string, bool) {
			switch name {
			case "service_name":
				return s.Service, true
			case "span_status.code":
				return s.Status.String(), true
			case "span_status.message":
				return s.StatusMessage, true
			case "span_kind":
				return fmt.Sprint(int(s.Kind)), true
			case "trace_id":
				return s.TraceID, true
			}
			if k, ok := strings.CutPrefix(name, "span_attributes."); ok {
				v, ok := s.Attributes[k]
				return fmt.Sprint(v), ok
			}
			if k, ok := strings.CutPrefix(name, "resource_attributes."); ok {
				v, ok := s.ResourceAttributes[k]
				return fmt.Sprint(v), ok
			}
			return "", false
		},
	}
}

// maxScannedSeconds bounds the seconds generated for a search; longer
// windows are sampled (demo data only).
const maxScannedSeconds = 6 * 3600

// matching returns the matching traces of the window, newest first.
func (t *Traces) matching(q obs.Query) ([]traces.Trace, error) {
	from, to := t.window(q)
	f, err := parseFilter(q.Text)
	if err != nil {
		return nil, err
	}
	first, last := from.Unix(), ceilSecond(to)
	stride := max((last-first)/maxScannedSeconds, 1)
	var out []traces.Trace
	for sec := last - 1; sec >= first; sec -= stride {
		ts := t.tracesAt(sec)
		for k := len(ts) - 1; k >= 0; k-- {
			root := ts[k].Spans[0]
			if root.Start.Before(from) || !root.Start.Before(to) || !f.matchDoc(spanDoc(root)) {
				continue
			}
			out = append(out, traces.Trace{Root: root})
		}
	}
	slices.SortStableFunc(out, func(a, b traces.Trace) int { return b.Root.Start.Compare(a.Root.Start) })
	if q.Order == obs.OrderSlowest {
		slices.SortStableFunc(out, func(a, b traces.Trace) int { return int(b.Root.Duration - a.Root.Duration) })
	}
	return out, nil
}

func (t *Traces) window(q obs.Query) (from, to time.Time) {
	now := t.now()
	from, to = q.Range.Resolve(now)
	if q.Cursor != nil && !q.Cursor.End.IsZero() {
		to = q.Cursor.End
	}
	if to.After(now) {
		to = now
	}
	return from, to
}

// Search implements obs.Searcher, newest or slowest first.
func (t *Traces) Search(ctx context.Context, q obs.Query) (obs.Page[traces.Trace], error) {
	if err := wait(ctx, t.Latency); err != nil {
		return obs.Page[traces.Trace]{}, err
	}
	all, err := t.matching(q)
	if err != nil {
		return obs.Page[traces.Trace]{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := 0
	if q.Cursor != nil {
		offset = q.Cursor.Offset
	}
	page := obs.Page[traces.Trace]{Total: int64(len(all)), Items: []traces.Trace{}}
	if offset < len(all) {
		page.Items = all[offset:min(offset+limit, len(all))]
	}
	if offset+len(page.Items) < len(all) {
		_, to := t.window(q)
		page.Next = &obs.Cursor{Offset: offset + len(page.Items), End: to}
	}
	return page, nil
}

// Histogram implements obs.Histogrammer: all traces and those in error.
func (t *Traces) Histogram(ctx context.Context, q obs.Query, buckets int) ([]obs.Series, error) {
	if err := wait(ctx, t.Latency); err != nil {
		return nil, err
	}
	from, to := t.window(q)
	f, err := parseFilter(q.Text)
	if err != nil {
		return nil, err
	}
	buckets = max(buckets, 1)
	step := max(to.Sub(from)/time.Duration(buckets), time.Second).Truncate(time.Second)
	all, errs := obs.Series{Name: "TRACES"}, obs.Series{Name: "ERRORS"}
	for b := from.Truncate(time.Second); b.Before(to); b = b.Add(step) {
		var n, e float64
		first, last := b.Unix(), min(b.Add(step).Unix(), ceilSecond(to))
		stride := max((last-first)/maxSampledSeconds, 1)
		for sec := first; sec < last; sec += stride {
			for _, d := range t.tracesAt(sec) {
				if root := d.Spans[0]; f.matchDoc(spanDoc(root)) {
					n += float64(stride)
					if root.Status == traces.StatusError {
						e += float64(stride)
					}
				}
			}
		}
		all.Points = append(all.Points, obs.Point{Time: b, Value: n})
		errs.Points = append(errs.Points, obs.Point{Time: b, Value: e})
	}
	return []obs.Series{all, errs}, nil
}

// Trace implements traces.Fetcher.
func (t *Traces) Trace(ctx context.Context, id string) (traces.Detail, error) {
	if err := wait(ctx, t.Latency); err != nil {
		return traces.Detail{}, err
	}
	sec, i, ok := parseTraceID(id)
	if ts := t.tracesAt(sec); ok && i < len(ts) {
		return ts[i], nil
	}
	return traces.Detail{}, fmt.Errorf("fake: trace %s not found", id)
}

var demoSpanFields = []obs.Field{
	{Name: "resource_attributes.deployment.environment", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "resource_attributes.service.version", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "service_name", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "span_attributes.http.method", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "span_attributes.http.route", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "span_attributes.http.status_code", Type: "long", Aggregatable: true},
	{Name: "span_kind", Type: "long", Aggregatable: true},
	{Name: "span_name", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "span_status.code", Type: "keyword", Aggregatable: true, FullText: true},
	{Name: "span_status.message", Type: "keyword", FullText: true},
	{Name: "trace_id", Type: "binary"},
}

// Fields implements obs.FieldSuggester with the fields of root spans.
func (t *Traces) Fields(ctx context.Context, _, _ time.Time) ([]obs.Field, error) {
	return append([]obs.Field(nil), demoSpanFields...), wait(ctx, t.Latency)
}

// Values implements obs.FieldSuggester over a sample of root spans.
func (t *Traces) Values(ctx context.Context, field, prefix string, from, to time.Time, limit int) ([]obs.FieldValue, error) {
	if err := wait(ctx, t.Latency); err != nil {
		return nil, err
	}
	all, err := t.matching(obs.Query{Range: obs.Between(from, to)})
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, tr := range all {
		if v, ok := spanDoc(tr.Root).field(field); ok && strings.HasPrefix(strings.ToLower(v), strings.ToLower(prefix)) {
			counts[v]++
		}
	}
	values := make([]obs.FieldValue, 0, len(counts))
	for v, n := range counts {
		values = append(values, obs.FieldValue{Value: v, Count: n})
	}
	slices.SortFunc(values, func(a, b obs.FieldValue) int {
		if a.Count != b.Count {
			return int(b.Count - a.Count)
		}
		return strings.Compare(a.Value, b.Value)
	})
	return values[:min(limit, len(values))], nil
}
