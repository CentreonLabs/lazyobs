// Package topomap lays out and draws a service map: services as boxes in
// columns following the calls (callers on the left), calls as right-angled
// lines with arrows.
package topomap

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
)

// Box sizes and spacing, in cells.
const (
	BoxW   = 26
	BoxH   = 5 // border and three lines: name, latency, volume
	colGap = 10
	rowGap = 1
	margin = 1
	lanes  = colGap - 5 // vertical lanes available between two columns
)

// Box is a service placed on the map.
type Box struct {
	traces.ServiceStats
	Col, Row int // column (call depth) and position in it
	X, Y     int // top-left cell
}

// Center is the cell in the middle of the box.
func (b Box) Center() (x, y int) { return b.X + BoxW/2, b.Y + BoxH/2 }

// Layout is a laid out map.
type Layout struct {
	Boxes []Box // by column, then row
	W, H  int   // size in cells
	edges []edge
	// Loops counts the calls that go backwards or stay in a column
	// (recursion, cycles): they are not drawn.
	Loops int
}

type edge struct {
	from, to int // box indexes
	lane     int
}

// Compute lays out a topology.
func Compute(t traces.Topology) Layout {
	// Every service, including those only seen as call endpoints.
	stats := map[string]traces.ServiceStats{}
	for _, s := range t.Services {
		stats[s.Name] = s
	}
	for _, c := range t.Calls {
		for _, n := range []string{c.From, c.To} {
			if _, ok := stats[n]; !ok {
				stats[n] = traces.ServiceStats{Name: n}
			}
		}
	}
	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)

	forward := acyclic(names, t.Calls)
	col := layers(names, forward)

	// Columns, ordered by name first, then by the mean position of callers.
	byCol := map[int][]string{}
	maxCol := 0
	for _, n := range names {
		byCol[col[n]] = append(byCol[col[n]], n)
		maxCol = max(maxCol, col[n])
	}
	pos := map[string]float64{}
	for k := 0; k <= maxCol; k++ {
		if k > 0 {
			bary := func(n string) float64 {
				sum, cnt := 0.0, 0.0
				for _, c := range forward {
					if c.To == n {
						sum += pos[c.From]
						cnt++
					}
				}
				if cnt == 0 {
					return 1e9 // services nobody calls go last
				}
				return sum / cnt
			}
			sort.SliceStable(byCol[k], func(i, j int) bool { return bary(byCol[k][i]) < bary(byCol[k][j]) })
		}
		for i, n := range byCol[k] {
			pos[n] = float64(i)
		}
	}

	maxRows := 0
	for _, ns := range byCol {
		maxRows = max(maxRows, len(ns))
	}
	var l Layout
	index := map[string]int{}
	for k := 0; k <= maxCol; k++ {
		offset := (maxRows - len(byCol[k])) * (BoxH + rowGap) / 2
		for r, n := range byCol[k] {
			index[n] = len(l.Boxes)
			l.Boxes = append(l.Boxes, Box{
				ServiceStats: stats[n], Col: k, Row: r,
				X: margin + k*(BoxW+colGap), Y: margin + offset + r*(BoxH+rowGap),
			})
		}
	}
	l.W = margin*2 + (maxCol+1)*(BoxW+colGap) - colGap + 2
	l.H = margin*2 + maxRows*(BoxH+rowGap)

	for _, c := range t.Calls {
		from, to := l.Boxes[index[c.From]], l.Boxes[index[c.To]]
		if to.Col <= from.Col {
			l.Loops++
			continue
		}
		l.edges = append(l.edges, edge{from: index[c.From], to: index[c.To], lane: from.Row % lanes})
	}
	return l
}

// acyclic drops the calls closing a cycle (found by depth-first search),
// so that layers can follow the remaining ones.
func acyclic(names []string, calls []traces.Call) []traces.Call {
	out := map[string][]traces.Call{}
	for _, c := range calls {
		if c.From != c.To {
			out[c.From] = append(out[c.From], c)
		}
	}
	const (
		unseen = iota
		onStack
		done
	)
	state := map[string]int{}
	var keep []traces.Call
	var visit func(n string)
	visit = func(n string) {
		state[n] = onStack
		for _, c := range out[n] {
			switch state[c.To] {
			case onStack: // back edge: closes a cycle
			case unseen:
				keep = append(keep, c)
				visit(c.To)
			default:
				keep = append(keep, c)
			}
		}
		state[n] = done
	}
	// Start from the services nobody calls, then whatever remains (cycles).
	called := map[string]bool{}
	for _, c := range calls {
		if c.From != c.To {
			called[c.To] = true
		}
	}
	for _, n := range names {
		if !called[n] && state[n] == unseen {
			visit(n)
		}
	}
	for _, n := range names {
		if state[n] == unseen {
			visit(n)
		}
	}
	return keep
}

