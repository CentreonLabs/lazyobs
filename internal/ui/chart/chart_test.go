package chart

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/obs"
)

var from = time.Date(2026, 11, 25, 11, 0, 0, 0, time.UTC)

func series(name string, step time.Duration, values ...float64) obs.Series {
	s := obs.Series{Name: name}
	for i, v := range values {
		s.Points = append(s.Points, obs.Point{Time: from.Add(time.Duration(i) * step), Value: v})
	}
	return s
}

func render(t *testing.T, w, h int, s ...obs.Series) []string {
	t.Helper()
	out, _ := Render(Options{
		Width: w, Height: h, From: from, To: from.Add(time.Hour), Location: time.UTC, Series: s,
		Color:  func(string) color.Color { return lipgloss.Color("#ff0000") },
		Marker: from.Add(30 * time.Minute),
	})
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) != h {
		t.Fatalf("rendered %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if n := ansi.StringWidth(l); n != w {
			t.Fatalf("line %d is %d cells wide, want %d: %q", i, n, w, l)
		}
	}
	return lines
}

func TestRenderShape(t *testing.T) {
	// 60 one-minute buckets, ramping from 0 to 59 logs per minute.
	values := make([]float64, 60)
	for i := range values {
		values[i] = float64(i)
	}
	lines := render(t, 80, 12, series("INFO", time.Minute, values...), series("ERROR", time.Minute, make([]float64, 60)...))

	plot := strings.Join(lines[:10], "\n")
	if !strings.ContainsAny(plot, "⠁⠂⠄⡀⠈⠐⠠⢀⣀⣿") {
		t.Error("no braille line drawn")
	}
	// Peak is 59/min, below 1/s: the axis switches to per-minute rates.
	if !strings.Contains(lines[0], "/min") || !strings.Contains(lines[9], "0/min") {
		t.Errorf("y axis: %q … %q", lines[0], lines[9])
	}
	if !strings.Contains(lines[10], "11:00") || !strings.Contains(lines[10], "11:30") {
		t.Errorf("x axis = %q", lines[10])
	}
	if !strings.Contains(lines[11], "ERROR 0") || !strings.Contains(lines[11], "INFO 1.8k") || !strings.Contains(lines[11], "1m0s buckets") {
		t.Errorf("legend = %q", lines[11])
	}
	if strings.Index(lines[11], "ERROR") > strings.Index(lines[11], "INFO") {
		t.Error("the last (most important) series must come first in the legend")
	}
	if !strings.Contains(plot, "│") {
		t.Error("marker missing")
	}
	// The ramp rises: the top row has data on its right half only.
	top := []rune(lines[0])
	if strings.ContainsAny(string(top[10:40]), "⠁⠂⠄⡀⠈⠐⠠⢀⣀") {
		t.Errorf("top row should be empty on the left: %q", lines[0])
	}
}

func TestRenderDegenerate(t *testing.T) {
	render(t, 40, 6)                                 // no series
	render(t, 40, 6, series("INFO", time.Minute))    // no points
	render(t, 12, 6, series("INFO", time.Minute, 1)) // too narrow for a plot
	render(t, 40, 2, series("INFO", time.Minute, 1)) // too short for a plot
}

func TestYTicks(t *testing.T) {
	cases := []struct {
		peak float64
		n    int
		want []float64
	}{
		{0, 4, []float64{0, 1}},
		{20, 4, []float64{0, 5, 10, 15, 20}},
		{23, 4, []float64{0, 10, 20, 30}},
		{0.7, 3, []float64{0, 0.25, 0.5, 0.75}},
		{1300, 3, []float64{0, 500, 1000, 1500}},
	}
	for _, tc := range cases {
		got := yTicks(tc.peak, tc.n)
		if len(got) != len(tc.want) {
			t.Errorf("yTicks(%v, %d) = %v, want %v", tc.peak, tc.n, got, tc.want)
			continue
		}
		for i := range got {
			if diff := got[i] - tc.want[i]; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("yTicks(%v, %d) = %v, want %v", tc.peak, tc.n, got, tc.want)
				break
			}
		}
	}
}

