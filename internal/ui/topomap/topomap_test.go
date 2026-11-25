package topomap

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

func demo() traces.Topology {
	return traces.Topology{
		Services: []traces.ServiceStats{
			{Name: "gateway", Spans: 10, Errors: 2, P95: 284 * time.Millisecond},
			{Name: "orders", Spans: 20, Errors: 2, P95: 1326 * time.Millisecond},
			{Name: "inventory", Spans: 20, P95: 55 * time.Millisecond},
			{Name: "payments", Spans: 10, Errors: 2, P95: 84 * time.Millisecond},
			{Name: "notifier", Spans: 8, P95: 11 * time.Millisecond},
		},
		Calls: []traces.Call{
			{From: "gateway", To: "orders", Count: 10},
			{From: "orders", To: "inventory", Count: 10},
			{From: "orders", To: "notifier", Count: 8},
			{From: "orders", To: "payments", Count: 10},
		},
	}
}

func colOf(l Layout, name string) int {
	for _, b := range l.Boxes {
		if b.Name == name {
			return b.Col
		}
	}
	return -1
}

func TestComputeColumns(t *testing.T) {
	l := Compute(demo())
	want := map[string]int{"gateway": 0, "orders": 1, "inventory": 2, "notifier": 2, "payments": 2}
	for name, col := range want {
		if got := colOf(l, name); got != col {
			t.Errorf("%s in column %d, want %d", name, got, col)
		}
	}
	if len(l.Boxes) != 5 || len(l.edges) != 4 || l.Loops != 0 {
		t.Fatalf("boxes %d, edges %d, loops %d", len(l.Boxes), len(l.edges), l.Loops)
	}
	// Boxes do not overlap.
	for i, a := range l.Boxes {
		for _, b := range l.Boxes[i+1:] {
			if a.X < b.X+BoxW && b.X < a.X+BoxW && a.Y < b.Y+BoxH && b.Y < a.Y+BoxH {
				t.Errorf("%s and %s overlap", a.Name, b.Name)
			}
		}
	}
}

func TestComputeSurvivesCyclesAndUnknownServices(t *testing.T) {
	topo := traces.Topology{Calls: []traces.Call{
		{From: "a", To: "b", Count: 1}, {From: "b", To: "c", Count: 1}, {From: "c", To: "a", Count: 1}, // cycle
		{From: "c", To: "c", Count: 1}, // recursion
		{From: "x", To: "b", Count: 1},
	}}
	l := Compute(topo)
	if len(l.Boxes) != 4 {
		t.Fatalf("boxes = %d", len(l.Boxes))
	}
	if l.Loops == 0 {
		t.Error("the call closing the cycle must be counted, not drawn")
	}
	// x is the only service nobody calls: the cycle is entered from it.
	if colOf(l, "x") >= colOf(l, "b") || colOf(l, "b") >= colOf(l, "c") || colOf(l, "c") >= colOf(l, "a") {
		t.Errorf("columns x=%d b=%d c=%d a=%d", colOf(l, "x"), colOf(l, "b"), colOf(l, "c"), colOf(l, "a"))
	}
	for _, e := range l.edges {
		if l.Boxes[e.to].Col <= l.Boxes[e.from].Col {
			t.Errorf("drawn call %s → %s does not go right", l.Boxes[e.from].Name, l.Boxes[e.to].Name)
		}
	}
}

func TestRender(t *testing.T) {
	l := Compute(demo())
	out := ansi.Strip(l.Render(Styles{}, 0, 0, 0, l.W, l.H))
	lines := strings.Split(out, "\n")
	if len(lines) != l.H {
		t.Fatalf("%d lines, want %d", len(lines), l.H)
	}
	for _, want := range []string{"gateway", "orders", "p95 1.33s", "20 spans ✗ 2", "▶", "╭", "─"} {
		if !strings.Contains(out, want) {
			t.Errorf("map misses %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "▶") != 4 {
		t.Errorf("want 4 arrows:\n%s", out)
	}
	// A fan-out from orders to three services makes a junction.
	if !strings.ContainsAny(out, "├┬┤┼") {
		t.Errorf("no junction drawn:\n%s", out)
	}
	// Cropping keeps the requested size.
	crop := strings.Split(ansi.Strip(l.Render(Styles{}, 0, 10, 2, 30, 6)), "\n")
	if len(crop) != 6 || len([]rune(crop[0])) != 30 {
		t.Errorf("crop = %d lines of %d", len(crop), len([]rune(crop[0])))
	}
}
