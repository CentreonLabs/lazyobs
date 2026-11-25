package logsview

import (
	"charm.land/bubbles/v2/key"

	"github.com/CentreonLabs/lazyobs/internal/ui/keys"
)

type keyMap struct {
	keys.List
	Query     key.Binding
	Ask       key.Binding
	ZoomOut   key.Binding
	Earlier   key.Binding
	Later     key.Binding
	Range     key.Binding
	Refresh   key.Binding
	Detail    key.Binding
	Focus     key.Binding
	Close     key.Binding
	Live      key.Binding
	Pause     key.Binding
	Copy      key.Binding
	Context   key.Binding
	Breakdown key.Binding
	Trace     key.Binding
	FilterIn  key.Binding
	FilterOut key.Binding
	UTC       key.Binding
	Layout    key.Binding

	// Mouse actions, listed in the help only (their keys never match a key press).
	MouseHover  key.Binding
	MouseZoom   key.Binding
	MouseJump   key.Binding
	MouseToggle key.Binding
	MouseSolo   key.Binding
	MouseSelect key.Binding
	MouseWheel  key.Binding

	// Query autocompletion.
	Accept      key.Binding
	SuggestNext key.Binding
	SuggestPrev key.Binding

	// Text editing (handled by the input, listed for help).
	DeleteWord key.Binding
	ClearLine  key.Binding

	// Input mode.
	Submit     key.Binding
	Cancel     key.Binding
	NextPreset key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		List:      keys.DefaultList(),
		Query:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "query")),
		Ask:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "ask in plain words")),
		ZoomOut:   key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "zoom out")),
		Earlier:   key.NewBinding(key.WithKeys("shift+left", "["), key.WithHelp("⇧← [", "move back in time")),
		Later:     key.NewBinding(key.WithKeys("shift+right", "]"), key.WithHelp("⇧→ ]", "move forward in time")),
		Range:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time range")),
		Refresh:   key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "refresh")),
		Detail:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Focus:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch panel")),
		Close:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close details")),
		Live:      key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "live on/off")),
		Pause:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "pause live")),
		Copy:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy log JSON")),
		Context:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "logs around this one")),
		Breakdown: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "top values of a field")),
		Trace:     key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "trace of the log")),
		FilterIn: key.NewBinding(key.WithKeys("+", "="),
			key.WithHelp("+", "details: filter on the field")),
		FilterOut: key.NewBinding(key.WithKeys("-"),
			key.WithHelp("-", "details: exclude the field")),
		UTC:    key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "local/UTC time")),
		Layout: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "split/compact timeline")),

		MouseHover:  mouseHelp("hover chart", "values of a bucket"),
		MouseZoom:   mouseHelp("drag on chart", "zoom in"),
		MouseJump:   mouseHelp("click on chart", "go to that bucket's logs"),
		MouseToggle: mouseHelp("click level", "show/hide it"),
		MouseSolo:   mouseHelp("right-click level", "show only it"),
		MouseSelect: mouseHelp("click row", "select log"),
		MouseWheel:  mouseHelp("wheel", "scroll"),

		Accept:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
		SuggestNext: key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next suggestion")),
		SuggestPrev: key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "previous suggestion")),

		DeleteWord: key.NewBinding(key.WithKeys("alt+backspace"), key.WithHelp("⌥⌫ ⇧⌫ ctrl+w", "typing: delete a word")),
		ClearLine:  key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("⌘⌫ ctrl+u", "typing: delete to the start")),

		Submit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		NextPreset: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next preset")),
	}
}

func (k keyMap) help() [][]key.Binding {
	return [][]key.Binding{
		{k.Query, k.Ask, k.Range, k.Detail, k.Context, k.Trace, k.Breakdown, k.Live, k.Pause},
		k.Bindings(),
		{k.FilterIn, k.FilterOut, k.ZoomOut, k.Earlier, k.Later, k.Refresh, k.Focus, k.Close, k.Copy, k.UTC, k.Layout},
		{k.DeleteWord, k.ClearLine},
		{k.MouseHover, k.MouseJump, k.MouseZoom, k.MouseToggle, k.MouseSolo, k.MouseSelect, k.MouseWheel},
	}
}

// mouseHelp documents a mouse action in the help screen.
func mouseHelp(action, desc string) key.Binding {
	return key.NewBinding(key.WithKeys("mouse:"+action), key.WithHelp(action, desc))
}
