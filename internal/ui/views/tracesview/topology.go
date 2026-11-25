package tracesview

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
	"github.com/CentreonLabs/lazyobs/internal/ui/topomap"
)

const (
	// maxMapServices is the size above which the topology starts as a table.
	maxMapServices = 30
	topoDetailW    = 38
)

// topology is the service map (or table) state.
type topology struct {
	id       int // search it belongs to
	data     traces.Topology
	layout   topomap.Layout
	loading  bool
	err      error
	selected int // index in layout.Boxes
	table    bool
	// Body position of the map panel content, for clicks.
	mapX, mapY, x0, y0 int
}

type topologyMsg struct {
	owner
	id   int
	data traces.Topology
	err  error
}

// openTopology shows the service map of the current time range.
func (m *Model) openTopology() tea.Cmd {
	if m.opts.Topology == nil {
		return app.Notify(app.LevelWarn, "the service map is not available here")
	}
	m.topo = &topology{loading: true}
	return m.fetchTopology()
}

func (m *Model) fetchTopology() tea.Cmd {
	if m.topo == nil || m.opts.Topology == nil {
		return nil
	}
	m.topo.loading, m.topo.id = true, m.searchID
	from, to := m.rng.Resolve(m.opts.Now())
	src, id, view := m.opts.Topology, m.searchID, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		data, err := src.Topology(ctx, from, to)
		return topologyMsg{owner: owner{view}, id: id, data: data, err: err}
	}
}

func (m *Model) onTopology(msg topologyMsg) {
	t := m.topo
	if t == nil || msg.id != m.searchID {
		return
	}
	t.loading, t.err = false, msg.err
	if msg.err != nil {
		return
	}
	selected := ""
	if t.selected < len(t.layout.Boxes) {
		selected = t.layout.Boxes[t.selected].Name
	}
	t.data, t.layout = msg.data, topomap.Compute(msg.data)
	t.table = t.table || len(t.layout.Boxes) > maxMapServices
	t.selected = 0
	for i, b := range t.layout.Boxes {
		if b.Name == selected {
			t.selected = i
		}
	}
}

func (m *Model) topologyKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	t, k := m.topo, m.keys
	n := len(t.layout.Boxes)
	switch {
	case key.Matches(msg, k.Close), key.Matches(msg, k.Map):
		m.topo = nil
	case key.Matches(msg, k.Down), msg.String() == "right":
		t.selected = (t.selected + 1) % max(n, 1)
	case key.Matches(msg, k.Up), msg.String() == "left":
		t.selected = (t.selected - 1 + max(n, 1)) % max(n, 1)
	case key.Matches(msg, k.Layout):
		t.table = !t.table
	case key.Matches(msg, k.Open):
		if s, ok := m.selectedService(); ok {
			m.topo, m.trace = nil, nil
			m.query = "service_name:" + quote(s.Name)
			return tea.Batch(m.search(), app.Notify(app.LevelInfo, "traces starting in %s (searches match root spans)", s.Name)), true
		}
	case key.Matches(msg, k.Logs):
		if s, ok := m.selectedService(); ok && m.opts.OpenLogs != nil {
			return m.opts.OpenLogs("service_name:"+quote(s.Name), m.rng.Pin(m.opts.Now())), true
		}
	default:
		return nil, false // range, zoom, refresh...: handled as in the list
	}
	return nil, true
}

func (m *Model) selectedService() (traces.ServiceStats, bool) {
	if m.topo == nil || m.topo.selected >= len(m.topo.layout.Boxes) {
		return traces.ServiceStats{}, false
	}
	return m.topo.layout.Boxes[m.topo.selected].ServiceStats, true
}

// renderTopology draws the service map or table with the details of the
// selected service.
func (m *Model) renderTopology(width, height int) string {
	t, th := m.topo, m.th
	title := fmt.Sprintf("Service map · %s", m.rng)
	switch {
	case t.err != nil:
		return m.panel(title, th.ErrorText.Render(draw.Wrap("Could not load the topology: "+t.err.Error(), width-4)), width-2, height-2, true)
	case t.loading && len(t.layout.Boxes) == 0:
		return m.panel(title, th.Dim.Render("Loading…"), width-2, height-2, true)
	case len(t.layout.Boxes) == 0:
		return m.panel(title, th.Dim.Render("No span over this time range."), width-2, height-2, true)
	}
	detailW := topoDetailW
	if width < 100 {
		detailW = 0
	}
	mapW := width - detailW
	var main string
	if t.table {
		main = m.panel("Services · "+m.rng.String(), m.renderServiceTable(mapW-2, height-2), mapW-2, height-2, true)
	} else {
		main = m.panel(title, m.renderMap(mapW-2, height-2), mapW-2, height-2, true)
	}
	if detailW == 0 {
		return main
	}
	s, _ := m.selectedService()
	detail := m.panel(s.Name, m.renderServiceDetail(s, detailW-4), detailW-2, height-2, false)
	return lipgloss.JoinHorizontal(lipgloss.Top, main, detail)
}

