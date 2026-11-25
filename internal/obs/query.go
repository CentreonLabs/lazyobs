// Package obs holds the signal-agnostic domain shared by every observability
// signal (logs, traces, metrics): queries, time ranges, pages and the source
// interfaces the application depends on.
//
// It must not import any other package of this project.
package obs

import (
	"fmt"
	"time"
)

// TimeRange is either relative to "now" (Last > 0) or absolute ([From, To)).
type TimeRange struct {
	Last time.Duration
	From time.Time
	To   time.Time
}

// Last returns a range covering the duration d up to now.
func Last(d time.Duration) TimeRange { return TimeRange{Last: d} }

// Between returns the absolute range [from, to).
func Between(from, to time.Time) TimeRange { return TimeRange{From: from, To: to} }

// IsRelative reports whether the range moves with the clock.
func (r TimeRange) IsRelative() bool { return r.Last > 0 }

// Resolve returns the absolute bounds of the range at instant now.
func (r TimeRange) Resolve(now time.Time) (from, to time.Time) {
	if r.IsRelative() {
		return now.Add(-r.Last), now
	}
	return r.From, r.To
}

// Pin returns the absolute equivalent of the range at instant now.
// Paginating over a pinned range keeps pages consistent while time passes.
func (r TimeRange) Pin(now time.Time) TimeRange {
	from, to := r.Resolve(now)
	return Between(from, to)
}

func (r TimeRange) String() string {
	if r.IsRelative() {
		return "last " + FormatDuration(r.Last)
	}
	const layout = "2006-01-02 15:04:05"
	return fmt.Sprintf("%s → %s", r.From.Format(layout), r.To.Format(layout))
}

// FormatDuration renders d compactly with its largest whole unit (90m → 90m, 2h → 2h, 48h → 2d).
func FormatDuration(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d >= day && d%day == 0:
		return fmt.Sprintf("%dd", d/day)
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// Cursor locates the next page of a search. It is produced by a Searcher and
// must be passed back unchanged; callers should not build or inspect it.
type Cursor struct {
	Offset int
	// End, when set, overrides the end of the query range (exclusive).
	End time.Time
}

// Query describes a search for any signal.
type Query struct {
	Text   string // backend query language (Quickwit syntax for the Centreon platform)
	Range  TimeRange
	Limit  int     // maximum number of items per page
	Cursor *Cursor // nil for the first page
}

// Page is one page of search results, newest first.
type Page[T any] struct {
	Items []T
	Total int64   // number of items matching the query over the queried window
	Next  *Cursor // nil when there is nothing more to fetch
}
