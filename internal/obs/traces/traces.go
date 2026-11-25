// Package traces is the trace signal domain: spans, traces and the
// reconstruction of a trace's span tree.
package traces

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"sort"
	"time"
)

// Status is the outcome of a span.
type Status int

// Span statuses, as in OpenTelemetry.
const (
	StatusUnset Status = iota
	StatusOK
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusError:
		return "error"
	default:
		return "unset"
	}
}

// ParseStatus reads a status code ("ok", "error", "unset", any case).
func ParseStatus(code string) Status {
	switch code {
	case "ok", "OK", "Ok":
		return StatusOK
	case "error", "ERROR", "Error":
		return StatusError
	default:
		return StatusUnset
	}
}

// Kind is the role of a span, as in OpenTelemetry.
type Kind int

// Span kinds.
const (
	KindUnspecified Kind = iota
	KindInternal
	KindServer
	KindClient
	KindProducer
	KindConsumer
)

func (k Kind) String() string {
	switch k {
	case KindInternal:
		return "internal"
	case KindServer:
		return "server"
	case KindClient:
		return "client"
	case KindProducer:
		return "producer"
	case KindConsumer:
		return "consumer"
	default:
		return "unspecified"
	}
}

// Span is one operation of a trace.
type Span struct {
	TraceID  string
	SpanID   string
	ParentID string // empty for a root span

	Service string
	Name    string
	Kind    Kind

	Start    time.Time
	Duration time.Duration

	Status        Status
	StatusMessage string

	Attributes         map[string]any
	ResourceAttributes map[string]any

	// Raw is the span as returned by the backend, for display and copy.
	Raw []byte
}

// End is when the span finished.
func (s Span) End() time.Time { return s.Start.Add(s.Duration) }

// Trace is a trace as listed by a search: its root span.
type Trace struct {
	Root Span
}

// ID returns the trace ID.
func (t Trace) ID() string { return t.Root.TraceID }

// Fingerprint identifies a trace for deduplication.
func (t Trace) Fingerprint() uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(t.Root.TraceID))
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(t.Root.Start.UnixNano())) //nolint:gosec // bit pattern only
	_, _ = h.Write(ts[:])
	return h.Sum64()
}

// Detail is a whole trace, with all its spans.
type Detail struct {
	TraceID  string
	Name     string
	Services []string
	Duration time.Duration
	Spans    []Span
}

// Node is a span placed in its trace tree.
type Node struct {
	Span
	Depth    int
	Children int
}

// Tree orders the spans of a trace depth first, each parent before its
// children and siblings by start time, as a waterfall shows them. Spans whose
// parent is missing (not received yet, or dropped) are shown as roots; every
// span appears exactly once.
func (d Detail) Tree() []Node {
	byID := make(map[string]bool, len(d.Spans))
	for _, s := range d.Spans {
		byID[s.SpanID] = true
	}
	children := map[string][]Span{}
	var roots []Span
	for _, s := range d.Spans {
		if s.ParentID == "" || !byID[s.ParentID] || s.ParentID == s.SpanID {
			roots = append(roots, s)
			continue
		}
		children[s.ParentID] = append(children[s.ParentID], s)
	}
	byStart := func(spans []Span) {
		sort.SliceStable(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
	}
	byStart(roots)
	out := make([]Node, 0, len(d.Spans))
	visited := map[string]bool{}
	var walk func(s Span, depth int)
	walk = func(s Span, depth int) {
		if visited[s.SpanID] { // guard against cycles in malformed data
			return
		}
		visited[s.SpanID] = true
		kids := children[s.SpanID]
		byStart(kids)
		out = append(out, Node{Span: s, Depth: depth, Children: len(kids)})
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	// Spans only reachable through a cycle (malformed parent links) are
	// shown as roots rather than dropped.
	rest := make([]Span, 0)
	for _, s := range d.Spans {
		if !visited[s.SpanID] {
			rest = append(rest, s)
		}
	}
	byStart(rest)
	for _, s := range rest {
		walk(s, 0)
	}
	return out
}

// Bounds returns the start of the earliest span and the end of the latest.
func (d Detail) Bounds() (start, end time.Time) {
	for i, s := range d.Spans {
		if i == 0 || s.Start.Before(start) {
			start = s.Start
		}
		if i == 0 || s.End().After(end) {
			end = s.End()
		}
	}
	return start, end
}

// Errors counts the spans in error.
func (d Detail) Errors() int {
	n := 0
	for _, s := range d.Spans {
		if s.Status == StatusError {
			n++
		}
	}
	return n
}

// Fetcher loads whole traces.
type Fetcher interface {
	Trace(ctx context.Context, traceID string) (Detail, error)
}
