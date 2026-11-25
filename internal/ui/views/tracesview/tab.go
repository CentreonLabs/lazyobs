package tracesview

import (
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
)

// traceWindow is the time shown around a trace opened from elsewhere.
const traceWindow = 15 * time.Minute

// Title implements tabset.Child.
func (m *Model) Title() string {
	if m.trace != nil {
		name := m.trace.detail.Name
		if name == "" {
			name = "trace " + shortID(m.trace.id)
		}
		return "⌁ " + name
	}
	if m.topo != nil {
		return "service map · " + shortRange(m.rng)
	}
	q := strings.TrimSpace(m.query)
	if q == "" || q == "*" {
		q = "all traces"
	}
	return q + " · " + shortRange(m.rng)
}

// Clone implements tabset.Child.
func (m *Model) Clone() tabset.Child {
	opts := m.opts
	opts.Query, opts.Range = m.query, m.rng
	c := New(opts)
	c.order, c.utc, c.compact = m.order, m.utc, m.compact
	return c
}

// ForTrace returns a view showing one trace, the list around it ready when
// leaving the waterfall.
func ForTrace(opts Options, traceID string, at time.Time) *Model {
	opts.Query = "trace_id:" + traceID
	opts.Range = obs.Between(at.Add(-traceWindow).Truncate(time.Second), at.Add(traceWindow).Truncate(time.Second))
	m := New(opts)
	m.trace = &openTrace{id: traceID, loading: true}
	return m
}

// State is what a traces tab remembers across sessions.
type State struct {
	Query   string        `json:"query,omitempty"`
	Range   obs.TimeRange `json:"range"`
	Slowest bool          `json:"slowest,omitempty"`
	UTC     bool          `json:"utc,omitempty"`
	Compact bool          `json:"compact,omitempty"`
	Trace   string        `json:"trace,omitempty"` // open waterfall
	Map     bool          `json:"map,omitempty"`   // service map shown
}

// Snapshot captures the state to save.
func (m *Model) Snapshot() State {
	s := State{Query: m.query, Range: m.rng, Slowest: m.order == obs.OrderSlowest, UTC: m.utc, Compact: m.compact}
	if m.trace != nil {
		s.Trace = m.trace.id
	}
	s.Map = m.topo != nil
	return s
}

// Restore returns a view initialized from a saved state.
func Restore(opts Options, s State) *Model {
	opts.Query, opts.Range = s.Query, s.Range
	m := New(opts)
	if s.Slowest {
		m.order = obs.OrderSlowest
	}
	m.utc, m.compact = s.UTC, s.Compact
	if s.Trace != "" {
		m.trace = &openTrace{id: s.Trace, loading: true}
	}
	if s.Map && opts.Topology != nil {
		m.topo = &topology{loading: true}
	}
	return m
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func shortRange(r obs.TimeRange) string {
	if r.IsRelative() {
		return obs.FormatDuration(r.Last)
	}
	from, to := r.From.Local(), r.To.Local()
	if from.YearDay() == to.YearDay() {
		return from.Format("01-02 15:04") + "→" + to.Format("15:04")
	}
	return from.Format("01-02 15:04") + "→" + to.Format("01-02 15:04")
}

// TraceTab returns a view on the same sources showing one trace.
func (m *Model) TraceTab(traceID string, at time.Time) *Model {
	t := ForTrace(m.opts, traceID, at)
	t.utc, t.compact = m.utc, m.compact
	return t
}
