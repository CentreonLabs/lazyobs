package fake

import (
	"context"
	"slices"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

// maxTopologyTraces bounds the traces sampled to build the demo topology.
const maxTopologyTraces = 3000

var _ traces.TopologyReader = (*Traces)(nil)

// Topology implements traces.TopologyReader from a sample of the demo traces.
func (t *Traces) Topology(ctx context.Context, from, to time.Time) (traces.Topology, error) {
	if err := wait(ctx, t.Latency); err != nil {
		return traces.Topology{}, err
	}
	if now := t.now(); to.After(now) {
		to = now
	}
	type acc struct {
		spans, errors int64
		durations     []time.Duration
	}
	services := map[string]*acc{}
	calls := map[[2]string]int64{}
	first, last := from.Unix(), ceilSecond(to)
	stride := max((last-first)*maxTracesPerSecond/maxTopologyTraces, 1)
	for sec := first; sec < last; sec += stride {
		for _, d := range t.tracesAt(sec) {
			byID := map[string]traces.Span{}
			for _, s := range d.Spans {
				byID[s.SpanID] = s
			}
			for _, s := range d.Spans {
				a := services[s.Service]
				if a == nil {
					a = &acc{}
					services[s.Service] = a
				}
				a.spans += stride
				a.durations = append(a.durations, s.Duration)
				if s.Status == traces.StatusError {
					a.errors += stride
				}
				if p, ok := byID[s.ParentID]; ok && p.Service != s.Service {
					calls[[2]string{p.Service, s.Service}] += stride
				}
			}
		}
	}
	var topo traces.Topology
	for name, a := range services {
		slices.Sort(a.durations)
		pct := func(p float64) time.Duration { return a.durations[int(p*float64(len(a.durations)-1))] }
		topo.Services = append(topo.Services, traces.ServiceStats{
			Name: name, Spans: a.spans, Errors: a.errors, P50: pct(0.50), P95: pct(0.95), P99: pct(0.99),
		})
	}
	slices.SortFunc(topo.Services, func(a, b traces.ServiceStats) int { return int(b.Spans - a.Spans) })
	for k, n := range calls {
		topo.Calls = append(topo.Calls, traces.Call{From: k[0], To: k[1], Count: n})
	}
	slices.SortFunc(topo.Calls, func(a, b traces.Call) int { return int(b.Count - a.Count) })
	return topo, nil
}
