package logsview

import (
	"strings"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

func points(from time.Time, step time.Duration, values ...float64) []obs.Point {
	out := make([]obs.Point, len(values))
	for i, v := range values {
		out[i] = obs.Point{Time: from.Add(time.Duration(i) * step), Value: v}
	}
	return out
}

func TestBucketize(t *testing.T) {
	from := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)

	// More points than columns: averaged, so a flat series stays flat.
	flat := points(from, 30*time.Second, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6)
	for c, v := range bucketize(flat, from, to, 7) {
		if v != 6 {
			t.Fatalf("column %d = %v, want 6 (no aliasing spikes)", c, v)
		}
	}

	// Fewer points than columns: each bucket is stretched over its columns.
	got := bucketize(points(from, 5*time.Minute, 1, 9), from, to, 4)
	if want := []float64{1, 1, 9, 9}; !equal(got, want) {
		t.Errorf("stretched = %v, want %v", got, want)
	}

	if got := bucketize(nil, from, to, 3); !equal(got, []float64{0, 0, 0}) {
		t.Errorf("empty = %v", got)
	}
}

func TestColumnOf(t *testing.T) {
	from := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	to := from.Add(100 * time.Second)
	cases := map[time.Duration]int{0: 0, 50 * time.Second: 5, 99 * time.Second: 9, 100 * time.Second: -1, -time.Second: -1}
	for off, want := range cases {
		if got := columnOf(from.Add(off), from, to, 10); got != want {
			t.Errorf("columnOf(+%v) = %d, want %d", off, got, want)
		}
	}
}

func TestTimeAxis(t *testing.T) {
	from := time.Date(2026, 11, 25, 11, 0, 0, 0, time.UTC)
	axis := timeAxis(from, from.Add(time.Hour), 60)
	if len([]rune(axis)) != 60 || !strings.HasPrefix(axis, "11:00") || !strings.HasSuffix(axis, "12:00") || !strings.Contains(axis, "11:30") {
		t.Errorf("axis = %q", axis)
	}
	if axis := timeAxis(from, from.Add(48*time.Hour), 40); !strings.HasPrefix(axis, "11-25 11:00") {
		t.Errorf("multi-day axis = %q", axis)
	}
	if axis := timeAxis(from, from.Add(time.Hour), 8); len([]rune(axis)) != 8 {
		t.Errorf("narrow axis must keep its width: %q", axis)
	}
}

func equal(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRoundDuration(t *testing.T) {
	cases := map[time.Duration]string{
		6 * time.Hour: "6h", 90 * time.Second: "2m", 6900 * time.Millisecond: "7s", 24 * time.Hour: "1d", 250 * time.Millisecond: "250ms",
	}
	for d, want := range cases {
		if got := roundDuration(d); got != want {
			t.Errorf("roundDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
