// Package logsview is the logs screen: query bar, volume histogram,
// infinitely scrolling list, detail panel and live mode.
package logsview

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
)

const (
	defaultPageSize = 200
	requestTimeout  = 60 * time.Second
	// loadAhead triggers the next page when the selection gets this close to the end.
	loadAhead = 50
)

// DefaultRange is the time range of a new session.
var DefaultRange = obs.Last(15 * time.Minute)

var rangePresets = []string{"5m", "15m", "1h", "6h", "24h", "7d", "30d"}

// Options configures the view.
type Options struct {
	Source    obs.Searcher[logs.Record]
	Histogram obs.Histogrammer // optional
	Theme     *theme.Theme
	Query     string
	Range     obs.TimeRange // DefaultRange when zero
	PageSize  int
	// LiveInterval is the polling period of live mode (engine default when zero).
	LiveInterval time.Duration
	// Now is the clock (time.Now when nil).
	Now func() time.Time
	// After schedules msg after d (tea.Tick when nil); tests replace it.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
}

type (
	focusArea int
	inputMode int
)

const (
	focusList focusArea = iota
	focusDetail
)

const (
	modeNormal inputMode = iota
	modeQuery
	modeRange
)

// Model is the logs view.
type Model struct {
	opts Options
	th   *theme.Theme
	keys keyMap

	width, height int

	query string
	rng   obs.TimeRange

	// Results of the current search.
	searchID int
	pager    *engine.Pager[logs.Record]
	records  []logs.Record
	loading  bool
	err      error
	elapsed  time.Duration
	hist     []obs.Series
	histFrom time.Time
	histTo   time.Time
	histCols int // number of buckets last requested

	cursor, top int

	showDetail bool
	focus      focusArea
	detail     viewport.Model

	mode      inputMode
	input     textinput.Model
	presetIdx int

	// Live mode.
	live     bool
	tail     *engine.Tail[logs.Record]
	liveGen  int
	liveNew  int
	liveErrs int

	utc     bool
	compact bool            // timeline shown as a one-line sparkline
	hidden  map[string]bool // severities hidden from the timeline

	// Clickable areas, in body coordinates, recorded by the last View.
	hits hitAreas
}

var _ app.SignalView = (*Model)(nil)

// New returns the logs view.
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
	if opts.After == nil {
		opts.After = func(d time.Duration, msg tea.Msg) tea.Cmd {
			return tea.Tick(d, func(time.Time) tea.Msg { return msg })
		}
	}
	in := textinput.New()
	in.Prompt = ""
	return &Model{
		opts:   opts,
		th:     opts.Theme,
		keys:   defaultKeys(),
		query:  opts.Query,
		rng:    opts.Range,
		detail: viewport.New(),
		input:  in,
		hidden: map[string]bool{},
	}
}

// Name implements app.SignalView.
func (m *Model) Name() string { return "Logs" }

// Help implements app.SignalView.
func (m *Model) Help() [][]key.Binding { return m.keys.help() }

// Capturing implements app.SignalView.
func (m *Model) Capturing() bool { return m.mode != modeNormal }

// Init implements app.SignalView.
func (m *Model) Init() tea.Cmd { return m.search() }

// Messages of asynchronous work, tagged with the search or live generation
// they belong to so stale results are dropped.
type (
	pageMsg struct {
		id       int
		items    []logs.Record
		warnings []string
		err      error
		elapsed  time.Duration
	}
	histMsg struct {
		id       int
		cols     int // number of buckets requested
		from, to time.Time
		series   []obs.Series
		err      error
	}
	tailTickMsg struct{ gen int }
	tailMsg     struct {
		gen    int
		update engine.TailUpdate[logs.Record]
		err    error
		delay  time.Duration
	}
)

