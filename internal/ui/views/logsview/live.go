package logsview

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// liveRange is used when live mode is enabled on an absolute range.
var liveRange = obs.Last(15 * time.Minute)

func (m *Model) toggleLive() tea.Cmd {
	if m.live {
		m.live = false
		m.stopTail()
		return app.Notify(app.LevelInfo, "live mode off")
	}
	m.live = true
	if !m.rng.IsRelative() {
		// Following new logs only makes sense on a range that ends now.
		m.rng = liveRange
		return tea.Batch(m.search(), app.Notify(app.LevelInfo, "live mode on, time range set to %s", liveRange))
	}
	if m.loading && len(m.records) == 0 {
		return nil // the tail starts with the first page (see onPage)
	}
	return m.startTail()
}

// startTail follows logs newer than the end of the current search window.
func (m *Model) startTail() tea.Cmd {
	since := m.opts.Now()
	if m.pager != nil {
		since = m.pager.Query().Range.To
	}
	m.tail = engine.NewTail(m.opts.Source, m.query, since, engine.TailOptions[logs.Record]{
		Interval:  m.opts.LiveInterval,
		Now:       m.opts.Now,
		Key:       logs.Record.Fingerprint,
		Timestamp: func(r logs.Record) time.Time { return r.Timestamp },
	})
	m.tail.Seed(m.records)
	m.liveGen++
	m.liveErrs = 0
	return m.poll()
}

func (m *Model) stopTail() {
	m.tail = nil
	m.liveGen++ // invalidates scheduled polls and in-flight results
	m.liveErrs = 0
}

func (m *Model) poll() tea.Cmd {
	gen, tail, viewID := m.liveGen, m.tail, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		u, err := tail.Poll(ctx)
		return tailMsg{owner: owner{viewID}, gen: gen, update: u, err: err, delay: tail.NextDelay()}
	}
}

func (m *Model) onTail(msg tailMsg) tea.Cmd {
	if msg.gen != m.liveGen || m.tail == nil {
		return nil
	}
	next := m.opts.After(msg.delay, tailTickMsg{owner: owner{m.id}, gen: m.liveGen})
	if msg.err != nil {
		m.liveErrs++
		return tea.Batch(next, app.Notify(app.LevelWarn, "live: %v (retrying in %s)", msg.err, msg.delay.Round(time.Second)))
	}
	m.liveErrs = 0
	m.prepend(msg.update.Items)
	cmds := []tea.Cmd{next, m.fetchHistogram(), m.fetchBreakdown()}
	for _, w := range msg.update.Warnings {
		cmds = append(cmds, app.Notify(app.LevelWarn, "%s", w))
	}
	return tea.Batch(cmds...)
}

func (m *Model) togglePause() tea.Cmd {
	if m.tail == nil {
		return nil
	}
	if !m.tail.Paused() {
		m.tail.Pause()
		return nil
	}
	items, dropped := m.tail.Resume()
	m.prepend(items)
	if dropped > 0 {
		return app.Notify(app.LevelWarn, "%d logs received while paused were dropped (buffer full)", dropped)
	}
	return nil
}

// prepend adds newer records on top. The selection stays on the same record
// unless it was on the newest one, in which case it follows the stream.
func (m *Model) prepend(items []logs.Record) {
	if len(items) == 0 {
		return
	}
	m.records = append(append(make([]logs.Record, 0, len(items)+len(m.records)), items...), m.records...)
	m.liveNew += len(items)
	if m.cursor > 0 || m.top > 0 {
		m.cursor += len(items)
		m.top += len(items)
	}
	m.setDetail()
}

func pausedLabel(buffered int) string {
	if buffered == 0 {
		return "PAUSED"
	}
	return fmt.Sprintf("PAUSED +%d", buffered)
}
