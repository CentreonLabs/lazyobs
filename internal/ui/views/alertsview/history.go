package alertsview

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

// History ranges, cycled with t.
var historyRanges = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

const (
	// DefaultHistoryRange is the period of the status history.
	DefaultHistoryRange = 24 * time.Hour
	// historyPoints is the resolution asked for, enough for the widest strip.
	historyPoints = 120
	// historyRefresh is how often the histories are read again: one request
	// per rule, so less often than the statuses.
	historyRefresh = 5 * time.Minute
	// historyParallel bounds the concurrent history requests.
	historyParallel = 4
	// maxTransitions is the number of status changes listed in the details.
	maxTransitions = 5
)

type historyMsg struct {
	owner
	id        int
	histories map[string]alerts.History
	err       error
}

// rangeLabel names a history range: 1h, 6h, 24h, 7d.
func rangeLabel(d time.Duration) string {
	if d%(24*time.Hour) == 0 && d > 24*time.Hour {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return fmt.Sprintf("%dh", d/time.Hour)
}

// historyStep is the resolution of a history range.
func historyStep(rng time.Duration) time.Duration {
	return max(rng/historyPoints, time.Minute).Truncate(time.Minute)
}

// historyDue reports whether the histories must be read again.
func (m *Model) historyDue() bool {
	return m.histAt.IsZero() || m.opts.Now().Sub(m.histAt) >= historyRefresh || m.histStale
}

// fetchHistory reads the history of every evaluated rule over the range.
func (m *Model) fetchHistory() tea.Cmd {
	var todo []alerts.Alert
	for _, a := range m.all {
		if a.Event != nil {
			todo = append(todo, a)
		}
	}
	m.histReq++
	m.histAt, m.histStale, m.histLoading = m.opts.Now(), false, true
	id, src, view := m.histReq, m.opts.Source, m.id
	to := m.opts.Now()
	from, step := to.Add(-m.histRange), historyStep(m.histRange)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		var (
			mu       sync.Mutex
			wg       sync.WaitGroup
			firstErr error
			out      = make(map[string]alerts.History, len(todo))
			slots    = make(chan struct{}, historyParallel)
		)
		for _, a := range todo {
			wg.Add(1)
			slots <- struct{}{}
			go func() {
				defer func() { <-slots; wg.Done() }()
				h, err := src.History(ctx, a, from, to, step)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", a.Rule.Name, err)
					}
					return
				}
				out[a.Rule.ID] = h
			}()
		}
		wg.Wait()
		return historyMsg{owner: owner{view}, id: id, histories: out, err: firstErr}
	}
}

func (m *Model) onHistory(msg historyMsg) tea.Cmd {
	if msg.id != m.histReq {
		return nil
	}
	m.histLoading = false
	for id, h := range msg.histories {
		m.history[id] = h
	}
	if msg.err != nil {
		m.histStale = true // try again on the next refresh
		if !m.histWarned {
			m.histWarned = true
			return app.Notify(app.LevelWarn, "could not read the status history of %v", msg.err)
		}
	}
	return nil
}

// cycleHistoryRange moves to the next history range.
func (m *Model) cycleHistoryRange() tea.Cmd {
	i := 0
	for k, r := range historyRanges {
		if r == m.histRange {
			i = (k + 1) % len(historyRanges)
		}
	}
	m.histRange = historyRanges[i]
	m.history = map[string]alerts.History{}
	return tea.Batch(m.fetchHistory(), app.Notify(app.LevelInfo, "status history over the last %s", rangeLabel(m.histRange)))
}

// strip draws the history of a rule on width cells: the higher and redder,
// the worse.
func (m *Model) strip(a alerts.Alert, width int) string {
	h, ok := m.history[a.Rule.ID]
	if !ok || width <= 0 {
		if a.Event != nil && m.histLoading {
			return m.th.Dim.Render(strings.Repeat("·", max(width, 0)))
		}
		return strings.Repeat(" ", max(width, 0))
	}
	hold := 2 * max(a.Rule.Frequency, h.Step)
	var b strings.Builder
	for _, s := range h.Cells(width, hold) {
		b.WriteString(m.cell(s))
	}
	return b.String()
}

func (m *Model) cell(s alerts.Status) string {
	switch s {
	case alerts.NoData:
		return lipgloss.NewStyle().Foreground(m.th.Subtle).Render("·")
	case alerts.StatusOK:
		return m.statusStyle(s).Render("▁")
	case alerts.StatusUnknown:
		return m.th.Dim.Render("▂")
	case alerts.StatusWarning:
		return m.statusStyle(s).Render("▄")
	case alerts.StatusError:
		return m.statusStyle(s).Render("▆")
	default:
		return m.statusStyle(s).Render("█")
	}
}

// historyLines are the history block of the details panel.
func (m *Model) historyLines(a alerts.Alert, width int) []string {
	t := m.th
	label := fmt.Sprintf("%-*s", labelW, "last "+rangeLabel(m.histRange))
	h, ok := m.history[a.Rule.ID]
	switch {
	case a.Event == nil:
		return nil
	case !ok && m.histLoading:
		return []string{t.Key.Render(label) + t.Dim.Render("loading…")}
	case !ok:
		return []string{t.Key.Render(label) + t.Dim.Render("no history")}
	}
	w := max(width-labelW, 10)
	loc := m.location()
	layout := "15:04"
	if m.histRange >= 24*time.Hour {
		layout = "01-02 15:04"
	}
	from, to := h.From.In(loc).Format(layout), "now"
	axis := from + strings.Repeat(" ", max(w-len(from)-len(to), 1)) + to
	lines := []string{
		t.Key.Render(label) + m.strip(a, w),
		strings.Repeat(" ", labelW) + t.Dim.Render(axis),
	}
	tr := h.Transitions()
	if len(tr) == 0 {
		word := "no evaluation"
		if len(h.Points) > 0 {
			word = "always " + h.Points[0].Status.String()
		}
		return append(lines, t.Key.Render(fmt.Sprintf("%-*s", labelW, "changes"))+t.Dim.Render("none, "+word))
	}
	for i := 0; i < min(len(tr), maxTransitions); i++ {
		c := tr[len(tr)-1-i]
		name := ""
		if i == 0 {
			name = "changes"
		}
		lines = append(lines, t.Key.Render(fmt.Sprintf("%-*s", labelW, name))+
			t.Dim.Render(c.At.In(loc).Format("01-02 15:04")+"  ")+
			m.statusStyle(c.From).Render(c.From.String())+t.Dim.Render(" → ")+m.statusStyle(c.To).Render(c.To.String()))
	}
	if len(tr) > maxTransitions {
		lines = append(lines, strings.Repeat(" ", labelW)+t.Dim.Render(fmt.Sprintf("… %d earlier", len(tr)-maxTransitions)))
	}
	return lines
}
