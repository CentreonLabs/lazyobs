// Package tabset holds several instances of a signal view as tabs (e.g.
// several log searches, each with its own query and time range), with a tab
// bar above them. It is itself an app.SignalView.
package tabset

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
)

const (
	// MaxTabs bounds the number of tabs.
	MaxTabs  = 9
	barLines = 1
	// minTitle is the width below which titles are not shortened further.
	minTitle = 8
	maxTitle = 32
)

// Child is a view that can live in a tab.
type Child interface {
	app.SignalView
	// Title is the tab label.
	Title() string
	// Clone returns a new view starting from this one's state (query, time
	// range, display preferences), for a new tab.
	Clone() Child
}

// Model is a set of tabs.
type Model struct {
	name   string
	tabs   []Child
	active int
	th     *theme.Theme
	keys   keyMap

	width, height int
	hits          []hit // clickable parts of the tab bar
}

type hitKind int

const (
	hitTab hitKind = iota
	hitClose
	hitNew
)

type hit struct {
	kind   hitKind
	tab    int
	x0, x1 int
}

type keyMap struct {
	New, Close, Next, Prev key.Binding
}

var _ app.SignalView = (*Model)(nil)

// New returns a tab set named name (shown in the header) with one tab.
func New(name string, th *theme.Theme, first Child) *Model {
	return NewWithTabs(name, th, []Child{first}, 0)
}

// NewWithTabs returns a tab set with several tabs (at least one), the
// active one being selected (e.g. when restoring a session).
func NewWithTabs(name string, th *theme.Theme, tabs []Child, active int) *Model {
	if len(tabs) > MaxTabs {
		tabs = tabs[:MaxTabs]
	}
	return &Model{
		name:   name,
		tabs:   tabs,
		active: min(max(active, 0), len(tabs)-1),
		th:     th,
		keys: keyMap{
			New:   key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "new tab from this one")),
			Close: key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "close tab")),
			Next:  key.NewBinding(key.WithKeys(">"), key.WithHelp(">", "next tab")),
			Prev:  key.NewBinding(key.WithKeys("<"), key.WithHelp("<", "previous tab")),
		},
	}
}

// Name implements app.SignalView.
func (m *Model) Name() string { return m.name }

// Init implements app.SignalView: every tab starts its search.
func (m *Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs))
	for _, t := range m.tabs {
		cmds = append(cmds, t.Init())
	}
	return tea.Batch(cmds...)
}

// Len is the number of tabs.
func (m *Model) Len() int { return len(m.tabs) }

// Active is the index of the selected tab.
func (m *Model) Active() int { return m.active }

// Tab returns the view of tab i.
func (m *Model) Tab(i int) Child { return m.tabs[i] }

// Tabs returns the views of every tab, in order.
func (m *Model) Tabs() []Child { return append([]Child(nil), m.tabs...) }

// Update implements app.SignalView.
func (m *Model) Update(msg tea.Msg) (app.SignalView, tea.Cmd) {
	switch msg := msg.(type) {
	case app.ResizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.broadcast(m.childSize())

	case tea.KeyPressMsg:
		if !m.current().Capturing() {
			if cmd, ok := m.handleKey(msg); ok {
				return m, cmd
			}
		}
		return m, m.updateActive(msg)

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Y < barLines {
			return m, m.onBarClick(mouse)
		}
		mouse.Y -= barLines
		return m, m.updateActive(tea.MouseClickMsg(mouse))

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Y < barLines {
			return m, nil
		}
		mouse.Y -= barLines
		return m, m.updateActive(tea.MouseWheelMsg(mouse))

	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		mouse.Y -= barLines
		return m, m.updateActive(tea.MouseMotionMsg(mouse))

	case tea.MouseReleaseMsg:
		mouse := msg.Mouse()
		mouse.Y -= barLines
		return m, m.updateActive(tea.MouseReleaseMsg(mouse))
	}
	// Async results and ticks: every tab may have work in flight (e.g. a live
	// tail running in the background).
	return m, m.broadcast(msg)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.New):
		return m.newTab(), true
	case key.Matches(msg, m.keys.Close):
		return m.closeTab(m.active), true
	case key.Matches(msg, m.keys.Next):
		m.active = (m.active + 1) % len(m.tabs)
		return nil, true
	case key.Matches(msg, m.keys.Prev):
		m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
		return nil, true
	}
	return nil, false
}

