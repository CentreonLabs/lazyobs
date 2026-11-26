package uitest

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestSVG(t *testing.T) {
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true).Render("err")
	bg := lipgloss.NewStyle().Background(lipgloss.Color("#00ff00")).Render("ok")
	out := SVG(red+" a<b & "+bg+"\n│ █", 20, 2, "demo")
	for _, want := range []string{
		`fill="#ff0000"`, `font-weight="bold"`, ">err<", ">a&lt;b<", ">&amp;<", `fill="#00ff00"`, ">│<", "<svg", "</svg>", ">demo<",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("svg misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ">█<") || !strings.Contains(out, `<rect x="32.8" y="64.0"`) {
		t.Error("full blocks must be drawn as rectangles")
	}
	if strings.Contains(out, "\x1b") {
		t.Error("escape sequences must not leak into the svg")
	}
}

func TestParseFrameColumns(t *testing.T) {
	g := parseFrame("ab\x1b[38;2;1;2;3mc\x1b[0md", 6, 1)
	if g[0][2].r != 'c' || g[0][2].style.fg != "#010203" || g[0][3].r != 'd' || g[0][3].style.fg != "" {
		t.Errorf("grid = %+v", g[0])
	}
}
