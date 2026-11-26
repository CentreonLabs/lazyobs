package alerts_test

import (
	"slices"
	"testing"

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
