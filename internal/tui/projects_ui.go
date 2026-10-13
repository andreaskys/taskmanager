package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// Projects (space p): everything about one project in a single place, across boards — its cards by
// state, what is late, what is done — plus the sticky notes tagged with the project's name.

type projRow struct {
	Name             string // "" = cards without project
	Open, Late, Done int
	Total            int
}

// projItem is one line of the right pane: a card or a note.
type projItem struct {
	card *store.DueCard
	note *store.Note
}

func (r projRow) label() string {
	if r.Name == "" {
		return "(no project)"
	}
	return r.Name
}

func isDoneCard(d store.DueCard) bool { return d.ColumnID == d.DoneColumnID }

func (m *Model) loadProjects() {
	all, err := m.st.AllCards()
	m.fail(err)
	m.projAll = all
	byName := map[string]*projRow{}
	for _, d := range all {
		r := byName[d.Project]
		if r == nil {
			r = &projRow{Name: d.Project}
			byName[d.Project] = r
		}
		r.Total++
		switch {
		case d.ArchivedAt != "" || isDoneCard(d):
			r.Done++
		default:
			r.Open++
			if overdue(d.Due) {
				r.Late++
			}
		}
	}
	m.projRows = m.projRows[:0]
	for _, r := range byName {
		m.projRows = append(m.projRows, *r)
	}
	sort.Slice(m.projRows, func(i, j int) bool {
		a, b := m.projRows[i], m.projRows[j]
		if (a.Name == "") != (b.Name == "") {
			return b.Name == "" // cards without project last
		}
		if a.Open != b.Open {
			return a.Open > b.Open
		}
		return fold(a.Name) < fold(b.Name)
	})
	m.projSel = max(0, min(m.projSel, len(m.projRows)-1))
	m.loadProjItems()
}

