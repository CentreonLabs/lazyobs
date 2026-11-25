// Package chart draws time-series line charts in the terminal with braille
// characters (2×4 dots per cell), in the spirit of Grafana panels: a light
// grid, round-numbered axes, one colored line per series and a legend.
package chart

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// Styles are the colors of the non-data elements.
type Styles struct {
	Axis   lipgloss.Style // tick labels
	Grid   lipgloss.Style // grid lines
	Marker lipgloss.Style // the vertical marker line
	Legend lipgloss.Style // legend text
}

// Options describe a chart.
type Options struct {
	Width, Height int // size in cells, axes and legend included
	From, To      time.Time
	Location      *time.Location
	Series        []obs.Series
	// Color returns the color of a series (by name).
	Color func(name string) color.Color
	// Marker, when non-zero, draws a vertical line at that time.
	Marker time.Time
	// Hidden series are not drawn nor used for the scale; they stay in the
	// legend, dimmed, so they can be shown again.
	Hidden map[string]bool
	// LegendHint is appended to the legend, dimmed.
	LegendHint string
	Styles     Styles
}

// Legend locates the legend entries in the rendered chart, so that clicks
// can be mapped to series.
type Legend struct {
	Row   int // line of the legend within the chart
	Items []LegendItem
}

// LegendItem is the horizontal extent [X0, X1) of a series in the legend.
type LegendItem struct {
	Name   string
	X0, X1 int
}

// At returns the series whose legend entry covers column x.
func (l Legend) At(x int) (string, bool) {
	for _, it := range l.Items {
		if x >= it.X0 && x < it.X1 {
			return it.Name, true
		}
	}
	return "", false
}

// Layout constants.
const (
	xAxisLines = 1
	legendRows = 1
	minPlotW   = 10
	minPlotH   = 2
)

// Render draws the chart, always exactly Height lines, and tells where the
// legend entries are.
func Render(o Options) (string, Legend) {
	if o.Location == nil {
		o.Location = time.Local
	}
	span := o.To.Sub(o.From)
	plotH := o.Height - xAxisLines - legendRows
	if span <= 0 || plotH < minPlotH {
		return pad("", o.Width, o.Height), Legend{Row: -1}
	}
	visible := make([]obs.Series, 0, len(o.Series))
	for _, s := range o.Series {
		if !o.Hidden[s.Name] {
			visible = append(visible, s)
		}
	}

	// Values are rates so different time ranges read the same way.
	step := BucketSize(o.Series, o.From, o.To)
	perUnit, unit := rateUnit(visible, step)

	// Y axis: round ticks from 0 to the first tick above the peak.
	var peak float64
	for _, s := range visible {
		for _, p := range s.Points {
			peak = max(peak, p.Value*perUnit)
		}
	}
	ticks := yTicks(peak, max(plotH/3, 2))
	yMax := ticks[len(ticks)-1]
	labels := make([]string, len(ticks))
	gutter := 0
	for i, v := range ticks {
		labels[i] = formatValue(v) + unit
		gutter = max(gutter, ansi.StringWidth(labels[i]))
	}
	gutter++ // space between labels and plot
	plotW := o.Width - gutter
	if plotW < minPlotW {
		return pad("", o.Width, o.Height), Legend{Row: -1}
	}

	c := newCanvas(plotW, plotH)

	// Grid: horizontal lines at Y ticks, vertical lines at X ticks.
	tickRow := make(map[int]string, len(ticks))
	for i, v := range ticks {
		row := c.rowOf(v / yMax)
		tickRow[row] = labels[i]
		c.hline(row)
	}
	xticks := xTicks(o.From, o.To, o.Location, plotW)
	for _, t := range xticks {
		c.vline(columnOf(t.at, o.From, o.To, plotW))
	}

	// Lines, least important first so the last series stays on top.
	for i, s := range o.Series {
		if o.Hidden[s.Name] {
			continue
		}
		xs, ys := samples(s.Points, o.From, o.To, plotW*2, step)
		for j := range ys {
			ys[j] = ys[j] * perUnit / yMax
		}
		c.plot(xs, ys, i)
	}
	if !o.Marker.IsZero() {
		if col := columnOf(o.Marker, o.From, o.To, plotW); col >= 0 {
			c.marker(col)
		}
	}

	styles := make([]lipgloss.Style, len(o.Series))
	for i, s := range o.Series {
		styles[i] = lipgloss.NewStyle().Foreground(o.Color(s.Name))
	}

	lines := make([]string, 0, o.Height)
	for row := range plotH {
		label := strings.Repeat(" ", gutter)
		if l, ok := tickRow[row]; ok {
			label = fmt.Sprintf("%*s ", gutter-1, l)
		}
		lines = append(lines, o.Styles.Axis.Render(label)+c.renderRow(row, styles, o.Styles))
	}
	lines = append(lines, strings.Repeat(" ", gutter)+o.Styles.Axis.Render(xAxis(xticks, o.From, o.To, plotW)))
	text, items := legend(o, step)
	lines = append(lines, text)
	return pad(strings.Join(lines, "\n"), o.Width, o.Height), Legend{Row: len(lines) - 1, Items: items}
}

