package fake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs/alerts"
)

// Alerts is a synthetic alerts reader: its rules are evaluated on the demo
// logs, so they turn red during the demo incidents.
type Alerts struct {
	logs *Logs
	now  func() time.Time
	// Latency, when set, delays every call to mimic the network.
	Latency time.Duration

	mu     sync.Mutex
	counts map[countKey]map[string]float64 // query counts per evaluation
}

var _ alerts.Reader = (*Alerts)(nil)

// NewAlerts returns alert rules evaluated on logs. A nil now uses time.Now.
func NewAlerts(logs *Logs, now func() time.Time) *Alerts {
	if now == nil {
		now = time.Now
	}
	return &Alerts{logs: logs, now: now}
}

type demoRule struct {
	alerts.Rule
	queries    []alerts.Query
	conditions []alerts.Condition
	ack        bool // acknowledged when not ok
	downtime   string
}

func cond(op string, threshold float64, s alerts.Status) alerts.Condition {
	return alerts.Condition{Operator: op, Threshold: threshold, Status: s}
}

var demoRules = []demoRule{
	{
		Rule: alerts.Rule{
			ID: "demo-error-burst", Name: "Error burst", Template: "count_logs", Description: "Errors across all services",
			Channels: []string{"Ops webhook"}, Links: []alerts.Link{{Label: "Runbook", URL: "https://example.com/runbooks/error-burst"}},
		},
		queries:    []alerts.Query{{Name: "query", Text: "severity_text:ERROR"}},
		conditions: []alerts.Condition{cond(">", 120, alerts.StatusCritical), cond(">", 60, alerts.StatusWarning)},
	},
	{
		Rule:       alerts.Rule{ID: "demo-payment-declines", Name: "Payment declines", Template: "count_logs", Channels: []string{"Ops webhook", "Payments team"}},
		queries:    []alerts.Query{{Name: "query", Text: "declined"}},
		conditions: []alerts.Condition{cond(">", 25, alerts.StatusCritical), cond(">", 12, alerts.StatusWarning)},
		ack:        true,
	},
	{
		Rule: alerts.Rule{ID: "demo-checkout-ratio", Name: "Checkout error ratio", Template: "ratio_logs", Description: "Share of checkout logs in error"},
		queries: []alerts.Query{
			{Name: "dividend", Text: "service_name:checkout AND severity_text:ERROR"},
			{Name: "divisor", Text: "service_name:checkout"},
		},
		conditions: []alerts.Condition{cond(">", 0.25, alerts.StatusCritical), cond(">", 0.18, alerts.StatusWarning)},
	},
	{
		Rule:       alerts.Rule{ID: "demo-auth-fatal", Name: "Auth fatal errors", Template: "count_logs"},
		queries:    []alerts.Query{{Name: "query", Text: "service_name:auth AND severity_text:FATAL"}},
		conditions: []alerts.Condition{cond(">", 0, alerts.StatusError)},
		downtime:   "Auth migration",
	},
	{
		Rule:       alerts.Rule{ID: "demo-inventory-watchdog", Name: "Inventory watchdog", Template: "count_logs", Description: "Inventory stopped logging"},
		queries:    []alerts.Query{{Name: "query", Text: "service_name:inventory"}},
		conditions: []alerts.Condition{cond("<", 1, alerts.StatusCritical)},
	},
	{
		Rule: alerts.Rule{ID: "demo-mailer-bounces", Name: "Mailer bounce ratio", Template: "ratio_logs"},
		queries: []alerts.Query{
			{Name: "dividend", Text: "service_name:mailer AND bounced"},
			{Name: "divisor", Text: "service_name:mailer"},
		},
		conditions: []alerts.Condition{cond(">", 0.1, alerts.StatusWarning)},
	},
	{
		Rule:       alerts.Rule{ID: "demo-legacy-syslog", Name: "Legacy syslog login failed", Template: "count_logs"},
		queries:    []alerts.Query{{Name: "query", Text: "login failed"}},
		conditions: []alerts.Condition{cond(">", 0, alerts.StatusCritical)},
	},
}

// disabledRule is the rule of the list that is not evaluated.
const disabledRule = "demo-legacy-syslog"

const demoFrequency = 5 * time.Minute

// Alerts implements alerts.Reader.
func (a *Alerts) Alerts(ctx context.Context) (alerts.Overview, error) {
	if err := wait(ctx, a.Latency); err != nil {
		return alerts.Overview{}, err
	}
	at := a.now().Truncate(demoFrequency)
	var out alerts.Overview
	for _, r := range demoRules {
		al := alerts.Alert{Rule: r.Rule}
		al.Rule.Enabled, al.Rule.Frequency, al.Rule.UpdatedAt = r.ID != disabledRule, demoFrequency, at.Add(-72*time.Hour)
		if !al.Rule.Enabled {
			out.Alerts = append(out.Alerts, al)
			continue
		}
		ev := &alerts.Event{ID: fmt.Sprintf("%s-%d", r.ID, at.Unix()), RunID: fmt.Sprint(at.Unix()), At: at}
		v, err := a.evaluate(ctx, r, at)
		if err != nil {
			ev.Status, ev.EvalError, ev.Message = alerts.StatusOK, err.Error(), "Evaluation failed, last known status kept"
		} else {
			ev.Status = status(r.conditions, v)
			ev.Message = "Event generated with status: " + ev.Status.String()
		}
		al.Event = ev
		if r.ack && ev.Status != alerts.StatusOK {
			al.Ack = &alerts.Ack{By: "oncall@example.com", At: at.Add(-time.Minute), Until: at.Add(time.Hour), Comment: "looking into the payment provider"}
		}
		al.Downtime = r.downtime
		out.Alerts = append(out.Alerts, al)
	}
	slices.SortFunc(out.Alerts, alerts.Compare)
	return out, nil
}