// Update implements app.SignalView.
func (m *Model) Update(msg tea.Msg) (app.SignalView, tea.Cmd) {
	switch msg := msg.(type) {
	case app.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutDetail()
		m.scrollToCursor()
		if m.pager != nil && m.timelineColumns() != m.histCols {
			return m, m.fetchHistogram() // match the new width
		}
		return m, nil
	case pageMsg:
		return m, m.onPage(msg)
	case histMsg:
		// Only the latest request counts: an older one (e.g. sized before the
		// window size was known) may complete after it.
		if msg.id == m.searchID && msg.cols == m.histCols && msg.err == nil {
			m.hist, m.histFrom, m.histTo = msg.series, msg.from, msg.to
		}
		return m, nil
	case tailTickMsg:
		if msg.gen != m.liveGen || m.tail == nil {
			return m, nil
		}
		return m, m.poll()
	case tailMsg:
		return m, m.onTail(msg)
	case tea.MouseClickMsg:
		return m, m.onClick(msg.Mouse())
	case tea.MouseWheelMsg:
		return m, m.onWheel(msg.Mouse())
	case tea.KeyPressMsg:
		if m.mode != modeNormal {
			return m, m.updateInput(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

// search starts a new search with the current query and range.
func (m *Model) search() tea.Cmd {
	m.searchID++
	m.stopTail()
	now := m.opts.Now()
	m.pager = engine.NewPager(m.opts.Source, obs.Query{Text: m.query, Range: m.rng, Limit: m.opts.PageSize}, now, logs.Record.Fingerprint)
	m.records, m.hist, m.err = nil, nil, nil
	m.histFrom, m.histTo = time.Time{}, time.Time{}
	m.cursor, m.top, m.liveNew = 0, 0, 0
	m.loading = true
	m.setDetail()
	return tea.Batch(m.fetchPage(), m.fetchHistogram())
}

func (m *Model) fetchPage() tea.Cmd {
	id, p := m.searchID, m.pager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		began := time.Now()
		items, warnings, err := p.Next(ctx)
		return pageMsg{id: id, items: items, warnings: warnings, err: err, elapsed: time.Since(began)}
	}
}

func (m *Model) fetchHistogram() tea.Cmd {
	if m.opts.Histogram == nil || m.width == 0 {
		return nil // sized on the first ResizeMsg
	}
	id, h := m.searchID, m.opts.Histogram
	q := m.pager.Query()
	if m.tail != nil {
		q.Range = m.rng.Pin(m.opts.Now()) // live: the timeline follows the clock
	}
	from, to := q.Range.Resolve(m.opts.Now())
	buckets := m.timelineColumns()
	m.histCols = buckets
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		series, err := h.Histogram(ctx, q, buckets)
		return histMsg{id: id, cols: buckets, from: from, to: to, series: series, err: err}
	}
}

func (m *Model) onPage(msg pageMsg) tea.Cmd {
	if msg.id != m.searchID {
		return nil
	}
	m.loading = false
	first := len(m.records) == 0
	if msg.err != nil {
		m.err = msg.err
		return app.Notify(app.LevelError, "search failed: %v", msg.err)
	}
	m.elapsed = msg.elapsed
	m.records = append(m.records, msg.items...)
	if first {
		m.setDetail()
	}
	var cmds []tea.Cmd
	for _, w := range msg.warnings {
		cmds = append(cmds, app.Notify(app.LevelWarn, "%s", w))
	}
	if first && m.live && m.tail == nil {
		cmds = append(cmds, m.startTail())
	}
	cmds = append(cmds, m.maybeLoadMore())
	return tea.Batch(cmds...)
}

// maybeLoadMore fetches the next page when the selection nears the end.
func (m *Model) maybeLoadMore() tea.Cmd {
	if m.loading || m.pager == nil || m.pager.Done() || m.err != nil {
		return nil
	}
	if len(m.records)-m.cursor > max(loadAhead, m.listRows()) {
		return nil
	}
	m.loading = true
	return m.fetchPage()
}

// Status implements app.SignalView.
func (m *Model) Status() string {
	switch {
	case m.tail != nil && m.tail.Paused():
		return m.th.Badge.Background(m.th.Warning).Render(pausedLabel(m.tail.Buffered()))
	case m.tail != nil && m.liveErrs > 0:
		return m.th.Badge.Background(m.th.Error).Render("LIVE ✗")
	case m.tail != nil:
		return m.th.Badge.Background(m.th.Success).Render("LIVE ●")
	case m.loading:
		return m.th.Dim.Render("loading…")
	}
	return ""
}

func (m *Model) location() *time.Location {
	if m.utc {
		return time.UTC
	}
	return time.Local
}