func (m *Model) renderMap(width, height int) string {
	t, th := m.topo, m.th
	b := t.layout.Boxes[t.selected]
	cx, cy := b.Center()
	t.x0 = clampIndex(cx-width/2, max(t.layout.W-width, 0))
	t.y0 = clampIndex(cy-height/2, max(t.layout.H-height, 0))
	t.mapX, t.mapY = 1, 2 // inside the panel, below the query bar
	out := t.layout.Render(topomap.Styles{
		Line: th.Dim, Arrow: lipgloss.NewStyle().Foreground(th.Accent),
		Box:      lipgloss.NewStyle().Foreground(th.Muted),
		BoxError: lipgloss.NewStyle().Foreground(th.Error),
		BoxSlow:  lipgloss.NewStyle().Foreground(th.Warning),
		BoxSel:   lipgloss.NewStyle().Foreground(th.Accent).Bold(true),
		Name:     th.PanelTitle, Text: th.Dim, ErrorText: th.ErrorText,
	}, t.selected, t.x0, t.y0, width, height)
	if t.layout.Loops > 0 {
		lines := strings.Split(out, "\n")
		lines[len(lines)-1] = th.Dim.Render(fmt.Sprintf(" %d call(s) closing cycles not drawn: see the details", t.layout.Loops))
		out = strings.Join(lines, "\n")
	}
	return out
}

func (m *Model) renderServiceTable(width, height int) string {
	t, th := m.topo, m.th
	rows := append([]topomap.Box(nil), t.layout.Boxes...)
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rows[order[a]].Spans > rows[order[b]].Spans })
	nameW := max(width-60, 12)
	lines := []string{th.Header.Render(draw.Fit(fmt.Sprintf("%-*s %10s %8s %7s %9s %9s %9s", nameW, "SERVICE", "SPANS", "ERRORS", "ERR%", "P50", "P95", "P99"), width))}
	selectedRow := 0
	for i, idx := range order {
		if idx == t.selected {
			selectedRow = i
		}
	}
	start := clampIndex(selectedRow-(height-2)/2, max(len(order)-(height-1), 0))
	for _, idx := range order[start:min(len(order), start+height-1)] {
		s := rows[idx].ServiceStats
		line := fmt.Sprintf("%-*s %10s %8s %6.1f%% %9s %9s %9s", nameW, ansi.Truncate(s.Name, nameW, "…"),
			draw.HumanCount(s.Spans), draw.HumanCount(s.Errors), s.ErrorRate()*100,
			formatDuration(s.P50), formatDuration(s.P95), formatDuration(s.P99))
		switch {
		case idx == t.selected:
			line = th.Selected.Render(draw.Fit(line, width))
		case s.Errors > 0:
			line = th.ErrorText.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderServiceDetail(s traces.ServiceStats, width int) string {
	th := m.th
	kv := func(k, v string) string { return th.Key.Render(fmt.Sprintf("%-8s", k)) + " " + v }
	errs := fmt.Sprintf("%s (%.1f%%)", draw.HumanCount(s.Errors), s.ErrorRate()*100)
	if s.Errors > 0 {
		errs = th.ErrorText.Render(errs)
	}
	lines := []string{
		kv("spans", draw.HumanCount(s.Spans)),
		kv("errors", errs),
		kv("p50", formatDuration(s.P50)),
		kv("p95", formatDuration(s.P95)),
		kv("p99", formatDuration(s.P99)),
	}
	calls := func(title string, list []traces.Call, other func(traces.Call) string, arrow string) {
		if len(list) == 0 {
			return
		}
		lines = append(lines, "", th.Header.Render(title))
		for _, c := range list {
			lines = append(lines, ansi.Truncate(fmt.Sprintf("%s %s %s", arrow, other(c), th.Dim.Render(draw.HumanCount(c.Count)+" calls")), width, "…"))
		}
	}
	topo := m.topo.data
	calls("CALLED BY", topo.Callers(s.Name), func(c traces.Call) string { return c.From }, "←")
	calls("CALLS", topo.Callees(s.Name), func(c traces.Call) string { return c.To }, "→")
	lines = append(lines, "", th.Dim.Render("enter: traces starting here"), th.Dim.Render("l: logs of the service"))
	return strings.Join(lines, "\n")
}

// clickMap selects the box under a body position.
func (m *Model) clickMap(x, y int) {
	t := m.topo
	if t == nil || t.table {
		return
	}
	gx, gy := x-t.mapX+t.x0, y-t.mapY+t.y0
	for i, b := range t.layout.Boxes {
		if gx >= b.X && gx < b.X+topomap.BoxW && gy >= b.Y && gy < b.Y+topomap.BoxH {
			t.selected = i
		}
	}
}

// quote wraps values the query syntax would split.
func quote(v string) string {
	if strings.ContainsAny(v, " :()\"") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}
