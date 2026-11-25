package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// maxSampledFieldSeconds bounds the seconds scanned to suggest values.
const maxSampledFieldSeconds = 600

var _ obs.FieldSuggester = (*Logs)(nil)

var demoFields = []obs.Field{
	{Name: "attributes.http.method", Type: "keyword", Aggregatable: true},
	{Name: "attributes.http.route", Type: "keyword", Aggregatable: true},
	{Name: "attributes.http.status_code", Type: "long", Aggregatable: true},
	{Name: "body.message", Type: "text"},
	{Name: "resource_attributes.host.name", Type: "keyword", Aggregatable: true},
	{Name: "resource_attributes.service.version", Type: "keyword", Aggregatable: true},
	{Name: "scope_name", Type: "keyword", Aggregatable: true},
	{Name: "service_name", Type: "keyword", Aggregatable: true},
	{Name: "severity_number", Type: "long", Aggregatable: true},
	{Name: "severity_text", Type: "keyword", Aggregatable: true},
	{Name: "span_id", Type: "binary"},
	{Name: "trace_id", Type: "binary"},
}

// Fields implements obs.FieldSuggester with the fields of the demo data.
func (l *Logs) Fields(ctx context.Context, _, _ time.Time) ([]obs.Field, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	return append([]obs.Field(nil), demoFields...), nil
}

// Values implements obs.FieldSuggester by counting values over a sample of
// the window.
func (l *Logs) Values(ctx context.Context, field, prefix string, from, to time.Time, limit int) ([]obs.FieldValue, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	if now := l.now(); to.After(now) {
		to = now
	}
	counts := map[string]int64{}
	first, last := from.Unix(), ceilSecond(to)
	stride := max((last-first)/maxSampledFieldSeconds, 1)
	for sec := first; sec < last; sec += stride {
		for _, r := range l.recordsAt(sec) {
			if v, ok := fieldValue(r, field); ok && strings.HasPrefix(strings.ToLower(v), strings.ToLower(prefix)) {
				counts[v] += stride
			}
		}
	}
	values := make([]obs.FieldValue, 0, len(counts))
	for v, n := range counts {
		values = append(values, obs.FieldValue{Value: v, Count: n})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		return values[i].Value < values[j].Value
	})
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

// fieldValue reads a field of a record by its platform name.
func fieldValue(r logs.Record, field string) (string, bool) {
	switch field {
	case "service_name":
		return r.Service, true
	case "severity_text":
		return r.SeverityText, true
	case "severity_number":
		return fmt.Sprint(r.SeverityNumber), true
	case "scope_name":
		return r.ScopeName, true
	}
	if k, ok := strings.CutPrefix(field, "attributes."); ok {
		v, ok := r.Attributes[k]
		return fmt.Sprint(v), ok
	}
	if k, ok := strings.CutPrefix(field, "resource_attributes."); ok {
		v, ok := r.ResourceAttributes[k]
		return fmt.Sprint(v), ok
	}
	return "", false
}
