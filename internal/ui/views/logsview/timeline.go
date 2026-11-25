package logsview

import (
	"image/color"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

const (
	// minSplitHeight is the body height below which the timeline collapses
	// to a one-line sparkline.
	minSplitHeight = 20
	// timelineGutter approximates the width of the y-axis labels.
	timelineGutter = 8
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

// timelineColumns is the number of buckets the histogram should have: about
// one per two cells of the chart, so lines read as curves rather than noise,
// or one per cell for the sparkline.
func (m *Model) timelineColumns() int {
	if m.layout().timeline > 0 {
		return max((m.width-2-timelineGutter)/2, 10)
	}
	return max(m.width-12, 10)
}

// renderTimeline draws the chart and records where its legend is. The chart
// is drawn at (originX, originY) in body coordinates.
func (m *Model) renderTimeline(width, height, originX, originY int) string {
	if len(m.hist) == 0 || !m.histFrom.Before(m.histTo) {
		msg := "Loading…"
		if !m.loading {
			msg = "No data"
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, m.th.Dim.Render(msg))
	}
	var marker time.Time
	if len(m.records) > 0 {
		marker = m.records[m.cursor].Timestamp
	}
	var selFrom, selTo time.Time
	if d := m.drag; d != nil {
		selFrom, selTo = d.start, d.end
	}
	out, layout := chart.Render(chart.Options{
		Width:      width,
		Height:     height,
		From:       m.histFrom,
		To:         m.histTo,
		Location:   m.location(),
		Series:     m.hist,
		Color:      func(name string) color.Color { return m.th.SeverityColor(logs.ParseSeverity(name)) },
		Marker:     marker,
		Hidden:     m.hidden,
		LegendHint: legendHint,
		SelectFrom: selFrom,
		SelectTo:   selTo,
		Hover:      m.hover,
		Styles:     m.th.Chart,
	})
	if layout.Legend.Row >= 0 {
		m.hits.legend, m.hits.legendX, m.hits.legendY = layout.Legend, originX, originY+layout.Legend.Row
		m.hits.chart = layout
		m.hits.plotX, m.hits.plotY = originX+layout.PlotX, originY
		m.hits.plotW, m.hits.plotH = layout.PlotW, layout.PlotH
	}
	return out
}

// totalSeries sums all series into one, for the sparkline.
func totalSeries(series []obs.Series, from, to time.Time, n int) []float64 {
	out := make([]float64, n)
	for _, s := range series {
		for i, v := range chart.Bucketize(s.Points, from, to, n) {
			out[i] += v
		}
	}
	return out
}
