package tracesview

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

// logsWindow widens a trace's time span when showing its logs, for clock
// skew and logs written just before or after the spans.
const logsWindow = time.Minute

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.trace != nil {
		return m.waterfallKey(msg)
	}
	k := m.keys
	switch {
	case key.Matches(msg, k.Query):
		return m.openInput(modeQuery, m.query, "service_name:demo-gateway AND span_status.code:error")
	case key.Matches(msg, k.Range):
		m.presetIdx = -1
		return m.openInput(modeRange, rangeValue(m.rng, m.location()), "1h, 24h, 7d or 2026-11-03 13:00 .. 2026-11-03 14:00")
	case key.Matches(msg, k.Refresh):
		return m.search()
	case key.Matches(msg, k.Sort):
		m.order = 1 - m.order
		label := "newest first"
		if m.order == obs.OrderSlowest {
			label = "slowest first"
		}
		return tea.Batch(m.search(), app.Notify(app.LevelInfo, "traces sorted %s", label))
	case key.Matches(msg, k.Open):
		if len(m.items) > 0 {
			return m.openTrace(m.items[m.cursor].ID())
		}
	case key.Matches(msg, k.Logs):
		if len(m.items) > 0 {
			r := m.items[m.cursor].Root
			return m.openLogs(r.TraceID, r.Start, r.End())
		}
	case key.Matches(msg, k.Copy):
		if len(m.items) > 0 {
			return tea.Batch(tea.SetClipboard(m.items[m.cursor].ID()), app.Notify(app.LevelInfo, "trace ID copied"))
		}
	case key.Matches(msg, k.UTC):
		m.utc = !m.utc
	case key.Matches(msg, k.Layout):
		m.compact = !m.compact
		m.scrollToCursor()
		return m.fetchHistogram()
	case key.Matches(msg, k.ZoomOut):
		return m.zoomOut()
	case key.Matches(msg, k.Earlier):
		return m.pan(-1)
	case key.Matches(msg, k.Later):
		return m.pan(+1)
	default:
		return m.moveCursor(msg)
	}
	return nil
}

func (m *Model) moveCursor(msg tea.KeyPressMsg) tea.Cmd {
	k, rows := m.keys, max(m.listRows(), 1)
	switch {
	case key.Matches(msg, k.Down):
		m.cursor++
	case key.Matches(msg, k.Up):
		m.cursor--
	case key.Matches(msg, k.Top):
		m.cursor = 0
	case key.Matches(msg, k.Bottom):
		m.cursor = len(m.items) - 1
	case key.Matches(msg, k.HalfDown):
		m.cursor += rows / 2
	case key.Matches(msg, k.HalfUp):
		m.cursor -= rows / 2
	case key.Matches(msg, k.PageDown):
		m.cursor += rows
	case key.Matches(msg, k.PageUp):
		m.cursor -= rows
	default:
		return nil
	}
	m.cursor = clampIndex(m.cursor, len(m.items)-1)
	m.scrollToCursor()
	return m.maybeLoadMore()
}

func (m *Model) scrollToCursor() {
	rows := max(m.listRows(), 1)
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
	m.top = clampIndex(m.top, max(len(m.items)-rows, 0))
}

// openLogs shows the logs of a trace, around its time span.
func (m *Model) openLogs(traceID string, start, end time.Time) tea.Cmd {
	if m.opts.OpenLogs == nil {
		return app.Notify(app.LevelWarn, "logs are not available here")
	}
	rng := obs.Between(start.Add(-logsWindow).Truncate(time.Second), end.Add(logsWindow).Truncate(time.Second))
	return m.opts.OpenLogs("trace_id:"+traceID, rng)
}

func (m *Model) openInput(md mode, value, placeholder string) tea.Cmd {
	m.mode = md
	m.input.SetValue(value)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) updateInput(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Cancel):
		m.mode = modeNormal
		m.input.Blur()
		return nil
	case key.Matches(msg, k.Submit):
		value := strings.TrimSpace(m.input.Value())
		if m.mode == modeRange {
			rng, err := obs.ParseTimeRange(value, m.location())
			if err != nil {
				return app.Notify(app.LevelError, "%v", err)
			}
			m.rng = rng
		} else {
			m.query = value
		}
		m.mode = modeNormal
		m.input.Blur()
		m.trace = nil
		return m.search()
	case m.mode == modeRange && key.Matches(msg, k.NextPreset):
		m.presetIdx = (m.presetIdx + 1) % len(rangePresets)
		m.input.SetValue(rangePresets[m.presetIdx])
		m.input.CursorEnd()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// Zoom and time moves, as in the logs view.

