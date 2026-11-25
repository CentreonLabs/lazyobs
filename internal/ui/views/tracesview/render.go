package tracesview

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
)

const (
	timeLayout      = "01-02 15:04:05.000"
	minSplitHeight  = 20
	timelineGutter  = 8
	maxServiceWidth = 22
)

// View implements app.SignalView.
func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	m.hits = hitAreas{}
	bar := m.renderBar()
	if m.trace != nil {
		return bar + "\n" + m.renderWaterfall(width, max(height-1, 3))
	}
	timeline, timelineH := m.renderSpark(), 1
	if th := m.timelineHeight(); th > 0 {
		timeline = m.panel("Traces over time", m.renderChart(width-2, th-2, 1, 2), width-2, th-2, false)
		timelineH = th
	}
	listH := max(height-1-timelineH, 3)
	m.hits.listTop, m.hits.listN = 1+timelineH+2, max(listH-3, 0)
	title := "Traces · newest first"
	if m.order != 0 {
		title = "Traces · slowest first"
	}
	list := m.panel(title, m.renderList(width-2, listH-2), width-2, listH-2, true)
	return lipgloss.JoinVertical(lipgloss.Left, bar, timeline, list)
}

// timelineHeight is the height of the chart panel, 0 for the one-line sparkline.
func (m *Model) timelineHeight() int {
	if m.compact || m.height < minSplitHeight {
		return 0
	}
	return (m.height - 1) / 2
}

func (m *Model) listRows() int {
	th := m.timelineHeight()
	if th == 0 {
		th = 1
	}
	return max(m.height-1-th-3, 1)
}

func (m *Model) timelineColumns() int {
	if m.timelineHeight() > 0 {
		return max((m.width-2-timelineGutter)/2, 10)
	}
	return max(m.width-12, 10)
}

func (m *Model) panel(title, content string, w, h int, focused bool) string {
	border, titleStyle := lipgloss.NewStyle().Foreground(m.th.Subtle), m.th.PanelTitle
	if focused {
		border, titleStyle = lipgloss.NewStyle().Foreground(m.th.Accent), titleStyle.Foreground(m.th.Accent)
	}
	return draw.Panel(title, content, w, h, border, titleStyle)
}

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
			q = t.Dim.Render("* (press / to search root spans)")
		}
		left = t.Key.Render("query ") + q + t.Dim.Render("  │  ") + t.Key.Render("range ") + m.rng.String()
	}
	var right string
	switch {
	case m.err != nil:
		right = t.ErrorText.Render("error")
	case m.pager != nil && (!m.loading || len(m.items) > 0):
		right = t.Dim.Render(fmt.Sprintf("%s traces · %s", draw.HumanCount(m.pager.Total()), m.elapsed.Round(time.Millisecond)))
	default:
		right = t.Dim.Render("searching…")
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// seriesColor colors the chart: all traces in the accent color, errors in red.
func (m *Model) seriesColor(name string) color.Color {
	if name == "ERRORS" {
		return m.th.Error
	}
	return m.th.Accent
}

func (m *Model) renderChart(width, height, originX, originY int) string {
	if len(m.hist) == 0 || !m.histFrom.Before(m.histTo) {
		msg := "Loading…"
		if !m.loading {
			msg = "No data"
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, m.th.Dim.Render(msg))
	}
	var marker time.Time
	if len(m.items) > 0 {
		marker = m.items[m.cursor].Root.Start
	}
	out, layout := chart.Render(chart.Options{
		Width: width, Height: height, From: m.histFrom, To: m.histTo, Location: m.location(),
		Series: m.hist, Color: m.seriesColor, Marker: marker, Hover: m.hover, Styles: m.th.Chart,
	})
	m.hits.chart, m.hits.plotX, m.hits.plotY = layout, originX+layout.PlotX, originY
	if d := m.drag; d != nil && layout.PlotW > 0 {
		// Redraw with the selection now that the layout is known.
		out, _ = chart.Render(chart.Options{
			Width: width, Height: height, From: m.histFrom, To: m.histTo, Location: m.location(),
			Series: m.hist, Color: m.seriesColor, Marker: marker, Styles: m.th.Chart,
			SelectFrom: layout.TimeAt(d.from, m.histFrom, m.histTo), SelectTo: layout.TimeAt(d.to+1, m.histFrom, m.histTo),
		})
	}
	return out
}

func (m *Model) renderSpark() string {
	if len(m.hist) == 0 || m.width < 20 || !m.histFrom.Before(m.histTo) {
		return ""
	}
	values := chart.Bucketize(m.hist[0].Points, m.histFrom, m.histTo, m.width-12)
	var peak float64
	for _, v := range values {
		peak = max(peak, v)
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range values {
		if peak == 0 || v == 0 {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(blocks[min(int(v/peak*float64(len(blocks)-1)+0.5), len(blocks)-1)])
	}
	return m.th.Spark.Render(b.String()) + m.th.Dim.Render(" max "+draw.HumanCount(int64(peak)))
}

func (m *Model) renderList(width, height int) string {
	t := m.th
	switch {
	case m.err != nil && len(m.items) == 0:
		return t.ErrorText.Render(draw.Wrap("Search failed: "+m.err.Error(), width))
	case m.loading && len(m.items) == 0:
		return t.Dim.Render("Searching…")
	case len(m.items) == 0:
		return t.Dim.Render(draw.Wrap(fmt.Sprintf("No trace matches this query over the %s. Queries match root spans only. Press t to widen the time range.", m.rng), width))
	}
	end := min(m.top+height-1, len(m.items))
	svcW, peak := 7, time.Duration(0)
	for _, tr := range m.items[m.top:end] {
		svcW = max(svcW, min(ansi.StringWidth(tr.Root.Service), maxServiceWidth))
		peak = max(peak, tr.Root.Duration)
	}
	const durW, barW = 8, 12
	nameW := max(width-len(timeLayout)-durW-svcW-barW-10, 8)
	header := fmt.Sprintf("%-*s  %*s    %-*s  %-*s  %s", len(timeLayout), "TIME", durW, "DURATION", svcW, "SERVICE", nameW, "OPERATION", "")
	lines := []string{t.Header.Render(draw.Fit(header, width))}
	loc := m.location()
	for i := m.top; i < end; i++ {
		r := m.items[i].Root
		status := " "
		if r.Status == traces.StatusError {
			status = t.ErrorText.Render("●")
		}
		bar := durationBar(r.Duration, peak, barW)
		row := fmt.Sprintf("%s  %*s  ", r.Start.In(loc).Format(timeLayout), durW, formatDuration(r.Duration))
		svc := fmt.Sprintf("%-*s", svcW, ansi.Truncate(r.Service, svcW, "…"))
		name := fmt.Sprintf("%-*s", nameW, ansi.Truncate(r.Name, nameW, "…"))
		if i == m.cursor {
			plain := row + ansi.Strip(status) + " " + svc + "  " + name + "  " + bar
			lines = append(lines, t.Selected.Render(draw.Fit(plain, width)))
			continue
		}
		lines = append(lines, t.Dim.Render(row)+status+" "+svc+"  "+name+"  "+t.Spark.Render(bar))
	}
	if m.loading {
		lines = append(lines, t.Dim.Render("loading more…"))
	}
	return strings.Join(lines, "\n")
}

// durationBar draws d relative to peak on width cells.
func durationBar(d, peak time.Duration, width int) string {
	if peak <= 0 || d <= 0 {
		return ""
	}
	return strings.Repeat("▇", max(int(float64(d)/float64(peak)*float64(width)+0.5), 1))
}
