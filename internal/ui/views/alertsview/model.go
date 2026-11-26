// Package alertsview is the alerts screen: every alert rule with its current
// status, worst first, refreshed periodically, and the details of a rule
// (queries, thresholds, last value) with a shortcut to the logs it watches.
package alertsview

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
	"github.com/CentreonLabs/lazyobs/internal/ui/inputs"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
)

const (
	requestTimeout = 30 * time.Second
	// DefaultRefresh is how often the statuses are read again. Rules are
	// evaluated every few minutes; this only shortens the wait for news.
	DefaultRefresh = 30 * time.Second
	// minLogsWindow is the shortest time range shown by the logs of a rule.
	minLogsWindow = time.Hour
)

// Options configures the view.
type Options struct {
	Source  alerts.Reader
	Theme   *theme.Theme
	Refresh time.Duration // DefaultRefresh when zero
	// OpenLogs shows the logs matching query over rng (in the logs signal).
	OpenLogs func(query string, rng obs.TimeRange) tea.Cmd
	Now      func() time.Time
	// After schedules msg after d (tea.Tick when nil); tests replace it.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
}

// Model is the alerts view.
type Model struct {
	id   uint64
	opts Options
	th   *theme.Theme
	keys keyMap

	width, height int

	all      []alerts.Alert // as read, worst first
	items    []alerts.Alert // after the filters
	loaded   bool
	loading  bool
	err      error
	readAt   time.Time
	reqID    int
	tickGen  int
	warnings map[string]bool // already reported

	cursor, top int

	filter       string
	problemsOnly bool
	filtering    bool
	input        textinput.Model

	// Details panel: shown for the selected rule when open.
	detailOpen bool
	details    map[string]detailEntry // by rule ID + event ID
	pendingLog string                 // rule whose logs open once its details arrive
	// restoreRule is the rule to select once the alerts are read (session).
	restoreRule string

	utc bool
}

type detailEntry struct {
	details alerts.Details
	err     error
	loading bool
}

var _ app.SignalView = (*Model)(nil)

var nextViewID atomic.Uint64

// New returns the alerts view.
func New(opts Options) *Model {
	if opts.Refresh <= 0 {
		opts.Refresh = DefaultRefresh
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.After == nil {
		opts.After = func(d time.Duration, msg tea.Msg) tea.Cmd {
			return tea.Tick(d, func(time.Time) tea.Msg { return msg })
		}
	}
	return &Model{
		id: nextViewID.Add(1), opts: opts, th: opts.Theme, keys: defaultKeys(), input: inputs.New(),
		details: map[string]detailEntry{}, warnings: map[string]bool{},
	}
}

type owner struct{ view uint64 }

type (
	overviewMsg struct {
		owner
		id       int
		overview alerts.Overview
		err      error
	}
	detailsMsg struct {
		owner
		key     string
		details alerts.Details
		err     error
	}
	tickMsg struct {
		owner
		gen int
	}
)

// Name implements app.SignalView.
func (m *Model) Name() string { return "Alerts" }

// Capturing implements app.SignalView.
func (m *Model) Capturing() bool { return m.filtering }

// Init implements app.SignalView.
func (m *Model) Init() tea.Cmd { return tea.Batch(m.load(), m.schedule()) }

func (m *Model) load() tea.Cmd {
	m.reqID++
	m.loading = true
	id, src, view := m.reqID, m.opts.Source, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		ov, err := src.Alerts(ctx)
		return overviewMsg{owner: owner{view}, id: id, overview: ov, err: err}
	}
}

// schedule plans the next refresh; a new generation cancels older ticks.
func (m *Model) schedule() tea.Cmd {
	m.tickGen++
	return m.opts.After(m.opts.Refresh, tickMsg{owner: owner{m.id}, gen: m.tickGen})
}

