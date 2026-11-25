// Package draw has small helpers to compose styled terminal output: framing
// rows in a box and drawing a box over existing lines (popups, tooltips).
package draw

import (
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
