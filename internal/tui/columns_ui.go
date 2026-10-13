package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The columns window (space m): create, rename, reorder, delete columns and set their WIP limit.
// The last column is the "Done" column: it feeds the today view, the archive and done-at tracking.

func (m *Model) openColumns() {
	m.colSel = max(0, min(m.col, len(m.columns)-1))
	m.mode = modeColumns
}

// colID returns the id of the selected column in the window (0 if none).
func (m *Model) selectedColumnID() int64 {
	if m.colSel < len(m.columns) {
		return m.columns[m.colSel].ID
	}
	return 0
}

func (m *Model) updateColumns(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	id := m.selectedColumnID()
	keep := func(selID int64) { // after a change, keep the same column selected
		m.fail(m.reload())
		for i, c := range m.columns {
			if c.ID == selID {
				m.colSel = i
			}
		}
	}
	switch msg.String() {
	case "esc", "q", "m":
		m.mode = modeBoard
		m.col = min(m.colSel, max(0, len(m.columns)-1))
		m.clamp()
	case "j", "down":
		m.colSel = min(m.colSel+1, len(m.columns)-1)
	case "k", "up":
		m.colSel = max(m.colSel-1, 0)
	case "J", "K":
		dir := 1
		if msg.String() == "K" {
			dir = -1
		}
		m.fail(m.st.MoveColumn(id, dir))
		keep(id)
	case "n":
		return m, m.ask("New column", "", func(s string) error {
			c, err := m.st.AddColumn(m.boards[m.boardIdx].ID, s)
			if err == nil {
				id = c.ID
			}
			return err
		}, withAfter(func() { keep(id) }), withHint("inserted before the last (Done) column"))
	case "r":
		if id == 0 {
			break
		}
		return m, m.ask("Rename column", m.columns[m.colSel].Name, func(s string) error {
			return m.st.RenameColumn(id, s)
		})
	case "w", "l":
		if id == 0 {
			break
		}
		cur := ""
		if lim := m.columns[m.colSel].WIPLimit; lim > 0 {
			cur = strconv.Itoa(lim)
		}
		return m, m.ask("WIP limit", cur, func(s string) error {
			s = strings.TrimSpace(s)
			n := 0
			if s != "" {
				var err error
				if n, err = strconv.Atoi(s); err != nil || n < 0 {
					return fmt.Errorf("the limit must be a number, 0 or more")
				}
			}
			return m.st.SetWIPLimit(id, n)
		}, withHint("max cards in this column · empty or 0 removes the limit · a full column refuses new cards"))
	case "d":
		if id == 0 {
			break
		}
		name := m.columns[m.colSel].Name
		m.confirm(fmt.Sprintf("Delete column %q? Its cards move to the previous column. (y/n)", name), func() error {
			err := m.st.DeleteColumn(id)
			m.colSel = max(0, m.colSel-1)
			return err
		})
	}
	return m, nil
}

func (m *Model) viewColumns(base string) string {
	sw, _ := m.screenSize()
	W := clamp(sw*60/100, 56, 90)
	var lines []string
	lines = append(lines, " "+dimStyle.Render("the last column is Done: today view, archive and done-dates use it"))
	lines = append(lines, "")
	for i, c := range m.columns {
		limit := dimStyle.Render("no limit")
		if c.WIPLimit > 0 {
			limit = fmt.Sprintf("limit %d", c.WIPLimit)
			if m.totals[i] >= c.WIPLimit {
				limit = lipgloss.NewStyle().Foreground(th.Warn).Render(limit + " (full)")
			}
		}
		role := ""
		if i == len(m.columns)-1 {
			role = dimStyle.Render("  ← Done")
		}
		count := dimStyle.Render(fmt.Sprintf("%d cards", m.totals[i]))
		name := c.Name
		line := fmt.Sprintf(" %-18s %s   %s%s", truncate(name, 18), count, limit, role)
		bar := "  "
		if i == m.colSel {
			line = lipgloss.NewStyle().Background(th.SelBg).Bold(true).Render(fit(line, W-4))
			bar = barStyle.Render("▌ ")
		}
		lines = append(lines, bar+line)
	}
	lines = append(lines, "")
	lines = append(lines, " "+keyStyle.Render("n")+dimStyle.Render(" new · ")+keyStyle.Render("r")+dimStyle.Render(" rename · ")+keyStyle.Render("w")+dimStyle.Render(" WIP limit · ")+keyStyle.Render("J/K")+dimStyle.Render(" move · ")+keyStyle.Render("d")+dimStyle.Render(" delete · ")+keyStyle.Render("esc")+dimStyle.Render(" close"))
	return m.center(base, drawBox("Columns · "+m.boards[m.boardIdx].Name, lines, W, len(lines)+2, th.Accent))
}