// loadProjItems lists the cards (open first) and tagged notes of the selected project.
func (m *Model) loadProjItems() {
	m.projItems = m.projItems[:0]
	if m.projSel >= len(m.projRows) {
		return
	}
	name := m.projRows[m.projSel].Name
	var open, done []store.DueCard
	for _, d := range m.projAll {
		if d.Project != name {
			continue
		}
		if d.ArchivedAt != "" || isDoneCard(d) {
			done = append(done, d)
		} else {
			open = append(open, d)
		}
	}
	// late first, then by due date (cards without date after the dated ones), then board order
	sort.SliceStable(open, func(i, j int) bool {
		a, b := open[i].Due, open[j].Due
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	for _, group := range [][]store.DueCard{open, done} {
		for i := range group {
			d := group[i]
			m.projItems = append(m.projItems, projItem{card: &d})
		}
	}
	if name != "" {
		notes, _ := m.st.Notes()
		for _, n := range notes {
			for _, tag := range strings.Split(n.Tags, ",") {
				if tag != "" && tag == fold(name) {
					nn := n
					m.projItems = append(m.projItems, projItem{note: &nn})
					break
				}
			}
		}
	}
	m.projCur = max(0, min(m.projCur, len(m.projItems)-1))
}

func (m *Model) openProjects() {
	m.projSel, m.projCur, m.projFocus = 0, 0, false
	m.loadProjects()
	m.mode = modeProjects
}

func (m *Model) updateProjects(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q", "p":
		if m.projFocus {
			m.projFocus = false
			break
		}
		m.mode = modeBoard
		m.fail(m.reload())
	case "j", "down":
		if m.projFocus {
			m.projCur = min(m.projCur+1, len(m.projItems)-1)
		} else if m.projSel < len(m.projRows)-1 {
			m.projSel++
			m.projCur = 0
			m.loadProjItems()
		}
	case "k", "up":
		if m.projFocus {
			m.projCur = max(m.projCur-1, 0)
		} else if m.projSel > 0 {
			m.projSel--
			m.projCur = 0
			m.loadProjItems()
		}
	case "l", "right", "tab":
		if len(m.projItems) > 0 {
			m.projFocus = true
		}
	case "h", "left", "shift+tab":
		m.projFocus = false
	case "enter":
		if !m.projFocus {
			if len(m.projItems) > 0 {
				m.projFocus = true
			}
			break
		}
		if m.projCur >= len(m.projItems) {
			break
		}
		it := m.projItems[m.projCur]
		switch {
		case it.card != nil:
			d := *it.card
			if d.ArchivedAt != "" {
				m.fail(m.unarchive(d.ID))
				m.notice = "restored from the archive"
			}
			m.jumpTo(d)
			if cur := m.current(); cur != nil && cur.ID == d.ID {
				m.openDetail()
			}
		case it.note != nil:
			m.openNotes()
			m.selectNote(it.note.ID)
		}
	case "/": // search for the project's name everywhere
		if m.projSel < len(m.projRows) && m.projRows[m.projSel].Name != "" {
			name := m.projRows[m.projSel].Name
			cmd := m.openSearch()
			if m.pick != nil {
				m.pick.input.SetValue(name)
				m.pick.input.CursorEnd()
				m.pick.refilter()
			}
			return m, cmd
		}
	}
	return m, nil
}

func (m *Model) viewProjects(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*90/100, 70, 160)
	H := clamp(sh*80/100, 14, 44) + 1
	lw := W * 2 / 5
	rw := W - lw - 1
	rows := H - 2 - 2

	// left: the projects
	var left []string
	start := 0
	if m.projSel >= rows {
		start = m.projSel - rows + 1
	}
	for i := start; i < len(m.projRows) && i < start+rows; i++ {
		r := m.projRows[i]
		sel := i == m.projSel
		bar, st := "  ", lipgloss.NewStyle()
		if sel {
			bar = barStyle.Render("▌ ")
			st = st.Background(th.SelBg)
		}
		name := lipgloss.NewStyle().Bold(true).Foreground(projectColor(r.Name)).Render(r.label())
		counts := dimStyle.Render(fmt.Sprintf(" %d open", r.Open))
		if r.Late > 0 {
			counts += lipgloss.NewStyle().Foreground(th.Err).Render(fmt.Sprintf(" · %d late", r.Late))
		}
		counts += dimStyle.Render(fmt.Sprintf(" · %d done", r.Done))
		left = append(left, st.Render(fit(bar+name+counts, lw-2)))
	}
	if len(m.projRows) == 0 {
		left = append(left, dimStyle.Render("  no cards yet"))
	}

	// right: cards and notes of the selected project
	var right []string
	title := "Project"
	if m.projSel < len(m.projRows) {
		r := m.projRows[m.projSel]
		title = r.label()
		pct := 0
		if r.Total > 0 {
			pct = r.Done * 100 / r.Total
		}
		filled := pct * 20 / 100
		right = append(right, " "+lipgloss.NewStyle().Foreground(th.Ok).Render(strings.Repeat("█", filled))+
			dimStyle.Render(strings.Repeat("░", 20-filled)+fmt.Sprintf(" %d%% done · %d card(s)", pct, r.Total)), "")
	}
	rrows := rows - len(right)
	cstart := 0
	if m.projCur >= rrows {
		cstart = m.projCur - rrows + 1
	}
	for i := cstart; i < len(m.projItems) && i < cstart+rrows; i++ {
		it := m.projItems[i]
		bar, bg := "  ", lipgloss.NewStyle()
		if i == m.projCur && m.projFocus {
			bar = barStyle.Render("▌ ")
			bg = bg.Background(th.SelBg)
		}
		var line string
		switch {
		case it.card != nil:
			d := it.card
			state := d.ColumnName
			ts := lipgloss.NewStyle()
			mark := "○"
			switch {
			case d.ArchivedAt != "":
				mark, state, ts = "🗃", "archived", dimStyle
			case isDoneCard(*d):
				mark, ts = "✔", dimStyle
			case overdue(d.Due):
				mark, ts = "!", lipgloss.NewStyle().Foreground(th.Err)
			}
			line = bar + mark + " " + ts.Render(d.Title) + dimStyle.Render(fmt.Sprintf("  #%d · %s › %s", d.ID, d.BoardName, state))
			if d.Due != "" {
				line += dimStyle.Render(" · ⏰ " + d.Due)
			}
		case it.note != nil:
			line = bar + "📝 " + noteLabel(*it.note) + dimStyle.Render("  note")
		}
		right = append(right, bg.Render(fit(line, rw-2)))
	}
	if len(m.projItems) == 0 && m.projSel < len(m.projRows) {
		right = append(right, dimStyle.Render("  nothing here"))
	}

	lb := drawBox("Projects", append(left, padTo(rows-len(left))...), lw, H-2, th.Accent)
	rb := drawBox(title, right, rw, H-2, th.Rule)
	box := make([]string, 0, H)
	for i := range lb {
		box = append(box, lb[i]+" "+rb[i])
	}
	return m.center(base, box)
}

func padTo(n int) []string {
	if n <= 0 {
		return nil
	}
	return make([]string, n)
}
