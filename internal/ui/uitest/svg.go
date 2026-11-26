package uitest

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Screenshot geometry and colors (a dark terminal).
const (
	cellW      = 8.4
	cellH      = 18.0
	fontSize   = 14
	padding    = 16.0
	titleBar   = 30.0
	defaultFG  = "#cdd6f4"
	defaultBG  = "#1e1e2e"
	windowEdge = "#45475a"
)

// SVG renders a frame of styled terminal output (ANSI SGR sequences) as an
// SVG image of a terminal window titled title.
func SVG(frame string, cols, rows int, title string) string {
	grid := parseFrame(frame, cols, rows)
	w := padding*2 + float64(cols)*cellW
	h := titleBar + padding*2 + float64(rows)*cellH

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`+"\n", w, h, w, h)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" rx="10" fill="%s" stroke="%s"/>`+"\n", defaultBG, windowEdge)
	for i, c := range []string{"#f38ba8", "#f9e2af", "#a6e3a1"} {
		fmt.Fprintf(&b, `<circle cx="%d" cy="15" r="6" fill="%s"/>`, 20+i*20, c)
	}
	fmt.Fprintf(&b, `<text x="%.0f" y="20" fill="#9399b2" font-family="sans-serif" font-size="13" text-anchor="middle">%s</text>`+"\n", w/2, escape(title))
	fmt.Fprintf(&b, `<g font-family="JetBrains Mono, SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace" font-size="%d" xml:space="preserve">`+"\n", fontSize)

	for y, row := range grid {
		top := titleBar + padding + float64(y)*cellH
		// Backgrounds, merged per run.
		for x := 0; x < len(row); {
			end := x
			for end < len(row) && row[end].bg == row[x].bg {
				end++
			}
			if bg := row[x].bg; bg != "" {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
					padding+float64(x)*cellW, top, float64(end-x)*cellW, cellH, bg)
			}
			x = end
		}
		// Text: each word at its exact column (renderers collapse spaces),
		// stretched to its cell width so the grid holds whatever the font.
		for x := 0; x < len(row); {
			if row[x].r == ' ' {
				x++
				continue
			}
			end := x
			for end < len(row) && row[end].r != ' ' && row[end].style == row[x].style {
				end++
			}
			var word strings.Builder
			for _, c := range row[x:end] {
				word.WriteRune(c.r)
			}
			st := row[x].style
			fg := st.fg
			if fg == "" {
				fg = defaultFG
			}
			// Full blocks as solid rectangles: glyphs leave gaps between rows.
			if strings.Trim(word.String(), "█") == "" {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
					padding+float64(x)*cellW, top, float64(end-x)*cellW, cellH, fg)
				x = end
				continue
			}
			attrs := ""
			if st.bold {
				attrs += ` font-weight="bold"`
			}
			if st.faint {
				attrs += ` opacity="0.6"`
			}
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s" textLength="%.1f" lengthAdjust="spacingAndGlyphs"%s>%s</text>`,
				padding+float64(x)*cellW, top+cellH*0.78, fg, float64(end-x)*cellW, attrs, escape(word.String()))
			x = end
		}
		b.WriteString("\n")
	}
	b.WriteString("</g>\n</svg>\n")
	return b.String()
}

type style struct {
	fg          string
	bold, faint bool
}

type cell struct {
	r     rune
	style style
	bg    string
}

// parseFrame lays styled text out on a cols×rows grid of cells.
func parseFrame(frame string, cols, rows int) [][]cell {
	grid := make([][]cell, rows)
	for y := range grid {
		grid[y] = make([]cell, cols)
		for x := range grid[y] {
			grid[y][x].r = ' '
		}
	}
	var cur style
	var bg string
	reverse := false
	for y, line := range strings.Split(frame, "\n") {
		if y >= rows {
			break
		}
		x := 0
		for i := 0; i < len(line); {
			if line[i] == 0x1b && i+1 < len(line) {
				switch line[i+1] {
				case '[': // CSI: only SGR matters
					j := i + 2
					for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
						j++
					}
					if j < len(line) && line[j] == 'm' {
						cur, bg, reverse = applySGR(line[i+2:j], cur, bg, reverse)
					}
					i = j + 1
					continue
				case ']': // OSC (hyperlinks...): skip to its terminator
					j := i + 2
					for j < len(line) && line[j] != 0x07 && !isST(line, j) {
						j++
					}
					if j < len(line) && line[j] == 0x1b {
						j++
					}
					i = j + 1
					continue
				}
			}
			r, size := decodeRune(line[i:])
			i += size
			if x >= cols {
				continue
			}
			c := cell{r: r, style: cur, bg: bg}
			if reverse {
				fg := cur.fg
				if fg == "" {
					fg = defaultFG
				}
				c.bg, c.style.fg = fg, bg
				if bg == "" {
					c.style.fg = defaultBG
				}
			}
			grid[y][x] = c
			x += max(ansi.StringWidth(string(r)), 1)
		}
	}
	return grid
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return ' ', 1
}

// applySGR updates the style with "Select Graphic Rendition" parameters.
func applySGR(params string, st style, bg string, reverse bool) (style, string, bool) {
	if params == "" {
		return style{}, "", false
	}
	p := strings.Split(params, ";")
	for i := 0; i < len(p); i++ {
		n, _ := strconv.Atoi(p[i])
		switch {
		case n == 0:
			st, bg, reverse = style{}, "", false
		case n == 1:
			st.bold = true
		case n == 2:
			st.faint = true
		case n == 22:
			st.bold, st.faint = false, false
		case n == 7:
			reverse = true
		case n == 27:
			reverse = false
		case n == 39:
			st.fg = ""
		case n == 49:
			bg = ""
		case n == 38 || n == 48:
			color, used := extendedColor(p[i+1:])
			i += used
			if n == 38 {
				st.fg = color
			} else {
				bg = color
			}
		case n >= 30 && n <= 37:
			st.fg = basic[n-30]
		case n >= 90 && n <= 97:
			st.fg = bright[n-90]
		case n >= 40 && n <= 47:
			bg = basic[n-40]
		case n >= 100 && n <= 107:
			bg = bright[n-100]
		}
	}
	return st, bg, reverse
}

var (
	basic  = [8]string{"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#bac2de"}
	bright = [8]string{"#585b70", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#a6adc8"}
)

// extendedColor reads "2;r;g;b" or "5;n" and returns the color and the
// number of parameters consumed.
func extendedColor(p []string) (string, int) {
	if len(p) >= 4 && p[0] == "2" {
		r, _ := strconv.Atoi(p[1])
		g, _ := strconv.Atoi(p[2])
		b, _ := strconv.Atoi(p[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b), 4
	}
	if len(p) >= 2 && p[0] == "5" {
		n, _ := strconv.Atoi(p[1])
		return xterm256(n), 2
	}
	return "", len(p)
}

func xterm256(n int) string {
	switch {
	case n < 8:
		return basic[n]
	case n < 16:
		return bright[n-8]
	case n < 232:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return fmt.Sprintf("#%02x%02x%02x", level(n/36), level(n/6%6), level(n%6))
	default:
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// isST reports a String Terminator (ESC \) at line[j].
func isST(line string, j int) bool {
	return line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\'
}
