package obs_test

import (
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func TestTimeRangeResolve(t *testing.T) {
	from, to := obs.Last(15 * time.Minute).Resolve(now)
	if !from.Equal(now.Add(-15*time.Minute)) || !to.Equal(now) {
		t.Fatalf("relative resolve = %v → %v", from, to)
	}

	abs := obs.Between(now.Add(-time.Hour), now.Add(-30*time.Minute))
	from, to = abs.Resolve(now.Add(time.Hour))
	if !from.Equal(abs.From) || !to.Equal(abs.To) {
		t.Fatalf("absolute range must ignore now, got %v → %v", from, to)
	}
}

func TestTimeRangePin(t *testing.T) {
	pinned := obs.Last(time.Hour).Pin(now)
	if pinned.IsRelative() {
		t.Fatal("pinned range must be absolute")
	}
	if !pinned.From.Equal(now.Add(-time.Hour)) || !pinned.To.Equal(now) {
		t.Fatalf("pinned = %v", pinned)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Minute:    "5m",
		90 * time.Minute:   "90m",
		2 * time.Hour:      "2h",
		7 * 24 * time.Hour: "7d",
		30 * time.Second:   "30s",
	}
	for d, want := range cases {
		if got := obs.FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := obs.Last(time.Hour).String(); got != "last 1h" {
		t.Errorf("String() = %q", got)
	}
}
