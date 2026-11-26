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
}
