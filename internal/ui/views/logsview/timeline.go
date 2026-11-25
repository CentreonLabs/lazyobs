package logsview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

const (
	// minSplitHeight is the body height below which the timeline collapses
	// to a one-line sparkline.
	minSplitHeight = 20
	// timelineGutter is the width of the y-axis labels.
	timelineGutter = 7
)

// layout describes how the body height is shared between the panels.
type layout struct {
	timeline int // height of the timeline panel, 0 when shown as a sparkline
	panels   int // height of the list and detail panels
}

func (m *Model) layout() layout {
	avail := max(m.height-1, 4) // minus the query bar
	if m.compact || m.height < minSplitHeight || (m.showDetail && m.width < sideBySideWidth) {
		return layout{panels: max(avail-1, 3)} // one line for the sparkline
	}
	top := avail / 2
	return layout{timeline: top, panels: avail - top}
}

// timelineColumns is the number of buckets the histogram should have.
func (m *Model) timelineColumns() int {
	if m.layout().timeline > 0 {
		return max(m.width-2-timelineGutter, 10)
	}
	return max(m.width-12, 10)
}

// bucketize maps the histogram points to cols columns spanning [from, to).
// Values stay "per bucket": with more points than columns, a column shows the
// average of its buckets (summing would draw spikes wherever a column happens
// to get one bucket more than its neighbors); with fewer, each column takes
// the value of the bucket it falls in.
func bucketize(points []obs.Point, from, to time.Time, cols int) []float64 {
	out := make([]float64, max(cols, 0))
	span := to.Sub(from)
	if span <= 0 || cols <= 0 || len(points) == 0 {
		return out
	}
	if len(points) >= cols {
		counts := make([]int, cols)
		for _, p := range points {
			c := clampIndex(int(float64(p.Time.Sub(from))/float64(span)*float64(cols)), cols-1)
			out[c] += p.Value
			counts[c]++
		}
		for c, n := range counts {
			if n > 0 {
				out[c] /= float64(n)
			}
		}
		return out
	}
	for c := range out {
		at := from.Add(time.Duration((float64(c) + 0.5) * float64(span) / float64(cols)))
		i := sort.Search(len(points), func(i int) bool { return points[i].Time.After(at) }) - 1
		if i >= 0 {
			out[c] = points[i].Value
		}
	}
	return out
}

// columnOf returns the column of t, or -1 when it is outside [from, to).
func columnOf(t, from, to time.Time, cols int) int {
	span := to.Sub(from)
	if span <= 0 || t.Before(from) || !t.Before(to) {
		return -1
	}
	return min(int(float64(t.Sub(from))/float64(span)*float64(cols)), cols-1)
}

// bucketSize is the duration covered by one histogram point.
func bucketSize(points []obs.Point, from, to time.Time) time.Duration {
	if len(points) > 1 {
		return points[1].Time.Sub(points[0].Time)
	}
	return to.Sub(from)
}

func (m *Model) renderTimeline(width, height int) string {
	t := m.th
	cols := max(width-timelineGutter, 1)
	rows := max(height-2, 1) // minus the title and the time axis
	from, to := m.histFrom, m.histTo

	if len(m.hist) == 0 || !from.Before(to) {
		msg := "Loading the timeline…"
		if !m.loading {
			msg = "No timeline for this search."
		}
		return t.Key.Render("timeline") + "\n" + t.Dim.Render(msg)
	}

	values := bucketize(m.hist, from, to, cols)
	var peak float64
	for _, v := range values {
		peak = max(peak, v)
	}
	marker := -1
	if len(m.records) > 0 {
		marker = columnOf(m.records[m.cursor].Timestamp, from, to, cols)
	}

	step := bucketSize(m.hist, from, to)
	lines := []string{t.Key.Render("timeline") + t.Dim.Render(fmt.Sprintf(
		"  logs per %s · peak %s · │ selected log", roundDuration(step), humanCount(int64(peak))))}

	blocks := []rune(sparkBlocks)
	for r := range rows {
		level := rows - 1 - r // 0 is the bottom row
		var left, right strings.Builder
		var mark string
		for c, v := range values {
			ch := ' '
			if peak > 0 && v > 0 {
				eighths := int(v/peak*float64(rows*8) + 0.5)
				if v > 0 && eighths == 0 && level == 0 {
					eighths = 1 // keep small values visible
				}
				switch fill := eighths - level*8; {
				case fill >= 8:
					ch = '█'
				case fill > 0:
					ch = blocks[fill-1]
				}
			}
			switch {
			case c == marker && ch == ' ':
				mark = t.Dim.Render("│")
			case c == marker:
				mark = t.WarnText.Render(string(ch))
			case marker >= 0 && c > marker:
				right.WriteRune(ch)
			default:
				left.WriteRune(ch)
			}
		}
		gutter := strings.Repeat(" ", timelineGutter)
		switch level {
		case rows - 1:
			gutter = fmt.Sprintf("%*s ", timelineGutter-1, humanCount(int64(peak)))
		case 0:
			gutter = fmt.Sprintf("%*s ", timelineGutter-1, "0")
		}
		lines = append(lines, t.Dim.Render(gutter)+t.Spark.Render(left.String())+mark+t.Spark.Render(right.String()))
	}
	lines = append(lines, strings.Repeat(" ", timelineGutter)+t.Dim.Render(timeAxis(from.In(m.location()), to.In(m.location()), cols)))
	return strings.Join(lines, "\n")
}

// timeAxis renders the start, middle and end times of the timeline.
func timeAxis(from, to time.Time, cols int) string {
	layout := "15:04"
	switch span := to.Sub(from); {
	case span > 24*time.Hour:
		layout = "01-02 15:04"
	case span <= 10*time.Minute:
		layout = "15:04:05"
	}
	axis := []rune(strings.Repeat(" ", cols))
	put := func(at int, s string) {
		r := []rune(s)
		at = clampIndex(at, cols-len(r))
		if at < 0 {
			return
		}
		copy(axis[at:], r)
	}
	start, end := from.Format(layout), to.Format(layout)
	put(0, start)
	if mid := from.Add(to.Sub(from) / 2).Format(layout); cols > 3*len(layout)+6 {
		put(cols/2-len(mid)/2, mid)
	}
	put(cols-len(end), end)
	return string(axis)
}

func roundDuration(d time.Duration) string {
	switch {
	case d >= time.Minute:
		return obs.FormatDuration(d.Round(time.Minute))
	case d >= time.Second:
		return obs.FormatDuration(d.Round(time.Second))
	default:
		return d.Round(time.Millisecond).String()
	}
}
