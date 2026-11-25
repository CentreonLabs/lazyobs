package tracesview

import (
	"charm.land/bubbles/v2/key"

	"github.com/CentreonLabs/lazyobs/internal/ui/keys"
)

type keyMap struct {
	keys.List
	Query, Range, Refresh, Open, Close, Sort, Logs, Copy, UTC, Layout key.Binding
	ZoomOut, Earlier, Later                                           key.Binding
	Submit, Cancel, NextPreset                                        key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		List:    keys.DefaultList(),
		Query:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "query (root spans)")),
		Range:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time range")),
		Refresh: key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "refresh")),
		Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open the waterfall")),
		Close:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back to the list")),
		Sort:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "newest/slowest first")),
		Logs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs of the trace")),
		Copy:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy span JSON / trace ID")),
		UTC:     key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "local/UTC time")),
		Layout:  key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "split/compact chart")),
		ZoomOut: key.NewBinding(key.WithKeys("-"), key.WithHelp("-", "zoom out")),
		Earlier: key.NewBinding(key.WithKeys("shift+left", "["), key.WithHelp("⇧← [", "move back in time")),
		Later:   key.NewBinding(key.WithKeys("shift+right", "]"), key.WithHelp("⇧→ ]", "move forward in time")),

		Submit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		NextPreset: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next preset")),
	}
}

func (k keyMap) help() [][]key.Binding {
	return [][]key.Binding{
		{k.Query, k.Range, k.Open, k.Close, k.Sort, k.Logs},
		k.Bindings(),
		{k.ZoomOut, k.Earlier, k.Later, k.Refresh, k.Copy, k.UTC, k.Layout},
	}
}
