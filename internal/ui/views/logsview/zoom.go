package logsview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// minZoomColumns is the narrowest drag that zooms; a shorter one is a click.
const minZoomColumns = 2

// dragSpan is a selection being dragged on the timeline, in plot columns.
type dragSpan struct {
	anchor   int // where the button was pressed
	from, to int // selected columns, inclusive, from <= to
}

func (m *Model) onDragMove(mouse tea.Mouse) {
	if m.drag == nil {
		return
	}
	col := clampIndex(mouse.X-m.hits.plotX, m.hits.plotW-1)
	m.drag.from, m.drag.to = min(m.drag.anchor, col), max(m.drag.anchor, col)
}

func (m *Model) onDragEnd(mouse tea.Mouse) tea.Cmd {
	if m.drag == nil {
		return nil
	}
	m.onDragMove(mouse)
	d := m.drag
	m.drag = nil
	if d.to-d.from+1 < minZoomColumns {
		return nil
	}
	from := m.hits.timeAt(d.from, m.histFrom, m.histTo)
	to := m.hits.timeAt(d.to+1, m.histFrom, m.histTo)
	return m.zoomTo(obs.Between(from.Truncate(time.Second), to.Add(time.Second-1).Truncate(time.Second)))
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
