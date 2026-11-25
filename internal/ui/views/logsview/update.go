package logsview

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Query):
		return m.openInput(modeQuery, m.query, "service_name:api AND severity_text:ERROR")
	case key.Matches(msg, k.Ask):
		return m.openAsk()
	case key.Matches(msg, k.ZoomOut):
		return m.zoomOut()
	case key.Matches(msg, k.Range):
		m.presetIdx = -1
		return m.openInput(modeRange, rangeInputValue(m.rng, m.location()), "15m, 2h, 7d or 2026-11-03 13:00 .. 2026-11-03 14:00")
	case key.Matches(msg, k.Refresh):
		return m.search()
	case key.Matches(msg, k.Live):
		return m.toggleLive()
	case key.Matches(msg, k.Pause):
		return m.togglePause()
	case key.Matches(msg, k.UTC):
		m.utc = !m.utc
		m.setDetail()
		return nil
	case key.Matches(msg, k.Layout):
		m.compact = !m.compact
		m.layoutDetail()
		m.scrollToCursor()
		return m.fetchHistogram() // the number of columns changed
	case key.Matches(msg, k.Copy):
		return m.copySelected()
	case key.Matches(msg, k.Detail):
		if len(m.records) > 0 {
			m.showDetail = true
			m.layoutDetail()
			m.setDetail()
		}
		return nil
	case key.Matches(msg, k.Close):
		m.showDetail, m.focus = false, focusList
		m.layoutDetail()
		m.scrollToCursor()
		return nil
	case key.Matches(msg, k.Focus):
		if m.showDetail {
			m.focus = 1 - m.focus
		}
		return nil
	}
	if m.focus == focusDetail && m.showDetail {
		return m.scrollDetail(msg)
	}
	return m.moveCursor(msg)
}

func (m *Model) moveCursor(msg tea.KeyPressMsg) tea.Cmd {
	k, rows := m.keys, max(m.listRows(), 1)
	prev := m.cursor
	switch {
	case key.Matches(msg, k.Down):
		m.cursor++
	case key.Matches(msg, k.Up):
		m.cursor--
	case key.Matches(msg, k.Top):
		m.cursor = 0
	case key.Matches(msg, k.Bottom):
		m.cursor = len(m.records) - 1
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
	m.cursor = clampIndex(m.cursor, len(m.records)-1)
	m.scrollToCursor()
	if m.cursor != prev {
		m.setDetail()
	}
	return m.maybeLoadMore()
}

func (m *Model) scrollDetail(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Down):
		m.detail.ScrollDown(1)
	case key.Matches(msg, k.Up):
		m.detail.ScrollUp(1)
	case key.Matches(msg, k.Top):
		m.detail.GotoTop()
	case key.Matches(msg, k.Bottom):
		m.detail.GotoBottom()
	case key.Matches(msg, k.HalfDown):
		m.detail.HalfPageDown()
	case key.Matches(msg, k.HalfUp):
		m.detail.HalfPageUp()
	case key.Matches(msg, k.PageDown):
		m.detail.PageDown()
	case key.Matches(msg, k.PageUp):
		m.detail.PageUp()
	}
	return nil
}

func (m *Model) scrollToCursor() {
	rows := max(m.listRows(), 1)
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
	m.top = clampIndex(m.top, max(len(m.records)-rows, 0))
}

func (m *Model) openInput(mode inputMode, value, placeholder string) tea.Cmd {
	m.mode = mode
	m.input.SetValue(value)
	m.input.Placeholder = placeholder
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) closeInput() {
	m.mode = modeNormal
	m.input.Blur()
}

func (m *Model) updateInput(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.closeInput()
		return nil
	case key.Matches(msg, m.keys.Submit):
		return m.submitInput()
	case m.mode == modeRange && key.Matches(msg, m.keys.NextPreset):
		m.presetIdx = (m.presetIdx + 1) % len(rangePresets)
		m.input.SetValue(rangePresets[m.presetIdx])
		m.input.CursorEnd()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *Model) submitInput() tea.Cmd {
	value := strings.TrimSpace(m.input.Value())
	switch m.mode {
	case modeAsk:
		m.closeInput()
		if value == "" {
			return nil
		}
		return m.translate(value)
	case modeQuery:
		m.query = value
	case modeRange:
		rng, err := obs.ParseTimeRange(value, m.location())
		if err != nil {
			return app.Notify(app.LevelError, "%v", err)
		}
		m.rng = rng
		if m.live && !rng.IsRelative() {
			m.live = false
			m.closeInput()
			return tea.Batch(m.search(), app.Notify(app.LevelInfo, "live mode stopped: it needs a relative time range"))
		}
	}
	m.closeInput()
	return m.search()
}

func (m *Model) copySelected() tea.Cmd {
	if len(m.records) == 0 {
		return nil
	}
	r := m.records[m.cursor]
	var buf bytes.Buffer
	if len(r.Raw) == 0 || json.Indent(&buf, r.Raw, "", "  ") != nil {
		buf.Reset()
		buf.WriteString(r.Body)
	}
	return tea.Batch(tea.SetClipboard(buf.String()), app.Notify(app.LevelInfo, "log copied to the clipboard"))
}

func rangeInputValue(r obs.TimeRange, loc *time.Location) string {
	if r.IsRelative() {
		return obs.FormatDuration(r.Last)
	}
	return r.From.In(loc).Format(time.DateTime) + " .. " + r.To.In(loc).Format(time.DateTime)
}

// clampIndex bounds v to [0, hi], returning 0 when hi is negative.
func clampIndex(v, hi int) int {
	if hi < 0 {
		return 0
	}
	return min(max(v, 0), hi)
}
