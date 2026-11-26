package alertsview

import (
	"charm.land/bubbles/v2/key"

	"github.com/CentreonLabs/lazyobs/internal/ui/keys"
)

type keyMap struct {
	keys.List
	Details, Close, Logs, Filter, Problems, Range, Refresh, Copy, UTC key.Binding
	Submit, Cancel                                                    key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		List:     keys.DefaultList(),
		Details:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "show/hide the rule details")),
		Close:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "hide the details / clear the filter")),
		Logs:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs the rule watches")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter by name")),
		Problems: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "problems only / all rules")),
		Range:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "history range: 1h, 6h, 24h, 7d")),
		Refresh:  key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "refresh")),
		Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy the rule query")),
		UTC:      key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "local/UTC time")),

		Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

func (k keyMap) help() [][]key.Binding {
	return [][]key.Binding{
		{k.Details, k.Close, k.Logs, k.Filter, k.Problems},
		k.Bindings(),
		{k.Range, k.Refresh, k.Copy, k.UTC},
	}
}
