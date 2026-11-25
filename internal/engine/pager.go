// Package engine holds the UI-independent logic driving searches: paging
// through results and following new data live. It is generic over the signal
// and depends only on the obs interfaces, so it is tested against fake sources.
package engine

import (
	"context"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// maxEmptyPages bounds how many consecutive pages made only of duplicates are
// skipped in a single Next call before giving back control.
const maxEmptyPages = 5

// Pager walks through the pages of a search, newest first, and removes the
// duplicates a Searcher may return between pages (e.g. when sliding its time
// window). It is not safe for concurrent use: issue one Next at a time.
type Pager[T any] struct {
	src   obs.Searcher[T]
	key   func(T) uint64
	query obs.Query

	next     *obs.Cursor
	started  bool
	done     bool
	total    int64
	seen     map[uint64]struct{}
	returned int
}

// NewPager prepares a search. A relative range is pinned at now so that every
// page belongs to the same window. key identifies an item for deduplication.
func NewPager[T any](src obs.Searcher[T], q obs.Query, now time.Time, key func(T) uint64) *Pager[T] {
	q.Range = q.Range.Pin(now)
	q.Cursor = nil
	return &Pager[T]{src: src, key: key, query: q, seen: map[uint64]struct{}{}}
}

// Query returns the query with its pinned range.
func (p *Pager[T]) Query() obs.Query { return p.query }

// Done reports whether every page has been fetched.
func (p *Pager[T]) Done() bool { return p.done }

// Total is the number of matches reported by the first page.
func (p *Pager[T]) Total() int64 { return p.total }

// Returned is the number of distinct items returned so far.
func (p *Pager[T]) Returned() int { return p.returned }

// Next fetches the next page and returns its new items. It returns no items
// once Done is true.
func (p *Pager[T]) Next(ctx context.Context) (items []T, warnings []string, err error) {
	for range maxEmptyPages {
		if p.done {
			return nil, warnings, nil
		}
		q := p.query
		q.Cursor = p.next
		page, err := p.src.Search(ctx, q)
		if err != nil {
			return nil, warnings, err
		}
		if !p.started {
			p.started, p.total = true, page.Total
		}
		warnings = append(warnings, page.Warnings...)
		p.next = page.Next
		p.done = page.Next == nil

		items = make([]T, 0, len(page.Items))
		for _, it := range page.Items {
			k := p.key(it)
			if _, dup := p.seen[k]; dup {
				continue
			}
			p.seen[k] = struct{}{}
			items = append(items, it)
		}
		p.returned += len(items)
		if len(items) > 0 || p.done {
			return items, warnings, nil
		}
	}
	return nil, warnings, nil
}
