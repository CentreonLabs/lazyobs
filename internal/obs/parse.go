package obs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses durations like "30s", "15m", "2h", "7d" or "1w",
// as well as anything time.ParseDuration accepts ("1h30m").
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n := len(s); n > 1 && (s[n-1] == 'd' || s[n-1] == 'w') {
		v, err := strconv.Atoi(s[:n-1])
		if err == nil && v > 0 {
			unit := 24 * time.Hour
			if s[n-1] == 'w' {
				unit *= 7
			}
			return time.Duration(v) * unit, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q: use e.g. 15m, 2h, 7d", s)
	}
	return d, nil
}

var absoluteLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// ParseTimeRange parses either a relative range ("15m", "last 2h") or an
// absolute one ("2026-11-03 13:00 .. 2026-11-03 14:00"; "→" and " to " also
// separate the bounds). Absolute times without a zone are read in loc.
func ParseTimeRange(s string, loc *time.Location) (TimeRange, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, "last "))
	for _, sep := range []string{"..", "→", " to "} {
		from, to, ok := strings.Cut(s, sep)
		if !ok {
			continue
		}
		f, err := parseInstant(from, loc)
		if err != nil {
			return TimeRange{}, err
		}
		t, err := parseInstant(to, loc)
		if err != nil {
			return TimeRange{}, err
		}
		if !f.Before(t) {
			return TimeRange{}, fmt.Errorf("invalid range: %s is not before %s", f.Format(time.DateTime), t.Format(time.DateTime))
		}
		return Between(f, t), nil
	}
	d, err := ParseDuration(s)
	if err != nil {
		return TimeRange{}, fmt.Errorf("invalid time range %q: use 15m, 2h, 7d or \"2026-11-03 13:00 .. 2026-11-03 14:00\"", s)
	}
	return Last(d), nil
}

func parseInstant(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range absoluteLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD HH:MM[:SS]", s)
}
