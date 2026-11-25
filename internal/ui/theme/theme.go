// Package theme holds the Lip Gloss styles shared by every view, adapted to
// the terminal background.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/CentreonLabs/lazyobs/internal/obs/logs"
	"github.com/CentreonLabs/lazyobs/internal/ui/chart"
)

// Theme is the set of styles used by the interface.
type Theme struct {
	Dark bool

	Accent  color.Color
	Muted   color.Color
	Subtle  color.Color
	Error   color.Color
	Warning color.Color
	Success color.Color

	Title      lipgloss.Style // application name in the header
	Tab        lipgloss.Style
	ActiveTab  lipgloss.Style
	Disabled   lipgloss.Style
	Header     lipgloss.Style // column headers
	Selected   lipgloss.Style // selected row
	Dim        lipgloss.Style
	Key        lipgloss.Style // field names in the detail panel
	StatusBar  lipgloss.Style
	ErrorText  lipgloss.Style
	WarnText   lipgloss.Style
	Badge      lipgloss.Style // e.g. LIVE / PAUSED
	Spark      lipgloss.Style
	PanelTitle lipgloss.Style
	Chart      chart.Styles

	severity      map[logs.Severity]lipgloss.Style
	severityColor map[logs.Severity]color.Color
}

// New builds the theme for a dark or light terminal background.
func New(dark bool) *Theme {
	ld := lipgloss.LightDark(dark)
	t := &Theme{
		Dark:    dark,
		Accent:  ld(lipgloss.Color("#5A3FC0"), lipgloss.Color("#A78BFA")),
		Muted:   ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF")),
		Subtle:  ld(lipgloss.Color("#E5E7EB"), lipgloss.Color("#374151")),
		Error:   ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171")),
		Warning: ld(lipgloss.Color("#B45309"), lipgloss.Color("#FBBF24")),
		Success: ld(lipgloss.Color("#047857"), lipgloss.Color("#34D399")),
	}
	fg := ld(lipgloss.Color("#111827"), lipgloss.Color("#F3F4F6"))
	selBg := ld(lipgloss.Color("#EDE9FE"), lipgloss.Color("#312E81"))

	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Tab = lipgloss.NewStyle().Foreground(t.Muted).Padding(0, 1)
	t.ActiveTab = lipgloss.NewStyle().Bold(true).Foreground(fg).Background(t.Subtle).Padding(0, 1)
	t.Disabled = lipgloss.NewStyle().Foreground(t.Subtle).Padding(0, 1)
	t.Header = lipgloss.NewStyle().Bold(true).Foreground(t.Muted)
	t.Selected = lipgloss.NewStyle().Background(selBg).Foreground(fg)
	t.Dim = lipgloss.NewStyle().Foreground(t.Muted)
	t.Key = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.StatusBar = lipgloss.NewStyle().Foreground(t.Muted)
	t.ErrorText = lipgloss.NewStyle().Foreground(t.Error)
	t.WarnText = lipgloss.NewStyle().Foreground(t.Warning)
	t.Badge = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(ld(lipgloss.Color("#FFFFFF"), lipgloss.Color("#111827")))
	t.Spark = lipgloss.NewStyle().Foreground(t.Accent)

	t.PanelTitle = lipgloss.NewStyle().Bold(true).Foreground(fg)
	t.Chart = chart.Styles{
		Axis:   lipgloss.NewStyle().Foreground(t.Muted),
		Grid:   lipgloss.NewStyle().Foreground(t.Subtle),
		Marker: lipgloss.NewStyle().Foreground(t.Accent),
		Legend: lipgloss.NewStyle().Foreground(fg),
	}

	// Grafana-like palette: green info, yellow warnings, red errors.
	t.severityColor = map[logs.Severity]color.Color{
		logs.SeverityUnknown: t.Muted,
		logs.SeverityTrace:   ld(lipgloss.Color("#9CA3AF"), lipgloss.Color("#6B7280")),
		logs.SeverityDebug:   ld(lipgloss.Color("#1D4ED8"), lipgloss.Color("#5794F2")),
		logs.SeverityInfo:    ld(lipgloss.Color("#15803D"), lipgloss.Color("#73BF69")),
		logs.SeverityWarn:    ld(lipgloss.Color("#B45309"), lipgloss.Color("#FADE2A")),
		logs.SeverityError:   ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F2495C")),
		logs.SeverityFatal:   ld(lipgloss.Color("#86198F"), lipgloss.Color("#E04FF0")),
	}
	t.severity = map[logs.Severity]lipgloss.Style{}
	for sev, c := range t.severityColor {
		t.severity[sev] = lipgloss.NewStyle().Foreground(c)
	}
	t.severity[logs.SeverityError] = t.severity[logs.SeverityError].Bold(true)
	t.severity[logs.SeverityFatal] = t.severity[logs.SeverityFatal].Bold(true)
	return t
}

// SeverityColor returns the color of a log level.
func (t *Theme) SeverityColor(s logs.Severity) color.Color {
	if c, ok := t.severityColor[s]; ok {
		return c
	}
	return t.Muted
}

// Severity returns the style of a log level.
func (t *Theme) Severity(s logs.Severity) lipgloss.Style {
	if st, ok := t.severity[s]; ok {
		return st
	}
	return t.severity[logs.SeverityUnknown]
}
