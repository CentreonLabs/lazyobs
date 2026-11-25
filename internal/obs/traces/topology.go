package traces

import (
	"context"
	"sort"
	"time"
)

// Topology is the graph of services seen in the spans of a time window.
type Topology struct {
	Services []ServiceStats
	Calls    []Call
}

// ServiceStats is the health of a service. Unknown latencies are zero.
type ServiceStats struct {
	Name          string
	Spans, Errors int64
	P50, P95, P99 time.Duration
}

// ErrorRate is the share of spans in error, from 0 to 1.
func (s ServiceStats) ErrorRate() float64 {
	if s.Spans == 0 {
		return 0
	}
	return float64(s.Errors) / float64(s.Spans)
}

// Call is a service calling another one.
type Call struct {
	From, To string
	Count    int64
}

// TopologyReader returns the service graph over a time window.
type TopologyReader interface {
	Topology(ctx context.Context, from, to time.Time) (Topology, error)
}

// Service returns the stats of a service.
func (t Topology) Service(name string) (ServiceStats, bool) {
	for _, s := range t.Services {
		if s.Name == name {
			return s, true
		}
	}
	return ServiceStats{}, false
}

// Callers lists the calls to a service, most frequent first.
func (t Topology) Callers(name string) []Call {
	return t.calls(func(c Call) bool { return c.To == name })
}

// Callees lists the calls from a service, most frequent first.
func (t Topology) Callees(name string) []Call {
	return t.calls(func(c Call) bool { return c.From == name })
}

func (t Topology) calls(keep func(Call) bool) []Call {
	var out []Call
	for _, c := range t.Calls {
		if keep(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}
