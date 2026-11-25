package remote

import (
	"context"
	"sort"

	"github.com/CentreonLabs/lazyobs/internal/api"
	"github.com/CentreonLabs/lazyobs/internal/obs"
)

var _ obs.Breakdowner = (*Logs)(nil)

// Breakdown implements obs.Breakdowner with the platform's per-field
// aggregation, which honors the query and returns at most 20 values.
func (l *Logs) Breakdown(ctx context.Context, q obs.Query, field string) (obs.Breakdown, error) {
	start, end := l.bounds(q)
	resp, err := l.api.SearchLogs(ctx, api.LogSearchRequest{
		Query:       queryText(q.Text),
		Start:       &start,
		End:         &end,
		MaxHits:     0,
		Aggregation: &api.Aggregation{CustomField: field, TargetResolution: 10},
	})
	if err != nil {
		return obs.Breakdown{}, err
	}
	b := obs.Breakdown{Total: resp.NumHits}
	for _, c := range resp.Curves {
		var n float64
		for _, v := range c.Data {
			n += v
		}
		value := ""
		for _, kv := range c.Attributes {
			if kv[0] == field {
				value = kv[1]
			}
		}
		if n > 0 {
			b.Values = append(b.Values, obs.FieldValue{Value: value, Count: int64(n + 0.5)})
		}
	}
	sort.SliceStable(b.Values, func(i, j int) bool { return b.Values[i].Count > b.Values[j].Count })
	return b, nil
}