// newTab opens a copy of the current tab right after it, with the query
// input open so the new search can be typed right away.
func (m *Model) newTab() tea.Cmd {
	if len(m.tabs) >= MaxTabs {
		return app.Notify(app.LevelWarn, "at most %d tabs: close one with ctrl+w", MaxTabs)
	}
	tab := m.current().Clone()
	m.active++
	m.tabs = append(m.tabs[:m.active], append([]Child{tab}, m.tabs[m.active:]...)...)
	var cmds []tea.Cmd
	cmds = append(cmds, tab.Init())
	v, cmd := tab.Update(m.childSize())
	m.tabs[m.active] = v.(Child)
	cmds = append(cmds, cmd)
	v, cmd = m.tabs[m.active].Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.tabs[m.active] = v.(Child)
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

func (m *Model) closeTab(i int) tea.Cmd {
	if len(m.tabs) == 1 {
		return app.Notify(app.LevelInfo, "this is the last tab")
	}
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	if m.active > i || m.active == len(m.tabs) {
		m.active--
	}
	return nil
}

func (m *Model) onBarClick(mouse tea.Mouse) tea.Cmd {
	if mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseMiddle {
		return nil
	}
	for _, h := range m.hits {
		if mouse.X < h.x0 || mouse.X >= h.x1 {
			continue
		}
		switch {
		case h.kind == hitNew:
			return m.newTab()
		case h.kind == hitClose || mouse.Button == tea.MouseMiddle:
			return m.closeTab(h.tab)
		default:
			m.active = h.tab
		}
		return nil
	}
	return nil
}

func (m *Model) current() Child { return m.tabs[m.active] }

func (m *Model) childSize() app.ResizeMsg {
	return app.ResizeMsg{Width: m.width, Height: max(m.height-barLines, 1)}
}

func (m *Model) updateActive(msg tea.Msg) tea.Cmd {
	v, cmd := m.current().Update(msg)
	m.tabs[m.active] = v.(Child)
	return cmd
}

func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs))
	for i, t := range m.tabs {
		v, cmd := t.Update(msg)
		m.tabs[i] = v.(Child)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// View implements app.SignalView.
func (m *Model) View(width, height int) string {
	m.width, m.height = width, height
	return m.renderBar(width) + "\n" + m.current().View(width, max(height-barLines, 1))
}

func (m *Model) renderBar(width int) string {
	t := m.th
	// Titles get the room first; the hint only shows if it still fits.
	hint := t.Dim.Render("  ctrl+t new · < > switch · ctrl+w close")
	room := width - 4 // "+" button and margins
	titleW := min(max(room/len(m.tabs)-8, minTitle), maxTitle)

	m.hits = m.hits[:0]
	var b strings.Builder
	x := 0
	for i, tab := range m.tabs {
		label := fmt.Sprintf("%d %s", i+1, ansi.Truncate(tab.Title(), titleW, "…"))
		style := t.Tab
		if i == m.active {
			style = t.ActiveTab
		}
		text := label
		if len(m.tabs) > 1 {
			text += " ×"
		}
		rendered := style.Render(text)
		w := ansi.StringWidth(rendered)
		// The style pads one cell on each side: × is the last cell before the right padding.
		if len(m.tabs) > 1 {
			closeAt := x + w - 2
			m.hits = append(m.hits, hit{kind: hitClose, tab: i, x0: closeAt, x1: closeAt + 1})
		}
		m.hits = append(m.hits, hit{kind: hitTab, tab: i, x0: x, x1: x + w})
		b.WriteString(rendered)
		x += w
		b.WriteString(" ")
		x++
	}
	if len(m.tabs) < MaxTabs {
		plus := t.Tab.Render("+")
		w := ansi.StringWidth(plus)
		m.hits = append(m.hits, hit{kind: hitNew, x0: x, x1: x + w})
		b.WriteString(plus)
		x += w
	}
	bar := b.String()
	if gap := width - x - ansi.StringWidth(hint); gap > 0 {
		bar += strings.Repeat(" ", gap) + hint
	}
	return ansi.Truncate(bar, width, "…")
}

// Help implements app.SignalView: the current tab's bindings plus the tab ones.
func (m *Model) Help() [][]key.Binding {
	k := m.keys
	return append(m.current().Help(), []key.Binding{k.New, k.Close, k.Next, k.Prev})
}

// ShortHelp implements app.SignalView.
func (m *Model) ShortHelp() []key.Binding { return m.current().ShortHelp() }

// Capturing implements app.SignalView.
func (m *Model) Capturing() bool { return m.current().Capturing() }

// Status implements app.SignalView.
func (m *Model) Status() string { return m.current().Status() }
