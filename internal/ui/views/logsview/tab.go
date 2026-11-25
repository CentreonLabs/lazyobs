package logsview

import (
	"strings"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
)

var _ tabset.Child = (*Model)(nil)

// Title implements tabset.Child: the query and the time range, with a dot
// when live mode runs.
func (m *Model) Title() string {
	q := strings.TrimSpace(m.query)
	if q == "" || q == "*" {
		q = "all logs"
	}
	title := q + " · " + shortRange(m.rng, m.location(), m.opts.Now())
	if m.tail != nil {
		title = "● " + title
	}
	return title
}

// Clone implements tabset.Child: a new view on the same source, starting
// from this one's query, time range and display preferences.
func (m *Model) Clone() tabset.Child {
	opts := m.opts
	opts.Query, opts.Range = m.query, m.rng
	c := New(opts)
	c.utc, c.compact = m.utc, m.compact
	for k, v := range m.hidden {
		c.hidden[k] = v
	}
	c.lastPrompt = m.lastPrompt
	c.bd.field = m.bd.field
	return c
}

// shortRange renders a range compactly for a tab title: "15m", "14:00→14:30"
// or "11-03 13:30→11-03 15:00".
func shortRange(r obs.TimeRange, loc *time.Location, now time.Time) string {
	if r.IsRelative() {
		return obs.FormatDuration(r.Last)
	}
	from, to := r.From.In(loc), r.To.In(loc)
	if from.YearDay() == to.YearDay() && from.Year() == to.Year() && to.Sub(from) < 24*time.Hour {
		if now = now.In(loc); now.YearDay() == from.YearDay() && now.Year() == from.Year() {
			return from.Format("15:04") + "→" + to.Format("15:04")
		}
		return from.Format("01-02 15:04") + "→" + to.Format("15:04")
	}
	return from.Format("01-02 15:04") + "→" + to.Format("01-02 15:04")
}