func TestXTicksAreRoundAndFit(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	start := time.Date(2026, 11, 25, 10, 7, 13, 0, loc)
	for _, tc := range []struct {
		span  time.Duration
		width int
		want  string
	}{
		{15 * time.Minute, 120, "10:08"},
		{time.Hour, 60, "10:10"},
		{30 * 24 * time.Hour, 100, "11-26"},
	} {
		ticks := xTicks(start, start.Add(tc.span), loc, tc.width)
		if len(ticks) < 2 || ticks[0].label != tc.want {
			t.Errorf("span %v: ticks %v, want first %q", tc.span, ticks, tc.want)
		}
		if axis := xAxis(ticks, start, start.Add(tc.span), tc.width); len([]rune(axis)) != tc.width {
			t.Errorf("axis width %d", len([]rune(axis)))
		}
	}
}

func TestBucketize(t *testing.T) {
	flat := series("x", 30*time.Second, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6).Points
	for i, v := range Bucketize(flat, from, from.Add(10*time.Minute), 7) {
		if v != 6 {
			t.Fatalf("sample %d = %v, want 6 (averaged, no aliasing)", i, v)
		}
	}
	got := Bucketize(series("x", 5*time.Minute, 1, 9).Points, from, from.Add(10*time.Minute), 4)
	if got[0] != 1 || got[1] != 1 || got[2] != 9 || got[3] != 9 {
		t.Errorf("stretched = %v", got)
	}
}

