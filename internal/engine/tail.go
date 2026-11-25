package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// Tail defaults.
const (
	DefaultTailInterval = 10 * time.Second
	DefaultTailOverlap  = time.Minute
	DefaultTailLimit    = 1000
	DefaultTailBuffer   = 10_000
	maxTailBackoff      = 2 * time.Minute
)

// TailOptions configures a Tail. Zero values select the defaults.
type TailOptions[T any] struct {
	// Interval between two polls.
	Interval time.Duration
	// Overlap re-queries this much time before the newest item seen, to catch
	// items indexed late. Duplicates are removed.
	Overlap time.Duration
	// Limit is the maximum number of items fetched per poll.
	Limit int
	// BufferSize bounds the items kept while paused; the oldest are dropped beyond it.
	BufferSize int
	// Now is the clock (time.Now when nil).
	Now func() time.Time
	// Key identifies an item for deduplication. Required.
	Key func(T) uint64
	// Timestamp returns the time of an item. Required.
	Timestamp func(T) time.Time
}

// TailUpdate is the outcome of a poll.
type TailUpdate[T any] struct {
	// Items are the new items to display, newest first. Empty while paused.
	Items []T
	// Buffered is the number of items waiting while paused.
	Buffered int
	// Warnings are non-fatal issues, such as a sampled stream.
	Warnings []string
}

// Tail follows new items of a search by polling, like tail -f.
// It is not safe for concurrent use: call Poll once at a time and schedule
// the next call after NextDelay.
type Tail[T any] struct {
	src  obs.Searcher[T]
	text string
	opts TailOptions[T]

	since     time.Time            // items older than this are never delivered
	watermark time.Time            // newest item time seen
	seen      map[uint64]time.Time // keys of items inside the overlap window
	failures  int

	paused  bool
	buffer  []T // newest first
	dropped int
}

// NewTail follows the items matching text from since on. Items older than
// since are never delivered, even when the overlap window covers them.
func NewTail[T any](src obs.Searcher[T], text string, since time.Time, opts TailOptions[T]) *Tail[T] {
	if opts.Interval <= 0 {
		opts.Interval = DefaultTailInterval
	}
	if opts.Overlap <= 0 {
		opts.Overlap = DefaultTailOverlap
	}
	if opts.Limit <= 0 {
		opts.Limit = DefaultTailLimit
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = DefaultTailBuffer
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Tail[T]{src: src, text: text, opts: opts, since: since, watermark: since, seen: map[uint64]time.Time{}}
}

// Seed marks items already displayed (e.g. the latest search results) so the
// first polls do not return them again.
func (t *Tail[T]) Seed(items []T) {
	for _, it := range items {
		ts := t.opts.Timestamp(it)
		t.seen[t.opts.Key(it)] = ts
		if ts.After(t.watermark) {
			t.watermark = ts
		}
	}
}

// Poll fetches the items that arrived since the previous poll.
func (t *Tail[T]) Poll(ctx context.Context) (TailUpdate[T], error) {
	now := t.opts.Now()
	from := t.watermark.Add(-t.opts.Overlap)
	page, err := t.src.Search(ctx, obs.Query{
		Text:  t.text,
		Range: obs.Between(from, now.Add(time.Second)), // include the current second
		Limit: t.opts.Limit,
	})
	if err != nil {
		t.failures++
		return TailUpdate[T]{Buffered: len(t.buffer)}, err
	}
	t.failures = 0

	update := TailUpdate[T]{Warnings: page.Warnings}
	fresh := make([]T, 0, len(page.Items))
	for _, it := range page.Items {
		k := t.opts.Key(it)
		if _, dup := t.seen[k]; dup {
			continue
		}
		ts := t.opts.Timestamp(it)
		if ts.Before(t.since) {
			continue
		}
		t.seen[k] = ts
		if ts.After(t.watermark) {
			t.watermark = ts
		}
		fresh = append(fresh, it)
	}
	if missed := page.Total - int64(len(page.Items)); missed > 0 && page.Next != nil {
		update.Warnings = append(update.Warnings, fmt.Sprintf(
			"live stream sampled: %d more matches than the %d fetched in this poll; narrow the query", missed, t.opts.Limit))
	}
	t.prune()

	if t.paused {
		t.buffer = append(fresh, t.buffer...)
		if over := len(t.buffer) - t.opts.BufferSize; over > 0 {
			t.buffer = t.buffer[:t.opts.BufferSize]
			t.dropped += over
		}
	} else {
		update.Items = fresh
	}
	update.Buffered = len(t.buffer)
	return update, nil
}

// prune forgets keys older than the overlap window: they cannot be fetched again.
func (t *Tail[T]) prune() {
	limit := t.watermark.Add(-t.opts.Overlap - time.Second)
	for k, ts := range t.seen {
		if ts.Before(limit) {
			delete(t.seen, k)
		}
	}
}

// NextDelay is the wait before the next poll: the interval, doubled after
// each consecutive failure up to two minutes.
func (t *Tail[T]) NextDelay() time.Duration {
	d := t.opts.Interval
	for range t.failures {
		d *= 2
		if d >= maxTailBackoff {
			return maxTailBackoff
		}
	}
	return d
}

// Failures is the number of consecutive failed polls.
func (t *Tail[T]) Failures() int { return t.failures }

// Pause keeps polling but buffers new items instead of returning them.
func (t *Tail[T]) Pause() { t.paused = true }

// Paused reports whether the tail is paused.
func (t *Tail[T]) Paused() bool { return t.paused }

// Resume returns the buffered items (newest first) and how many were dropped
// because the buffer was full, then clears the buffer.
func (t *Tail[T]) Resume() (items []T, dropped int) {
	items, dropped = t.buffer, t.dropped
	t.paused, t.buffer, t.dropped = false, nil, 0
	return items, dropped
}

// Buffered is the number of items waiting while paused.
func (t *Tail[T]) Buffered() int { return len(t.buffer) }
