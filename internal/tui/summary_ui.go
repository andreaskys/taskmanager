package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Summary (space w): what got done in a week or a month — numbers, a bar per day, projects and the
// list of finished cards — and a Markdown version (y copies it) to paste in a daily or a report.

// weekStart returns the Monday of the week that holds day.
func weekStart(day string) string {
	t, err := time.ParseInLocation(isoDate, day, time.Local)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format(isoDate)
}

func monthBounds(day string) (string, string) {
	t, err := time.ParseInLocation(isoDate, day, time.Local)
	if err != nil {
		return day, day
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.Local)
	return first.Format(isoDate), first.AddDate(0, 1, -1).Format(isoDate)
}

// sumRange gives the period shown for the anchor day.
func (m *Model) sumRange() (string, string) {
	if m.sumMonth {
		return monthBounds(m.sumAnchor)
	}
	from := weekStart(m.sumAnchor)
	return from, addDays(from, 6)
}

func (m *Model) loadSummary() {
	from, to := m.sumRange()
	s, err := m.st.Summarize(from, to)
	m.fail(err)
	m.sum = s
	m.sumOff = 0
}

func (m *Model) openSummary() {
	m.sumAnchor, m.sumMonth = todayStr(), false
	m.loadSummary()
	m.mode = modeSummary
}

// clipboard copies text with the first tool found (wl-copy, xclip, xsel).
var clipboard = func(text string) error {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool (wl-copy, xclip, xsel)")
}

// saveSummary writes the Markdown next to the database and returns the path.
func (m *Model) saveSummary(md string) (string, error) {
	dir := filepath.Dir(m.st.Path())
	path := filepath.Join(dir, fmt.Sprintf("summary-%s_%s.md", m.sum.From, m.sum.To))
	return path, os.WriteFile(path, []byte(md), 0o644)
}

func (m *Model) updateSummary(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	step := 7
	if m.sumMonth {
		step = 0
	}
	shift := func(dir int) {
		if m.sumMonth {
			t, _ := time.ParseInLocation(isoDate, m.sumAnchor, time.Local)
			m.sumAnchor = time.Date(t.Year(), t.Month()+time.Month(dir), 1, 0, 0, 0, 0, time.Local).Format(isoDate)
		} else {
			m.sumAnchor = addDays(m.sumAnchor, dir*step)
		}
		m.loadSummary()
	}
	switch msg.String() {
	case "esc", "q", "w":
		m.mode = modeBoard
		m.fail(m.reload())
	case "[", "h", "left":
		shift(-1)
	case "]", "l", "right":
		shift(1)
	case "t":
		m.sumAnchor = todayStr()
		m.loadSummary()
	case "m": // week ↔ month
		m.sumMonth = !m.sumMonth
		m.loadSummary()
	case "j", "down":
		m.sumOff++
	case "k", "up":
		m.sumOff = max(0, m.sumOff-1)
	case "y": // copy as Markdown (or save it when there is no clipboard tool)
		md := m.sum.Markdown()
		if err := clipboard(md); err == nil {
			m.notice = "summary copied as Markdown"
		} else if path, werr := m.saveSummary(md); werr == nil {
			m.notice = "no clipboard tool: saved to " + path
		} else {
			m.fail(werr)
		}
	case "s": // save the Markdown file
		if path, err := m.saveSummary(m.sum.Markdown()); err == nil {
			m.notice = "saved " + path
		} else {
			m.fail(err)
		}
	}
	return m, nil
}

func (m *Model) viewSummary(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*80/100, 60, 130)
	H := clamp(sh*84/100, 16, 48)
	sm := m.sum
	title := "Week"
	if m.sumMonth {
		title = "Month"
	}
	lines := []string{" " + titleStyle.Render(fmt.Sprintf("%s  %s → %s", title, sm.From, sm.To)), ""}
	stat := func(label string, v any, st lipgloss.Style) string {
		return st.Render(fmt.Sprintf("%v", v)) + dimStyle.Render(" "+label)
	}
	overdueStyle := lipgloss.NewStyle()
	if sm.Overdue > 0 {
		overdueStyle = overdueStyle.Foreground(th.Err)
	}
	bold := lipgloss.NewStyle().Bold(true)
	row := stat("done", sm.Done, bold.Foreground(th.Ok)) + dimStyle.Render("  ·  ") + stat("created", sm.Created, bold) +
		dimStyle.Render("  ·  ") + stat("moves", sm.Moves, bold) + dimStyle.Render("  ·  ") + stat("overdue now", sm.Overdue, overdueStyle.Bold(true))
	if sm.Done > 0 {
		row += dimStyle.Render(fmt.Sprintf("  ·  avg %.1f days to finish", sm.AvgDays))
	}
	if sm.Journal > 0 {
		row += dimStyle.Render(fmt.Sprintf("  ·  %d diary day(s)", sm.Journal))
	}
	lines = append(lines, " "+row, "")

	// done per day
	perDay := map[string]int{}
	for _, d := range sm.DoneCards {
		if t, err := time.Parse("2006-01-02 15:04:05", d.DoneAt); err == nil {
			perDay[t.In(time.Local).Format(isoDate)]++
		}
	}
	if !m.sumMonth {
		peak := 1
		for _, n := range perDay {
			peak = max(peak, n)
		}
		for i := 0; i < 7; i++ {
			day := addDays(sm.From, i)
			t, _ := time.ParseInLocation(isoDate, day, time.Local)
			n := perDay[day]
			bar := strings.Repeat("█", n*24/peak)
			if n > 0 && bar == "" {
				bar = "▏"
			}
			lines = append(lines, fmt.Sprintf(" %s %s %s %s", dimStyle.Render(t.Format("Mon")), dimStyle.Render(day[5:]),
				lipgloss.NewStyle().Foreground(th.Ok).Render(fit(bar, 24)), dimStyle.Render(fmt.Sprint(n))))
		}
		lines = append(lines, "")
	}

	// done per project
	var projs []string
	for p := range sm.Projects {
		projs = append(projs, p)
	}
	sort.Slice(projs, func(i, j int) bool {
		if sm.Projects[projs[i]] != sm.Projects[projs[j]] {
			return sm.Projects[projs[i]] > sm.Projects[projs[j]]
		}
		return projs[i] < projs[j]
	})
	if len(projs) > 0 {
		var parts []string
		for _, p := range projs {
			name := p
			if name == "" {
				name = "(no project)"
			}
			parts = append(parts, fmt.Sprintf("%s %d", name, sm.Projects[p]))
		}
		lines = append(lines, " "+dimStyle.Render("By project: ")+strings.Join(parts, dimStyle.Render("  ·  ")), "")
	}

	// finished cards (scrolls)
	var done []string
	for _, d := range sm.DoneCards {
		p := ""
		if d.Project != "" {
			p = lipgloss.NewStyle().Bold(true).Foreground(projectColor(d.Project)).Render(d.Project) + " "
		}
		done = append(done, "  ✔ "+p+d.Title+dimStyle.Render(fmt.Sprintf("  #%d · %s", d.ID, d.BoardName)))
	}
	if len(done) == 0 {
		done = []string{dimStyle.Render("  nothing finished in this period")}
	}
	rows := max(2, H-2-len(lines))
	m.sumOff = max(0, min(m.sumOff, max(0, len(done)-rows)))
	for i := m.sumOff; i < len(done) && i < m.sumOff+rows; i++ {
		lines = append(lines, done[i])
	}
	return m.center(base, drawBox("Summary", lines, W, H, th.Accent))
}
