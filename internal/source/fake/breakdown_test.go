package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
)

func TestBreakdown(t *testing.T) {
	src := fake.NewLogs(6, clock(now))
	all, err := src.Breakdown(context.Background(), obs.Query{Range: obs.Last(10 * time.Minute)}, "service_name")
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, v := range all.Values {
		sum += v.Count
	}
	if len(all.Values) != 5 || sum != all.Total || all.Values[0].Count < all.Values[4].Count {
		t.Fatalf("breakdown = %+v", all)
	}
	errors, _ := src.Breakdown(context.Background(), obs.Query{Text: "severity_text:ERROR", Range: obs.Last(10 * time.Minute)}, "service_name")
	if errors.Total == 0 || errors.Total >= all.Total {
		t.Errorf("the query must be honored: %d errors of %d", errors.Total, all.Total)
	}
}
