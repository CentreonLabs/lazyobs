package chart

import (
	"fmt"
	"sort"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// tooltipGap is the distance between the pointer and the tooltip.
const tooltipGap = 2

// valueAt returns the value of the bucket containing t, and whether there is one.
func valueAt(points []obs.Point, t time.Time, step time.Duration) (float64, bool) {
	i := sort.Search(len(points), func(i int) bool { return points[i].Time.After(t) }) - 1
	if i < 0 || !t.Before(points[i].Time.Add(step)) {
		return 0, false
	}
	return points[i].Value, true
}

// bucketStart returns the start of the bucket containing t, falling back to t.
func bucketStart(series []obs.Series, t time.Time, step time.Duration) time.Time {
	for _, s := range series {
		i := sort.Search(len(s.Points), func(i int) bool { return s.Points[i].Time.After(t) }) - 1
		if i >= 0 && t.Before(s.Points[i].Time.Add(step)) {
			return s.Points[i].Time
		}
	}
	return t
}

// tooltip renders the values of the visible series in the bucket at t, most
// important series first.
func tooltip(o Options, visible []obs.Series, t time.Time, step time.Duration, perUnit float64, unit string) []string {
	start := bucketStart(visible, t, step).In(o.Location)
	layout := "15:04:05"
	if step >= 24*time.Hour {
		layout = "01-02 15:04"
	}
	rows := []string{o.Styles.Legend.Bold(true).Render(start.Format(layout) + " → " + start.Add(step).Format(layout))}

	nameW := 0
	for _, s := range visible {
		nameW = max(nameW, len(s.Name))
	}
	var total float64
	for i := len(visible) - 1; i >= 0; i-- {
		s := visible[i]
		v, ok := valueAt(s.Points, t, step)
		if !ok {
			continue
		}
		total += v
		swatch := lipgloss.NewStyle().Foreground(o.Color(s.Name)).Render("━")
		// Pad before styling: escape sequences would skew the widths.
		rows = append(rows, fmt.Sprintf("%s %-*s %8s %s", swatch, nameW, s.Name,
			formatValue(v*perUnit)+unit, o.Styles.Axis.Render(fmt.Sprintf("%7s", formatValue(v)))))
	}
	if len(rows) > 2 {
		rows = append(rows, fmt.Sprintf("  %-*s %8s %s", nameW, "total", formatValue(total*perUnit)+unit,
			o.Styles.Axis.Render(fmt.Sprintf("%7s", formatValue(total)))))
	}
	return rows
}
