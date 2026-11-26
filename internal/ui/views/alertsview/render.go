package alertsview

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
)

const (
	// minSideWidth is the width from which the details sit beside the list.
	minSideWidth = 110
	statusW      = 8
	templateW    = 10
	everyW       = 5
	checkW       = 8
)

// layout places the list and the details panel.
type layout struct {
	side            bool // details on the right of the list
	listW, listH    int  // outer size of the list panel
	detailW, detail int  // outer size of the details panel (detail is its height)
	listTop         int  // body row of the first rule
	listRows        int
}

func (m *Model) layout() layout {
	h := max(m.height-1, 4) // below the bar
	l := layout{listW: m.width, listH: h}
	if m.detailOpen {
		if m.width >= minSideWidth {
			l.side = true
			l.listW = m.width * 55 / 100
			l.detailW, l.detail = m.width-l.listW, h
		} else {
			l.listH = max(h/2, 4)
			l.detailW, l.detail = m.width, h-l.listH
		}
	}
	l.listTop = 1 + 1 + 1 // bar, top border, column header
	l.listRows = max(l.listH-3, 1)
	return l
}

func (m *Model) listRows() int { return m.layout().listRows }

// View implements app.SignalView.
func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	l := m.layout()
	list := m.panel(m.listTitle(), m.renderList(l.listW-2, l.listH-2), l.listW-2, l.listH-2, true)
	body := list
	if m.detailOpen {
		a := m.Selected()
		details := m.panel(a.Rule.Name, m.renderDetails(a, l.detailW-2), l.detailW-2, l.detail-2, false)
		if l.side {
			body = lipgloss.JoinHorizontal(lipgloss.Top, list, details)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left, list, details)
		}
	}
	return m.renderBar() + "\n" + body
}

func (m *Model) panel(title, content string, w, h int, focused bool) string {
	border, titleStyle := lipgloss.NewStyle().Foreground(m.th.Subtle), m.th.PanelTitle
	if focused {
		border, titleStyle = lipgloss.NewStyle().Foreground(m.th.Accent), titleStyle.Foreground(m.th.Accent)
	}
	return draw.Panel(title, content, w, h, border, titleStyle)
}

func (m *Model) listTitle() string {
	title := "Alert rules"
	if m.problemsOnly {
		title += " · problems only"
	}
	if m.filter != "" {
		title += " · “" + m.filter + "”"
	}
	if len(m.items) != len(m.all) {
		title += fmt.Sprintf(" · %d of %d", len(m.items), len(m.all))
	}
	return title
}

