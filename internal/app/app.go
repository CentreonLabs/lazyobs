// Package app is the root Bubble Tea model: it lays out the header, the
// active signal view and the status bar, routes messages and owns global
// keys. Signals register as SignalView implementations.
package app

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/ui/keys"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
)

// SignalView is one signal's screen (logs, traces, metrics...).
type SignalView interface {
	// Name is shown in the header tabs.
	Name() string
	Init() tea.Cmd
	Update(msg tea.Msg) (SignalView, tea.Cmd)
	// View renders the body of the screen within the given size.
	View(width, height int) string
	// Help lists the view's key bindings, grouped in columns.
	Help() [][]key.Binding
	// ShortHelp lists the few bindings shown in the status bar, which may
	// depend on the view's state.
	ShortHelp() []key.Binding
	// Capturing reports whether a text input has focus: global keys are then
	// passed to the view instead of being interpreted.
	Capturing() bool
	// Status is shown on the right of the status bar.
	Status() string
}

// Badger is implemented by views that show a short summary next to their
// header tab, such as the number of failing alerts.
type Badger interface {
	Badge() string
}

// Views also receive tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg
// (pointer moves, with or without a button held) and tea.MouseReleaseMsg, with coordinates relative
// to their body (0,0 is the top-left cell of the body).

// Tab is an entry of the header. A nil View marks a signal not available yet.
type Tab struct {
	Title string
	View  SignalView
}

// Level qualifies a notification.
type Level int

// Notification levels.
const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// NotifyMsg shows a message in the status bar.
type NotifyMsg struct {
	Level Level
	Text  string
}

// Notify returns a command displaying a message in the status bar.
func Notify(level Level, format string, args ...any) tea.Cmd {
	return func() tea.Msg { return NotifyMsg{Level: level, Text: fmt.Sprintf(format, args...)} }
}

// RouteMsg switches to the signal named Signal (its tab title) and delivers
// Msg to its view, e.g. to open a trace from a log.
type RouteMsg struct {
	Signal string
	Msg    tea.Msg
}

// Route returns a command routing msg to a signal.
func Route(signal string, msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return RouteMsg{Signal: signal, Msg: msg} }
}

// ResizeMsg gives views the size available for their body.
type ResizeMsg struct{ Width, Height int }

// chromeHeight is the number of lines used by the header and the status bar.
const chromeHeight = 2

type clearNotifyMsg struct{ id int }

const notifyDuration = 6 * time.Second

// Model is the root model.
type Model struct {
	tabs    []Tab
	active  int
	context string // "context · org" shown in the header
	theme   *theme.Theme
	keys    keys.Global
	help    help.Model

	width, height int
	showHelp      bool

	notice   NotifyMsg
	noticeID int

	signalHits [][2]int  // header columns [from, to) of each signal tab
	startup    []tea.Cmd // notifications queued before the program starts
}

// New returns the root model. The first tab with a view is active.
func New(context string, th *theme.Theme, tabs ...Tab) *Model {
	m := &Model{tabs: tabs, context: context, theme: th, keys: keys.DefaultGlobal(), help: help.New()}
	m.help.Styles = help.DefaultStyles(th.Dark)
	for i, t := range tabs {
		if t.View != nil {
			m.active = i
			break
		}
	}
	return m
}