func (m *Model) zoomTo(rng obs.TimeRange) tea.Cmd {
	m.zoomStack = append(m.zoomStack, m.rng)
	m.rng = rng
	return tea.Batch(m.search(), app.Notify(app.LevelInfo, "zoomed in · press - to zoom out"))
}

func (m *Model) zoomOut() tea.Cmd {
	if n := len(m.zoomStack); n > 0 {
		m.rng, m.zoomStack = m.zoomStack[n-1], m.zoomStack[:n-1]
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

// pan moves the range by a quarter of its span; reaching now makes it relative again.
func (m *Model) pan(dir int) tea.Cmd {
	now := m.opts.Now()
	from, to := m.rng.Resolve(now)
	span := to.Sub(from)
	if dir > 0 && !to.Before(now.Add(-time.Second)) {
		return app.Notify(app.LevelInfo, "already showing the latest traces")
	}
	shift := time.Duration(dir) * span / 4
	if to = to.Add(shift); !to.Before(now) {
		m.rng = obs.Last(span)
	} else {
		m.rng = obs.Between(from.Add(shift).Truncate(time.Second), to.Truncate(time.Second))
	}
	return m.search()
}

// Mouse: drag on the chart zooms, hovering shows the bucket values, clicks
// select list rows and the wheel scrolls.

type dragSpan struct{ anchor, from, to int }

type hitAreas struct {
	chart          chart.Layout
	plotX, plotY   int
	listTop, listN int
	rowsTop        int // waterfall: body row of the first span
}

func (h hitAreas) inPlot(x, y int) bool {
	return h.chart.PlotW > 0 && x >= h.plotX && x < h.plotX+h.chart.PlotW && y >= h.plotY && y < h.plotY+h.chart.PlotH
}

func (m *Model) onClick(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	h := m.hits
	if m.trace == nil && h.inPlot(mouse.X, mouse.Y) {
		col := mouse.X - h.plotX
		m.drag = &dragSpan{anchor: col, from: col, to: col}
		return nil
	}
	if m.trace != nil {
		if i := m.trace.top + mouse.Y - h.rowsTop; mouse.Y >= h.rowsTop && i < len(m.trace.nodes) {
			m.trace.selected = i
		}
		return nil
	}
	if row := mouse.Y - h.listTop; row >= 0 && row < h.listN && m.top+row < len(m.items) {
		m.cursor = m.top + row
		return m.maybeLoadMore()
	}
	return nil
}

func (m *Model) onMouseMove(mouse tea.Mouse) {
	m.hover = nil
	if m.drag != nil {
		col := clampIndex(mouse.X-m.hits.plotX, m.hits.chart.PlotW-1)
		m.drag.from, m.drag.to = min(m.drag.anchor, col), max(m.drag.anchor, col)
		return
	}
	if m.trace == nil && m.hits.inPlot(mouse.X, mouse.Y) && len(m.hist) > 0 {
		m.hover = &chart.Cell{Col: mouse.X - m.hits.plotX, Row: mouse.Y - m.hits.plotY}
	}
}

func (m *Model) onDragEnd(mouse tea.Mouse) tea.Cmd {
	if m.drag == nil {
		return nil
	}
	m.onMouseMove(mouse)
	d := m.drag
	m.drag = nil
	if d.to-d.from < 1 {
		return nil
	}
	from := m.hits.chart.TimeAt(d.from, m.histFrom, m.histTo)
	to := m.hits.chart.TimeAt(d.to+1, m.histFrom, m.histTo)
	return m.zoomTo(obs.Between(from.Truncate(time.Second), to.Add(time.Second-1).Truncate(time.Second)))
}

func (m *Model) onWheel(mouse tea.Mouse) {
	delta := 3
	if mouse.Button == tea.MouseWheelUp {
		delta = -3
	} else if mouse.Button != tea.MouseWheelDown {
		return
	}
	if m.trace != nil {
		m.trace.selected = clampIndex(m.trace.selected+delta, len(m.trace.nodes)-1)
		return
	}
	m.cursor = clampIndex(m.cursor+delta, len(m.items)-1)
	m.scrollToCursor()
}

func rangeValue(r obs.TimeRange, loc *time.Location) string {
	if r.IsRelative() {
		return obs.FormatDuration(r.Last)
	}
	return r.From.In(loc).Format(time.DateTime) + " .. " + r.To.In(loc).Format(time.DateTime)
}

func clampIndex(v, hi int) int {
	if hi < 0 {
		return 0
	}
	return min(max(v, 0), hi)
}