// renderBar summarizes the statuses, or shows the filter input.
func (m *Model) renderBar() string {
	t := m.th
	if m.filtering {
		return ansi.Truncate(t.Key.Render("filter › ")+m.input.View(), m.width, "…")
	}
	counts := map[alerts.Status]int{}
	var disabled, failing, muted int
	for _, a := range m.all {
		switch {
		case !a.Rule.Enabled:
			disabled++
			continue
		case a.Downtime != "" || a.Ack != nil:
			muted++
		}
		counts[a.Status()]++
		if a.Failing() {
			failing++
		}
	}
	var parts []string
	for s := alerts.StatusCritical; s >= alerts.StatusOK; s-- {
		if n := counts[s]; n > 0 {
			parts = append(parts, m.statusStyle(s).Render(fmt.Sprintf("● %d %s", n, s)))
		}
	}
	if failing > 0 {
		parts = append(parts, t.WarnText.Render(fmt.Sprintf("⚠ %d failing", failing)))
	}
	if muted > 0 {
		parts = append(parts, t.Dim.Render(fmt.Sprintf("◌ %d muted", muted)))
	}
	if disabled > 0 {
		parts = append(parts, t.Dim.Render(fmt.Sprintf("○ %d disabled", disabled)))
	}
	left := t.Key.Render("alerts ") + strings.Join(parts, t.Dim.Render("  "))
	if !m.loaded {
		left = t.Key.Render("alerts ") + t.Dim.Render("reading the alert rules…")
	}
	var right string
	switch {
	case m.err != nil:
		right = t.ErrorText.Render("refresh failed")
	case m.loading && m.loaded:
		right = t.Dim.Render("refreshing…")
	case m.loaded:
		right = t.Dim.Render("updated " + m.readAt.In(m.location()).Format("15:04:05"))
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) statusStyle(s alerts.Status) lipgloss.Style {
	t := m.th
	switch s {
	case alerts.StatusCritical, alerts.StatusError:
		return t.ErrorText.Bold(true)
	case alerts.StatusWarning:
		return t.WarnText
	case alerts.StatusOK:
		return lipgloss.NewStyle().Foreground(t.Success)
	default:
		return t.Dim
	}
}

// statusLabel is the status column of an alert.
func statusLabel(a alerts.Alert) string {
	switch {
	case !a.Rule.Enabled:
		return "disabled"
	case a.Event == nil:
		return "pending"
	}
	return strings.ToUpper(a.Status().String())
}

func (m *Model) renderList(width, height int) string {
	t := m.th
	switch {
	case m.err != nil && !m.loaded:
		return t.ErrorText.Render(draw.Wrap("Could not read the alerts: "+m.err.Error(), width))
	case !m.loaded:
		return t.Dim.Render("Loading…")
	case len(m.all) == 0:
		return t.Dim.Render(draw.Wrap("No alert rule in this organization yet. Rules are created in the web interface.", width))
	case len(m.items) == 0:
		return t.Dim.Render(draw.Wrap("No rule matches the filters. Press esc to clear the name filter, p to show all rules.", width))
	}
	const maxNameW = 36
	free := width - statusW - templateW - everyW - checkW - 10
	nameW, noteW := max(free, 10), 0
	if free >= 24+20 { // room for notes
		nameW = min(maxNameW, max(24, free*45/100))
		noteW = free - nameW - 2
	}
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %*s  %-*s", statusW, "STATUS", nameW, "RULE", templateW, "TYPE", everyW, "EVERY", checkW, "CHECKED")
	if noteW > 0 {
		header += "  NOTE"
	}
	lines := []string{t.Header.Render(draw.Fit(header, width))}
	now := m.opts.Now()
	end := min(m.top+height-1, len(m.items))
	for i := m.top; i < end; i++ {
		a := m.items[i]
		label := fmt.Sprintf("%-*s", statusW, statusLabel(a))
		checked := "—"
		if a.Event != nil {
			checked = a.Event.At.In(m.location()).Format("15:04:05")
		}
		rest := fmt.Sprintf("  %-*s  %-*s  %*s  %-*s", nameW, ansi.Truncate(a.Rule.Name, nameW, "…"),
			templateW, ansi.Truncate(template(a.Rule.Template), templateW, "…"), everyW, every(a.Rule.Frequency), checkW, checked)
		note := ""
		if noteW > 0 {
			note = "  " + ansi.Truncate(m.note(a, now), noteW, "…")
		}
		if i == m.cursor {
			lines = append(lines, t.Selected.Render(draw.Fit(label+rest+ansi.Strip(note), width)))
			continue
		}
		style := m.statusStyle(a.Status())
		if !a.Rule.Enabled || a.Event == nil {
			style = t.Dim
		}
		row := style.Render(label) + rest
		if !a.Rule.Enabled {
			row = t.Dim.Render(label + rest)
		}
		lines = append(lines, row+note)
	}
	return strings.Join(lines, "\n")
}

// note tells what is special about an alert: failing evaluation, muted, flapping.
func (m *Model) note(a alerts.Alert, now time.Time) string {
	t := m.th
	var parts []string
	if a.Failing() {
		parts = append(parts, t.WarnText.Render("⚠ evaluation failed"))
	}
	if a.Downtime != "" {
		parts = append(parts, t.Dim.Render("◌ downtime "+a.Downtime))
	}
	if a.Ack != nil {
		parts = append(parts, t.Dim.Render("✓ acknowledged by "+a.Ack.By))
	}
	if a.Event != nil && a.Event.Flapping {
		parts = append(parts, t.WarnText.Render("↯ flapping"))
	}
	if len(parts) == 0 && a.Event != nil && a.Rule.Enabled && a.Rule.Frequency > 0 && now.Sub(a.Event.At) > 3*a.Rule.Frequency {
		parts = append(parts, t.WarnText.Render("not evaluated for "+strings.TrimSuffix(ago(a.Event.At, now), " ago")))
	}
	return strings.Join(parts, t.Dim.Render(" · "))
}

func template(name string) string {
	switch name {
	case "count_logs":
		return "log count"
	case "ratio_logs":
		return "log ratio"
	}
	return name
}

func every(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d%time.Hour == 0:
		return itoa(int(d.Hours())) + "h"
	case d%time.Minute == 0:
		return itoa(int(d.Minutes())) + "m"
	}
	return itoa(int(d.Seconds())) + "s"
}

