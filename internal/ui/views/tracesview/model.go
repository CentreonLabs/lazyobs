// Package tracesview is the traces screen: query bar, trace volume chart,
// trace list sorted by time or duration, and a waterfall of a trace's spans
// with their details.
package tracesview

import (
	"context"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/traces"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
	"github.com/CentreonLabs/lazyobs/internal/ui/inputs"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
)

const (
	defaultPageSize = 100
	requestTimeout  = 60 * time.Second
	loadAhead       = 30
)

// DefaultRange is the time range of a new traces view.
var DefaultRange = obs.Last(time.Hour)

var rangePresets = []string{"15m", "1h", "6h", "24h", "7d", "30d"}

// Options configures the view.
type Options struct {
	Source    obs.Searcher[traces.Trace]
	Histogram obs.Histogrammer // optional
	Fetcher   traces.Fetcher
	Topology  traces.TopologyReader // optional, enables the service map
	Theme     *theme.Theme
	Query     string
	Range     obs.TimeRange // DefaultRange when zero
	PageSize  int
	// OpenLogs shows the logs matching query over rng (in the logs signal).
	OpenLogs func(query string, rng obs.TimeRange) tea.Cmd
	Now      func() time.Time
}

type mode int

const (
	modeNormal mode = iota
	modeQuery
	modeRange
)

// Model is the traces view.
type Model struct {
	id   uint64
	opts Options
	th   *theme.Theme
	keys keyMap

	width, height int

	query string
	rng   obs.TimeRange
	order obs.Order

	searchID int
	pager    *engine.Pager[traces.Trace]
	items    []traces.Trace
	loading  bool
	err      error
	elapsed  time.Duration

	hist             []obs.Series
	histFrom, histTo time.Time
	histCols         int
	compact          bool

	cursor, top int

	mode      mode
	input     textinput.Model
	presetIdx int

	zoomStack []obs.TimeRange
	drag      *dragSpan
	hover     *chart.Cell
	hits      hitAreas

	// The open trace (waterfall), nil in list mode.
	trace *openTrace
	// The service map, nil when not shown.
	topo *topology

	utc bool
}

var (
	_ app.SignalView = (*Model)(nil)
	_ tabset.Child   = (*Model)(nil)
)

var nextViewID atomic.Uint64

// New returns the traces view.
func New(opts Options) *Model {
	if opts.Range == (obs.TimeRange{}) {
		opts.Range = DefaultRange
	}
	if opts.PageSize <= 0 {
		opts.PageSize = defaultPageSize
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Model{
		id: nextViewID.Add(1), opts: opts, th: opts.Theme, keys: defaultKeys(),
		query: opts.Query, rng: opts.Range, input: inputs.New(),
	}
}

// owner tags async results with the view that asked for them.
type owner struct{ view uint64 }

type (
	pageMsg struct {
		owner
		id       int
		items    []traces.Trace
		warnings []string
		err      error
		elapsed  time.Duration
	}
	histMsg struct {
		owner
		id       int
		cols     int
		from, to time.Time
		series   []obs.Series
		err      error
	}
	traceMsg struct {
		owner
		traceID string
		detail  traces.Detail
		err     error
	}
)

// Name implements app.SignalView.
func (m *Model) Name() string { return "Traces" }

// Capturing implements app.SignalView.
func (m *Model) Capturing() bool { return m.mode != modeNormal }

// Init implements app.SignalView.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.search()}
	if m.trace != nil {
		cmds = append(cmds, m.fetchTrace(m.trace.id))
	}
	return tea.Batch(cmds...)
}

