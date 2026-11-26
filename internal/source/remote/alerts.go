package remote

import (
	"context"
	"fmt"
	"slices"
	"sync"
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
	AlertHistory(ctx context.Context, eventID string, start, end time.Time, interval time.Duration) ([]api.MetricsCurve, error)
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
	// The listings are independent: read them all at once.
	var (
		wg                       sync.WaitGroup
		rules                    []api.AlertRule
		events                   []api.AlertEvent
		rulesErr, eventsErr      error
		channels                 = map[string]string{}
		acks                     = map[string]*alerts.Ack{}
		downtimes                = map[string]string{}
		chanErr, ackErr, downErr error
		downWarnings             []string
	)
	run := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	run(func() { rules, rulesErr = a.api.ListAlertRules(ctx) })
	run(func() { events, eventsErr = a.api.ListAlertEvents(ctx) })
	run(func() {
		var list []api.NotificationChannel
		if list, chanErr = a.api.ListNotificationChannels(ctx); chanErr == nil {
			for _, c := range list {
				channels[c.ID] = c.Name
			}
		}
	})
	run(func() {
		var list []api.Acknowledgement
		if list, ackErr = a.api.ListAcknowledgements(ctx); ackErr == nil {
			for _, k := range list {
				ack := &alerts.Ack{By: k.AckBy, At: k.AckAt, Until: k.ExpiresAt}
				if k.AckComment != nil {
					ack.Comment = *k.AckComment
				}
				acks[k.AlertRuleID] = ack
			}
		}
	})
	run(func() {
		var list []api.Downtime
		if list, downErr = a.api.ListDowntimes(ctx, "active"); downErr != nil {
			return
		}
		for _, d := range list {
			if d.AlertRulesCount == 0 {
				continue
			}
			full, err := a.api.GetDowntime(ctx, d.ID)
			if err != nil {
				downWarnings = append(downWarnings, fmt.Sprintf("could not read the downtime %s: %v", d.Name, err))
				continue
			}
			for _, id := range full.AlertRuleIDs {
				downtimes[id] = d.Name
			}
		}
	})
	wg.Wait()
	if rulesErr != nil {
		return alerts.Overview{}, rulesErr
	}
	if eventsErr != nil {
		return alerts.Overview{}, eventsErr
	}

	var out alerts.Overview
	for _, w := range []struct {
		what string
		err  error
	}{{"notification channels", chanErr}, {"acknowledgements", ackErr}, {"downtimes", downErr}} {
		if w.err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("could not read the %s: %v", w.what, w.err))
		}
	}
	out.Warnings = append(out.Warnings, downWarnings...)

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

// History implements alerts.Reader. The platform keeps the history of a
// rule's event, whose ID does not change with the status.
func (a *Alerts) History(ctx context.Context, al alerts.Alert, from, to time.Time, step time.Duration) (alerts.History, error) {
	step = max(step, time.Minute).Truncate(time.Second)
	h := alerts.History{From: from, To: to, Step: step}
	if al.Event == nil || al.Event.ID == "" {
		return h, nil
	}
	curves, err := a.api.AlertHistory(ctx, al.Event.ID, from, to, step)
	if err != nil {
		return h, err
	}
	for _, c := range curves {
		for i, ms := range c.Times {
			if i < len(c.Data) {
				h.Points = append(h.Points, alerts.StatusPoint{At: time.UnixMilli(ms), Status: historyStatus(c.Data[i])})
			}
		}
	}
	slices.SortFunc(h.Points, func(x, y alerts.StatusPoint) int { return x.At.Compare(y.At) })
	return h, nil
}

func historyStatus(v float64) alerts.Status {
	switch int(v) {
	case api.AlertHistoryOK:
		return alerts.StatusOK
	case api.AlertHistoryWarning:
		return alerts.StatusWarning
	case api.AlertHistoryError:
		return alerts.StatusError
	case api.AlertHistoryCritical:
		return alerts.StatusCritical
	}
	return alerts.StatusUnknown
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
