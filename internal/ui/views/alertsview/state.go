package alertsview

import (
	"slices"
	"time"
)

// State is what the alerts view remembers across sessions.
type State struct {
	Filter       string `json:"filter,omitempty"`
	ProblemsOnly bool   `json:"problemsOnly,omitempty"`
	Details      bool   `json:"details,omitempty"`
	Rule         string `json:"rule,omitempty"` // selected rule ID
	UTC          bool   `json:"utc,omitempty"`
	// History is the range of the status history; the default when zero.
	History time.Duration `json:"history,omitempty"`
}

// Snapshot captures the state to save.
func (m *Model) Snapshot() State {
	return State{
		Filter: m.filter, ProblemsOnly: m.problemsOnly, Details: m.detailOpen, Rule: m.Selected().Rule.ID, UTC: m.utc,
		History: m.histRange,
	}
}

// Restore returns a view initialized from a saved state; the selected rule
// is found again once the alerts are read.
func Restore(opts Options, s State) *Model {
	m := New(opts)
	m.filter, m.problemsOnly, m.detailOpen, m.utc = s.Filter, s.ProblemsOnly, s.Details, s.UTC
	m.restoreRule = s.Rule
	if slices.Contains(historyRanges, s.History) {
		m.histRange = s.History
	}
	return m
}
