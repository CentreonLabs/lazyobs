package logsview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
)

const (
	// contextWindow is the time shown on each side of a log opened in context.
	contextWindow = 30 * time.Second
	// maxAnchorPages bounds the pages loaded to find the anchor log.
	maxAnchorPages = 20
)

// openContext opens a tab with the logs of the same service around the
// selected one, which is selected and marked there.
func (m *Model) openContext() tea.Cmd {
	if len(m.records) == 0 {
		return nil
	}
	r := m.records[m.cursor]
	query := ""
	if r.Service != "" {
		query = "service_name:" + quoteValue(r.Service)
	}
	ts := r.Timestamp
	child := Restore(m.opts, State{
		Query:   query,
		Range:   obs.Between(ts.Add(-contextWindow).Truncate(time.Second), ts.Add(contextWindow+time.Second).Truncate(time.Second)),
		UTC:     m.utc,
		Compact: m.compact,
	})
	child.anchor = r.Fingerprint()
	child.showDetail = true
	from := m
	return tea.Batch(
		func() tea.Msg { return tabset.OpenMsg{From: from, Child: child} },
		app.Notify(app.LevelInfo, "logs around %s · - to widen · < to go back", ts.In(m.location()).Format("15:04:05.000")),
	)
}

// findAnchor selects the anchor log once loaded, or keeps loading pages
// until it is found.
func (m *Model) findAnchor() tea.Cmd {
	if m.anchor == 0 || m.anchorFound {
		return nil
	}
	for i, r := range m.records {
		if r.Fingerprint() == m.anchor {
			m.anchorFound = true
			m.cursor = i
			m.top = max(i-m.listRows()/2, 0)
			m.scrollToCursor()
			m.layoutDetail()
			m.setDetail()
			return nil
		}
	}
	if m.loading || m.pager == nil || m.pager.Done() || m.anchorPages >= maxAnchorPages {
		return nil
	}
	m.anchorPages++
	m.loading = true
	return m.fetchPage()
}

// isAnchor reports whether r is the log this view was opened around.
func (m *Model) isAnchor(fingerprint uint64) bool { return m.anchor != 0 && fingerprint == m.anchor }
