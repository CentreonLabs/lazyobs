package logsview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

const (
	// minZoomColumns is the narrowest drag that zooms; a shorter one is a click.
	minZoomColumns = 2
	// panDivisor: moving in time shifts the range by 1/panDivisor of its span.
	panDivisor = 4
)

// dragSpan is a selection being dragged on the timeline.
type dragSpan struct {
	anchor   int // plot column where the button was pressed
	from, to int // selected plot columns, inclusive, from <= to
	// The selected span as times, computed when the mouse moves (while the
	// chart geometry of the frame under the pointer is known) and drawn by
	// the next frames.
	start, end time.Time
}

// startDrag begins a selection at plot column col.
func (m *Model) startDrag(col int) {
	m.drag = &dragSpan{anchor: col}
	m.updateDrag(col)
}

// onMouseMove extends a drag in progress, or follows the pointer over the
// timeline for the tooltip.
func (m *Model) onMouseMove(mouse tea.Mouse) {
	m.hover = nil
	if m.drag != nil {
		m.updateDrag(mouse.X - m.hits.plotX)
		return
	}
	if m.hits.inPlot(mouse.X, mouse.Y) && len(m.hist) > 0 {
		m.hover = &chart.Cell{Col: mouse.X - m.hits.plotX, Row: mouse.Y - m.hits.plotY}
	}
}

func (m *Model) updateDrag(col int) {
	col = clampIndex(col, m.hits.plotW-1)
	d := m.drag
	d.from, d.to = min(d.anchor, col), max(d.anchor, col)
	d.start = m.hits.timeAt(d.from, m.histFrom, m.histTo)
	d.end = m.hits.timeAt(d.to+1, m.histFrom, m.histTo)
}

func (m *Model) onDragEnd(mouse tea.Mouse) tea.Cmd {
	if m.drag == nil {
		return nil
	}
	m.onMouseMove(mouse)
	d := m.drag
	m.drag = nil
	if d.to-d.from+1 < minZoomColumns {
		return m.jumpToColumn(d.anchor) // a click: go to that bucket in the list
	}
	return m.zoomTo(obs.Between(d.start.Truncate(time.Second), d.end.Add(time.Second-1).Truncate(time.Second)))
}

// dragLabel describes the span being selected, e.g. "15:35:27 → 15:39:37 · 4m10s".
func (m *Model) dragLabel() string {
	d, loc := m.drag, m.location()
	layout := "15:04:05"
	if d.end.Sub(d.start) > 24*time.Hour || d.start.In(loc).YearDay() != d.end.In(loc).YearDay() {
		layout = "01-02 15:04"
	}
	return d.start.In(loc).Format(layout) + " → " + d.end.In(loc).Format(layout) +
		" · " + d.end.Sub(d.start).Round(time.Second).String() + " · release to zoom"
}

// zoomTo searches rng, remembering the current range for zooming out.
func (m *Model) zoomTo(rng obs.TimeRange) tea.Cmd {
	m.zoomStack = append(m.zoomStack, m.rng)
	m.rng = rng
	notice := app.Notify(app.LevelInfo, "zoomed in · press - to zoom out")
	if m.live {
		m.live = false
		notice = app.Notify(app.LevelInfo, "zoomed in, live mode stopped · press - to zoom out")
	}
	return tea.Batch(m.search(), notice)
}

// pan moves the time range by a quarter of its span: dir < 0 goes back in time,
// dir > 0 forward. Moving back freezes a relative range; moving forward up to
// now makes it relative again, so live mode can be resumed.
func (m *Model) pan(dir int) tea.Cmd {
	now := m.opts.Now()
	from, to := m.rng.Resolve(now)
	span := to.Sub(from)
	if dir > 0 && !to.Before(now.Add(-time.Second)) {
		return app.Notify(app.LevelInfo, "already showing the latest logs")
	}
	shift := time.Duration(dir) * span / panDivisor
	from, to = from.Add(shift), to.Add(shift)

	var notice tea.Cmd
	switch {
	case !to.Before(now):
		m.rng = obs.Last(span) // back to the present
		notice = app.Notify(app.LevelInfo, "back to the latest %s", obs.FormatDuration(span))
	default:
		m.rng = obs.Between(from.Truncate(time.Second), to.Truncate(time.Second))
		if m.live {
			m.live = false
			notice = app.Notify(app.LevelInfo, "live mode stopped while looking at the past")
		}
	}
	return tea.Batch(m.search(), notice)
}

// zoomOut goes back to the range before the last zoom, or doubles the
// current range around its center.
func (m *Model) zoomOut() tea.Cmd {
	if n := len(m.zoomStack); n > 0 {
		m.rng = m.zoomStack[n-1]
		m.zoomStack = m.zoomStack[:n-1]
		return m.search()
	}
	if m.rng.IsRelative() {
		m.rng = obs.Last(m.rng.Last * 2)
		return m.search()
	}
	span := m.rng.To.Sub(m.rng.From)
	from, to := m.rng.From.Add(-span/2), m.rng.To.Add(span/2)
	if now := m.opts.Now(); to.After(now) {
		from, to = from.Add(-to.Sub(now)), now
	}
	m.rng = obs.Between(from, to)
	return m.search()
}
