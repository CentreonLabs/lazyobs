//go:build integration

package api_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/secret"
)

// Run against the live platform with:
//
//	LAZYOBS_TOKEN=... LAZYOBS_ORG=... go test -tags integration ./internal/api/
func TestIntegrationSearchLogs(t *testing.T) {
	token, org := os.Getenv("LAZYOBS_TOKEN"), os.Getenv("LAZYOBS_ORG")
	if token == "" || org == "" {
		t.Skip("LAZYOBS_TOKEN and LAZYOBS_ORG are required")
	}
	c, err := api.New(os.Getenv("LAZYOBS_URL"), org, secret.New(token))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := int64(-30 * 24 * 3600)
	resp, err := c.SearchLogs(ctx, api.LogSearchRequest{
		Query:       "*",
		Start:       &start,
		MaxHits:     5,
		Aggregation: &api.Aggregation{Type: api.AggregateCount, TargetResolution: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("num_hits=%d hits=%d curves=%d elapsed=%dµs", resp.NumHits, len(resp.Hits), len(resp.Curves), resp.ElapsedTimeMicros)

	var prev int64
	for i, raw := range resp.Hits {
		h, err := api.DecodeLogHit(raw)
		if err != nil {
			t.Fatal(err)
		}
		if h.TimestampNanos == 0 {
			t.Errorf("hit %d has no timestamp", i)
		}
		if i > 0 && h.TimestampNanos/1e9 > prev/1e9 {
			t.Errorf("hits are not sorted newest first (per second)")
		}
		prev = h.TimestampNanos
	}
}
