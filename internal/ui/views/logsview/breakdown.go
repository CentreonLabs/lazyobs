package logsview

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

const (
	// minBreakdownWidth is the terminal width below which the panel is not shown.
	minBreakdownWidth = 90
	// othersLabel names the logs whose value is not among the listed ones.
	othersLabel           = "(others)"
	defaultBreakdownField = "service_name"
)

// breakdown is the "top values of a field" panel next to the timeline.
type breakdown struct {
	field    string // empty when the panel is closed
	data     obs.Breakdown
	loading  bool
	err      error
	selected int
	offset   int // first row shown
}

type breakdownMsg struct {
	owner
	id    int
	field string
	data  obs.Breakdown
	err   error
}

// breakdownShown reports whether the panel has room on screen.
func (m *Model) breakdownShown() bool {
	return m.bd.field != "" && m.width >= minBreakdownWidth && m.layout().timeline > 0
}

// openBreakdownPicker asks for the field to break the logs down by.
func (m *Model) openBreakdownPicker() tea.Cmd {
	if m.opts.Breakdown == nil {
		return app.Notify(app.LevelWarn, "breakdowns are not available with this source")
	}
	value := m.bd.field
	if value == "" {
		value = defaultBreakdownField
	}
	return tea.Batch(m.openInput(modeBreakdown, value, "a field, e.g. service_name or resource_attributes.host.name"), m.startCompletion())
}

// setBreakdown shows the panel for field, or closes it when field is empty.
func (m *Model) setBreakdown(field string) tea.Cmd {
	if field != "" {
		if f, known := m.field(field); m.compl.fields != nil && (!known || !f.Aggregatable) {
			return app.Notify(app.LevelError, "%s cannot be broken down: pick a field listed by the completion", field)
		}
	}
	m.bd = breakdown{field: field}
	if field == "" {
		if m.focus == focusBreakdown {
			m.focus = focusList
		}
		return nil
	}
	m.compact = false // the panel lives next to the full timeline
	m.layoutDetail()
	return m.fetchBreakdown()
}

func (m *Model) fetchBreakdown() tea.Cmd {
	if m.bd.field == "" || m.opts.Breakdown == nil || m.pager == nil || m.width == 0 {
		return nil
	}
	m.bd.loading = true
	q := m.pager.Query()
	if m.tail != nil {
		q.Range = m.rng.Pin(m.opts.Now()) // live: follow the clock like the timeline
	}
	src, id, field, viewID := m.opts.Breakdown, m.searchID, m.bd.field, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		data, err := src.Breakdown(ctx, q, field)
		return breakdownMsg{owner: owner{viewID}, id: id, field: field, data: data, err: err}
	}
}

func (m *Model) onBreakdown(msg breakdownMsg) {
	if msg.id != m.searchID || msg.field != m.bd.field {
		return
	}
	m.bd.loading, m.bd.err = false, msg.err
	if msg.err == nil {
		m.bd.data = msg.data
		m.bd.selected = clampIndex(m.bd.selected, len(m.breakdownRows())-1)
	}
}

// breakdownRow is a line of the panel.
type breakdownRow struct {
	value  string
	count  int64
	others bool
}

func (m *Model) breakdownRows() []breakdownRow {
	rows := make([]breakdownRow, 0, len(m.bd.data.Values)+1)
	var listed int64
	for _, v := range m.bd.data.Values {
		rows = append(rows, breakdownRow{value: v.Value, count: v.Count})
		listed += v.Count
	}
	if rest := m.bd.data.Total - listed; rest > 0 && len(rows) > 0 {
		rows = append(rows, breakdownRow{value: othersLabel, count: rest, others: true})
	}
	return rows
}

// breakdownKey handles the keys of the focused panel.
func (m *Model) breakdownKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.focus != focusBreakdown || !m.breakdownShown() {
		return nil, false
	}
	k, rows := m.keys, m.breakdownRows()
	switch {
	case key.Matches(msg, k.Down):
		m.bd.selected = clampIndex(m.bd.selected+1, len(rows)-1)
	case key.Matches(msg, k.Up):
		m.bd.selected = clampIndex(m.bd.selected-1, len(rows)-1)
	case key.Matches(msg, k.FilterIn), key.Matches(msg, k.Detail):
		return m.filterOnBreakdown(false), true
	case key.Matches(msg, k.FilterOut):
		return m.filterOnBreakdown(true), true
	case key.Matches(msg, k.Close):
		return m.setBreakdown(""), true
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) filterOnBreakdown(exclude bool) tea.Cmd {
	rows := m.breakdownRows()
	if len(rows) == 0 {
		return nil
	}
	r := rows[m.bd.selected]
	if r.others {
		return app.Notify(app.LevelInfo, "%s groups the values beyond the top %d: pick a listed value", othersLabel, len(rows)-1)
	}
	_, text := m.searchable(m.bd.field)
	return m.applyFilter(filterEntry{field: m.bd.field, value: r.value, text: text}, exclude)
}

// renderBreakdown draws the panel content: one bar per value.
func (m *Model) renderBreakdown(width, height int) string {
	t := m.th
	rows := m.breakdownRows()
	switch {
	case m.bd.err != nil:
		return t.ErrorText.Render(wrap(m.bd.err.Error(), width))
	case len(rows) == 0 && m.bd.loading:
		return t.Dim.Render("Loading…")
	case len(rows) == 0:
		return t.Dim.Render("No logs")
	}
	// Keep the selection in view.
	if m.bd.selected < m.bd.offset {
		m.bd.offset = m.bd.selected
	}
	if m.bd.selected >= m.bd.offset+height {
		m.bd.offset = m.bd.selected - height + 1
	}
	m.bd.offset = clampIndex(m.bd.offset, max(len(rows)-height, 0))

	var peak int64
	for _, r := range rows {
		peak = max(peak, r.count)
	}
	valueW := 6
	for _, r := range rows {
		valueW = max(valueW, ansi.StringWidth(r.value))
	}
	valueW = min(valueW, width*45/100)
	const countW, pctW = 6, 4
	barW := max(width-valueW-countW-pctW-3, 4)

	lines := make([]string, 0, height)
	for i := m.bd.offset; i < min(len(rows), m.bd.offset+height); i++ {
		r := rows[i]
		pct := 0.0
		if m.bd.data.Total > 0 {
			pct = float64(r.count) * 100 / float64(m.bd.data.Total)
		}
		value := fmt.Sprintf("%-*s", valueW, ansi.Truncate(r.value, valueW, "…"))
		bar := fmt.Sprintf("%-*s", barW, barOf(r.count, peak, barW))
		tail := fmt.Sprintf(" %*s %3.0f%%", countW, humanCount(r.count), pct)
		if i == m.bd.selected && m.focus == focusBreakdown {
			lines = append(lines, t.Selected.Render(fit(value+" "+bar+tail, width)))
			continue
		}
		style := t.Spark
		if r.others {
			style, value = t.Dim, t.Dim.Render(value)
		}
		lines = append(lines, value+" "+style.Render(bar)+t.Dim.Render(tail))
	}
	return strings.Join(lines, "\n")
}

// barOf draws a horizontal bar of count relative to peak, with eighths.
func barOf(count, peak int64, width int) string {
	if peak <= 0 || count <= 0 {
		return ""
	}
	eighths := int(float64(count) / float64(peak) * float64(width*8))
	eighths = max(eighths, 1)
	full, rest := eighths/8, eighths%8
	bar := strings.Repeat("█", full)
	if rest > 0 {
		bar += string([]rune("▏▎▍▌▋▊▉")[rest-1])
	}
	return bar
}