func TestFormatValue(t *testing.T) {
	for v, want := range map[float64]string{0: "0", 2.5: "2.5", 0.25: "0.25", 1500: "1.5k", 2_000_000: "2M", 20: "20"} {
		if got := formatValue(v); got != want {
			t.Errorf("formatValue(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestHiddenSeriesAndLegendPositions(t *testing.T) {
	big := series("INFO", time.Minute, 600, 600, 600)
	small := series("ERROR", time.Minute, 60, 60, 60)
	opts := Options{
		Width: 80, Height: 8, From: from, To: from.Add(3 * time.Minute), Location: time.UTC,
		Series: []obs.Series{big, small},
		Color:  func(string) color.Color { return lipgloss.Color("#ff0000") },
	}

	out, layout := Render(opts)
	legend := layout.Legend
	lines := strings.Split(ansi.Strip(out), "\n")
	if legend.Row != len(lines)-1 || len(legend.Items) != 2 {
		t.Fatalf("legend = %+v", legend)
	}
	row := []rune(lines[legend.Row])
	for _, it := range legend.Items {
		if got := string(row[it.X0:it.X1]); !strings.Contains(got, it.Name) {
			t.Errorf("legend item %q covers %q", it.Name, got)
		}
		if name, ok := legend.At(it.X0 + 1); !ok || name != it.Name {
			t.Errorf("At(%d) = %q", it.X0+1, name)
		}
	}
	if _, ok := legend.At(legend.Items[0].X1 + 1); ok {
		t.Error("the gap between entries must not match")
	}
	if !strings.HasPrefix(lines[0], "10/s") {
		t.Errorf("peak of INFO (10/s) should set the scale: %q", lines[0])
	}

	// Hiding INFO rescales to ERROR (1/s) and dims its legend entry.
	opts.Hidden = map[string]bool{"INFO": true}
	opts.LegendHint = "click to toggle"
	out, layout = Render(opts)
	legend = layout.Legend
	lines = strings.Split(ansi.Strip(out), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "1/s") {
		t.Errorf("hidden series must not drive the scale: %q", lines[0])
	}
	if !strings.Contains(lines[legend.Row], "── INFO") || !strings.Contains(lines[legend.Row], "click to toggle") {
		t.Errorf("legend = %q", lines[legend.Row])
	}
}

func TestLayoutAndSelection(t *testing.T) {
	to := from.Add(time.Hour)
	opts := Options{
		Width: 70, Height: 8, From: from, To: to, Location: time.UTC,
		Series: []obs.Series{series("INFO", time.Minute, 1, 2, 3)},
		Color:  func(string) color.Color { return lipgloss.Color("#00ff00") },
		Styles: Styles{Selection: lipgloss.Color("#333333")},
	}
	plain, layout := Render(opts)
	if layout.PlotX <= 0 || layout.PlotX+layout.PlotW != 70 || layout.PlotH != 6 {
		t.Fatalf("layout = %+v", layout)
	}
	if got := layout.TimeAt(layout.PlotW/2, from, to); got.Sub(from.Add(30*time.Minute)).Abs() > time.Minute {
		t.Errorf("TimeAt(middle) = %v", got)
	}

	opts.SelectFrom, opts.SelectTo = from.Add(40*time.Minute), from.Add(20*time.Minute) // reversed on purpose
	selected, _ := Render(opts)
	if selected == plain || ansi.Strip(selected) != ansi.Strip(plain) {
		t.Error("a selection must only change the background")
	}
}

func TestHoverTooltip(t *testing.T) {
	to := from.Add(10 * time.Minute)
	info := series("INFO", time.Minute, 600, 600, 600, 600, 600, 1200, 600, 600, 600, 600)
	errs := series("ERROR", time.Minute, 60, 60, 60, 60, 60, 120, 60, 60, 60, 60)
	opts := Options{
		Width: 100, Height: 14, From: from, To: to, Location: time.UTC,
		Series: []obs.Series{info, errs},
		Color:  func(string) color.Color { return lipgloss.Color("#00ff00") },
	}
	_, layout := Render(opts)
	col := layout.PlotW * 11 / 20 // in the 6th bucket (11:05-11:06)
	opts.Hover = &Cell{Col: col, Row: 4}
	out, _ := Render(opts)
	screen := ansi.Strip(out)
	for _, want := range []string{"11:05:00 → 11:06:00", "INFO", "20/s", "1.2k", "ERROR", "2/s", "120", "total", "22/s", "1.3k"} {
		if !strings.Contains(screen, want) {
			t.Errorf("tooltip misses %q:\n%s", want, screen)
		}
	}
	lines := strings.Split(screen, "\n")
	if strings.Index(lines[4], "INFO") > strings.Index(lines[5], "ERROR") && strings.Contains(lines[5], "ERROR") {
		t.Error("most important series first")
	}
	for i, l := range lines {
		if ansi.StringWidth(l) != 100 {
			t.Fatalf("line %d is %d wide", i, ansi.StringWidth(l))
		}
	}

	// Near the right edge the tooltip flips to the left of the pointer.
	opts.Hover = &Cell{Col: layout.PlotW - 1, Row: 0}
	out, _ = Render(opts)
	for _, l := range strings.Split(ansi.Strip(out), "\n") {
		if i := strings.Index(l, "╮"); i >= 0 {
			if x := len([]rune(l[:i])); x >= layout.PlotX+layout.PlotW-1 {
				t.Errorf("tooltip should be left of the pointer, ends at %d", x)
			}
			break
		}
	}

	// Outside the plot: no tooltip.
	opts.Hover = &Cell{Col: layout.PlotW + 5, Row: 0}
	if out, _ = Render(opts); strings.Contains(ansi.Strip(out), "total") {
		t.Error("no tooltip outside the plot")
	}
}

func TestTooltipColumnsAreAligned(t *testing.T) {
	o := Options{
		Location: time.UTC,
		Color:    func(string) color.Color { return lipgloss.Color("#00ff00") },
		Styles:   Styles{Axis: lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))},
	}
	visible := []obs.Series{series("INFO", time.Minute, 6000), series("ERROR", time.Minute, 6)}
	rows := tooltip(o, visible, from.Add(30*time.Second), time.Minute, 1.0/60, "/s")
	ends := map[int]bool{}
	for _, r := range rows[1:] {
		ends[ansi.StringWidth(r)] = true
	}
	if len(ends) != 1 {
		t.Errorf("rows have different widths %v:\n%s", ends, ansi.Strip(strings.Join(rows, "\n")))
	}
}
