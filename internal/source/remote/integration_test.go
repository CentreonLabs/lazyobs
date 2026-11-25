//go:build integration

package remote_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/secret"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

// Run against the live platform with:
//
//	LAZYOBS_TOKEN=... LAZYOBS_ORG=... go test -tags integration ./internal/source/remote/
func TestIntegrationTraces(t *testing.T) {
	token, org := os.Getenv("LAZYOBS_TOKEN"), os.Getenv("LAZYOBS_ORG")
	if token == "" || org == "" {
		t.Skip("LAZYOBS_TOKEN and LAZYOBS_ORG are required")
	}
	c, err := api.New(os.Getenv("LAZYOBS_URL"), org, secret.New(token))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	src := remote.NewTraces(c, nil)
	rng := obs.Last(7 * 24 * time.Hour)

	for _, order := range []obs.Order{obs.OrderNewest, obs.OrderSlowest} {
		page, err := src.Search(ctx, obs.Query{Range: rng, Limit: 50, Order: order})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Skip("no trace in the last week")
		}
		for i := 1; i < len(page.Items); i++ {
			prev, cur := page.Items[i-1].Root, page.Items[i].Root
			if order == obs.OrderNewest && cur.Start.After(prev.Start) || order == obs.OrderSlowest && cur.Duration > prev.Duration {
				t.Errorf("order %v: not descending at %d", order, i)
			}
		}
		t.Logf("order %v: %d traces, first %s %v", order, page.Total, page.Items[0].Root.Name, page.Items[0].Root.Duration)
	}

	page, _ := src.Search(ctx, obs.Query{Range: rng, Limit: 1})
	d, err := src.Trace(ctx, page.Items[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	tree := d.Tree()
	if len(tree) != len(d.Spans) || len(tree) == 0 || tree[0].Depth != 0 {
		t.Fatalf("tree of %d spans: %d nodes", len(d.Spans), len(tree))
	}
	for _, n := range tree {
		t.Logf("%*s%s %s %v %s", n.Depth*2, "", n.Service, n.Name, n.Duration, n.Status)
	}

	series, err := src.Histogram(ctx, obs.Query{Range: rng}, 20)
	if err != nil || len(series) != 2 {
		t.Fatalf("histogram: %v %v", series, err)
	}
}
