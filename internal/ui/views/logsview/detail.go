package logsview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// filterEntry is a field of the detail panel that can be added to the query.
type filterEntry struct {
	field string // platform field name, e.g. attributes.http.method
	value string
	line  int // line of the entry in the detail content
}

// layoutDetail sizes the detail viewport to its panel.
func (m *Model) layoutDetail() {
	w := m.width
	if m.width >= sideBySideWidth {
		w = m.width - m.width*55/100
	}
	m.detail.SetWidth(max(w-2, 10))
	m.detail.SetHeight(max(m.layout().panels-2, 1))
}

// setDetail shows the selected record in the detail panel, from the top.
func (m *Model) setDetail() {
	m.detailField = 0
	m.renderDetailContent()
	m.detail.GotoTop()
}

// renderDetailContent renders the selected record, keeping the scroll
// position, and makes sure the selected field is visible.
func (m *Model) renderDetailContent() {
	if !m.showDetail || len(m.records) == 0 {
		m.detail.SetContent("")
		m.detailEntries = nil
		return
	}
	content, entries := m.renderDetail(m.records[m.cursor], max(m.detail.Width(), 20))
	m.detailEntries = entries
	m.detailField = clampIndex(m.detailField, len(entries)-1)
	offset := m.detail.YOffset()
	m.detail.SetContent(content)
	m.detail.SetYOffset(offset)
	if len(entries) > 0 {
		line := entries[m.detailField].line
		if line < m.detail.YOffset() {
			m.detail.SetYOffset(line)
		} else if bottom := m.detail.YOffset() + m.detail.Height() - 1; line > bottom {
			m.detail.SetYOffset(line - m.detail.Height() + 1)
		}
	}
}

// selectDetailField moves the field selection of the detail panel.
func (m *Model) selectDetailField(delta int) {
	if len(m.detailEntries) == 0 {
		return
	}
	m.detailField = clampIndex(m.detailField+delta, len(m.detailEntries)-1)
	m.renderDetailContent()
}

func (m *Model) renderDetail(r logs.Record, width int) (string, []filterEntry) {
	t := m.th
	loc := m.location()
	var lines []string
	var entries []filterEntry
	selected := m.detailField
	add := func(text, field, value string) {
		if field != "" && value != "" {
			if len(entries) == selected && m.detailFocused() {
				text = t.Selected.Render(fit(ansi.Strip(text), width))
			}
			entries = append(entries, filterEntry{field: field, value: value, line: len(lines)})
		}
		lines = append(lines, text)
	}
	kv := func(name, value, field, filterValue string) {
		if value == "" {
			return
		}
		add(t.Key.Render(fmt.Sprintf("%-10s", name))+" "+value, field, filterValue)
	}

	kv("time", r.Timestamp.In(loc).Format("2006-01-02 15:04:05.000000000 MST"), "", "")
	if !r.ObservedTimestamp.IsZero() {
		kv("observed", r.ObservedTimestamp.In(loc).Format("2006-01-02 15:04:05.000 MST")+
			t.Dim.Render(fmt.Sprintf(" (+%s)", r.ObservedTimestamp.Sub(r.Timestamp).Round(time.Millisecond))), "", "")
	}
	sev := t.Severity(r.Severity).Render(r.Severity.String())
	if r.SeverityText != "" && !strings.EqualFold(r.SeverityText, r.Severity.String()) {
		sev += t.Dim.Render(" (" + r.SeverityText + ")")
	}
	if r.SeverityText != "" {
		kv("severity", sev, "severity_text", r.SeverityText)
	} else {
		kv("severity", sev, "severity_number", fmt.Sprint(r.SeverityNumber))
	}
	kv("service", r.Service, "service_name", r.Service)
	kv("trace_id", r.TraceID, "trace_id", r.TraceID)
	kv("span_id", r.SpanID, "span_id", r.SpanID)
	if r.ScopeName != "" {
		kv("scope", strings.TrimSpace(r.ScopeName+" "+r.ScopeVersion), "scope_name", r.ScopeName)
	}

	lines = append(lines, "", t.Header.Render("MESSAGE"))
	bodyField := messageField(r)
	for i, l := range strings.Split(lipgloss.NewStyle().Width(width).Render(r.Body), "\n") {
		if i == 0 && bodyField != "" {
			add(l, bodyField, r.Body)
			continue
		}
		lines = append(lines, l)
	}

	section := func(title, prefix string, attrs map[string]any) {
		if len(attrs) == 0 {
			return
		}
		lines = append(lines, "", t.Header.Render(title))
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := formatValue(attrs[k])
			add(t.Key.Render(k)+" "+v, prefix+k, v)
		}
	}
	section("ATTRIBUTES", "attributes.", r.Attributes)
	section("RESOURCE", "resource_attributes.", r.ResourceAttributes)

	if len(r.Raw) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, r.Raw, "", "  ") == nil {
			lines = append(lines, "", t.Header.Render("RAW")+t.Dim.Render("  (y to copy)"))
			for _, l := range strings.Split(strings.TrimRight(pretty.String(), "\n"), "\n") {
				lines = append(lines, t.Dim.Render(l))
			}
		}
	}
	return strings.Join(lines, "\n"), entries
}

// messageField is the platform field holding the message: body.message for
// a structured body, body for a plain string, none otherwise.
func messageField(r logs.Record) string {
	var doc struct {
		Body json.RawMessage `json:"body"`
	}
	if r.Body == "" || json.Unmarshal(r.Raw, &doc) != nil {
		return ""
	}
	b := bytes.TrimSpace(doc.Body)
	switch {
	case len(b) > 0 && b[0] == '"':
		return "body"
	case len(b) > 0 && b[0] == '{' && bytes.Contains(b, []byte(`"message"`)):
		return "body.message"
	}
	return ""
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
