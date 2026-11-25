// Package draw has small helpers to compose styled terminal output: framing
// rows in a box and drawing a box over existing lines (popups, tooltips).
package draw

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay draws box (lines of equal width) over lines, its top-left corner at
// column x, row y. Parts of the box outside lines are dropped.
func Overlay(lines, box []string, x, y int) {
	for i, b := range box {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		w := ansi.StringWidth(b)
		line := lines[row]
		lines[row] = ansi.Truncate(line, x, "") + strings.Repeat(" ", max(x-ansi.StringWidth(line), 0)) +
			b + ansi.TruncateLeft(line, x+w, "")
	}
}

// Boxed frames rows with a rounded border, padding them to the same width.
func Boxed(rows []string, border lipgloss.Style) []string {
	w := 0
	for _, r := range rows {
		w = max(w, ansi.StringWidth(r))
	}
	out := []string{border.Render("╭" + strings.Repeat("─", w+2) + "╮")}
	for _, r := range rows {
		out = append(out, border.Render("│ ")+r+strings.Repeat(" ", w-ansi.StringWidth(r))+border.Render(" │"))
	}
	return append(out, border.Render("╰"+strings.Repeat("─", w+2)+"╯"))
}

// Panel draws content inside a rounded border of the given inner size, with
// the title set in the top border like a dashboard panel. Content is cut or
// padded to fit.
func Panel(title, content string, innerW, innerH int, border, titleStyle lipgloss.Style) string {
	lines := strings.Split(content, "\n")
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	title = ansi.Truncate(title, max(innerW-4, 0), "…")
	rest := max(innerW-ansi.StringWidth(title)-3, 0)
	top := border.Render("╭─ ") + titleStyle.Render(title) + border.Render(" "+strings.Repeat("─", rest)+"╮")
	if title == "" {
		top = border.Render("╭" + strings.Repeat("─", innerW) + "╮")
	}
	out := make([]string, 0, innerH+2)
	out = append(out, top)
	side := border.Render("│")
	for _, l := range lines {
		out = append(out, side+Fit(l, innerW)+side)
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(out, "\n")
}

// Fit truncates or pads s to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// OneLine puts multi-line text on one line.
func OneLine(s string) string {
	return strings.NewReplacer("\r\n", " ⏎ ", "\n", " ⏎ ", "\t", " ").Replace(s)
}

// Wrap wraps s to w cells (at least 10).
func Wrap(s string, w int) string { return lipgloss.NewStyle().Width(max(w, 10)).Render(s) }

// HumanCount renders a count compactly: 999, 12.3k, 4.5M.
func HumanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprint(n)
	}
}
