package logsview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

// maxJumpPages bounds the pages loaded to reach a clicked bucket; farther
// buckets are reached by restarting the list at the bucket instead.
const maxJumpPages = 5

// jump is a pending move of the list to the logs older than a time, while
// the pages that contain them are loaded.
type jump struct {
	before time.Time // select the newest log before this time
	latest bool      // the last bucket of the timeline
	label  string
	pages  int
}

// jumpToColumn selects the newest log of the bucket under a plot column.
func (m *Model) jumpToColumn(col int) tea.Cmd {
	if len(m.hist) == 0 {
		return nil
	}
	colSpan := m.histTo.Sub(m.histFrom) / time.Duration(max(m.hits.plotW, 1))
	at := m.hits.timeAt(col, m.histFrom, m.histTo).Add(colSpan / 2)
	start, end, total, ok := chart.BucketAt(m.hist, m.histFrom, m.histTo, at)
	if !ok {
		start, end = at, at.Add(colSpan)
	}
	loc := m.location()
	j := &jump{
		before: end,
		// No room for another bucket after it: the latest one.
		latest: end.Add(chart.BucketSize(m.hist, m.histFrom, m.histTo)).After(m.histTo),
		label:  start.In(loc).Format("15:04:05") + " → " + end.In(loc).Format("15:04:05") + " · " + humanCount(int64(total)) + " logs",
	}
	// Buckets newer than the top of a restarted list are not in it at all;
	// far older ones would take too many pages: restart the list there.
	if !m.listEnd.IsZero() && end.After(m.listEnd) {
		return m.seek(j)
	}
	if !m.loaded(end) && m.newerThan(end)-int64(len(m.records)) > int64(maxJumpPages*m.opts.PageSize) {
		return m.seek(j)
	}
	m.jump = j
	return m.continueJump()
}

// loaded reports whether a log older than t is already in the list.
func (m *Model) loaded(t time.Time) bool {
	return len(m.records) > 0 && m.records[len(m.records)-1].Timestamp.Before(t)
}

// newerThan estimates from the timeline how many logs are at or after t.
func (m *Model) newerThan(t time.Time) int64 {
	var n float64
	for _, s := range m.hist {
		for _, p := range s.Points {
			if !p.Time.Before(t) {
				n += p.Value
			}
		}
	}
	return int64(n)
}

// continueJump selects the target log if loaded, or loads the next page.
func (m *Model) continueJump() tea.Cmd {
	j := m.jump
	if j == nil {
		return nil
	}
	for i, r := range m.records {
		if r.Timestamp.Before(j.before) {
			m.jump = nil
			m.selectAndCenter(i)
			return app.Notify(app.LevelInfo, "bucket %s", j.label)
		}
	}
	switch {
	case m.loading:
		return nil // the page on its way will call back
	case m.pager == nil || m.pager.Done() || m.err != nil:
		m.jump = nil
		if len(m.records) > 0 {
			m.selectAndCenter(len(m.records) - 1)
		}
		return app.Notify(app.LevelInfo, "no log in or before bucket %s", j.label)
	case j.pages >= maxJumpPages:
		m.jump = nil
		return m.seek(j)
	}
	j.pages++
	m.loading = true
	return m.fetchPage()
}

// seek restarts the list at the logs older than j.before, keeping the time
// range and the timeline. r (refresh) goes back to the latest logs.
func (m *Model) seek(j *jump) tea.Cmd {
	m.searchID++
	if m.live {
		m.live = false
	}
	m.stopTail()
	from, to := m.rng.Resolve(m.opts.Now())
	end := j.before
	m.listEnd = end
	if j.latest || !end.Before(to) { // back to the normal list
		end, m.listEnd = to, time.Time{}
	}
	m.pager = engine.NewPager[logs.Record](m.opts.Source,
		obs.Query{Text: m.query, Range: obs.Between(from, end), Limit: m.opts.PageSize},
		m.opts.Now(), logs.Record.Fingerprint)
	m.records, m.err, m.cursor, m.top = nil, nil, 0, 0
	m.loading = true
	m.jump = &jump{before: j.before, latest: j.latest, label: j.label, pages: maxJumpPages} // select once the first page is in
	m.setDetail()
	return m.fetchPage()
}

// selectAndCenter selects record i and scrolls it to the middle of the list.
func (m *Model) selectAndCenter(i int) {
	m.cursor = i
	m.focus = focusList
	m.top = max(i-m.listRows()/2, 0)
	m.scrollToCursor()
	m.setDetail()
}
