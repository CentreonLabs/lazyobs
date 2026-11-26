// Package seed converts the demo data of source/fake into OpenTelemetry
// (OTLP/JSON) export requests and sends them to an ingestion endpoint, to fill
// an organization with realistic, linked logs and traces.
package seed

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

// ServicePrefix is prepended to the demo log services so that they stand out
// on a shared organization; the demo trace services already carry it.
const ServicePrefix = "demo-"

// Scope identifies the instrumentation that produced the seeded data.
var scope = map[string]any{"name": "lazyobs-seed", "version": "1"}

// LogsRequest builds an OTLP ExportLogsServiceRequest, one resource per
// service and host.
func LogsRequest(recs []logs.Record) map[string]any {
	type key struct{ service, host string }
	groups := map[key][]any{}
	resources := map[key]map[string]any{}
	for _, r := range recs {
		svc := prefixed(r.Service)
		attrs := maps.Clone(r.ResourceAttributes)
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs["service.name"] = svc
		k := key{svc, fmt.Sprint(attrs["host.name"])}
		resources[k] = attrs
		rec := map[string]any{
			"timeUnixNano":         nanos(r.Timestamp.UnixNano()),
			"observedTimeUnixNano": nanos(r.ObservedTimestamp.UnixNano()),
			"severityNumber":       r.SeverityNumber,
			"severityText":         r.SeverityText,
			"body":                 anyValue(r.Body),
			"attributes":           keyValues(r.Attributes),
		}
		if r.TraceID != "" {
			rec["traceId"], rec["spanId"], rec["flags"] = r.TraceID, r.SpanID, r.TraceFlags
		}
		groups[k] = append(groups[k], rec)
	}
	out := []any{}
	for _, k := range sortedKeys(groups, func(k key) string { return k.service + "\x00" + k.host }) {
		out = append(out, map[string]any{
			"resource":  map[string]any{"attributes": keyValues(resources[k])},
			"scopeLogs": []any{map[string]any{"scope": scope, "logRecords": groups[k]}},
		})
	}
	return map[string]any{"resourceLogs": out}
}

// TracesRequest builds an OTLP ExportTraceServiceRequest, one resource per
// service.
func TracesRequest(ds []traces.Detail) map[string]any {
	groups := map[string][]any{}
	resources := map[string]map[string]any{}
	for _, d := range ds {
		for _, s := range d.Spans {
			attrs := maps.Clone(s.ResourceAttributes)
			if attrs == nil {
				attrs = map[string]any{}
			}
			attrs["service.name"] = s.Service
			resources[s.Service] = attrs
			span := map[string]any{
				"traceId":           s.TraceID,
				"spanId":            s.SpanID,
				"name":              s.Name,
				"kind":              int(s.Kind),
				"startTimeUnixNano": nanos(s.Start.UnixNano()),
				"endTimeUnixNano":   nanos(s.End().UnixNano()),
				"attributes":        keyValues(s.Attributes),
				"status":            status(s),
			}
			if s.ParentID != "" {
				span["parentSpanId"] = s.ParentID
			}
			groups[s.Service] = append(groups[s.Service], span)
		}
	}
	out := []any{}
	for _, svc := range sortedKeys(groups, func(s string) string { return s }) {
		out = append(out, map[string]any{
			"resource":   map[string]any{"attributes": keyValues(resources[svc])},
			"scopeSpans": []any{map[string]any{"scope": scope, "spans": groups[svc]}},
		})
	}
	return map[string]any{"resourceSpans": out}
}

func prefixed(svc string) string {
	if len(svc) >= len(ServicePrefix) && svc[:len(ServicePrefix)] == ServicePrefix {
		return svc
	}
	return ServicePrefix + svc
}

func status(s traces.Span) map[string]any {
	st := map[string]any{"code": int(s.Status)} // the OTLP codes: unset 0, ok 1, error 2
	if s.StatusMessage != "" {
		st["message"] = s.StatusMessage
	}
	return st
}

// nanos encodes a 64-bit integer as OTLP/JSON does: a decimal string.
func nanos(n int64) string { return strconv.FormatInt(n, 10) }

func keyValues(m map[string]any) []any {
	out := []any{}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, map[string]any{"key": k, "value": anyValue(m[k])})
	}
	return out
}

func anyValue(v any) map[string]any {
	switch v := v.(type) {
	case string:
		return map[string]any{"stringValue": v}
	case bool:
		return map[string]any{"boolValue": v}
	case int:
		return map[string]any{"intValue": strconv.Itoa(v)}
	case int64:
		return map[string]any{"intValue": strconv.FormatInt(v, 10)}
	case float64:
		return map[string]any{"doubleValue": v}
	default:
		return map[string]any{"stringValue": fmt.Sprint(v)}
	}
}

func sortedKeys[K comparable, V any](m map[K]V, by func(K) string) []K {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b K) int {
		x, y := by(a), by(b)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	return keys
}
