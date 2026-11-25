// Package keys centralizes key bindings. They drive both input handling and
// the help screen, so the two cannot drift apart.
package keys

import "charm.land/bubbles/v2/key"

// Global are the bindings available in every view.
type Global struct {
	Quit    key.Binding
	Help    key.Binding
	Signal1 key.Binding
	Signal2 key.Binding
	Signal3 key.Binding
	ForceQ  key.Binding
}

// DefaultGlobal returns the global bindings.
func DefaultGlobal() Global {
	return Global{
		Quit:    key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQ:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
		Signal1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "logs screen (or click its tab)")),
		Signal2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "traces screen")),
		Signal3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "metrics screen")),
	}
}

// ShortHelp implements help.KeyMap.
func (g Global) ShortHelp() []key.Binding { return []key.Binding{g.Help, g.Quit} }

// FullHelp implements help.KeyMap.
func (g Global) FullHelp() [][]key.Binding {
	return [][]key.Binding{{g.Signal1, g.Signal2, g.Signal3}, {g.Help, g.Quit, g.ForceQ}}
}

// List are the bindings to move through a list or a scrollable panel.
type List struct {
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	HalfDown key.Binding
	HalfUp   key.Binding
	PageDown key.Binding
	PageUp   key.Binding
}

// DefaultList returns vim-style list bindings.
func DefaultList() List {
	return List{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
	}
}

// Bindings lists the list bindings for the help screen.
func (l List) Bindings() []key.Binding {
	return []key.Binding{l.Up, l.Down, l.Top, l.Bottom, l.HalfDown, l.HalfUp, l.PageDown, l.PageUp}
}
