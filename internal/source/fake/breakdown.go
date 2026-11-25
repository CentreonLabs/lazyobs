package fake

import (
	"context"
	"sort"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

// maxBreakdownValues mirrors the platform's per-field aggregation limit.
const maxBreakdownValues = 20

var _ obs.Breakdowner = (*Logs)(nil)

// Breakdown implements obs.Breakdowner by counting values over a sample of
// the matching logs.
func (l *Logs) Breakdown(ctx context.Context, q obs.Query, field string) (obs.Breakdown, error) {
	if err := l.wait(ctx); err != nil {
		return obs.Breakdown{}, err
	}
	from, to := l.window(q)
	f, err := parseFilter(q.Text)
	if err != nil {
		return obs.Breakdown{}, err
	}
	counts := map[string]int64{}
	var total int64
	first, last := from.Unix(), ceilSecond(to)
	stride := max((last-first)/maxSampledFieldSeconds, 1)
	for sec := first; sec < last; sec += stride {
		for _, r := range l.recordsAt(sec) {
			if r.Timestamp.Before(from) || !r.Timestamp.Before(to) || !f.match(r) {
				continue
			}
			total += stride
			if v, ok := fieldValue(r, field); ok {
				counts[v] += stride
			}
		}
	}
	b := obs.Breakdown{Total: total}
	for v, n := range counts {
		b.Values = append(b.Values, obs.FieldValue{Value: v, Count: n})
	}
	sort.Slice(b.Values, func(i, j int) bool {
		if b.Values[i].Count != b.Values[j].Count {
			return b.Values[i].Count > b.Values[j].Count
		}
		return b.Values[i].Value < b.Values[j].Value
	})
	if len(b.Values) > maxBreakdownValues {
		b.Values = b.Values[:maxBreakdownValues]
	}
	return b, nil
}
