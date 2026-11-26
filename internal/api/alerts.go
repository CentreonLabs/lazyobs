package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// maxPageSize is the largest page the alerting endpoints accept.
const maxPageSize = 100

// maxPages bounds a listing, as a safety net against a looping pager.
const maxPages = 50

// AlertRule is an alert rule. Params, EvalErrorHandling and the notification
// conditions are only returned by GetAlertRule.
type AlertRule struct {
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	TemplateName           string         `json:"templateName"`
	TemplateVersion        int            `json:"templateVersion"`
	Enabled                bool           `json:"enabled"`
	FrequencySeconds       int            `json:"frequency"`
	Attributes             map[string]any `json:"attributes"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
	NotificationChannelIDs []string       `json:"notificationChannelIds"`
	Params                 *AlertParams   `json:"params,omitempty"`
}

// AlertParams are the template parameters of the log templates: one query
// (count_logs) or a dividend and a divisor (ratio_logs).
type AlertParams struct {
	AlertConditions []AlertCondition `json:"alertConditions"`
	Query           string           `json:"query,omitempty"`
	QueryDividend   string           `json:"queryDividend,omitempty"`
	QueryDivisor    string           `json:"queryDivisor,omitempty"`
}

// AlertCondition gives a status when the value compares to the threshold.
type AlertCondition struct {
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Status    string  `json:"status"`
}

// AlertEvent is the latest evaluation of a rule.
type AlertEvent struct {
	ID         string         `json:"id"`
	RuleID     string         `json:"ruleId"`
	RunID      string         `json:"runId"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	Status     string         `json:"status"` // unknown, ok, warning, error, critical
	Message    string         `json:"message"`
	Attributes map[string]any `json:"attributes"`
}

// Event attributes set by the platform.
const (
	AttrEvalErrorDetails = "centreon.obs.alert.event.eval_error.details"
	AttrFlapping         = "centreon.obs.alert.event.flapping_detected"
	AttrExternalLinks    = "centreon.obs.alert.rule.external_links"
)

// AlertEventContext is what an evaluation computed: type "count" with
// {query, count}, or "ratio" with {ratio, queryDividend, queryDivisor}.
type AlertEventContext struct {
	Type               string         `json:"type"`
	IntermediateValues map[string]any `json:"intermediateValues"`
}

// Acknowledgement is an active acknowledgement of an alert event.
type Acknowledgement struct {
	ID           string    `json:"id"`
	AckBy        string    `json:"ackBy"`
	AckAt        time.Time `json:"ackAt"`
	AckComment   *string   `json:"ackComment"`
	ExpiresAt    time.Time `json:"expiresAt"`
	AlertEventID string    `json:"alertEventId"`
	AlertRuleID  string    `json:"alertRuleId"`
}

