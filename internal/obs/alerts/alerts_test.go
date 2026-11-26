package alerts_test

import (
	"slices"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]alerts.Status{
		"ok": alerts.StatusOK, "CRITICAL": alerts.StatusCritical, "Warning": alerts.StatusWarning,
		"error": alerts.StatusError, "unknown": alerts.StatusUnknown, "bogus": alerts.StatusUnknown,
	} {
		if got := alerts.ParseStatus(in); got != want {
			t.Errorf("ParseStatus(%q) = %v, want %v", in, got, want)
		}
	}
	if alerts.Status(42).String() != "unknown" {
		t.Error("out of range status")
	}
}

func TestCompareWorstFirst(t *testing.T) {
	ev := func(s alerts.Status, evalErr string) *alerts.Event {
		return &alerts.Event{Status: s, EvalError: evalErr}
	}
	list := []alerts.Alert{
		{Rule: alerts.Rule{Name: "disabled", Enabled: false}, Event: ev(alerts.StatusCritical, "")},
		{Rule: alerts.Rule{Name: "b ok", Enabled: true}, Event: ev(alerts.StatusOK, "")},
		{Rule: alerts.Rule{Name: "never", Enabled: true}},
		{Rule: alerts.Rule{Name: "a ok", Enabled: true}, Event: ev(alerts.StatusOK, "")},
		{Rule: alerts.Rule{Name: "failing", Enabled: true}, Event: ev(alerts.StatusOK, "division by zero")},
		{Rule: alerts.Rule{Name: "crit", Enabled: true}, Event: ev(alerts.StatusCritical, "")},
		{Rule: alerts.Rule{Name: "warn", Enabled: true}, Event: ev(alerts.StatusWarning, "")},
	}
	slices.SortFunc(list, alerts.Compare)
	var got []string
	for _, a := range list {
		got = append(got, a.Rule.Name)
	}
	want := []string{"crit", "warn", "never", "failing", "a ok", "b ok", "disabled"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestHistoryCells(t *testing.T) {
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	h := alerts.History{From: base, To: at(60), Step: 5 * time.Minute, Points: []alerts.StatusPoint{
		{At: at(0), Status: alerts.StatusOK},
		{At: at(5), Status: alerts.StatusCritical},
		{At: at(10), Status: alerts.StatusOK},
		{At: at(15), Status: alerts.StatusWarning},
		{At: at(20), Status: alerts.StatusOK},
		// nothing evaluated from 25 to 45
		{At: at(45), Status: alerts.StatusOK},
		{At: at(50), Status: alerts.StatusOK},
		{At: at(55), Status: alerts.StatusOK},
	}}
	// Two cells per step: a step's status fills both.
	cells := h.Cells(24, 5*time.Minute)
	want := []alerts.Status{0, 0, 4, 4, 0, 0, 2, 2, 0, 0, -1, -1, -1, -1, -1, -1, -1, -1, 0, 0, 0, 0, 0, 0}
	if !slices.Equal(cells, want) {
		t.Errorf("cells = %v\n want %v", cells, want)
	}
	// One cell for 20 minutes: the worst status wins.
	if got := h.Cells(3, 5*time.Minute); !slices.Equal(got, []alerts.Status{alerts.StatusCritical, alerts.StatusOK, alerts.StatusOK}) {
		t.Errorf("coarse cells = %v", got)
	}
	if got := (alerts.History{}).Cells(4, time.Minute); len(got) != 4 {
		t.Errorf("empty history = %v", got)
	}

	tr := h.Transitions()
	if len(tr) != 4 || tr[0] != (alerts.Transition{At: at(5), From: alerts.StatusOK, To: alerts.StatusCritical}) || tr[3].To != alerts.StatusOK {
		t.Errorf("transitions = %+v", tr)
	}
}
