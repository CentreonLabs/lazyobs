package fake

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

func demoAlertsAt(t *testing.T, now time.Time) (*Alerts, map[string]alerts.Alert) {
	t.Helper()
	clock := func() time.Time { return now }
	a := NewAlerts(NewLogs(2026, clock), clock)
	ov, err := a.Alerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]alerts.Alert{}
	for _, al := range ov.Alerts {
		byName[al.Rule.Name] = al
	}
	if len(byName) != len(demoRules) {
		t.Fatalf("got %d alerts", len(byName))
	}
	return a, byName
}

func TestDemoAlertsFollowIncidents(t *testing.T) {
	// An incident starts every 20 minutes: 14:00 is its start, the
	// evaluation of 14:05 covers it, the one of 14:10 does not.
	base := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	_, calm := demoAlertsAt(t, base.Add(10*time.Minute+30*time.Second))
	_, after := demoAlertsAt(t, base.Add(5*time.Minute+30*time.Second))
	if s := calm["Error burst"].Status(); s != alerts.StatusOK {
		t.Errorf("calm error burst = %v", s)
	}
	if s := after["Error burst"].Status(); s != alerts.StatusCritical {
		t.Errorf("error burst after an incident = %v", s)
	}
	if ack := after["Payment declines"].Ack; ack == nil || after["Payment declines"].Status() == alerts.StatusOK {
		t.Errorf("payment declines must be failing and acknowledged: %+v", after["Payment declines"])
	}
	if calm["Payment declines"].Ack != nil {
		t.Error("an ok rule is not acknowledged")
	}
}

func TestDemoAlertsSpecialCases(t *testing.T) {
	a, all := demoAlertsAt(t, time.Date(2026, 11, 25, 14, 10, 30, 0, time.UTC))
	if m := all["Mailer bounce ratio"]; !m.Failing() || m.Event.EvalError != errDivisionByZero.Error() {
		t.Errorf("mailer = %+v", m.Event)
	}
	if off := all["Legacy syslog login failed"]; off.Rule.Enabled || off.Event != nil {
		t.Errorf("disabled rule = %+v", off)
	}
	if all["Auth fatal errors"].Downtime == "" {
		t.Error("auth rule must be in downtime")
	}
	d, err := a.Details(context.Background(), all["Checkout error ratio"])
	if err != nil || len(d.Queries) != 2 || d.Measure != "ratio" || d.Value <= 0 || d.Value >= 1 {
		t.Errorf("details = %+v, %v", d, err)
	}
	if d, _ := a.Details(context.Background(), all["Mailer bounce ratio"]); d.Measure != "" {
		t.Errorf("a failing rule has no value: %+v", d)
	}
	if _, err := a.Details(context.Background(), alerts.Alert{Rule: alerts.Rule{ID: "nope"}}); err == nil {
		t.Error("unknown rule must fail")
	}
}

func TestDemoAlertHistory(t *testing.T) {
	now := time.Date(2026, 11, 25, 14, 7, 0, 0, time.UTC)
	a, all := demoAlertsAt(t, now)
	burst := all["Error burst"]
	h, err := a.History(context.Background(), burst, now.Add(-2*time.Hour), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if h.Step != 5*time.Minute || len(h.Points) != 24 {
		t.Fatalf("step %v, %d points", h.Step, len(h.Points))
	}
	// An incident starts every 20 minutes; only the evaluation 5 minutes
	// later can see it.
	var critical int
	for _, p := range h.Points {
		if p.Status == alerts.StatusCritical {
			critical++
			if p.At.Minute()%20 != 5 {
				t.Errorf("critical outside an incident at %s", p.At.Format("15:04"))
			}
		}
	}
	if critical < 3 {
		t.Errorf("only %d critical evaluations in 2 hours", critical)
	}
	fine := h
	// The last point is the current status.
	if last := h.Points[len(h.Points)-1]; last.Status != burst.Status() {
		t.Errorf("last point %v, current %v", last.Status, burst.Status())
	}
	// Coarser steps keep the worst status of the evaluations they hold.
	h, _ = a.History(context.Background(), burst, now.Add(-2*time.Hour), now, 20*time.Minute)
	for _, p := range h.Points {
		worst := alerts.NoData
		for _, f := range fine.Points {
			if !f.At.Before(p.At) && f.At.Before(p.At.Add(20*time.Minute)) {
				worst = max(worst, f.Status)
			}
		}
		if p.Status != worst {
			t.Errorf("step %s = %v, want %v", p.At.Format("15:04"), p.Status, worst)
		}
	}
	if h, _ := a.History(context.Background(), all["Legacy syslog login failed"], now.Add(-time.Hour), now, time.Minute); len(h.Points) != 0 {
		t.Error("a disabled rule has no history")
	}
}