// Notify shows a message in the status bar once the program starts.
func (m *Model) Notify(level Level, format string, args ...any) {
	m.startup = append(m.startup, Notify(level, format, args...))
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	cmds := append([]tea.Cmd{tea.RequestBackgroundColor}, m.startup...)
	for _, t := range m.tabs {
		if t.View != nil {
			cmds = append(cmds, t.View.Init())
		}
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		return m, m.broadcast(ResizeMsg{Width: msg.Width, Height: max(msg.Height-chromeHeight, 1)})

	case tea.BackgroundColorMsg:
		*m.theme = *theme.New(msg.IsDark()) // views share the pointer
		m.help.Styles = help.DefaultStyles(m.theme.Dark)
		return m, nil

	case RouteMsg:
		for i, t := range m.tabs {
			if t.View != nil && strings.EqualFold(t.Title, msg.Signal) {
				m.active, m.showHelp = i, false
				return m, m.updateActive(msg.Msg)
			}
		}
		return m, Notify(LevelWarn, "%s are not available", strings.ToLower(msg.Signal))

	case NotifyMsg:
		m.noticeID++
		m.notice = msg
		id := m.noticeID
		return m, tea.Tick(notifyDuration, func(time.Time) tea.Msg { return clearNotifyMsg{id} })

	case clearNotifyMsg:
		if msg.id == m.noticeID {
			m.notice = NotifyMsg{}
		}
		return m, nil

	case tea.MouseClickMsg:
		if mouse := msg.Mouse(); mouse.Y == 0 && mouse.Button == tea.MouseLeft {
			return m, m.clickHeader(mouse.X)
		}
		if mouse, ok := m.bodyMouse(msg.Mouse()); ok {
			return m, m.updateActive(tea.MouseClickMsg(mouse))
		}
		return m, nil

	case tea.MouseWheelMsg:
		if mouse, ok := m.bodyMouse(msg.Mouse()); ok {
			return m, m.updateActive(tea.MouseWheelMsg(mouse))
		}
		return m, nil

	// Drags may end anywhere, even outside the body: always forward them.
	case tea.MouseMotionMsg:
		if mouse := msg.Mouse(); !m.showHelp {
			mouse.Y--
			return m, m.updateActive(tea.MouseMotionMsg(mouse))
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if mouse := msg.Mouse(); !m.showHelp {
			mouse.Y--
			return m, m.updateActive(tea.MouseReleaseMsg(mouse))
		}
		return m, nil

	case tea.KeyPressMsg:
		if cmd, handled := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		if m.showHelp {
			return m, nil
		}
		return m, m.updateActive(msg)
	}

	// Other messages (async results, ticks) go to every view: a background
	// tab may still have requests in flight.
	return m, m.broadcast(msg)
}

func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.tabs {
		if m.tabs[i].View == nil {
			continue
		}
		v, cmd := m.tabs[i].View.Update(msg)
		m.tabs[i].View = v
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if key.Matches(msg, m.keys.ForceQ) {
		return tea.Quit, true
	}
	if v := m.activeView(); v != nil && v.Capturing() {
		return nil, false
	}
	switch {
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return nil, true
	case m.showHelp && msg.String() == "esc":
		m.showHelp = false
		return nil, true
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit, true
	case key.Matches(msg, m.keys.Signal1, m.keys.Signal2, m.keys.Signal3, m.keys.Signal4):
		i := int(msg.String()[0] - '1')
		if i >= len(m.tabs) || m.tabs[i].View == nil {
			return Notify(LevelInfo, "%s are not available yet", strings.ToLower(m.tabNameAt(i))), true
		}
		m.active = i
		return nil, true
	}
	return nil, false
}

// bodyMouse converts screen coordinates to the view body's (below the
// header). Events outside the body, or while the help is shown, are dropped.
func (m *Model) bodyMouse(mouse tea.Mouse) (tea.Mouse, bool) {
	mouse.Y-- // header line
	if m.showHelp || mouse.Y < 0 || mouse.Y >= m.height-chromeHeight {
		return mouse, false
	}
	return mouse, true
}

// clickHeader shows the signal whose tab is under column x.
func (m *Model) clickHeader(x int) tea.Cmd {
	for i, h := range m.signalHits {
		if x < h[0] || x >= h[1] {
			continue
		}
		if m.tabs[i].View == nil {
			return Notify(LevelInfo, "%s are not available yet", strings.ToLower(m.tabs[i].Title))
		}
		m.active, m.showHelp = i, false
		return nil
	}
	return nil
}

// Active is the index of the shown signal.
func (m *Model) Active() int { return m.active }

// SetActive shows signal i if it has a view (e.g. when restoring a session).
func (m *Model) SetActive(i int) {
	if i >= 0 && i < len(m.tabs) && m.tabs[i].View != nil {
		m.active = i
	}
}

func (m *Model) tabNameAt(i int) string {
	if i < len(m.tabs) {
		return m.tabs[i].Title
	}
	return "this signal"
}

func (m *Model) activeView() SignalView {
	if m.active < len(m.tabs) {
		return m.tabs[m.active].View
	}
	return nil
}

func (m *Model) updateActive(msg tea.Msg) tea.Cmd {
	v := m.activeView()
	if v == nil {
		return nil
	}
	v, cmd := v.Update(msg)
	m.tabs[m.active].View = v
	return cmd
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	// Clicks, wheel and pointer moves (for hover tooltips) are reported to
	// views; terminals keep shift+drag for native text selection.
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "lazyobs"
	return v
}

func (m *Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "" // wait for the first WindowSizeMsg
	}
	header := m.renderHeader()
	status := m.renderStatus()
	bodyHeight := max(m.height-chromeHeight, 1)

	var body string
	switch v := m.activeView(); {
	case m.showHelp:
		body = m.renderHelp(bodyHeight)
	case v != nil:
		body = v.View(m.width, bodyHeight)
	}
	body = lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
}

func (m *Model) renderHeader() string {
	t := m.theme
	parts := []string{t.Title.Render(" lazyobs ")}
	x := lipgloss.Width(parts[0])
	m.signalHits = m.signalHits[:0]
	for i, tab := range m.tabs {
		label := fmt.Sprintf("%d %s", i+1, tab.Title)
		var part string
		switch {
		case tab.View == nil:
			part = t.Disabled.Render(label)
		case i == m.active:
			part = t.ActiveTab.Render(label)
		default:
			part = t.Tab.Render(label)
		}
		if b, ok := tab.View.(Badger); ok {
			if badge := b.Badge(); badge != "" {
				part += badge + " "
			}
		}
		w := lipgloss.Width(part)
		m.signalHits = append(m.signalHits, [2]int{x, x + w})
		x += w
		parts = append(parts, part)
	}
	left := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	right := t.Dim.Render(m.context + " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderStatus() string {
	t := m.theme
	var left string
	switch m.notice.Level {
	case LevelError:
		left = t.ErrorText.Render("✗ " + m.notice.Text)
	case LevelWarn:
		left = t.WarnText.Render("! " + m.notice.Text)
	default:
		left = t.StatusBar.Render(m.notice.Text)
	}
	if m.notice.Text == "" {
		left = m.help.ShortHelpView(m.shortHelp())
	}
	var right string
	if v := m.activeView(); v != nil {
		right = v.Status()
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(m.width-lipgloss.Width(right)-1, 0), "…")
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) shortHelp() []key.Binding {
	var b []key.Binding
	if v := m.activeView(); v != nil {
		b = append(b, v.ShortHelp()...)
	}
	return append(b, m.keys.ShortHelp()...)
}

func (m *Model) renderHelp(height int) string {
	groups := m.keys.FullHelp()
	if v := m.activeView(); v != nil {
		groups = append(v.Help(), groups...)
	}
	h := m.help
	h.ShowAll = true
	content := lipgloss.JoinVertical(lipgloss.Left,
		m.theme.Title.Render("Keyboard and mouse shortcuts"),
		"",
		h.FullHelpView(groups),
		"",
		m.theme.Dim.Render("Press ? or esc to close."),
	)
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, content)
}
