//go:build screenshots

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tui "github.com/CentreonLabs/lazyobs/internal/app"
	"github.com/CentreonLabs/lazyobs/internal/source/fake"
	"github.com/CentreonLabs/lazyobs/internal/ui/tabset"
	"github.com/CentreonLabs/lazyobs/internal/ui/theme"
	"github.com/CentreonLabs/lazyobs/internal/ui/uitest"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/logsview"
	"github.com/CentreonLabs/lazyobs/internal/ui/views/tracesview"
)

// Regenerate the README screenshots (demo data, fixed time) with:
//
//	make screenshots
const (
	shotCols = 160
	shotRows = 44
)

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("LAZYOBS_SCREENSHOTS")
	if dir == "" {
		t.Skip("LAZYOBS_SCREENSHOTS is not set")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	shots := []struct {
		name, title string
		steps       func(d *uitest.Driver, root *tui.Model)
	}{
		{"logs", "lazyobs — logs, timeline and top values", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("b", "enter")
			_ = d.Screen() // lay out the chart before hovering it
			hover(d, 50, 10)
		}},
		{"logs-detail", "lazyobs — log details and filters", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("j", "j", "enter", "tab", "j")
		}},
		{"logs-autocomplete", "lazyobs — query autocompletion", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("/")
			d.Type("service_name:")
		}},
		{"traces", "lazyobs — traces", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("2", "s")
		}},
		{"waterfall", "lazyobs — trace waterfall", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("2", "/")
			d.Type("span_status.code:error")
			d.Keys("enter", "enter", "j", "j", "j", "j", "j") // the failed payment
		}},
		{"service-map", "lazyobs — service map", func(d *uitest.Driver, _ *tui.Model) {
			d.Keys("2", "m", "j")
		}},
	}
	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d, root := screenshotApp(t)
			s.steps(d, root)
			path := filepath.Join(dir, s.name+".svg")
			if err := os.WriteFile(path, []byte(uitest.SVG(d.Model.View().Content, shotCols, shotRows, s.title)), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s", path)
		})
	}
}

// screenshotApp builds the whole interface on demo data at a fixed time.
func screenshotApp(t *testing.T) (*uitest.Driver, *tui.Model) {
	t.Helper()
	now := time.Date(2026, 11, 25, 14, 7, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	logSrc, traceSrc := fake.NewLogs(2026, clock), fake.NewTraces(2026, clock)
	th := theme.New(true)
	logOpts, traceOpts := linkSignals(
		logsview.Options{
			Source: logSrc, Histogram: logSrc, Breakdown: logSrc, Fields: logSrc, Translator: fake.Translator{}, Theme: th, Now: clock,
			After: func(_ time.Duration, msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } },
		},
		tracesview.Options{Source: traceSrc, Histogram: traceSrc, Fetcher: traceSrc, Topology: traceSrc, Theme: th, Now: clock},
	)
	root := tui.New("demo data", th,
		tui.Tab{Title: signalLogs, View: tabset.New(signalLogs, th, logsview.New(logOpts))},
		tui.Tab{Title: signalTraces, View: tabset.New(signalTraces, th, tracesview.New(traceOpts))},
		tui.Tab{Title: "Metrics"},
	)
	return uitest.New(t, root, shotCols, shotRows), root
}

func hover(d *uitest.Driver, x, y int) { d.Send(tea.MouseMotionMsg{X: x, Y: y}) }
