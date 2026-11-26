package remote

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

// AlertsAPI is the subset of the API client used by Alerts.
type AlertsAPI interface {
	ListAlertRules(ctx context.Context) ([]api.AlertRule, error)
	GetAlertRule(ctx context.Context, id string) (api.AlertRule, error)
	ListAlertEvents(ctx context.Context) ([]api.AlertEvent, error)
	AlertEventContext(ctx context.Context, ruleID, eventID, runID string) (api.AlertEventContext, error)
	ListAcknowledgements(ctx context.Context) ([]api.Acknowledgement, error)
	ListDowntimes(ctx context.Context, status string) ([]api.Downtime, error)
	GetDowntime(ctx context.Context, id string) (api.Downtime, error)
	ListNotificationChannels(ctx context.Context) ([]api.NotificationChannel, error)
}

// Alerts reads the alert rules of the platform and their state.
type Alerts struct{ api AlertsAPI }

var _ alerts.Reader = (*Alerts)(nil)

// NewAlerts returns an alerts reader.
func NewAlerts(c AlertsAPI) *Alerts { return &Alerts{api: c} }

// Alerts implements alerts.Reader. Rules and events are required; the
// acknowledgements, downtimes and channel names only decorate them, so
// failing to read those gives warnings.
func (a *Alerts) Alerts(ctx context.Context) (alerts.Overview, error) {
	rules, err := a.api.ListAlertRules(ctx)
	if err != nil {
		return alerts.Overview{}, err
	}
	events, err := a.api.ListAlertEvents(ctx)
	if err != nil {
		return alerts.Overview{}, err
	}
	var out alerts.Overview
	warn := func(what string, err error) {
		out.Warnings = append(out.Warnings, fmt.Sprintf("could not read the %s: %v", what, err))
	}

	channels := map[string]string{}
	if list, err := a.api.ListNotificationChannels(ctx); err != nil {
		warn("notification channels", err)
	} else {
		for _, c := range list {
			channels[c.ID] = c.Name
		}
	}
	acks := map[string]*alerts.Ack{}
	if list, err := a.api.ListAcknowledgements(ctx); err != nil {
		warn("acknowledgements", err)
	} else {
		for _, k := range list {
			ack := &alerts.Ack{By: k.AckBy, At: k.AckAt, Until: k.ExpiresAt}
			if k.AckComment != nil {
				ack.Comment = *k.AckComment
			}
			acks[k.AlertRuleID] = ack
		}
	}
	downtimes := map[string]string{}
	if list, err := a.api.ListDowntimes(ctx, "active"); err != nil {
		warn("downtimes", err)
	} else {
		for _, d := range list {
			if d.AlertRulesCount == 0 {
				continue
			}
			full, err := a.api.GetDowntime(ctx, d.ID)
			if err != nil {
				warn("downtime "+d.Name, err)
				continue
			}
			for _, id := range full.AlertRuleIDs {
				downtimes[id] = d.Name
			}
		}
	}

	byRule := map[string]api.AlertEvent{}
	for _, e := range events {
		if cur, ok := byRule[e.RuleID]; !ok || e.UpdatedAt.After(cur.UpdatedAt) {
			byRule[e.RuleID] = e
		}
	}
	for _, r := range rules {
		al := alerts.Alert{Rule: rule(r, channels), Ack: acks[r.ID], Downtime: downtimes[r.ID]}
		if e, ok := byRule[r.ID]; ok {
			al.Event = event(e)
		}
		out.Alerts = append(out.Alerts, al)
	}
	slices.SortFunc(out.Alerts, alerts.Compare)
	return out, nil
}

// Details implements alerts.Reader.
func (a *Alerts) Details(ctx context.Context, al alerts.Alert) (alerts.Details, error) {
	r, err := a.api.GetAlertRule(ctx, al.Rule.ID)
	if err != nil {
		return alerts.Details{}, err
	}
	var d alerts.Details
	if p := r.Params; p != nil {
		for _, q := range []alerts.Query{{Name: "query", Text: p.Query}, {Name: "dividend", Text: p.QueryDividend}, {Name: "divisor", Text: p.QueryDivisor}} {
			if q.Text != "" {
				d.Queries = append(d.Queries, q)
			}
		}
		for _, c := range p.AlertConditions {
			d.Conditions = append(d.Conditions, alerts.Condition{Operator: c.Operator, Threshold: c.Threshold, Status: alerts.ParseStatus(c.Status)})
		}
	}
	if e := al.Event; e != nil && e.ID != "" {
		// The last value is a bonus: a rule whose evaluation fails has none.
		if c, err := a.api.AlertEventContext(ctx, al.Rule.ID, e.ID, e.RunID); err == nil {
			if v, ok := c.IntermediateValues[c.Type].(float64); ok {
				d.Measure, d.Value = c.Type, v
			}
		}
	}
	return d, nil
}

func rule(r api.AlertRule, channels map[string]string) alerts.Rule {
	out := alerts.Rule{
		ID: r.ID, Name: r.Name, Description: r.Description, Template: r.TemplateName, Enabled: r.Enabled,
		Frequency: time.Duration(r.FrequencySeconds) * time.Second, UpdatedAt: r.UpdatedAt,
	}
	for _, id := range r.NotificationChannelIDs {
		name := channels[id]
		if name == "" {
			name = id
		}
		out.Channels = append(out.Channels, name)
	}
	for _, l := range api.DecodeExternalLinks(r.Attributes) {
		label := l.Label
		if label == "" {
			label = l.Description
		}
		out.Links = append(out.Links, alerts.Link{Label: label, URL: l.URL})
	}
	return out
}

func event(e api.AlertEvent) *alerts.Event {
	ev := &alerts.Event{ID: e.ID, RunID: e.RunID, Status: alerts.ParseStatus(e.Status), Message: e.Message, At: e.UpdatedAt}
	if s, ok := e.Attributes[api.AttrEvalErrorDetails].(string); ok {
		ev.EvalError = s
	}
	ev.Flapping, _ = e.Attributes[api.AttrFlapping].(bool)
	return ev
}
