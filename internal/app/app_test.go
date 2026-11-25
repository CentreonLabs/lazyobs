package app_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/obs"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.Local)

func start(t *testing.T, width, height int, opts logsview.Options) *uitest.Driver {
	t.Helper()
	src := fake.NewLogs(42, func() time.Time { return now })
	th := theme.New(true)
	opts.Source, opts.Histogram, opts.Theme = src, src, th
	opts.Now = func() time.Time { return now }
	root := app.New("demo · my-org", th,
		app.Tab{Title: "Logs", View: logsview.New(opts)},
		app.Tab{Title: "Traces"},
	)
	return uitest.New(t, root, width, height)
}

// dump prints the screen when LAZYOBS_SCREENS is set, to eyeball layouts.
func dump(t *testing.T, d *uitest.Driver) {
	t.Helper()
	if os.Getenv("LAZYOBS_SCREENS") != "" {
		t.Logf("\n%s", d.Screen())
	}
}

func lines(s string) []string { return strings.Split(s, "\n") }

func TestScreenLayout(t *testing.T) {
	d := start(t, 140, 30, logsview.Options{})
	screen := d.Screen()
	dump(t, d)

	ls := lines(screen)
	if len(ls) != 30 {
		t.Fatalf("screen has %d lines, want 30", len(ls))
	}
	for i, l := range ls {
		if w := len([]rune(l)); w > 140 {
			t.Errorf("line %d is %d cells wide", i, w)
		}
	}
	for _, want := range []string{"lazyobs", "1 Logs", "2 Traces", "demo · my-org", "query", "range last 15m", "TIME", "LEVEL", "SERVICE", "MESSAGE", "? toggle help"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen misses %q", want)
		}
	}
}

func TestNavigationAndDetail(t *testing.T) {
	d := start(t, 140, 30, logsview.Options{})
	d.Keys("j", "j", "enter")
	screen := d.Screen()
	dump(t, d)
	for _, want := range []string{"severity", "service", "MESSAGE", "ATTRIBUTES", "http.method", "RESOURCE", "RAW"} {
		if !strings.Contains(screen, want) {
			t.Errorf("detail misses %q", want)
		}
	}
	d.Keys("esc")
	if strings.Contains(d.Screen(), "ATTRIBUTES") {
		t.Error("esc must close the detail panel")
	}

	// Narrow terminals show the detail instead of the list.
	n := start(t, 80, 24, logsview.Options{})
	n.Keys("enter")
	if s := n.Screen(); !strings.Contains(s, "MESSAGE") || strings.Contains(s, "LEVEL") {
		t.Errorf("narrow detail layout:\n%s", s)
	}
}

func TestQueryAndRangeInputs(t *testing.T) {
	d := start(t, 140, 30, logsview.Options{})
	d.Keys("/")
	d.Type("severity_text:ERROR")
	d.Keys("enter")
	screen := d.Screen()
	if !strings.Contains(screen, "query severity_text:ERROR") {
		t.Fatalf("query not applied:\n%s", screen)
	}
	for _, l := range lines(screen)[4:10] {
		if strings.Contains(l, " INFO ") || strings.Contains(l, " DEBUG ") {
			t.Fatalf("filter not applied: %q", l)
		}
	}

	// q while typing is text, not quit.
	d.Keys("/")
	d.Type("q")
	if !strings.Contains(d.Screen(), "query › ") {
		t.Fatal("input should stay open")
	}
	d.Keys("esc")

	d.Keys("t", "tab", "tab", "tab", "enter") // presets: 5m, 15m, 1h
	if s := d.Screen(); !strings.Contains(s, "range last 1h") {
		t.Fatalf("range not applied:\n%s", s)
	}

	d.Keys("t")
	for range 3 {
		d.Keys("backspace")
	}
	d.Type("soon")
	d.Keys("enter")
	if s := d.Screen(); !strings.Contains(s, "invalid time range") || !strings.Contains(s, "range › ") {
		t.Fatalf("invalid range must be reported and keep the input open:\n%s", s)
	}
}

func TestEmptyResultAndHelp(t *testing.T) {
	d := start(t, 120, 24, logsview.Options{Query: "nothing-matches-this"})
	if s := d.Screen(); !strings.Contains(s, "No logs match") {
		t.Fatalf("empty state missing:\n%s", s)
	}
	d.Keys("?")
	s := d.Screen()
	dump(t, d)
	for _, want := range []string{"Keyboard shortcuts", "time range", "live on/off", "half page down", "quit"} {
		if !strings.Contains(s, want) {
			t.Errorf("help misses %q", want)
		}
	}
	d.Keys("?")
	if strings.Contains(d.Screen(), "Keyboard shortcuts") {
		t.Error("? must close the help")
	}
	d.Keys("2")
	if !strings.Contains(d.Screen(), "traces are not available yet") {
		t.Error("unavailable tab must be reported")
	}
}

func TestInfiniteScroll(t *testing.T) {
	d := start(t, 120, 24, logsview.Options{Range: obs.Last(time.Hour), PageSize: 50})
	for range 6 {
		d.Keys("G")
	}
	// Six jumps to the bottom must have loaded several pages.
	if s := d.Screen(); !strings.Contains(s, "logs") {
		t.Fatal(s)
	}
	d.Keys("g")
	dump(t, d)
}