// layers places each service one column right of its deepest caller.
func layers(names []string, calls []traces.Call) map[string]int {
	col := make(map[string]int, len(names))
	for range names { // at most one pass per service in an acyclic graph
		changed := false
		for _, c := range calls {
			if col[c.To] < col[c.From]+1 {
				col[c.To] = col[c.From] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return col
}

// Styles of the map.
type Styles struct {
	Line, Arrow                    lipgloss.Style
	Box, BoxError, BoxSlow, BoxSel lipgloss.Style // borders
	Name, Text, ErrorText          lipgloss.Style
}

// Render draws the part of the map starting at (x0, y0), w×h cells, with
// the selected box highlighted (-1 for none).
func (l Layout) Render(st Styles, selected, x0, y0, w, h int) string {
	g := newGrid(l.W, l.H)
	for _, e := range l.edges {
		from, to := l.Boxes[e.from], l.Boxes[e.to]
		sx, sy := from.X+BoxW, from.Y+BoxH/2
		tx, ty := to.X-2, to.Y+BoxH/2
		laneX := sx + 2 + e.lane
		g.hline(sy, sx, laneX)
		g.vline(laneX, sy, ty)
		g.hline(ty, laneX, tx)
		g.arrow(tx+1, ty)
	}
	slowest := -1
	for i, b := range l.Boxes {
		if b.P95 > 0 && (slowest < 0 || b.P95 > l.Boxes[slowest].P95) {
			slowest = i
		}
	}
	for i, b := range l.Boxes {
		style := boxNormal
		switch {
		case i == selected:
			style = boxSelected
		case b.Errors > 0:
			style = boxError
		case i == slowest:
			style = boxSlow
		}
		g.box(b, style)
	}
	return g.render(st, x0, y0, w, h)
}

// BoxLines are the three lines of text of a service box.
func BoxLines(s traces.ServiceStats) (name, latency, volume string) {
	name = s.Name
	latency = "p95 " + formatDuration(s.P95)
	if s.P95 == 0 {
		latency = "p95 n/a"
	}
	volume = fmt.Sprintf("%s spans", count(s.Spans))
	if s.Errors > 0 {
		volume += fmt.Sprintf(" ✗ %s", count(s.Errors))
	}
	return name, latency, volume
}

func count(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// Drawing grid: lines merge into box-drawing characters.

const (
	up = 1 << iota
	down
	left
	right
)

var lineRunes = map[uint8]rune{
	left | right: '─', up | down: '│', left: '─', right: '─', up: '│', down: '│',
	down | right: '┌', down | left: '┐', up | right: '└', up | left: '┘',
	up | down | right: '├', up | down | left: '┤', down | left | right: '┬', up | left | right: '┴',
	up | down | left | right: '┼',
}

type kind uint8

const (
	cellEmpty kind = iota
	cellLine
	cellArrow
	boxNormal
	boxError
	boxSlow
	boxSelected
	textName
	textPlain
	textError
)

type cell struct {
	r    rune
	bits uint8
	kind kind
}

type grid struct {
	w, h  int
	cells [][]cell
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h, cells: make([][]cell, h)}
	for y := range g.cells {
		g.cells[y] = make([]cell, w)
	}
	return g
}

func (g *grid) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.cells[y][x]
}

func (g *grid) line(x, y int, bits uint8) {
	if c := g.at(x, y); c != nil && (c.kind == cellEmpty || c.kind == cellLine) {
		c.kind, c.bits = cellLine, c.bits|bits
		c.r = lineRunes[c.bits]
	}
}

func (g *grid) hline(y, x0, x1 int) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		var b uint8
		if x > x0 {
			b |= left
		}
		if x < x1 {
			b |= right
		}
		if b != 0 {
			g.line(x, y, b)
		}
	}
}

func (g *grid) vline(x, y0, y1 int) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		var b uint8
		if y > y0 {
			b |= up
		}
		if y < y1 {
			b |= down
		}
		if b != 0 {
			g.line(x, y, b)
		}
	}
}

func (g *grid) arrow(x, y int) {
	if c := g.at(x, y); c != nil {
		c.kind, c.r = cellArrow, '▶'
	}
}

func (g *grid) box(b Box, style kind) {
	for y := b.Y; y < b.Y+BoxH; y++ {
		for x := b.X; x < b.X+BoxW; x++ {
			c := g.at(x, y)
			if c == nil {
				continue
			}
			c.kind, c.bits = style, 0
			switch {
			case y == b.Y && x == b.X:
				c.r = '╭'
			case y == b.Y && x == b.X+BoxW-1:
				c.r = '╮'
			case y == b.Y+BoxH-1 && x == b.X:
				c.r = '╰'
			case y == b.Y+BoxH-1 && x == b.X+BoxW-1:
				c.r = '╯'
			case y == b.Y || y == b.Y+BoxH-1:
				c.r = '─'
			case x == b.X || x == b.X+BoxW-1:
				c.r = '│'
			default:
				c.r = ' '
			}
		}
	}
	name, latency, volume := BoxLines(b.ServiceStats)
	textKind := textPlain
	if b.Errors > 0 {
		textKind = textError
	}
	g.text(b.X+2, b.Y+1, name, textName)
	g.text(b.X+2, b.Y+2, latency, textPlain)
	g.text(b.X+2, b.Y+3, volume, textKind)
}

func (g *grid) text(x, y int, s string, k kind) {
	for i, r := range []rune(ansi.Truncate(s, BoxW-4, "…")) {
		if c := g.at(x+i, y); c != nil {
			c.r, c.kind = r, k
		}
	}
}

func (g *grid) render(st Styles, x0, y0, w, h int) string {
	styleOf := map[kind]lipgloss.Style{
		cellLine: st.Line, cellArrow: st.Arrow,
		boxNormal: st.Box, boxError: st.BoxError, boxSlow: st.BoxSlow, boxSelected: st.BoxSel,
		textName: st.Name, textPlain: st.Text, textError: st.ErrorText,
	}
	lines := make([]string, 0, h)
	for y := y0; y < y0+h; y++ {
		var out, run strings.Builder
		cur := cellEmpty
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if s, ok := styleOf[cur]; ok {
				out.WriteString(s.Render(run.String()))
			} else {
				out.WriteString(run.String())
			}
			run.Reset()
		}
		for x := x0; x < x0+w; x++ {
			c := g.at(x, y)
			r, k := ' ', cellEmpty
			if c != nil && c.kind != cellEmpty {
				r, k = c.r, c.kind
			}
			if k != cur {
				flush()
				cur = k
			}
			run.WriteRune(r)
		}
		flush()
		lines = append(lines, out.String())
	}
	return strings.Join(lines, "\n")
}
