package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTail(src obs.Searcher[logs.Record], c *clock, opts engine.TailOptions[logs.Record]) *engine.Tail[logs.Record] {
	opts.Now = c.now
	opts.Key = key
	opts.Timestamp = func(r logs.Record) time.Time { return r.Timestamp }
	return engine.NewTail(src, "", c.now(), opts)
}

func poll(t *testing.T, tail *engine.Tail[logs.Record]) engine.TailUpdate[logs.Record] {
	t.Helper()
	u, err := tail.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTailFollowsNewItems(t *testing.T) {
	c := &clock{now}
	src := fake.NewLogs(21, c.now)
	tail := newTail(src, c, engine.TailOptions[logs.Record]{})

	if u := poll(t, tail); len(u.Items) != 0 {
		t.Fatalf("nothing is newer than the start, got %d", len(u.Items))
	}

	seen := map[uint64]bool{}
	var count int
	for range 6 {
		start := c.t
		c.advance(10 * time.Second)
		u := poll(t, tail)
		for i, r := range u.Items {
			if !r.Timestamp.After(now) || r.Timestamp.After(c.t) {
				t.Fatalf("item %v outside (%v, %v]", r.Timestamp, start, c.t)
			}
			if seen[r.Fingerprint()] {
				t.Fatal("duplicate item")
			}
			seen[r.Fingerprint()] = true
			if i > 0 && r.Timestamp.After(u.Items[i-1].Timestamp) {
				t.Fatal("items must be newest first")
			}
		}
		count += len(u.Items)
	}
	// Everything that happened in the minute must have been delivered once.
	all, err := src.Search(context.Background(), obs.Query{Range: obs.Between(now.Add(time.Nanosecond), c.t), Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if int64(count) != all.Total {
		t.Fatalf("delivered %d, expected %d", count, all.Total)
	}
}

func TestTailPauseResumeAndBufferLimit(t *testing.T) {
	c := &clock{now}
	tail := newTail(fake.NewLogs(4, c.now), c, engine.TailOptions[logs.Record]{BufferSize: 25})
	tail.Pause()
	var lastBuffered int
	for range 5 {
		c.advance(10 * time.Second)
		u := poll(t, tail)
		if len(u.Items) != 0 {
			t.Fatal("paused tail must not deliver items")
		}
		lastBuffered = u.Buffered
	}
	if lastBuffered != 25 || tail.Buffered() != 25 || !tail.Paused() {
		t.Fatalf("buffered %d", lastBuffered)
	}
	items, dropped := tail.Resume()
	if len(items) != 25 || dropped == 0 || tail.Paused() || tail.Buffered() != 0 {
		t.Fatalf("resume: %d items, %d dropped", len(items), dropped)
	}
	for i := 1; i < len(items); i++ {
		if items[i].Timestamp.After(items[i-1].Timestamp) {
			t.Fatal("buffer must be newest first")
		}
	}
	// The newest items are kept, the oldest dropped.
	if c.t.Sub(items[len(items)-1].Timestamp) > 20*time.Second {
		t.Errorf("oldest kept item is too old: %v", items[len(items)-1].Timestamp)
	}
}

func TestTailBackoff(t *testing.T) {
	c := &clock{now}
	stub := &pagesStub{err: errors.New("down")}
	tail := newTail(stub, c, engine.TailOptions[logs.Record]{Interval: 10 * time.Second})
	want := []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 2 * time.Minute, 2 * time.Minute}
	for i, w := range want {
		if _, err := tail.Poll(context.Background()); err == nil {
			t.Fatal("expected an error")
		}
		if got := tail.NextDelay(); got != w || tail.Failures() != i+1 {
			t.Fatalf("after %d failures delay = %v, want %v", i+1, got, w)
		}
	}
	stub.err = nil
	stub.pages = []obs.Page[logs.Record]{{}}
	poll(t, tail)
	if tail.NextDelay() != 10*time.Second || tail.Failures() != 0 {
		t.Fatalf("success must reset the backoff, delay %v", tail.NextDelay())
	}
}

func TestTailLateArrivalsAndSeed(t *testing.T) {
	c := &clock{now}
	shown := rec(now.Unix()-5, "already displayed")
	late := rec(now.Unix()-20, "indexed late") // older than the watermark, within the overlap
	tooOld := rec(now.Unix()-40, "before the tail started")
	stub := &pagesStub{pages: []obs.Page[logs.Record]{
		{Items: []logs.Record{shown}},
		{Items: []logs.Record{shown, late, tooOld}},
		{Items: []logs.Record{shown, late, tooOld}},
	}}
	opts := engine.TailOptions[logs.Record]{Now: c.now, Key: key, Timestamp: func(r logs.Record) time.Time { return r.Timestamp }}
	tail := engine.NewTail[logs.Record](stub, "", now.Add(-30*time.Second), opts)
	tail.Seed([]logs.Record{shown})

	if u := poll(t, tail); len(u.Items) != 0 {
		t.Fatalf("seeded item delivered again: %v", u.Items)
	}
	if u := poll(t, tail); len(u.Items) != 1 || u.Items[0].Body != "indexed late" {
		t.Fatalf("late item not delivered: %v", u.Items)
	}
	if u := poll(t, tail); len(u.Items) != 0 {
		t.Fatalf("late item delivered twice: %v", u.Items)
	}
}

func TestTailSamplingWarning(t *testing.T) {
	c := &clock{now}
	tail := newTail(fake.NewLogs(9, c.now), c, engine.TailOptions[logs.Record]{Limit: 3})
	c.advance(time.Minute)
	u := poll(t, tail)
	if len(u.Items) != 3 || len(u.Warnings) != 1 || !strings.Contains(u.Warnings[0], "sampled") {
		t.Fatalf("items=%d warnings=%v", len(u.Items), u.Warnings)
	}
}

func TestTailPauseDuringPoll(t *testing.T) {
	c := &clock{now}
	src := fake.NewLogs(2, c.now)
	src.Latency = 20 * time.Millisecond
	opts := engine.TailOptions[logs.Record]{Key: key, Timestamp: func(r logs.Record) time.Time { return r.Timestamp }, Now: c.now}
	tail := engine.NewTail[logs.Record](src, "", now.Add(-time.Minute), opts)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = tail.Poll(context.Background())
	}()
	tail.Pause()
	_ = tail.Buffered()
	<-done
	if items, _ := tail.Resume(); len(items) == 0 {
		t.Error("items polled while pausing must be buffered")
	}
}
