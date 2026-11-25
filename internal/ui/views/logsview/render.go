package logsview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

const (
	// sideBySideWidth is the minimum width to show the list and the detail
	// panel next to each other; below it the detail panel replaces the list.
	sideBySideWidth = 110
	timeLayout      = "01-02 15:04:05.000"
	maxServiceWidth = 18
	sparkBlocks     = "▁▂▃▄▅▆▇█"
)

// Layout: query bar (1 line), histogram (1 line), then bordered panels.
const barLines = 2

// View implements app.SignalView.
func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	bar := m.renderBar()
	spark := m.renderSpark()
	panelH := max(height-barLines, 3)

	var panels string
	switch {
	case !m.showDetail:
		panels = m.box(m.renderList(width-2, panelH-2), width-2, panelH-2, m.focus == focusList)
	case width >= sideBySideWidth:
		lw := width * 55 / 100
		dw := width - lw
		panels = lipgloss.JoinHorizontal(lipgloss.Top,
			m.box(m.renderList(lw-2, panelH-2), lw-2, panelH-2, m.focus == focusList),
			m.box(m.detail.View(), dw-2, panelH-2, m.focus == focusDetail),
		)
	default:
		panels = m.box(m.detail.View(), width-2, panelH-2, true)
	}
	return lipgloss.JoinVertical(lipgloss.Left, bar, spark, panels)
}

// listRows is the number of records visible in the list.
func (m *Model) listRows() int { return max(m.height-barLines-2-1, 1) } // borders and column header

func (m *Model) renderBar() string {
	t := m.th
	var left string
	switch m.mode {
	case modeQuery:
		left = t.Key.Render("query › ") + m.input.View()
	case modeRange:
		left = t.Key.Render("range › ") + m.input.View() + t.Dim.Render("  tab: "+strings.Join(rangePresets, " "))
	default:
		q := m.query
		if strings.TrimSpace(q) == "" {
			q = t.Dim.Render("* (press / to search)")
		}
		left = t.Key.Render("query ") + q + t.Dim.Render("  │  ") + t.Key.Render("range ") + m.rng.String()
		if m.utc {
			left += t.Dim.Render(" UTC")
		}
	}
	right := m.resultSummary()
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) resultSummary() string {
	t := m.th
	switch {
	case m.err != nil:
		return t.ErrorText.Render("error")
	case m.pager == nil || (m.loading && len(m.records) == 0):
		return t.Dim.Render("searching…")
	}
	total := m.pager.Total()
	s := fmt.Sprintf("%s logs", humanCount(total))
	if m.liveNew > 0 {
		s += fmt.Sprintf(" +%s live", humanCount(int64(m.liveNew)))
	}
	return t.Dim.Render(fmt.Sprintf("%s · %s", s, m.elapsed.Round(time.Millisecond)))
}

func (m *Model) renderSpark() string {
	if len(m.hist) == 0 || m.width < 20 {
		return ""
	}
	width := m.width - 12
	values := resample(m.hist, width)
	var peak float64
	for _, v := range values {
		peak = max(peak, v)
	}
	var b strings.Builder
	blocks := []rune(sparkBlocks)
	for _, v := range values {
		if peak == 0 || v == 0 {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(blocks[min(int(v/peak*float64(len(blocks)-1)+0.5), len(blocks)-1)])
	}
	return m.th.Spark.Render(b.String()) + m.th.Dim.Render(fmt.Sprintf(" max %s", humanCount(int64(peak))))
}

// resample sums points into n buckets.
func resample(points []obs.Point, n int) []float64 {
	out := make([]float64, n)
	if len(points) == 0 || n <= 0 {
		return out
	}
	for i, p := range points {
		out[min(i*n/len(points), n-1)] += p.Value
	}
	if len(points) < n { // stretch instead of leaving gaps
		for i := range out {
			out[i] = points[min(i*len(points)/n, len(points)-1)].Value
		}
	}
	return out
}

func (m *Model) renderList(width, height int) string {
	t := m.th
	switch {
	case m.err != nil && len(m.records) == 0:
		return t.ErrorText.Render(wrap("Search failed: "+m.err.Error(), width)) + "\n\n" + t.Dim.Render("Press r to retry, / to change the query.")
	case m.loading && len(m.records) == 0:
		return t.Dim.Render("Searching…")
	case len(m.records) == 0:
		return t.Dim.Render(wrap(fmt.Sprintf("No logs match this query over the %s. Press t to widen the time range or / to change the query.", m.rng), width))
	}

	svcW := 7
	for _, r := range m.records[m.top:min(m.top+height, len(m.records))] {
		svcW = max(svcW, min(ansi.StringWidth(r.Service), maxServiceWidth))
	}
	header := fmt.Sprintf("%-*s  %-5s  %-*s  %s", len(timeLayout), "TIME", "LEVEL", svcW, "SERVICE", "MESSAGE")
	lines := []string{t.Header.Render(fit(header, width))}

	end := min(m.top+height-1, len(m.records))
	loc := m.location()
	for i := m.top; i < end; i++ {
		r := m.records[i]
		ts := r.Timestamp.In(loc).Format(timeLayout)
		sev := fmt.Sprintf("%-5s", r.Severity.String())
		if r.Severity == logs.SeverityUnknown && r.SeverityText != "" {
			sev = fmt.Sprintf("%-5.5s", strings.ToUpper(r.SeverityText))
		}
		svc := fmt.Sprintf("%-*s", svcW, ansi.Truncate(r.Service, svcW, "…"))
		msgW := max(width-len(timeLayout)-5-svcW-6, 1)
		msg := ansi.Truncate(oneLine(r.Body), msgW, "…")
		if i == m.cursor {
			row := fit(fmt.Sprintf("%s  %s  %s  %s", ts, sev, svc, msg), width)
			lines = append(lines, t.Selected.Render(row))
			continue
		}
		lines = append(lines, t.Dim.Render(ts)+"  "+t.Severity(r.Severity).Render(sev)+"  "+svc+"  "+msg)
	}
	if m.loading {
		lines = append(lines, t.Dim.Render("loading more…"))
	}
	return strings.Join(lines, "\n")
}

// box draws content inside a rounded border of the given inner size.
func (m *Model) box(content string, innerW, innerH int, focused bool) string {
	style := m.th.Border
	if focused {
		style = m.th.FocusedBox
	}
	lines := strings.Split(content, "\n")
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = fit(l, innerW)
	}
	return style.Render(strings.Join(lines, "\n"))
}

// fit truncates or pads s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ⏎ ", "\n", " ⏎ ", "\t", " ").Replace(s)
}

func wrap(s string, w int) string { return lipgloss.NewStyle().Width(max(w, 10)).Render(s) }

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprint(n)
	}
}