// samples places the points on the dot grid: each point at the center of its
// bucket, or averaged per dot when there are more points than dots.
func samples(points []obs.Point, from, to time.Time, dots int, step time.Duration) (xs []int, ys []float64) {
	if len(points) > dots {
		ys = Bucketize(points, from, to, dots)
		xs = make([]int, len(ys))
		for i := range xs {
			xs[i] = i
		}
		return xs, ys
	}
	span := float64(to.Sub(from))
	for _, p := range points {
		center := p.Time.Add(step / 2)
		if center.Before(from) || !center.Before(to) {
			continue
		}
		xs = append(xs, int(float64(center.Sub(from))/span*float64(dots)))
		ys = append(ys, p.Value)
	}
	return xs, ys
}

// BucketSize is the duration covered by one point of the series.
func BucketSize(series []obs.Series, from, to time.Time) time.Duration {
	for _, s := range series {
		if len(s.Points) > 1 {
			return s.Points[1].Time.Sub(s.Points[0].Time)
		}
	}
	return to.Sub(from)
}

// rateUnit picks per-second or per-minute rates depending on the volume.
func rateUnit(series []obs.Series, step time.Duration) (factor float64, unit string) {
	secs := max(step.Seconds(), 1e-9)
	var peak float64
	for _, s := range series {
		for _, p := range s.Points {
			peak = max(peak, p.Value/secs)
		}
	}
	if peak >= 1 || peak == 0 {
		return 1 / secs, "/s"
	}
	return 60 / secs, "/min"
}

// Bucketize maps points to n samples spanning [from, to). Several points in
// a sample are averaged; a sample between points takes the value of the
// point it falls in.
func Bucketize(points []obs.Point, from, to time.Time, n int) []float64 {
	out := make([]float64, max(n, 0))
	span := to.Sub(from)
	if span <= 0 || n <= 0 || len(points) == 0 {
		return out
	}
	if len(points) >= n {
		counts := make([]int, n)
		for _, p := range points {
			i := min(max(int(float64(p.Time.Sub(from))/float64(span)*float64(n)), 0), n-1)
			out[i] += p.Value
			counts[i]++
		}
		for i, c := range counts {
			if c > 0 {
				out[i] /= float64(c)
			}
		}
		return out
	}
	for i := range out {
		at := from.Add(time.Duration((float64(i) + 0.5) * float64(span) / float64(n)))
		j := sort.Search(len(points), func(j int) bool { return points[j].Time.After(at) }) - 1
		if j >= 0 {
			out[i] = points[j].Value
		}
	}
	return out
}

func columnOf(t, from, to time.Time, cols int) int {
	span := to.Sub(from)
	if span <= 0 || t.Before(from) || !t.Before(to) {
		return -1
	}
	return min(int(float64(t.Sub(from))/float64(span)*float64(cols)), cols-1)
}

// yTicks returns round tick values from 0 to at least peak, about n intervals.
func yTicks(peak float64, n int) []float64 {
	if peak <= 0 {
		return []float64{0, 1}
	}
	step := niceStep(peak / float64(n))
	top := math.Ceil(peak/step-1e-9) * step
	var ticks []float64
	for v := 0.0; v <= top+step/2; v += step {
		ticks = append(ticks, v)
	}
	return ticks
}

