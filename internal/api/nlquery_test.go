package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
)

func TestTranslateQuery(t *testing.T) {
	var got api.TranslateRequest
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orgs/my-org_1/log-analysis/nlquery/translate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"query":"service_name:query AND severity_number:>=17","time_range":7200000,"warning":null}`))
	})
	resp, err := c.TranslateQuery(context.Background(), api.TranslateRequest{Prompt: "errors from query", AvailableServices: []string{"query"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "errors from query" || len(got.AvailableServices) != 1 {
		t.Errorf("request = %+v", got)
	}
	if resp.Query != "service_name:query AND severity_number:>=17" || resp.Warning != nil {
		t.Errorf("response = %+v", resp)
	}
	if d, ok := resp.LookBack(); !ok || d != 2*time.Hour {
		t.Errorf("LookBack = %v %v", d, ok)
	}
	if _, _, ok := resp.Window(); ok {
		t.Error("a relative hint is not a window")
	}

	if _, err := c.TranslateQuery(context.Background(), api.TranslateRequest{}); err == nil {
		t.Error("empty prompt must fail before calling the API")
	}
}

func TestTranslateTimeHints(t *testing.T) {
	window := api.TranslateResponse{TimeRange: json.RawMessage(`{"start":1793709000000,"end":1793714400000}`)}
	from, to, ok := window.Window()
	if !ok || !from.Equal(time.UnixMilli(1793709000000)) || !to.Equal(time.UnixMilli(1793714400000)) {
		t.Errorf("Window = %v %v %v", from, to, ok)
	}
	if _, ok := window.LookBack(); ok {
		t.Error("a window is not a look-back")
	}
	none := api.TranslateResponse{TimeRange: json.RawMessage(`null`)}
	if _, ok := none.LookBack(); ok {
		t.Error("null has no hint")
	}
	if _, _, ok := none.Window(); ok {
		t.Error("null has no hint")
	}
}

func TestLogFieldTerms(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/logs/fields/service_name/terms" ||
			r.URL.Query().Get("maxTerms") != "100" || r.URL.Query().Get("startTimestamp") != "1700000000" ||
			r.URL.Query().Get("filter") != "a" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, _ = w.Write([]byte(`[{"value":"api","count":12},{"value":404,"count":3}]`))
	})
	terms, err := c.LogFieldTerms(context.Background(), "service_name", "a", time.Unix(1700000000, 0), time.Unix(1700003600, 0), 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 2 || terms[0].String() != "api" || terms[1].String() != "404" || terms[0].Count != 12 {
		t.Errorf("terms = %+v", terms)
	}
}

func TestLogFieldCapabilities(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orgs/my-org_1/logs/fields/capabilities" || r.URL.Query().Get("endTimestamp") != "1700003600" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"fields":{"service_name":{"keyword":{"type":"keyword","searchable":true,"aggregatable":true}},` +
			`"body.message":{"text":{"type":"text","searchable":true,"aggregatable":false}}}}`))
	})
	fields, err := c.LogFieldCapabilities(context.Background(), time.Unix(1700000000, 0), time.Unix(1700003600, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !fields["service_name"]["keyword"].Aggregatable || fields["body.message"]["text"].Aggregatable || !fields["body.message"]["text"].Searchable {
		t.Errorf("fields = %+v", fields)
	}
}
