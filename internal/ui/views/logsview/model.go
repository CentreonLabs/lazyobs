// Package logsview is the logs screen: query bar, volume histogram,
// infinitely scrolling list, detail panel and live mode.
package logsview

import (
	"context"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/engine"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
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
	Source     obs.Searcher[logs.Record]
	Histogram  obs.Histogrammer    // optional
	Translator obs.QueryTranslator // optional, enables natural-language queries
	Fields     obs.FieldSuggester  // optional, enables query autocompletion
	Theme      *theme.Theme
	Query      string
	Range      obs.TimeRange // DefaultRange when zero
	PageSize   int
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
	modeAsk
)

// Model is the logs view.
type Model struct {
	id   uint64
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

	showDetail    bool
	focus         focusArea
	detail        viewport.Model
	detailEntries []filterEntry // filterable fields of the detail panel
	detailField   int           // selected entry

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

	// Zoom: the span being dragged on the timeline, and the ranges to go
	// back to when zooming out.
	drag      *dragSpan
	zoomStack []obs.TimeRange
	hover     *chart.Cell // pointer over the timeline plot, nil otherwise

	// Query autocompletion.
	compl completion

	// The log this view was opened around (see openContext), 0 if none.
	anchor      uint64
	anchorFound bool
	anchorPages int

	// Natural-language queries.
	lastPrompt  string
	translating bool
	askID       int
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
		id:     nextViewID.Add(1),
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

// ShortHelp implements app.SignalView: the main actions, plus those that
// make sense in the current state.
func (m *Model) ShortHelp() []key.Binding {
	k := m.keys
	switch m.mode {
	case modeQuery:
		switch {
		case m.compl.open() && m.compl.selected >= 0:
			return []key.Binding{short(k.Submit, "complete"), short(k.SuggestNext, "choose"), short(k.Cancel, "close list")}
		case m.compl.open():
			return []key.Binding{short(k.Submit, "search"), short(k.SuggestNext, "choose"), k.Accept, short(k.Cancel, "close list")}
		}
		return []key.Binding{short(k.Submit, "search"), k.Cancel}
	case modeRange:
		return []key.Binding{k.NextPreset, k.Submit, k.Cancel}
	case modeAsk:
		return []key.Binding{short(k.Submit, "ask"), k.Cancel}
	}
	if m.detailFocused() {
		return []key.Binding{
			short(k.FilterIn, "filter on field"), short(k.FilterOut, "exclude"),
			short(k.Down, "field"), short(k.Focus, "list"), short(k.Context, "around"), k.Close,
		}
	}
	b := []key.Binding{k.Query}
	if m.opts.Translator != nil {
		b = append(b, short(k.Ask, "ask"))
	}
	b = append(b, short(k.Range, "range"), k.Detail)
	if m.showDetail {
		b = append(b, short(k.Context, "around"))
	}
	b = append(b, short(k.Live, "live"))
	if m.tail != nil {
		b = append(b, short(k.Pause, "pause"))
	}
	if len(m.zoomStack) > 0 {
		b = append(b, k.ZoomOut)
	}
	if !m.rng.IsRelative() {
		b = append(b, short(k.Earlier, "earlier"), short(k.Later, "later"))
	}
	return b
}

// short relabels a binding for the status bar; the help screen keeps the
// full description.
func short(b key.Binding, desc string) key.Binding {
	b.SetHelp(b.Help().Key, desc)
	return b
}

// Capturing implements app.SignalView.
func (m *Model) Capturing() bool { return m.mode != modeNormal }

// Init implements app.SignalView.
func (m *Model) Init() tea.Cmd { return m.search() }

// nextViewID numbers view instances: several logs views (tabs) receive the
// same broadcast messages and must only handle their own.
var nextViewID atomic.Uint64

// owner tags asynchronous messages with the view that issued them.
type owner struct{ view uint64 }

func (o owner) ownerID() uint64 { return o.view }

type ownedMsg interface{ ownerID() uint64 }

// Messages of asynchronous work, tagged with their view and with the search
// or live generation they belong to so stale results are dropped.
type (
	pageMsg struct {
		owner
		id       int
		items    []logs.Record
		warnings []string
		err      error
		elapsed  time.Duration
	}
	histMsg struct {
		owner
		id       int
		cols     int // number of buckets requested
		from, to time.Time
		series   []obs.Series
		err      error
	}
	tailTickMsg struct {
		owner
		gen int
	}
	translateMsg struct {
		owner
		id     int
		prompt string
		result obs.Translation
		err    error
	}
	tailMsg struct {
		owner
		gen    int
		update engine.TailUpdate[logs.Record]
		err    error
		delay  time.Duration
	}
)

// Update implements app.SignalView.
func (m *Model) Update(msg tea.Msg) (app.SignalView, tea.Cmd) {
	if o, ok := msg.(ownedMsg); ok && o.ownerID() != m.id {
		return m, nil // another view's result
	}
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
	case translateMsg:
		return m, m.onTranslate(msg)
	case fieldsMsg:
		return m, m.onFields(msg)
	case suggestTickMsg:
		return m, m.onSuggestTick(msg)
	case valuesMsg:
		return m, m.onValues(msg)
	case tea.MouseMotionMsg:
		m.onMouseMove(msg.Mouse())
		return m, nil
	case tea.MouseReleaseMsg:
		return m, m.onDragEnd(msg.Mouse())
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
	id, p, viewID := m.searchID, m.pager, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		began := time.Now()
		items, warnings, err := p.Next(ctx)
		return pageMsg{owner: owner{viewID}, id: id, items: items, warnings: warnings, err: err, elapsed: time.Since(began)}
	}
}

func (m *Model) fetchHistogram() tea.Cmd {
	if m.opts.Histogram == nil || m.width == 0 {
		return nil // sized on the first ResizeMsg
	}
	id, h, viewID := m.searchID, m.opts.Histogram, m.id
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
		return histMsg{owner: owner{viewID}, id: id, cols: buckets, from: from, to: to, series: series, err: err}
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
	if cmd := m.findAnchor(); cmd != nil {
		return tea.Batch(append(cmds, cmd)...)
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
	case m.drag != nil && m.drag.to > m.drag.from:
		return m.th.Key.Render(m.dragLabel())
	case m.tail != nil && m.tail.Paused():
		return m.th.Badge.Background(m.th.Warning).Render(pausedLabel(m.tail.Buffered()))
	case m.tail != nil && m.liveErrs > 0:
		return m.th.Badge.Background(m.th.Error).Render("LIVE ✗")
	case m.tail != nil:
		return m.th.Badge.Background(m.th.Success).Render("LIVE ●")
	case m.translating:
		return m.th.Dim.Render("translating…")
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