// Update implements app.SignalView.
func (m *Model) Update(msg tea.Msg) (app.SignalView, tea.Cmd) {
	switch msg := msg.(type) {
	case app.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		if m.pager != nil && m.timelineColumns() != m.histCols {
			return m, m.fetchHistogram()
		}
		return m, nil
	case pageMsg:
		if msg.view == m.id {
			return m, m.onPage(msg)
		}
	case histMsg:
		if msg.view == m.id && msg.id == m.searchID && msg.cols == m.histCols && msg.err == nil {
			m.hist, m.histFrom, m.histTo = msg.series, msg.from, msg.to
		}
	case traceMsg:
		if msg.view == m.id {
			return m, m.onTrace(msg)
		}
	case topologyMsg:
		if msg.view == m.id {
			m.onTopology(msg)
		}
	case tea.MouseClickMsg:
		return m, m.onClick(msg.Mouse())
	case tea.MouseMotionMsg:
		m.onMouseMove(msg.Mouse())
	case tea.MouseReleaseMsg:
		return m, m.onDragEnd(msg.Mouse())
	case tea.MouseWheelMsg:
		m.onWheel(msg.Mouse())
	case tea.KeyPressMsg:
		if m.mode != modeNormal {
			return m, m.updateInput(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) search() tea.Cmd {
	m.searchID++
	m.pager = engine.NewPager(m.opts.Source, obs.Query{Text: m.query, Range: m.rng, Limit: m.opts.PageSize, Order: m.order},
		m.opts.Now(), traces.Trace.Fingerprint)
	m.items, m.err, m.hist = nil, nil, nil
	m.cursor, m.top = 0, 0
	m.loading = true
	return tea.Batch(m.fetchPage(), m.fetchHistogram(), m.fetchTopology())
}

func (m *Model) fetchPage() tea.Cmd {
	id, p, view := m.searchID, m.pager, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		began := time.Now()
		items, warnings, err := p.Next(ctx)
		return pageMsg{owner: owner{view}, id: id, items: items, warnings: warnings, err: err, elapsed: time.Since(began)}
	}
}

func (m *Model) fetchHistogram() tea.Cmd {
	if m.opts.Histogram == nil || m.width == 0 || m.pager == nil {
		return nil
	}
	q := m.pager.Query()
	from, to := q.Range.Resolve(m.opts.Now())
	cols := m.timelineColumns()
	m.histCols = cols
	id, h, view := m.searchID, m.opts.Histogram, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		series, err := h.Histogram(ctx, q, cols)
		return histMsg{owner: owner{view}, id: id, cols: cols, from: from, to: to, series: series, err: err}
	}
}

func (m *Model) onPage(msg pageMsg) tea.Cmd {
	if msg.id != m.searchID {
		return nil
	}
	m.loading = false
	if msg.err != nil {
		m.err = msg.err
		return app.Notify(app.LevelError, "trace search failed: %v", msg.err)
	}
	m.elapsed = msg.elapsed
	m.items = append(m.items, msg.items...)
	cmds := make([]tea.Cmd, 0, len(msg.warnings)+1)
	for _, w := range msg.warnings {
		cmds = append(cmds, app.Notify(app.LevelWarn, "%s", w))
	}
	return tea.Batch(append(cmds, m.maybeLoadMore())...)
}

func (m *Model) maybeLoadMore() tea.Cmd {
	if m.loading || m.pager == nil || m.pager.Done() || m.err != nil || len(m.items)-m.cursor > max(loadAhead, m.listRows()) {
		return nil
	}
	m.loading = true
	return m.fetchPage()
}

// Status implements app.SignalView.
func (m *Model) Status() string {
	if m.loading {
		return m.th.Dim.Render("loading…")
	}
	return ""
}

// Help implements app.SignalView.
func (m *Model) Help() [][]key.Binding { return m.keys.help() }

// ShortHelp implements app.SignalView.
func (m *Model) ShortHelp() []key.Binding {
	k := m.keys
	switch {
	case m.mode != modeNormal:
		return []key.Binding{k.Submit, k.Cancel}
	case m.trace != nil:
		return []key.Binding{short(k.Down, "span"), k.Logs, k.Copy, short(k.Close, "back to traces")}
	case m.topo != nil:
		return []key.Binding{short(k.Down, "service"), short(k.Open, "traces from it"), short(k.Logs, "its logs"), short(k.Layout, "map/table"), short(k.Close, "back")}
	}
	b := []key.Binding{k.Query, short(k.Range, "range"), short(k.Open, "waterfall"), k.Sort, k.Logs, short(k.Map, "service map")}
	if len(m.zoomStack) > 0 {
		b = append(b, k.ZoomOut)
	}
	return b
}

func short(b key.Binding, desc string) key.Binding {
	b.SetHelp(b.Help().Key, desc)
	return b
}

func (m *Model) location() *time.Location {
	if m.utc {
		return time.UTC
	}
	return time.Local
}

// Selected returns the selected trace, or a zero trace when the list is empty.
func (m *Model) Selected() traces.Trace {
	if len(m.items) == 0 {
		return traces.Trace{}
	}
	return m.items[m.cursor]
}