// renderDetails describes a rule: status, what it evaluates, notifications.
func (m *Model) renderDetails(a alerts.Alert, width int) string {
	t := m.th
	if a.Rule.ID == "" {
		return t.Dim.Render("No rule selected.")
	}
	w := max(width-1, 10)
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	const labelW = 13
	field := func(name, value string) {
		// Wrapped values stay aligned after the label.
		value = strings.ReplaceAll(value, "\n", "\n"+strings.Repeat(" ", labelW))
		add(t.Key.Render(fmt.Sprintf("%-*s", labelW, name)) + value)
	}
	now := m.opts.Now()

	if a.Rule.Description != "" {
		add(t.Dim.Render(draw.Wrap(a.Rule.Description, w)), "")
	}
	switch {
	case !a.Rule.Enabled:
		field("status", t.Dim.Render("disabled: not evaluated"))
	case a.Event == nil:
		field("status", t.Dim.Render("not evaluated yet"))
	default:
		ev := a.Event
		field("status", m.statusStyle(ev.Status).Render(strings.ToUpper(ev.Status.String()))+
			t.Dim.Render(fmt.Sprintf("  checked %s (%s)", ev.At.In(m.location()).Format("15:04:05"), ago(ev.At, now))))
		if ev.Message != "" {
			field("message", ev.Message)
		}
		if ev.EvalError != "" {
			field("error", t.WarnText.Render(ev.EvalError))
		}
		if ev.Flapping {
			field("flapping", t.WarnText.Render("the status changes too often"))
		}
	}
	if a.Ack != nil {
		ack := fmt.Sprintf("by %s until %s", a.Ack.By, a.Ack.Until.In(m.location()).Format("01-02 15:04"))
		if a.Ack.Comment != "" {
			ack += " · " + a.Ack.Comment
		}
		field("acknowledged", ack)
	}
	if a.Downtime != "" {
		field("downtime", a.Downtime+t.Dim.Render(" (notifications muted)"))
	}
	add("")

	entry, ok := m.details[detailKey(a)]
	switch {
	case !ok || entry.loading:
		add(t.Dim.Render("Loading the rule…"))
	case entry.err != nil:
		add(t.ErrorText.Render(draw.Wrap(entry.err.Error(), w)))
	default:
		d := entry.details
		for _, q := range d.Queries {
			field(q.Name, strings.TrimRight(draw.Wrap(q.Text, max(w-labelW, 10)), " "))
		}
		switch d.Measure {
		case "":
		case "ratio":
			field("last ratio", formatValue(d.Value)+t.Dim.Render(fmt.Sprintf("  (%.1f %%)", d.Value*100)))
		default:
			field("last "+d.Measure, formatValue(d.Value))
		}
		for i, c := range d.Conditions {
			name := ""
			if i == 0 {
				name = "thresholds"
			}
			field(name, fmt.Sprintf("%s %s → ", c.Operator, formatValue(c.Threshold))+m.statusStyle(c.Status).Render(c.Status.String()))
		}
	}
	add("")
	field("evaluated", fmt.Sprintf("every %s · %s", every(a.Rule.Frequency), template(a.Rule.Template)))
	if len(a.Rule.Channels) > 0 {
		field("notifies", strings.Join(a.Rule.Channels, ", "))
	} else {
		field("notifies", t.Dim.Render("nobody"))
	}
	for i, l := range a.Rule.Links {
		name := ""
		if i == 0 {
			name = "links"
		}
		field(name, l.Label+" "+t.Dim.Render(l.URL))
	}
	add("", t.Dim.Render("l: logs of the query · y: copy the query"))
	return strings.Join(lines, "\n")
}

// formatValue prints counts without decimals and other values with three at most.
func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}
