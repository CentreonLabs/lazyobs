package tracesview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
)

// openTrace is the trace shown as a waterfall.
type openTrace struct {
	id       string
	detail   traces.Detail
	nodes    []traces.Node
	loading  bool
	err      error
	selected int
	top      int
	scroll   int // first line of the span detail panel
}

// openTrace loads a trace and shows its waterfall.
func (m *Model) openTrace(id string) tea.Cmd {
	m.trace = &openTrace{id: id, loading: true}
	return m.fetchTrace(id)
}

func (m *Model) fetchTrace(id string) tea.Cmd {
	f, view := m.opts.Fetcher, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		d, err := f.Trace(ctx, id)
		return traceMsg{owner: owner{view}, traceID: id, detail: d, err: err}
	}
}

func (m *Model) onTrace(msg traceMsg) tea.Cmd {
	t := m.trace
	if t == nil || t.id != msg.traceID {
		return nil
	}
	t.loading, t.err = false, msg.err
	if msg.err != nil {
		return app.Notify(app.LevelError, "could not load trace %s: %v", msg.traceID, msg.err)
	}
	t.detail, t.nodes = msg.detail, msg.detail.Tree()
	return nil
}

func (m *Model) waterfallKey(msg tea.KeyPressMsg) tea.Cmd {
	t, k := m.trace, m.keys
	n := len(t.nodes)
	switch {
	case key.Matches(msg, k.Close):
		m.trace = nil
	case key.Matches(msg, k.Down):
		t.selected, t.scroll = clampIndex(t.selected+1, n-1), 0
	case key.Matches(msg, k.Up):
		t.selected, t.scroll = clampIndex(t.selected-1, n-1), 0
	case key.Matches(msg, k.Top):
		t.selected, t.scroll = 0, 0
	case key.Matches(msg, k.Bottom):
		t.selected, t.scroll = max(n-1, 0), 0
	case key.Matches(msg, k.HalfDown), key.Matches(msg, k.PageDown):
		t.scroll += 5 // the span detail panel
	case key.Matches(msg, k.HalfUp), key.Matches(msg, k.PageUp):
		t.scroll = max(t.scroll-5, 0)
	case key.Matches(msg, k.Logs):
		start, end := t.detail.Bounds()
		if n == 0 {
			r := m.Selected().Root
			start, end = r.Start, r.End()
		}
		return m.openLogs(t.id, start, end)
	case key.Matches(msg, k.Copy):
		if n == 0 {
			return nil
		}
		var pretty bytes.Buffer
		s := t.nodes[t.selected].Span
		if json.Indent(&pretty, s.Raw, "", "  ") != nil {
			pretty.Reset()
			pretty.WriteString(s.SpanID)
		}
		return tea.Batch(tea.SetClipboard(pretty.String()), app.Notify(app.LevelInfo, "span copied to the clipboard"))
	case key.Matches(msg, k.UTC):
		m.utc = !m.utc
	}
	return nil
}

// renderWaterfall draws the trace: a summary line, the span tree with a
// bar per span on a common time axis, and the selected span's details.
func (m *Model) renderWaterfall(width, height int) string {
	t, th := m.trace, m.th
	switch {
	case t.loading:
		return th.Dim.Render("Loading trace " + t.id + "…")
	case t.err != nil:
		return th.ErrorText.Render(draw.Wrap("Could not load the trace: "+t.err.Error(), width)) + "\n\n" + th.Dim.Render("esc: back to the list")
	case len(t.nodes) == 0:
		return th.Dim.Render("This trace has no span yet.")
	}
	d := t.detail
	start, end := d.Bounds()
	total := max(end.Sub(start), time.Microsecond)

	summary := th.Key.Render(d.Name) + th.Dim.Render(fmt.Sprintf("  %s · %d spans · %d services", formatDuration(total), len(d.Spans), len(d.Services)))
	if errs := d.Errors(); errs > 0 {
		summary += th.ErrorText.Render(fmt.Sprintf(" · %d errors", errs))
	}
	summary += th.Dim.Render("  " + start.In(m.location()).Format("2006-01-02 15:04:05.000") + "  " + d.TraceID)

	// The span tree takes what it needs (up to 3/5 of the height), the
	// selected span's details the rest.
	treeH := min(len(t.nodes)+3, max(height*3/5, 6))
	detailH := max(height-treeH-1, 4)
	tree := m.panel("Spans", m.renderSpans(start, total, width-2, treeH-2), width-2, treeH-2, true)
	detail := m.panel("Span", m.renderSpan(t.nodes[t.selected].Span, start, width-2, detailH-2), width-2, detailH-2, false)
	m.hits.rowsTop = 1 + 1 + 1 // summary line, panel border, axis line
	return lipgloss.JoinVertical(lipgloss.Left, draw.Fit(summary, width), tree, detail)
}

