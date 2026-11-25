package obs_test

import (
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

func TestParseTimeRange(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	cases := []struct {
		in   string
		want obs.TimeRange
	}{
		{"15m", obs.Last(15 * time.Minute)},
		{" last 2h ", obs.Last(2 * time.Hour)},
		{"7d", obs.Last(7 * 24 * time.Hour)},
		{"1w", obs.Last(7 * 24 * time.Hour)},
		{"1h30m", obs.Last(90 * time.Minute)},
		{"2026-11-03 13:00 .. 2026-11-03 14:30:15", obs.Between(
			time.Date(2026, 11, 3, 13, 0, 0, 0, loc), time.Date(2026, 11, 3, 14, 30, 15, 0, loc))},
		{"2026-11-03 → 2026-11-04", obs.Between(
			time.Date(2026, 11, 3, 0, 0, 0, 0, loc), time.Date(2026, 11, 4, 0, 0, 0, 0, loc))},
		{"2026-11-03T12:00:00Z to 2026-11-03T13:00:00Z", obs.Between(
			time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 13, 0, 0, 0, time.UTC))},
	}
	for _, tc := range cases {
		got, err := obs.ParseTimeRange(tc.in, loc)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got.Last != tc.want.Last || !got.From.Equal(tc.want.From) || !got.To.Equal(tc.want.To) {
			t.Errorf("%q = %+v, want %+v", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"", "soon", "-5m", "0d", "2026-11-04 .. 2026-11-03", "2026-13-01 .. 2026-11-03"} {
		if _, err := obs.ParseTimeRange(bad, loc); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