// Downtime mutes alert rules during a time window. AlertRuleIDs is only
// returned by GetDowntime.
type Downtime struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`   // fixed, recurring
	Status          string    `json:"status"` // active, upcoming, finished
	AlertRulesCount int       `json:"alertRulesCount"`
	StartDate       time.Time `json:"startDate"`
	EndDate         time.Time `json:"endDate"`
	AlertRuleIDs    []string  `json:"alertRuleIds,omitempty"`
}

// NotificationChannel is where alert notifications are sent.
type NotificationChannel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Integration string `json:"integration"` // webhook, email, ...
	Enabled     bool   `json:"enabled"`
}

type pageMeta struct {
	Total int     `json:"total"`
	Next  *string `json:"next"`
}

// listAll reads every page of a paginated listing.
func listAll[T any](ctx context.Context, c *Client, path []string, query url.Values) ([]T, error) {
	var all []T
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", strconv.Itoa(maxPageSize))
	for page := 1; page <= maxPages; page++ {
		query.Set("page", strconv.Itoa(page))
		var resp struct {
			Results []T      `json:"results"`
			Meta    pageMeta `json:"meta"`
		}
		if err := c.do(ctx, http.MethodGet, c.orgURL(path...)+"?"+query.Encode(), nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		if resp.Meta.Next == nil || len(resp.Results) == 0 || len(all) >= resp.Meta.Total {
			break
		}
	}
	return all, nil
}

// ListAlertRules lists every alert rule, enabled or not.
func (c *Client) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	return listAll[AlertRule](ctx, c, []string{"alerts"}, url.Values{"status": {"all"}})
}

// GetAlertRule returns a rule with its template parameters.
func (c *Client) GetAlertRule(ctx context.Context, id string) (AlertRule, error) {
	var r AlertRule
	err := c.do(ctx, http.MethodGet, c.orgURL("alerts", id), nil, &r)
	return r, err
}

// ListAlertEvents lists the current event of every evaluated rule.
func (c *Client) ListAlertEvents(ctx context.Context) ([]AlertEvent, error) {
	return listAll[AlertEvent](ctx, c, []string{"alerts", "events"}, nil)
}

// AlertEventContext returns what the evaluation of an event computed.
func (c *Client) AlertEventContext(ctx context.Context, ruleID, eventID, runID string) (AlertEventContext, error) {
	var out AlertEventContext
	endpoint := c.orgURL("alerts", ruleID, "events", eventID)
	if runID != "" {
		endpoint += "?" + url.Values{"runId": {runID}}.Encode()
	}
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out, err
}

// ListAcknowledgements lists the active acknowledgements.
func (c *Client) ListAcknowledgements(ctx context.Context) ([]Acknowledgement, error) {
	return listAll[Acknowledgement](ctx, c, []string{"alerts", "acknowledgements"}, nil)
}

// ListDowntimes lists the downtimes with the given status (active, upcoming,
// finished); an empty status lists them all.
func (c *Client) ListDowntimes(ctx context.Context, status string) ([]Downtime, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	return listAll[Downtime](ctx, c, []string{"alerts", "downtimes"}, q)
}

// GetDowntime returns a downtime with the rules it mutes.
func (c *Client) GetDowntime(ctx context.Context, id string) (Downtime, error) {
	var d Downtime
	err := c.do(ctx, http.MethodGet, c.orgURL("alerts", "downtimes", id), nil, &d)
	return d, err
}

// ListNotificationChannels lists the notification channels.
func (c *Client) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	return listAll[NotificationChannel](ctx, c, []string{"notifications", "channel-configs"}, nil)
}

// DecodeExternalLinks reads the external links attribute of a rule.
func DecodeExternalLinks(attrs map[string]any) []ExternalLink {
	raw, ok := attrs[AttrExternalLinks]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var links []ExternalLink
	if json.Unmarshal(b, &links) != nil {
		return nil
	}
	return links
}

// ExternalLink is a link attached to a rule.
type ExternalLink struct {
	Label       string `json:"label"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// AlertHistoryRequest is the body of POST
// /v1/orgs/{org}/datasources/alert-events/query/metrics.
type AlertHistoryRequest struct {
	Version         string      `json:"version"`
	EventID         string      `json:"eventId"`
	Period          AlertPeriod `json:"period"`
	IntervalSeconds int         `json:"interval,omitempty"`
}

// AlertPeriod is an absolute time window.
type AlertPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// MetricsCurve is a series of the metrics datasources. Times are Unix
// milliseconds.
type MetricsCurve struct {
	Metric string    `json:"metric"`
	Times  []int64   `json:"times"`
	Data   []float64 `json:"data"`
}

// Alert status codes in the history curve: the worst status of each
// interval. Intervals without evaluation are left out.
const (
	AlertHistoryUnknown  = 0
	AlertHistoryOK       = 1
	AlertHistoryWarning  = 2
	AlertHistoryError    = 3
	AlertHistoryCritical = 4
)

// AlertHistory returns the status curve of a rule's event over a window.
func (c *Client) AlertHistory(ctx context.Context, eventID string, start, end time.Time, interval time.Duration) ([]MetricsCurve, error) {
	req := AlertHistoryRequest{
		Version: "1", EventID: eventID, Period: AlertPeriod{Start: start.UTC(), End: end.UTC()},
		IntervalSeconds: max(int(interval/time.Second), 1),
	}
	var resp struct {
		Curves []MetricsCurve `json:"curves"`
	}
	err := c.do(ctx, http.MethodPost, c.orgURL("datasources", "alert-events", "query", "metrics"), req, &resp)
	return resp.Curves, err
}
