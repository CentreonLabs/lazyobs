package logsview

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

const wheelStep = 3

// legendHint tells how to use the clickable legend.
const legendHint = "click: show/hide · right-click: only this one"

// hitAreas are the clickable parts of the last rendered frame.
type hitAreas struct {
	legend       chart.Legend // X relative to legendX
	legendX      int
	legendY      int // body row of the legend, -1 when not shown
	listTop      int // body row of the first record
	listRows     int
	listRight    int // first column after the list panel
	detailActive bool
}

func (m *Model) onClick(mouse tea.Mouse) tea.Cmd {
	h := m.hits
	if mouse.Y == h.legendY {
		if name, ok := h.legend.At(mouse.X - h.legendX); ok {
			solo := mouse.Button == tea.MouseRight || mouse.Mod&(tea.ModShift|tea.ModAlt|tea.ModCtrl) != 0
			if solo {
				return m.soloSeries(name)
			}
			return m.toggleSeries(name)
		}
		return nil
	}
	if mouse.Button != tea.MouseLeft {
		return nil
	}
	if h.detailActive && mouse.X >= h.listRight {
		m.focus = focusDetail
		return nil
	}
	if row := mouse.Y - h.listTop; row >= 0 && row < h.listRows && mouse.X < h.listRight {
		if i := m.top + row; i < len(m.records) {
			m.focus = focusList
			if i != m.cursor {
				m.cursor = i
				m.setDetail()
			}
			return m.maybeLoadMore()
		}
	}
	return nil
}

func (m *Model) onWheel(mouse tea.Mouse) tea.Cmd {
	delta := 0
	switch mouse.Button {
	case tea.MouseWheelDown:
		delta = wheelStep
	case tea.MouseWheelUp:
		delta = -wheelStep
	default:
		return nil
	}
	if m.hits.detailActive && mouse.X >= m.hits.listRight {
		if delta > 0 {
			m.detail.ScrollDown(delta)
		} else {
			m.detail.ScrollUp(-delta)
		}
		return nil
	}
	if len(m.records) == 0 {
		return nil
	}
	prev := m.cursor
	m.cursor = clampIndex(m.cursor+delta, len(m.records)-1)
	m.scrollToCursor()
	if m.cursor != prev {
		m.setDetail()
	}
	return m.maybeLoadMore()
}

// seriesNames lists the series of the timeline.
func (m *Model) seriesNames() []string {
	names := make([]string, 0, len(m.hist))
	for _, s := range m.hist {
		names = append(names, s.Name)
	}
	return names
}

func (m *Model) toggleSeries(name string) tea.Cmd {
	if m.hidden[name] {
		delete(m.hidden, name)
		return nil
	}
	m.hidden[name] = true
	return app.Notify(app.LevelInfo, "%s hidden from the timeline · click it again to show it", name)
}

// soloSeries shows only name, or every series when name is already the only one shown.
func (m *Model) soloSeries(name string) tea.Cmd {
	names := m.seriesNames()
	alone := !m.hidden[name]
	for _, n := range names {
		if n != name && !m.hidden[n] {
			alone = false
		}
	}
	m.hidden = map[string]bool{}
	if alone {
		return app.Notify(app.LevelInfo, "all levels shown")
	}
	for _, n := range names {
		if n != name {
			m.hidden[n] = true
		}
	}
	return app.Notify(app.LevelInfo, "only %s shown · right-click it again to show all levels", strings.ToUpper(name))
}
