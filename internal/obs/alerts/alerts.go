// Package alerts is the alerting domain: alert rules evaluated periodically
// by the platform, their current status, and what mutes them.
package alerts

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Status is the outcome of a rule evaluation, from best to worst.
type Status int

// Statuses, ordered by severity.
const (
	StatusOK Status = iota
	StatusUnknown
	StatusWarning
	StatusError
	StatusCritical
)

var statusNames = [...]string{"ok", "unknown", "warning", "error", "critical"}

func (s Status) String() string {
	if s < 0 || int(s) >= len(statusNames) {
		return "unknown"
	}
	return statusNames[s]
}

// ParseStatus reads a status as the platform writes it (any case).
func ParseStatus(s string) Status {
	if i := slices.Index(statusNames[:], strings.ToLower(strings.TrimSpace(s))); i >= 0 {
		return Status(i)
	}
	return StatusUnknown
}

// Rule is an alert rule as listed.
type Rule struct {
	ID          string
	Name        string
	Description string
	Template    string // count_logs, ratio_logs, ...
	Enabled     bool
	Frequency   time.Duration // between two evaluations
	Channels    []string      // notification channel names
	Links       []Link
	UpdatedAt   time.Time
}

// Link points to a runbook, a dashboard or any page about the rule.
type Link struct{ Label, URL string }

// Event is the latest evaluation of a rule.
type Event struct {
	ID, RunID string
	Status    Status
	Message   string
	At        time.Time
	// EvalError explains why the evaluation failed; the status is then the
	// last known one.
	EvalError string
	Flapping  bool
}

// Ack is an acknowledgement of a rule's current problem.
type Ack struct {
	By      string
	At      time.Time
	Until   time.Time
	Comment string
}

// Alert is a rule with its current state.
type Alert struct {
	Rule  Rule
	Event *Event // nil when the rule was never evaluated (or is disabled)
	Ack   *Ack   // nil when not acknowledged
	// Downtime names the active downtime muting the rule, if any.
	Downtime string
}

// Status is the current status of the alert, StatusUnknown when never evaluated.
func (a Alert) Status() Status {
	if a.Event == nil {
		return StatusUnknown
	}
	return a.Event.Status
}

// Failing reports whether the last evaluation failed.
func (a Alert) Failing() bool { return a.Event != nil && a.Event.EvalError != "" }

// Compare orders alerts worst first: enabled before disabled, by status,
// failing evaluations first, then by name.
func Compare(a, b Alert) int {
	switch {
	case a.Rule.Enabled != b.Rule.Enabled:
		if a.Rule.Enabled {
			return -1
		}
		return 1
	case a.Status() != b.Status():
		return int(b.Status() - a.Status())
	case a.Failing() != b.Failing():
		if a.Failing() {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Rule.Name), strings.ToLower(b.Rule.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Rule.ID, b.Rule.ID)
}

// Query is a search a rule evaluates, named after its role ("query",
// "dividend", "divisor").
type Query struct{ Name, Text string }

// Condition maps the evaluated value to a status: value Operator Threshold.
type Condition struct {
	Operator  string
	Threshold float64
	Status    Status
}

// Details are what a rule evaluates and what it measured last.
type Details struct {
	Queries    []Query
	Conditions []Condition
	// Measure names the evaluated value ("count", "ratio"); empty when unknown.
	Measure string
	Value   float64
}

// Overview is the state of every rule. Warnings report what could not be
// read without preventing the listing (acknowledgements, downtimes...).
type Overview struct {
	Alerts   []Alert
	Warnings []string
}

// Reader reads the alerts of an organization.
type Reader interface {
	// Alerts lists every rule with its current state.
	Alerts(ctx context.Context) (Overview, error)
	// Details returns the queries and conditions of a rule and its last value.
	Details(ctx context.Context, a Alert) (Details, error)
	// History returns the worst status of a rule per step over [from, to).
	// Rules never evaluated have an empty history.
	History(ctx context.Context, a Alert, from, to time.Time, step time.Duration) (History, error)
}

// NoData marks a time without any evaluation in History.Cells.
const NoData Status = -1

// StatusPoint is the worst status of a rule over [At, At+step).
type StatusPoint struct {
	At     time.Time
	Status Status
}

// History is the status of a rule over time, points sorted by time; steps
// without evaluation have no point.
type History struct {
	From, To time.Time
	Step     time.Duration
	Points   []StatusPoint
}

// Cells spreads the history over n cells, each showing the worst status of
// its time span. A cell without point keeps the status of the previous one
// while the rule is still expected to be evaluated (within hold), so that
// cells shorter than the evaluation period do not flicker; otherwise it is
// NoData.
func (h History) Cells(n int, hold time.Duration) []Status {
	cells := make([]Status, max(n, 0))
	if n <= 0 || !h.From.Before(h.To) {
		return cells
	}
	span := h.To.Sub(h.From)
	hold = max(hold, h.Step)
	k := 0
	var last *StatusPoint
	for i := range cells {
		start := h.From.Add(span * time.Duration(i) / time.Duration(n))
		end := h.From.Add(span * time.Duration(i+1) / time.Duration(n))
		cells[i] = NoData
		for ; k < len(h.Points) && h.Points[k].At.Before(end); k++ {
			p := h.Points[k]
			if !p.At.Before(start) || p.At.Add(h.Step).After(start) {
				cells[i] = max(cells[i], p.Status)
			}
			last = &h.Points[k]
		}
		if cells[i] == NoData && last != nil && last.At.Add(hold).After(start) {
			cells[i] = last.Status
		}
	}
	return cells
}

// Transition is a change of status.
type Transition struct {
	At       time.Time
	From, To Status
}

// Transitions lists the status changes, oldest first.
func (h History) Transitions() []Transition {
	var out []Transition
	for i := 1; i < len(h.Points); i++ {
		if a, b := h.Points[i-1], h.Points[i]; a.Status != b.Status {
			out = append(out, Transition{At: b.At, From: a.Status, To: b.Status})
		}
	}
	return out
}
