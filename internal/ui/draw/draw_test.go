package draw_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/CentreonLabs/lazyobs/internal/ui/draw"
)

func TestBoxedAndOverlay(t *testing.T) {
	box := draw.Boxed([]string{"ab", "abcd"}, lipgloss.NewStyle())
	if len(box) != 4 || box[0] != "╭──────╮" || box[1] != "│ ab   │" {
		t.Fatalf("box = %q", box)
	}
	lines := []string{"0123456789", lipgloss.NewStyle().Bold(true).Render("0123456789"), "01", "0123456789"}
	draw.Overlay(lines, box, 3, 1)
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = ansi.Strip(l)
	}
	want := []string{"0123456789", "012╭──────╮", "01 │ ab   │", "012│ abcd │"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("overlay:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
