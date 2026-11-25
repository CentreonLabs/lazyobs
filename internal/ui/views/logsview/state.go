package logsview

import (
	"sort"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
)

// State is what a logs tab remembers across sessions.
type State struct {
	Query   string        `json:"query,omitempty"`
	Range   obs.TimeRange `json:"range"`
	Live    bool          `json:"live,omitempty"`
	UTC     bool          `json:"utc,omitempty"`
	Compact bool          `json:"compact,omitempty"`
	Hidden  []string      `json:"hidden,omitempty"` // severities hidden from the timeline
	Prompt  string        `json:"prompt,omitempty"` // last natural-language question
}

// Snapshot captures the state to save.
func (m *Model) Snapshot() State {
	s := State{
		Query: m.query, Range: m.rng, Live: m.live,
		UTC: m.utc, Compact: m.compact, Prompt: m.lastPrompt,
	}
	for name, hidden := range m.hidden {
		if hidden {
			s.Hidden = append(s.Hidden, name)
		}
	}
	sort.Strings(s.Hidden)
	return s
}

// Restore returns a view initialized from a saved state.
func Restore(opts Options, s State) *Model {
	opts.Query, opts.Range = s.Query, s.Range
	m := New(opts)
	m.live, m.utc, m.compact, m.lastPrompt = s.Live && s.Range.IsRelative(), s.UTC, s.Compact, s.Prompt
	for _, name := range s.Hidden {
		m.hidden[name] = true
	}
	return m
}

// Selected returns the selected log, or a zero record when the list is empty.
func (m *Model) Selected() logs.Record {
	if len(m.records) == 0 {
		return logs.Record{}
	}
	return m.records[m.cursor]
}
