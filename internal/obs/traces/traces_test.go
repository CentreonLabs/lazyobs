package traces_test

import (
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

var t0 = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func span(id, parent string, startMs, durMs int) traces.Span {
	return traces.Span{SpanID: id, ParentID: parent, Start: t0.Add(time.Duration(startMs) * time.Millisecond), Duration: time.Duration(durMs) * time.Millisecond}
}

func TestTree(t *testing.T) {
	d := traces.Detail{Spans: []traces.Span{
		span("notify", "orders", 95, 10),
		span("db", "orders", 3, 20),
		span("root", "", 0, 130),
		span("orphan", "gone", 50, 5), // parent never received
		span("orders", "root", 1, 120),
		span("inv", "orders", 30, 40),
		span("redis", "inv", 32, 2),
	}}
	var got []string
	var depths []int
	for _, n := range d.Tree() {
		got = append(got, n.SpanID)
		depths = append(depths, n.Depth)
	}
	want := []string{"root", "orders", "db", "inv", "redis", "notify", "orphan"}
	wantDepths := []int{0, 1, 2, 2, 3, 2, 0}
	for i := range want {
		if got[i] != want[i] || depths[i] != wantDepths[i] {
			t.Fatalf("tree = %v %v, want %v %v", got, depths, want, wantDepths)
		}
	}
	start, end := d.Bounds()
	if !start.Equal(t0) || !end.Equal(t0.Add(130*time.Millisecond)) {
		t.Errorf("bounds = %v → %v", start, end)
	}
}

func TestTreeSurvivesCycles(t *testing.T) {
	d := traces.Detail{Spans: []traces.Span{span("a", "b", 0, 1), span("b", "a", 1, 1), span("c", "c", 2, 1)}}
	if n := len(d.Tree()); n != 3 {
		t.Fatalf("every span must be shown once despite cycles, got %d", n)
	}
}

func TestStatusAndKind(t *testing.T) {
	if traces.ParseStatus("error") != traces.StatusError || traces.ParseStatus("ok") != traces.StatusOK || traces.ParseStatus("") != traces.StatusUnset {
		t.Error("ParseStatus")
	}
	if traces.KindServer.String() != "server" || traces.Kind(9).String() != "unspecified" {
		t.Error("Kind.String")
	}
	d := traces.Detail{Spans: []traces.Span{{Status: traces.StatusError}, {Status: traces.StatusOK}, {Status: traces.StatusError}}}
	if d.Errors() != 2 {
		t.Errorf("Errors = %d", d.Errors())
	}
}
