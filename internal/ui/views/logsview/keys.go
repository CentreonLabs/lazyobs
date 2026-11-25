package logsview

import (
	"charm.land/bubbles/v2/key"

	"github.com/CentreonLabs/lazyobs/internal/ui/keys"
)

type keyMap struct {
	keys.List
	Query   key.Binding
	Range   key.Binding
	Refresh key.Binding
	Detail  key.Binding
	Focus   key.Binding
	Close   key.Binding
	Live    key.Binding
	Pause   key.Binding
	Copy    key.Binding
	UTC     key.Binding
	Layout  key.Binding

	// Mouse actions, listed in the help only (their keys never match a key press).
	MouseToggle key.Binding
	MouseSolo   key.Binding
	MouseSelect key.Binding
	MouseWheel  key.Binding

	// Input mode.
	Submit     key.Binding
	Cancel     key.Binding
	NextPreset key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		List:    keys.DefaultList(),
		Query:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "query")),
		Range:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time range")),
		Refresh: key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "refresh")),
		Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Focus:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
		Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close details")),
		Live:    key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "live on/off")),
		Pause:   key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "pause live")),
		Copy:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy log JSON")),
		UTC:     key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "local/UTC time")),
		Layout:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "split/compact timeline")),

		MouseToggle: mouseHelp("click level", "show/hide it"),
		MouseSolo:   mouseHelp("right-click level", "show only it"),
		MouseSelect: mouseHelp("click row", "select log"),
		MouseWheel:  mouseHelp("wheel", "scroll"),

		Submit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		NextPreset: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next preset")),
	}
}

func (k keyMap) help() [][]key.Binding {
	return [][]key.Binding{
		{k.Query, k.Range, k.Detail, k.Live, k.Pause},
		k.Bindings(),
		{k.Refresh, k.Focus, k.Close, k.Copy, k.UTC, k.Layout},
		{k.MouseToggle, k.MouseSolo, k.MouseSelect, k.MouseWheel},
	}
}

// mouseHelp documents a mouse action in the help screen.
func mouseHelp(action, desc string) key.Binding {
	return key.NewBinding(key.WithKeys("mouse:"+action), key.WithHelp(action, desc))
}