func (m *Model) renderSpans(start time.Time, total time.Duration, width, height int) string {
	t, th := m.trace, m.th
	labelW := min(max(width*2/5, 20), 60)
	const durW = 9
	barW := max(width-labelW-durW-2, 10)
	rows := max(height-1, 1) // minus the axis

	if t.selected < t.top {
		t.top = t.selected
	}
	if t.selected >= t.top+rows {
		t.top = t.selected - rows + 1
	}
	t.top = clampIndex(t.top, max(len(t.nodes)-rows, 0))

	axis := []rune(strings.Repeat(" ", barW))
	put := func(at int, s string) {
		at = clampIndex(at, barW-len([]rune(s)))
		copy(axis[at:], []rune(s))
	}
	put(0, "0")
	put(barW/2-3, formatDuration(total/2))
	put(barW-len(formatDuration(total)), formatDuration(total))
	lines := []string{strings.Repeat(" ", labelW+1) + th.Dim.Render(string(axis))}

	for i := t.top; i < min(len(t.nodes), t.top+rows); i++ {
		n := t.nodes[i]
		label := strings.Repeat("  ", n.Depth) + n.Service + " " + th.Dim.Render(n.Name)
		if i == t.selected {
			label = th.Selected.Render(draw.Fit(strings.Repeat("  ", n.Depth)+n.Service+" "+n.Name, labelW))
		} else {
			label = draw.Fit(label, labelW)
		}
		x0 := int(float64(n.Start.Sub(start)) / float64(total) * float64(barW))
		x1 := int(float64(n.End().Sub(start)) / float64(total) * float64(barW))
		x0 = clampIndex(x0, barW-1)
		x1 = min(max(x1, x0+1), barW)
		color := m.th.ColorOf(n.Service)
		if n.Status == traces.StatusError {
			color = m.th.Error
		}
		bar := strings.Repeat(" ", x0) + lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", x1-x0)) + strings.Repeat(" ", barW-x1)
		dur := fmt.Sprintf("%*s", durW, formatDuration(n.Duration))
		if n.Status == traces.StatusError {
			dur = th.ErrorText.Render(dur)
		} else {
			dur = th.Dim.Render(dur)
		}
		lines = append(lines, label+" "+bar+" "+dur)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderSpan(s traces.Span, traceStart time.Time, width, height int) string {
	th := m.th
	var lines []string
	kv := func(k, v string) {
		if v != "" {
			lines = append(lines, th.Key.Render(fmt.Sprintf("%-10s", k))+" "+v)
		}
	}
	status := s.Status.String()
	if s.Status == traces.StatusError {
		status = th.ErrorText.Render("error")
		if s.StatusMessage != "" {
			status += th.ErrorText.Render(": " + s.StatusMessage)
		}
	}
	kv("operation", s.Name)
	kv("service", s.Service)
	kv("kind", s.Kind.String())
	kv("status", status)
	kv("duration", formatDuration(s.Duration)+th.Dim.Render(fmt.Sprintf("  (starts at +%s)", formatDuration(s.Start.Sub(traceStart)))))
	kv("start", s.Start.In(m.location()).Format("2006-01-02 15:04:05.000000"))
	kv("span_id", s.SpanID)
	kv("parent_id", s.ParentID)
	section := func(title string, attrs map[string]any) {
		if len(attrs) == 0 {
			return
		}
		lines = append(lines, "", th.Header.Render(title))
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, th.Key.Render(k)+" "+fmt.Sprint(attrs[k]))
		}
	}
	section("ATTRIBUTES", s.Attributes)
	section("RESOURCE", s.ResourceAttributes)

	t := m.trace
	t.scroll = clampIndex(t.scroll, max(len(lines)-height, 0))
	lines = lines[t.scroll:]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return strings.Join(lines, "\n")
}

// formatDuration renders a duration compactly: 850µs, 12.3ms, 1.68s.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}
