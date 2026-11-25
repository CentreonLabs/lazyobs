package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

func TestFieldSuggestions(t *testing.T) {
	src := fake.NewLogs(4, clock(now))
	fields, err := src.Fields(context.Background(), now.Add(-time.Hour), now)
	if err != nil || len(fields) < 8 {
		t.Fatalf("fields = %v, %v", fields, err)
	}
	values, err := src.Values(context.Background(), "service_name", "", now.Add(-time.Hour), now, 3)
	if err != nil || len(values) != 3 || values[0].Count < values[2].Count {
		t.Fatalf("values = %+v, %v", values, err)
	}
	prefixed, _ := src.Values(context.Background(), "attributes.http.method", "p", now.Add(-time.Hour), now, 10)
	for _, v := range prefixed {
		if v.Value != "POST" && v.Value != "PUT" {
			t.Errorf("prefix filter leaked %q", v.Value)
		}
	}
	if len(prefixed) != 2 {
		t.Errorf("prefixed = %+v", prefixed)
	}
	// Suggested values are searchable.
	page, err := src.Search(context.Background(), obs.Query{
		Text: "resource_attributes.host.name:" + mustValue(t, src, "resource_attributes.host.name"), Range: obs.Last(time.Hour), Limit: 5,
	})
	if err != nil || page.Total == 0 {
		t.Errorf("searching a suggested value found nothing: %v", err)
	}
}

func mustValue(t *testing.T, src *fake.Logs, field string) string {
	t.Helper()
	v, err := src.Values(context.Background(), field, "", now.Add(-time.Hour), now, 1)
	if err != nil || len(v) == 0 {
		t.Fatalf("no value for %s: %v", field, err)
	}
	return v[0].Value
}
