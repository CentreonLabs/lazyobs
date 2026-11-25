package logsview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// layoutDetail sizes the detail viewport to its panel.
func (m *Model) layoutDetail() {
	w := m.width
	if m.width >= sideBySideWidth {
		w = m.width - m.width*55/100
	}
	m.detail.SetWidth(max(w-2, 10))
	m.detail.SetHeight(max(m.height-barLines-2, 1))
}

// setDetail refreshes the detail panel with the selected record.
func (m *Model) setDetail() {
	if !m.showDetail || len(m.records) == 0 {
		m.detail.SetContent("")
		return
	}
	m.detail.SetContent(m.renderDetail(m.records[m.cursor], max(m.detail.Width(), 20)))
	m.detail.GotoTop()
}

func (m *Model) renderDetail(r logs.Record, width int) string {
	t := m.th
	loc := m.location()
	var b strings.Builder
	field := func(name, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s %s\n", t.Key.Render(fmt.Sprintf("%-10s", name)), value)
	}

	field("time", r.Timestamp.In(loc).Format("2006-01-02 15:04:05.000000000 MST"))
	if !r.ObservedTimestamp.IsZero() {
		field("observed", r.ObservedTimestamp.In(loc).Format("2006-01-02 15:04:05.000 MST")+
			t.Dim.Render(fmt.Sprintf(" (+%s)", r.ObservedTimestamp.Sub(r.Timestamp).Round(time.Millisecond))))
	}
	sev := t.Severity(r.Severity).Render(r.Severity.String())
	if r.SeverityText != "" && !strings.EqualFold(r.SeverityText, r.Severity.String()) {
		sev += t.Dim.Render(" (" + r.SeverityText + ")")
	}
	field("severity", sev)
	field("service", r.Service)
	field("trace_id", r.TraceID)
	field("span_id", r.SpanID)
	if r.ScopeName != "" {
		field("scope", strings.TrimSpace(r.ScopeName+" "+r.ScopeVersion))
	}

	b.WriteString("\n" + t.Header.Render("MESSAGE") + "\n")
	b.WriteString(lipgloss.NewStyle().Width(width).Render(r.Body) + "\n")

	section := func(title string, attrs map[string]any) {
		if len(attrs) == 0 {
			return
		}
		b.WriteString("\n" + t.Header.Render(title) + "\n")
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %s\n", t.Key.Render(k), formatValue(attrs[k]))
		}
	}
	section("ATTRIBUTES", r.Attributes)
	section("RESOURCE", r.ResourceAttributes)

	if len(r.Raw) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, r.Raw, "", "  ") == nil {
			b.WriteString("\n" + t.Header.Render("RAW") + t.Dim.Render("  (y to copy)") + "\n")
			b.WriteString(t.Dim.Render(pretty.String()) + "\n")
		}
	}
	return b.String()
}

func formatValue(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}
