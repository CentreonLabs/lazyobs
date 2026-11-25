package chart

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Braille dot bits, indexed by [x][y] within a 2×4 cell.
var dotBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

const brailleBase = 0x2800

type cellKind int

const (
	cellEmpty cellKind = iota
	cellHGrid
	cellVGrid
	cellCross
	cellMarker
	cellHover
	cellData
)

type cell struct {
	kind   cellKind
	dots   rune
	series int // series index of a data cell (the last one drawn wins)
}

// canvas is a grid of cells addressed in dots: (0,0) is the top-left dot.
type canvas struct {
	w, h  int // in cells
	cells [][]cell
	// selFrom..selTo (inclusive) are highlighted columns; selFrom < 0 when none.
	selFrom, selTo int
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([][]cell, h), selFrom: -1, selTo: -1}
	for i := range c.cells {
		c.cells[i] = make([]cell, w)
	}
	return c
}

// rowOf returns the cell row of a value normalized to [0, 1] (1 is the top).
func (c *canvas) rowOf(v float64) int {
	return c.h - 1 - min(max(int(v*float64(c.h*4-1)+0.5)/4, 0), c.h-1)
}

func (c *canvas) hline(row int) {
	for x := range c.w {
		c.grid(row, x, cellHGrid)
	}
}

func (c *canvas) vline(col int) {
	if col < 0 || col >= c.w {
		return
	}
	for y := range c.h {
		c.grid(y, col, cellVGrid)
	}
}

func (c *canvas) grid(row, col int, kind cellKind) {
	cl := &c.cells[row][col]
	switch {
	case cl.kind == cellEmpty:
		cl.kind = kind
	case cl.kind != kind && (cl.kind == cellHGrid || cl.kind == cellVGrid):
		cl.kind = cellCross
	}
}

// marker draws a vertical line (cellMarker or cellHover) on the cells of col
// not used by data.
func (c *canvas) marker(col int, kind cellKind) {
	for y := range c.h {
		if c.cells[y][col].kind != cellData {
			c.cells[y][col].kind = kind
		}
	}
}

// plot draws a polyline through points given in horizontal dots, with values
// normalized to [0, 1].
func (c *canvas) plot(xs []int, values []float64, series int) {
	dh := c.h*4 - 1
	prevX, prevY := -1, 0
	for i, v := range values {
		x := xs[i]
		y := dh - min(max(int(v*float64(dh)+0.5), 0), dh)
		if prevX >= 0 {
			c.line(prevX, prevY, x, y, series)
		} else {
			c.dot(x, y, series)
		}
		prevX, prevY = x, y
	}
}

// line draws a segment between two dots (Bresenham).
func (c *canvas) line(x0, y0, x1, y1, series int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx + dy
	for {
		c.dot(x0, y0, series)
		if x0 == x1 && y0 == y1 {
			return
		}
		if e2 := 2 * err; e2 >= dy {
			err += dy
			x0 += sx
		} else {
			err += dx
			y0 += sy
		}
	}
}

func (c *canvas) dot(x, y, series int) {
	col, row := x/2, y/4
	if col < 0 || col >= c.w || row < 0 || row >= c.h {
		return
	}
	cl := &c.cells[row][col]
	if cl.kind != cellData {
		cl.kind, cl.dots = cellData, 0
	}
	cl.dots |= dotBits[x%2][y%4]
	cl.series = series
}

// renderRow renders a row, grouping cells of the same style.
func (c *canvas) renderRow(row int, series []lipgloss.Style, st Styles) string {
	type runKey struct {
		style    *lipgloss.Style
		selected bool
	}
	var out, run strings.Builder
	var cur runKey
	flush := func() {
		if run.Len() == 0 {
			return
		}
		style := lipgloss.NewStyle()
		if cur.style != nil {
			style = *cur.style
		}
		if cur.selected && st.Selection != nil {
			style = style.Background(st.Selection)
		}
		out.WriteString(style.Render(run.String()))
		run.Reset()
	}
	for x, cl := range c.cells[row] {
		var ch rune
		var style *lipgloss.Style
		switch cl.kind {
		case cellEmpty:
			ch = ' '
		case cellHGrid:
			ch, style = '┈', &st.Grid
		case cellVGrid:
			ch, style = '┊', &st.Grid
		case cellCross:
			ch, style = '┼', &st.Grid
		case cellMarker:
			ch, style = '│', &st.Marker
		case cellHover:
			ch, style = '│', &st.Hover
		case cellData:
			ch, style = brailleBase+cl.dots, &series[cl.series]
		}
		key := runKey{style: style, selected: c.selFrom >= 0 && x >= c.selFrom && x <= c.selTo}
		if key != cur {
			flush()
			cur = key
		}
		run.WriteRune(ch)
	}
	flush()
	return out.String()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