// Update implements app.SignalView.
func (m *Model) Update(msg tea.Msg) (app.SignalView, tea.Cmd) {
	switch msg := msg.(type) {
	case app.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
	case overviewMsg:
		if msg.view == m.id && msg.id == m.reqID {
			return m, m.onOverview(msg)
		}
	case detailsMsg:
		if msg.view == m.id {
			return m, m.onDetails(msg)
		}
	case tickMsg:
		if msg.view == m.id && msg.gen == m.tickGen {
			return m, tea.Batch(m.load(), m.schedule())
		}
	case tea.MouseClickMsg:
		return m, m.onClick(msg.Mouse())
	case tea.MouseWheelMsg:
		m.onWheel(msg.Mouse())
		return m, m.fetchDetails()
	case tea.KeyPressMsg:
		if m.filtering {
			return m, m.updateFilter(msg)
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) onOverview(msg overviewMsg) tea.Cmd {
	m.loading = false
	if msg.err != nil {
		m.err = msg.err
		return app.Notify(app.LevelError, "could not read the alerts: %v", msg.err)
	}
	selected := m.Selected().Rule.ID
	if m.restoreRule != "" {
		selected, m.restoreRule = m.restoreRule, ""
	}
	m.err, m.loaded, m.readAt = nil, true, m.opts.Now()
	m.all = msg.overview.Alerts
	m.applyFilters(selected)
	var cmds []tea.Cmd
	for _, w := range msg.overview.Warnings {
		if !m.warnings[w] { // a refresh must not repeat them every 30 s
			m.warnings[w] = true
			cmds = append(cmds, app.Notify(app.LevelWarn, "%s", w))
		}
	}
	return tea.Batch(append(cmds, m.fetchDetails())...)
}

// applyFilters rebuilds the shown list, keeping the selected rule if possible.
func (m *Model) applyFilters(selected string) {
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	m.items = m.items[:0]
	for _, a := range m.all {
		if m.problemsOnly && !problem(a) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(a.Rule.Name+" "+a.Rule.Description), needle) {
			continue
		}
		m.items = append(m.items, a)
	}
	m.cursor = min(m.cursor, max(len(m.items)-1, 0))
	for i, a := range m.items {
		if a.Rule.ID == selected {
			m.cursor = i
			break
		}
	}
	m.scrollToCursor()
}

// problem reports whether an alert needs attention.
func problem(a alerts.Alert) bool {
	return a.Rule.Enabled && (a.Status() != alerts.StatusOK || a.Failing())
}

// Selected returns the selected alert, or a zero alert when the list is empty.
func (m *Model) Selected() alerts.Alert {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return alerts.Alert{}
	}
	return m.items[m.cursor]
}

func detailKey(a alerts.Alert) string {
	if a.Event == nil {
		return a.Rule.ID
	}
	return a.Rule.ID + "/" + a.Event.ID
}

// fetchDetails loads the details of the selected rule when they are shown
// (or needed) and not known yet.
func (m *Model) fetchDetails() tea.Cmd {
	a := m.Selected()
	if a.Rule.ID == "" || (!m.detailOpen && m.pendingLog != a.Rule.ID) {
		return nil
	}
	k := detailKey(a)
	if _, ok := m.details[k]; ok {
		return nil
	}
	m.details[k] = detailEntry{loading: true}
	src, view := m.opts.Source, m.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		d, err := src.Details(ctx, a)
		return detailsMsg{owner: owner{view}, key: k, details: d, err: err}
	}
}

func (m *Model) onDetails(msg detailsMsg) tea.Cmd {
	m.details[msg.key] = detailEntry{details: msg.details, err: msg.err}
	if msg.err != nil {
		delete(m.details, msg.key) // retry on the next refresh
		m.pendingLog = ""
		return app.Notify(app.LevelError, "could not read the alert rule: %v", msg.err)
	}
	if a := m.Selected(); m.pendingLog != "" && m.pendingLog == a.Rule.ID && detailKey(a) == msg.key {
		m.pendingLog = ""
		return m.openLogs(a, msg.details)
	}
	return nil
}

// Status implements app.SignalView.
func (m *Model) Status() string {
	if m.loading && !m.loaded {
		return m.th.Dim.Render("loading…")
	}
	return ""
}

// Badge implements app.Badger: the number of rules in trouble.
func (m *Model) Badge() string {
	var bad, warn int
	for _, a := range m.all {
		if !a.Rule.Enabled || a.Downtime != "" || a.Ack != nil {
			continue
		}
		switch a.Status() {
		case alerts.StatusCritical, alerts.StatusError:
			bad++
		case alerts.StatusWarning:
			warn++
		}
	}
	var parts []string
	if bad > 0 {
		parts = append(parts, m.th.ErrorText.Bold(true).Render("●"+itoa(bad)))
	}
	if warn > 0 {
		parts = append(parts, m.th.WarnText.Render("●"+itoa(warn)))
	}
	return strings.Join(parts, " ")
}

// Help implements app.SignalView.
func (m *Model) Help() [][]key.Binding { return m.keys.help() }

// ShortHelp implements app.SignalView.
func (m *Model) ShortHelp() []key.Binding {
	k := m.keys
	if m.filtering {
		return []key.Binding{k.Submit, k.Cancel}
	}
	details := short(k.Details, "details")
	if m.detailOpen {
		details = short(k.Close, "hide details")
	}
	problems := short(k.Problems, "problems only")
	if m.problemsOnly {
		problems = short(k.Problems, "all rules")
	}
	return []key.Binding{short(k.Down, "rule"), details, k.Logs, k.Filter, problems, k.Refresh}
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
