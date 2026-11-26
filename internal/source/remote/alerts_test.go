package remote_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

type fakeAlertsAPI struct {
	rules    []api.AlertRule
	events   []api.AlertEvent
	acksErr  error
	detail   api.AlertRule
	ctx      api.AlertEventContext
	ctxErr   error
	ctxCalls int
}

func (f *fakeAlertsAPI) ListAlertRules(context.Context) ([]api.AlertRule, error) { return f.rules, nil }

func (f *fakeAlertsAPI) GetAlertRule(context.Context, string) (api.AlertRule, error) {
	return f.detail, nil
}

func (f *fakeAlertsAPI) ListAlertEvents(context.Context) ([]api.AlertEvent, error) {
	return f.events, nil
}

func (f *fakeAlertsAPI) AlertEventContext(context.Context, string, string, string) (api.AlertEventContext, error) {
	f.ctxCalls++
	return f.ctx, f.ctxErr
}

func (f *fakeAlertsAPI) ListAcknowledgements(context.Context) ([]api.Acknowledgement, error) {
	if f.acksErr != nil {
		return nil, f.acksErr
	}
	return []api.Acknowledgement{{AckBy: "someone", AlertRuleID: "r-warn", ExpiresAt: time.Unix(100, 0)}}, nil
}

func (f *fakeAlertsAPI) ListDowntimes(_ context.Context, status string) ([]api.Downtime, error) {
	if status != "active" {
		return nil, errors.New("only active downtimes are expected")
	}
	return []api.Downtime{{ID: "dt", Name: "Maintenance", AlertRulesCount: 1}, {ID: "empty", Name: "Empty"}}, nil
}

func (f *fakeAlertsAPI) GetDowntime(_ context.Context, id string) (api.Downtime, error) {
	if id != "dt" {
		return api.Downtime{}, errors.New("an empty downtime must not be fetched")
	}
	return api.Downtime{ID: "dt", Name: "Maintenance", AlertRuleIDs: []string{"r-ok"}}, nil
}

func (f *fakeAlertsAPI) ListNotificationChannels(context.Context) ([]api.NotificationChannel, error) {
	return []api.NotificationChannel{{ID: "c1", Name: "Ops webhook"}}, nil
}

func sampleAlertsAPI() *fakeAlertsAPI {
	at := time.Date(2026, 11, 26, 10, 15, 0, 0, time.UTC)
	return &fakeAlertsAPI{
		rules: []api.AlertRule{
			{ID: "r-ok", Name: "Healthy", Enabled: true, FrequencySeconds: 300, NotificationChannelIDs: []string{"c1", "gone"}},
			{ID: "r-off", Name: "Disabled", Enabled: false, FrequencySeconds: 300},
			{
				ID: "r-crit", Name: "Broken", Enabled: true, FrequencySeconds: 60, TemplateName: "count_logs",
				Attributes: map[string]any{api.AttrExternalLinks: []any{map[string]any{"label": "Runbook", "url": "https://example.com"}}},
			},
			{ID: "r-warn", Name: "Shaky", Enabled: true, FrequencySeconds: 300},
		},
		events: []api.AlertEvent{
			{ID: "e-old", RuleID: "r-crit", Status: "ok", UpdatedAt: at.Add(-time.Hour)},
			{ID: "e-crit", RuleID: "r-crit", RunID: "run", Status: "critical", UpdatedAt: at},
			{
				ID: "e-ok", RuleID: "r-ok", Status: "ok", UpdatedAt: at,
				Attributes: map[string]any{api.AttrEvalErrorDetails: "division by zero", api.AttrFlapping: true},
			},
			{ID: "e-warn", RuleID: "r-warn", Status: "warning", UpdatedAt: at},
		},
	}
}

func TestAlertsOverview(t *testing.T) {
	ov, err := remote.NewAlerts(sampleAlertsAPI()).Alerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Warnings) != 0 {
		t.Errorf("warnings = %v", ov.Warnings)
	}
	var names []string
	for _, a := range ov.Alerts {
		names = append(names, a.Rule.Name)
	}
	if len(names) != 4 || names[0] != "Broken" || names[1] != "Shaky" || names[2] != "Healthy" || names[3] != "Disabled" {
		t.Fatalf("order = %v", names)
	}
	crit, warn, ok, off := ov.Alerts[0], ov.Alerts[1], ov.Alerts[2], ov.Alerts[3]
	if crit.Event.ID != "e-crit" || crit.Status() != alerts.StatusCritical || crit.Rule.Frequency != time.Minute ||
		len(crit.Rule.Links) != 1 || crit.Rule.Links[0].Label != "Runbook" {
		t.Errorf("critical = %+v / %+v", crit, crit.Event)
	}
	if warn.Ack == nil || warn.Ack.By != "someone" {
		t.Errorf("ack = %+v", warn.Ack)
	}
	if ok.Downtime != "Maintenance" || !ok.Failing() || !ok.Event.Flapping || len(ok.Rule.Channels) != 2 ||
		ok.Rule.Channels[0] != "Ops webhook" || ok.Rule.Channels[1] != "gone" {
		t.Errorf("ok = %+v / %+v", ok, ok.Event)
	}
	if off.Event != nil || off.Status() != alerts.StatusUnknown {
		t.Errorf("disabled = %+v", off)
	}
}

func TestAlertsOverviewWarnsOnDecorations(t *testing.T) {
	f := sampleAlertsAPI()
	f.acksErr = errors.New("forbidden")
	ov, err := remote.NewAlerts(f).Alerts(context.Background())
	if err != nil || len(ov.Alerts) != 4 || len(ov.Warnings) != 1 {
		t.Errorf("overview = %+v, %v", ov, err)
	}
}

func TestAlertDetails(t *testing.T) {
	f := sampleAlertsAPI()
	f.detail = api.AlertRule{Params: &api.AlertParams{
		QueryDividend: "a", QueryDivisor: "b",
		AlertConditions: []api.AlertCondition{{Operator: ">", Threshold: 0.05, Status: "critical"}},
	}}
	f.ctx = api.AlertEventContext{Type: "ratio", IntermediateValues: map[string]any{"ratio": 0.08, "queryDividend": "a"}}
	r := remote.NewAlerts(f)
	al := alerts.Alert{Rule: alerts.Rule{ID: "r"}, Event: &alerts.Event{ID: "e", RunID: "run"}}
	d, err := r.Details(context.Background(), al)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Queries) != 2 || d.Queries[0] != (alerts.Query{Name: "dividend", Text: "a"}) || d.Measure != "ratio" || d.Value != 0.08 ||
		len(d.Conditions) != 1 || d.Conditions[0].Status != alerts.StatusCritical {
		t.Errorf("details = %+v", d)
	}

	// No event: no context to read. A failing context read is not an error.
	f.ctxCalls = 0
	if _, err := r.Details(context.Background(), alerts.Alert{Rule: alerts.Rule{ID: "r"}}); err != nil || f.ctxCalls != 0 {
		t.Errorf("err = %v, calls = %d", err, f.ctxCalls)
	}
	f.ctxErr = errors.New("boom")
	if d, err := r.Details(context.Background(), al); err != nil || d.Measure != "" {
		t.Errorf("details = %+v, %v", d, err)
	}
}