// niceStep rounds x up to 1, 2, 2.5 or 5 times a power of ten.
func niceStep(x float64) float64 {
	exp := math.Pow(10, math.Floor(math.Log10(x)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*exp >= x-1e-12 {
			return m * exp
		}
	}
	return 10 * exp
}

func formatValue(v float64) string {
	switch {
	case v >= 1e6:
		return trimZero(fmt.Sprintf("%.1f", v/1e6)) + "M"
	case v >= 1e3:
		return trimZero(fmt.Sprintf("%.1f", v/1e3)) + "k"
	case v == math.Trunc(v):
		return fmt.Sprintf("%.0f", v)
	case v >= 1:
		return trimZero(fmt.Sprintf("%.1f", v))
	default:
		return trimZero(fmt.Sprintf("%.2f", v))
	}
}

func trimZero(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

type xtick struct {
	at    time.Time
	label string
}

var tickIntervals = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour, 14 * 24 * time.Hour, 30 * 24 * time.Hour,
}

// xTicks picks evenly spaced, round times whose labels fit in width cells.
func xTicks(from, to time.Time, loc *time.Location, width int) []xtick {
	span := to.Sub(from)
	for _, iv := range tickIntervals {
		layout := tickLayout(iv, span)
		if int(span/iv)*(len(layout)+3) > width {
			continue
		}
		var ticks []xtick
		for t := alignUp(from.In(loc), iv); t.Before(to); t = t.Add(iv) {
			ticks = append(ticks, xtick{at: t, label: t.Format(layout)})
		}
		return ticks
	}
	return nil
}

func tickLayout(iv, span time.Duration) string {
	switch {
	case iv < time.Minute:
		return "15:04:05"
	case iv >= 24*time.Hour:
		return "01-02"
	case span > 24*time.Hour:
		return "01-02 15:04"
	default:
		return "15:04"
	}
}

// alignUp returns the first multiple of iv at or after t, counted from local midnight.
func alignUp(t time.Time, iv time.Duration) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	if iv >= 24*time.Hour {
		if day.Before(t) {
			day = day.AddDate(0, 0, 1)
		}
		return day
	}
	n := (t.Sub(day) + iv - 1) / iv
	return day.Add(n * iv)
}

func xAxis(ticks []xtick, from, to time.Time, width int) string {
	axis := []rune(strings.Repeat(" ", width))
	next := 0 // first free cell, to avoid overlapping labels
	for _, t := range ticks {
		col := columnOf(t.at, from, to, width)
		label := []rune(t.label)
		at := min(max(col-len(label)/2, 0), width-len(label))
		if col < 0 || at < next || at < 0 {
			continue
		}
		copy(axis[at:], label)
		next = at + len(label) + 2
	}
	return string(axis)
}

func legend(o Options, step time.Duration) (string, []LegendItem) {
	const sep = "   "
	var b strings.Builder
	var items []LegendItem
	x := 0
	for i := len(o.Series) - 1; i >= 0; i-- { // most important first
		s := o.Series[i]
		var total float64
		for _, p := range s.Points {
			total += p.Value
		}
		swatch := lipgloss.NewStyle().Foreground(o.Color(s.Name)).Render("━━")
		label := o.Styles.Legend.Render(fmt.Sprintf("%s %s", s.Name, formatValue(total)))
		if o.Hidden[s.Name] {
			swatch = o.Styles.Grid.Render("──")
			label = o.Styles.Axis.Render(fmt.Sprintf("%s %s", s.Name, formatValue(total)))
		}
		if len(items) > 0 {
			b.WriteString(sep)
			x += len(sep)
		}
		entry := swatch + " " + label
		w := ansi.StringWidth(entry)
		items = append(items, LegendItem{Name: s.Name, X0: x, X1: x + w})
		b.WriteString(entry)
		x += w
	}
	tail := fmt.Sprintf("%s· %s buckets", sep, formatStep(step))
	if o.LegendHint != "" {
		tail += " · " + o.LegendHint
	}
	b.WriteString(o.Styles.Axis.Render(tail))
	return b.String(), items
}

func formatStep(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d >= day && d%day == 0:
		return fmt.Sprintf("%dd", d/day)
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Minute:
		return d.Round(time.Second).String()
	case d >= time.Second:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Millisecond).String()
	}
}

// pad makes s exactly w×h cells.
func pad(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		l = ansi.Truncate(l, w, "")
		if gap := w - ansi.StringWidth(l); gap > 0 {
			l += strings.Repeat(" ", gap)
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}
