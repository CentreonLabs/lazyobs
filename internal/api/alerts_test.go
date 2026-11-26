package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
)

func TestListAlertRulesReadsEveryPage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v1/orgs/my-org_1/alerts" || q.Get("status") != "all" || q.Get("limit") != "100" {
			t.Errorf("unexpected %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		switch q.Get("page") {
		case "1":
			_, _ = w.Write(fixture(t, "alert_rules.json"))
		case "2":
			_, _ = w.Write(fixture(t, "alert_rules_page2.json"))
		default:
			t.Errorf("unexpected page %s", q.Get("page"))
		}
	})
	rules, err := c.ListAlertRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 || rules[2].ID != "rule-new" {
		t.Fatalf("rules = %+v", rules)
	}
	r := rules[0]
	if r.Name != "Apache 404 ratio" || r.TemplateName != "ratio_logs" || !r.Enabled || r.FrequencySeconds != 300 ||
		len(r.NotificationChannelIDs) != 1 || r.Params != nil || rules[1].Enabled {
		t.Errorf("rule = %+v", r)
	}
	links := api.DecodeExternalLinks(r.Attributes)
	if len(links) != 1 || links[0].Label != "Runbook" || links[0].URL != "https://example.com/runbook" {
		t.Errorf("links = %+v", links)
	}
	if api.DecodeExternalLinks(rules[1].Attributes) != nil {
		t.Error("no links expected")
	}
}

func TestGetAlertRule(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/alerts/rule-ratio" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write(fixture(t, "alert_rule.json"))
	})
	r, err := c.GetAlertRule(context.Background(), "rule-ratio")
	if err != nil {
		t.Fatal(err)
	}
	p := r.Params
	if p == nil || p.QueryDivisor != "service_name:httpd" || p.QueryDividend == "" || len(p.AlertConditions) != 2 ||
		p.AlertConditions[0] != (api.AlertCondition{Operator: ">", Threshold: 0.05, Status: "critical"}) {
		t.Errorf("params = %+v", p)
	}
}

func TestListAlertEvents(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/alerts/events" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		_, _ = w.Write(fixture(t, "alert_events.json"))
	})
	evs, err := c.ListAlertEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Status != "critical" || evs[0].RuleID != "rule-ratio" || evs[0].RunID != "run-1" ||
		!evs[0].UpdatedAt.Equal(time.Date(2026, 11, 26, 10, 15, 0, 178e6, time.UTC)) {
		t.Errorf("events = %+v", evs)
	}
	if evs[1].Attributes[api.AttrEvalErrorDetails] == nil || evs[1].Attributes[api.AttrFlapping] != true {
		t.Errorf("attributes = %+v", evs[1].Attributes)
	}
}

func TestAlertEventContext(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/alerts/rule-1/events/ev-1" || r.URL.Query().Get("runId") != "run-1" {
			t.Errorf("unexpected %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"type":"count","intermediateValues":{"query":"*","count":12.0}}`))
	})
	ctx, err := c.AlertEventContext(context.Background(), "rule-1", "ev-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Type != "count" || ctx.IntermediateValues["count"] != 12.0 {
		t.Errorf("context = %+v", ctx)
	}
}

func TestDowntimesAndAcknowledgements(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/orgs/my-org_1/alerts/downtimes":
			if r.URL.Query().Get("status") != "active" {
				t.Errorf("status = %q", r.URL.Query().Get("status"))
			}
			_, _ = w.Write([]byte(`{"results":[{"id":"dt-1","name":"Maintenance","kind":"fixed","status":"active","alertRulesCount":1,
				"startDate":"2026-11-26T09:00:00Z","endDate":"2026-11-26T11:00:00Z","createdAt":"2026-11-26T08:00:00Z","updatedAt":"2026-11-26T08:00:00Z"}],
				"meta":{"total":1,"page":1,"limit":100,"first":"","last":""}}`))
		case "/v1/orgs/my-org_1/alerts/downtimes/dt-1":
			_, _ = w.Write([]byte(`{"id":"dt-1","name":"Maintenance","alertRuleIds":["rule-1"],"kind":"fixed","status":"active",
				"startDate":"2026-11-26T09:00:00Z","endDate":"2026-11-26T11:00:00Z"}`))
		case "/v1/orgs/my-org_1/alerts/acknowledgements":
			_, _ = w.Write([]byte(`{"results":[{"id":"ack-1","ackBy":"someone","ackAt":"2026-11-26T09:30:00Z","ackComment":"on it",
				"ackDuration":3600,"expiresAt":"2026-11-26T10:30:00Z","alertEventId":"ev-1","alertRuleId":"rule-2"}],
				"meta":{"total":1,"page":1,"limit":100,"first":"","last":""}}`))
		case "/v1/orgs/my-org_1/notifications/channel-configs":
			_, _ = w.Write([]byte(`{"results":[{"id":"chan-1","name":"Ops webhook","integration":"webhook","enabled":true}],
				"meta":{"total":1,"page":1,"limit":100,"first":"","last":""}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	dts, err := c.ListDowntimes(ctx, "active")
	if err != nil || len(dts) != 1 || dts[0].Name != "Maintenance" {
		t.Fatalf("downtimes = %+v, %v", dts, err)
	}
	dt, err := c.GetDowntime(ctx, "dt-1")
	if err != nil || len(dt.AlertRuleIDs) != 1 {
		t.Fatalf("downtime = %+v, %v", dt, err)
	}
	acks, err := c.ListAcknowledgements(ctx)
	if err != nil || len(acks) != 1 || acks[0].AckBy != "someone" || *acks[0].AckComment != "on it" || acks[0].AlertRuleID != "rule-2" {
		t.Fatalf("acks = %+v, %v", acks, err)
	}
	chans, err := c.ListNotificationChannels(ctx)
	if err != nil || len(chans) != 1 || chans[0].Name != "Ops webhook" {
		t.Fatalf("channels = %+v, %v", chans, err)
	}
}
