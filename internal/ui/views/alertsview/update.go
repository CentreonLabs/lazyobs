package alertsview

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Details):
		m.detailOpen = !m.detailOpen
		m.scrollToCursor()
		return m.fetchDetails()
	case key.Matches(msg, k.Close):
		switch {
		case m.detailOpen:
			m.detailOpen = false
		case m.filter != "":
			m.filter = ""
			m.applyFilters(m.Selected().Rule.ID)
		}
		m.scrollToCursor()
	case key.Matches(msg, k.Logs):
		return m.logsOfSelected()
	case key.Matches(msg, k.Filter):
		m.filtering = true
		m.input.SetValue(m.filter)
		m.input.Placeholder = "part of a rule name"
		m.input.CursorEnd()
		return m.input.Focus()
	case key.Matches(msg, k.Problems):
		m.problemsOnly = !m.problemsOnly
		m.applyFilters(m.Selected().Rule.ID)
		return m.fetchDetails()
	case key.Matches(msg, k.Refresh):
		return tea.Batch(m.load(), m.schedule())
	case key.Matches(msg, k.Copy):
		a := m.Selected()
		if e, ok := m.details[detailKey(a)]; ok && !e.loading && len(e.details.Queries) > 0 {
			return tea.Batch(tea.SetClipboard(e.details.Queries[0].Text), app.Notify(app.LevelInfo, "query copied"))
		}
		if a.Rule.ID != "" {
			return tea.Batch(tea.SetClipboard(a.Rule.ID), app.Notify(app.LevelInfo, "rule ID copied"))
		}
	case key.Matches(msg, k.UTC):
		m.utc = !m.utc
	default:
		return m.moveCursor(msg)
	}
	return nil
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	switch {
	case key.Matches(msg, k.Cancel):
		m.filtering = false
		m.input.Blur()
		return nil
	case key.Matches(msg, k.Submit):
		m.filtering = false
		m.input.Blur()
		m.filter = strings.TrimSpace(m.input.Value())
		m.applyFilters(m.Selected().Rule.ID)
		return m.fetchDetails()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Filter as you type: the list is small and local.
	m.filter = strings.TrimSpace(m.input.Value())
	m.applyFilters(m.Selected().Rule.ID)
	return cmd
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
	m.cursor = min(max(m.cursor, 0), max(len(m.items)-1, 0))
	m.scrollToCursor()
	if m.cursor != prev {
		return m.fetchDetails()
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
	m.top = min(max(m.top, 0), max(len(m.items)-rows, 0))
}

// logsOfSelected opens the logs of the selected rule's query, loading the
// rule first when its details are not known yet.
func (m *Model) logsOfSelected() tea.Cmd {
	a := m.Selected()
	if a.Rule.ID == "" {
		return nil
	}
	if e, ok := m.details[detailKey(a)]; ok && !e.loading {
		return m.openLogs(a, e.details)
	}
	m.pendingLog = a.Rule.ID
	return m.fetchDetails()
}

// openLogs shows the logs a rule counts (the dividend of a ratio) over the
// last hour at least, so that the evaluated window comes with its context.
func (m *Model) openLogs(a alerts.Alert, d alerts.Details) tea.Cmd {
	if m.opts.OpenLogs == nil {
		return app.Notify(app.LevelWarn, "logs are not available here")
	}
	if len(d.Queries) == 0 {
		return app.Notify(app.LevelWarn, "the rule %q has no log query (template %s)", a.Rule.Name, a.Rule.Template)
	}
	window := max(minLogsWindow, 12*a.Rule.Frequency)
	return m.opts.OpenLogs(d.Queries[0].Text, obs.Last(window))
}

// Mouse: a click selects a rule, a click on the selected rule toggles its
// details; the wheel moves the selection.
func (m *Model) onClick(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	l := m.layout()
	row := mouse.Y - l.listTop
	if mouse.X >= l.listW || row < 0 || row >= l.listRows || m.top+row >= len(m.items) {
		return nil
	}
	if i := m.top + row; i == m.cursor {
		m.detailOpen = !m.detailOpen
	} else {
		m.cursor = i
	}
	m.scrollToCursor()
	return m.fetchDetails()
}

func (m *Model) onWheel(mouse tea.Mouse) {
	switch mouse.Button {
	case tea.MouseWheelDown:
		m.cursor = min(m.cursor+1, max(len(m.items)-1, 0))
	case tea.MouseWheelUp:
		m.cursor = max(m.cursor-1, 0)
	default:
		return
	}
	m.scrollToCursor()
}

func itoa(n int) string { return strconv.Itoa(n) }

// ago renders how long ago t was, coarsely.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s ago"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours()/24)) + "d ago"
	}
}