// Details implements alerts.Reader.
func (a *Alerts) Details(ctx context.Context, al alerts.Alert) (alerts.Details, error) {
	if err := wait(ctx, a.Latency); err != nil {
		return alerts.Details{}, err
	}
	i := slices.IndexFunc(demoRules, func(r demoRule) bool { return r.ID == al.Rule.ID })
	if i < 0 {
		return alerts.Details{}, fmt.Errorf("fake: alert rule %s not found", al.Rule.ID)
	}
	r := demoRules[i]
	d := alerts.Details{Queries: r.queries, Conditions: r.conditions}
	if al.Event != nil && !al.Failing() {
		if v, err := a.evaluate(ctx, r, al.Event.At); err == nil {
			d.Measure, d.Value = "count", v
			if r.Template == "ratio_logs" {
				d.Measure = "ratio"
			}
		}
	}
	return d, nil
}

var errDivisionByZero = errors.New("invalid operation in flow: tried to make a division by zero")

// evaluate computes the rule's value over the frequency window ending at at.
func (a *Alerts) evaluate(ctx context.Context, r demoRule, at time.Time) (float64, error) {
	return a.evaluateSampled(ctx, r, at, 1)
}

// evaluateSampled evaluates on one second out of stride, scaling the counts.
func (a *Alerts) evaluateSampled(ctx context.Context, r demoRule, at time.Time, stride int64) (float64, error) {
	counts, err := a.countsAt(ctx, at, stride)
	if err != nil {
		return 0, err
	}
	n := counts[r.queries[0].Text]
	if len(r.queries) == 1 {
		return n, nil
	}
	d := counts[r.queries[1].Text]
	if d == 0 {
		return 0, errDivisionByZero
	}
	return n / d, nil
}

// maxCachedEvaluations bounds the cache of evaluation windows: a few weeks
// of 5-minute evaluations, enough for every history range at once.
const maxCachedEvaluations = 4 * 7 * 24 * 12

type countKey struct{ at, stride int64 }

// countsAt counts the logs matching every rule query over the window ending
// at at, generating the window once for all rules (one second out of
// stride), and caches the counts: demo data never changes.
func (a *Alerts) countsAt(ctx context.Context, at time.Time, stride int64) (map[string]float64, error) {
	key := countKey{at.Unix(), max(stride, 1)}
	a.mu.Lock()
	c, ok := a.counts[key]
	a.mu.Unlock()
	if ok {
		return c, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filters := map[string]filter{}
	for _, r := range demoRules {
		for _, q := range r.queries {
			f, err := parseFilter(q.Text)
			if err != nil {
				return nil, err
			}
			filters[q.Text] = f
		}
	}
	c = make(map[string]float64, len(filters))
	for sec := at.Add(-demoFrequency).Unix(); sec < key.at; sec += key.stride {
		for _, rec := range a.logs.recordsAt(sec) {
			for q, f := range filters {
				if f.match(rec) {
					c[q] += float64(key.stride)
				}
			}
		}
	}
	a.mu.Lock()
	if a.counts == nil || len(a.counts) >= maxCachedEvaluations {
		a.counts = map[countKey]map[string]float64{}
	}
	a.counts[key] = c
	a.mu.Unlock()
	return c, nil
}

// maxHistorySeconds bounds the seconds of logs generated for a history;
// longer ones sample the evaluation windows (demo data only).
const maxHistorySeconds = 2 * 3600

// History implements alerts.Reader: the rule is evaluated every 5 minutes
// over the window, each step keeping the worst status.
func (a *Alerts) History(ctx context.Context, al alerts.Alert, from, to time.Time, step time.Duration) (alerts.History, error) {
	if err := wait(ctx, a.Latency); err != nil {
		return alerts.History{}, err
	}
	step = max(step, demoFrequency).Truncate(demoFrequency)
	h := alerts.History{From: from, To: to, Step: step}
	i := slices.IndexFunc(demoRules, func(r demoRule) bool { return r.ID == al.Rule.ID })
	if i < 0 || al.Event == nil {
		return h, nil
	}
	r := demoRules[i]
	last := a.now().Truncate(demoFrequency)
	stride := max(int64(to.Sub(from)/time.Second)/maxHistorySeconds, 1)
	for b := from.Truncate(step); b.Before(to); b = b.Add(step) {
		p := alerts.StatusPoint{At: b, Status: alerts.NoData}
		for at := b; at.Before(b.Add(step)) && !at.After(last); at = at.Add(demoFrequency) {
			if at.Before(from) {
				continue
			}
			s := alerts.StatusOK // a failing evaluation keeps the last status: ok here
			if v, err := a.evaluateSampled(ctx, r, at, stride); err == nil {
				s = status(r.conditions, v)
			} else if ctx.Err() != nil {
				return h, ctx.Err()
			}
			p.Status = max(p.Status, s)
		}
		if p.Status != alerts.NoData {
			h.Points = append(h.Points, p)
		}
	}
	return h, nil
}

// status applies the conditions, first match wins.
func status(conds []alerts.Condition, v float64) alerts.Status {
	for _, c := range conds {
		if (c.Operator == ">" && v > c.Threshold) || (c.Operator == ">=" && v >= c.Threshold) ||
			(c.Operator == "<" && v < c.Threshold) || (c.Operator == "<=" && v <= c.Threshold) {
			return c.Status
		}
	}
	return alerts.StatusOK
}
