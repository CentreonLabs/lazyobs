package remote_test

import (
	"context"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/remote"
)

func TestBreakdown(t *testing.T) {
	stub := &stubAPI{respond: func(req api.LogSearchRequest) (*api.LogSearchResponse, error) {
		if req.Aggregation == nil || req.Aggregation.CustomField != "service_name" || req.MaxHits != 0 || req.Query != "severity_text:ERROR" {
			t.Errorf("unexpected request %+v (aggregation %+v)", req, req.Aggregation)
		}
		return &api.LogSearchResponse{NumHits: 100, Curves: []api.Curve{
			{Attributes: [][2]string{{"service_name", "query"}}, Data: []float64{10, 20}},
			{Attributes: [][2]string{{"service_name", "ingest"}}, Data: []float64{30, 25}},
			{Attributes: [][2]string{{"service_name", "idle"}}, Data: []float64{0, 0}},
		}}, nil
	}}
	b, err := remote.NewLogs(stub, func() time.Time { return now }).
		Breakdown(context.Background(), obs.Query{Text: "severity_text:ERROR", Range: obs.Last(time.Hour)}, "service_name")
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 100 || len(b.Values) != 2 || b.Values[0] != (obs.FieldValue{Value: "ingest", Count: 55}) || b.Values[1].Count != 30 {
		t.Errorf("breakdown = %+v", b)
	}
}
