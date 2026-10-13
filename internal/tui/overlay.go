package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// overlay draws the box (multi-line string) over base with its top-left corner at (x, y).
func overlay(base, box string, x, y int) string {
	bl := strings.Split(base, "\n")
	ol := strings.Split(box, "\n")
	for len(bl) < y+len(ol) {
		bl = append(bl, "")
	}
	for i, o := range ol {
		line := bl[y+i]
		ow := lipgloss.Width(o)
		left := ansi.Truncate(line, x, "")
		if w := lipgloss.Width(left); w < x {
			left += strings.Repeat(" ", x-w)
		}
		right := ansi.TruncateLeft(line, x+ow, "")
		bl[y+i] = left + "\x1b[0m" + o + "\x1b[0m" + right
	}
	return strings.Join(bl, "\n")
}

// fit pads or truncates a styled line to exactly w cells.
func fit(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// drawBox returns a w×h bordered box with the title embedded in the top border (like a nvim float).
func drawBox(title string, lines []string, w, h int, border color.Color) []string {
	bs := lipgloss.NewStyle().Foreground(border)
	inner := w - 2
	t := ""
	if title != "" {
		t = " " + truncate(title, max(1, inner-6)) + " "
	}
	fill := max(0, w-3-lipgloss.Width(t)) // "╭─" + title + fill + "╮"
	out := []string{bs.Render("╭─") + titleStyle.Render(t) + bs.Render(strings.Repeat("─", fill)+"╮")}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, bs.Render("│")+fit(l, inner)+bs.Render("│"))
	}
	return append(out, bs.Render("╰"+strings.Repeat("─", inner)+"╯"))
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// screenSize returns the terminal size, with sane fallbacks before the first resize event.
func (m *Model) screenSize() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	return w, h
}

// dimBackdrop drains the color of everything under a floating window so the window is the only thing
// that draws the eye (a modal backdrop); the text stays readable in the dim color.
func dimBackdrop(base string) string {
	lines := strings.Split(base, "\n")
	for i, l := range lines {
		lines[i] = dimStyle.Render(ansi.Strip(l))
	}
	return strings.Join(lines, "\n")
}

// center places box in the middle of base.
func (m *Model) center(base string, box []string) string {
	w, h := m.screenSize()
	bw := 0
	for _, l := range box {
		bw = max(bw, lipgloss.Width(l))
	}
	x, y := max(0, (w-bw)/2), max(0, (h-len(box))/2-1)
	return overlay(dimBackdrop(base), strings.Join(box, "\n"), x, y)
}
