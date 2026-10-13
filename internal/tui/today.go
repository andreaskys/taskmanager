package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// Buckets of the today view, in display order.
const (
	bucketFocus = iota
	bucketOverdue
	bucketToday
	bucketWeek
	buckets
)

var bucketTitles = []string{"★ FOCUS", "OVERDUE", "TODAY", "NEXT 7 DAYS"}

// bucketOf places a card: pinned cards are always "focus", the rest go by due date.
func bucketOf(d store.DueCard) int {
	switch t := todayStr(); {
	case d.Focus:
		return bucketFocus
	case d.Due < t:
		return bucketOverdue
	case d.Due == t:
		return bucketToday
	}
	return bucketWeek
}

// loadToday lists focus cards first, then everything due within a week (a pinned card is listed once).
func (m *Model) loadToday() error {
	until := time.Now().AddDate(0, 0, 7).Format(isoDate)
	focus, err := m.st.FocusCards()
	if err != nil {
		return err
	}
	due, err := m.st.DueCards(until)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	m.dueCards = m.dueCards[:0]
	for _, d := range focus {
		seen[d.ID] = true
		m.dueCards = append(m.dueCards, d)
	}
	for _, d := range due {
		if !seen[d.ID] {
			m.dueCards = append(m.dueCards, d)
		}
	}
	m.todayIdx = max(0, min(m.todayIdx, len(m.dueCards)-1))
	return nil
}

// jumpTo opens the board that holds the card and selects it.
func (m *Model) jumpTo(d store.DueCard) {
	for i, b := range m.boards {
		if b.ID == d.BoardID {
			m.boardIdx = i
		}
	}
	m.clearFilters()
	m.mode = modeBoard
	m.fail(m.reload())
	m.selectCard(d.ID)
}

func (m *Model) updateToday(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.leader { // <space> x = done, t = back to board, u = undo
		m.leader = false
		switch key {
		case "x", "t", "u":
		case "h":
			m.openHelp()
			return m, nil
		case "esc":
			return m, nil
		default:
			m.notice = "no leader action on " + key
			return m, nil
		}
		if key == "t" {
			key = "esc"
		}
	} else if key == "space" {
		m.leader = true
		return m, nil
	}
	switch key {
	case "esc", "q", "T":
		m.mode = modeBoard
	case "j", "down":
		if m.todayIdx < len(m.dueCards)-1 {
			m.todayIdx++
		}
	case "k", "up":
		if m.todayIdx > 0 {
			m.todayIdx--
		}
	case "enter":
		if m.todayIdx < len(m.dueCards) {
			m.jumpTo(m.dueCards[m.todayIdx])
		}
	case "x": // mark done: move to the last column of the card's board
		if m.todayIdx < len(m.dueCards) {
			d := m.dueCards[m.todayIdx]
			m.fail(m.moveCard(d.Card, d.DoneColumnID))
			m.fail(m.reload())
		}
	case "u", "ctrl+z":
		m.undoLast()
	}
	return m, nil
}

func (m *Model) todayCounts() [buckets]int {
	var n [buckets]int
	for _, d := range m.dueCards {
		n[bucketOf(d)]++
	}
	return n
}

// dueLabel renders "Mon 15/10" plus a relative hint ("3d late", "today", "in 2d").
func dueLabel(due string) string {
	t, err := time.ParseInLocation(isoDate, due, time.Local)
	if err != nil {
		return "no date"
	}
	now, _ := time.ParseInLocation(isoDate, todayStr(), time.Local)
	d := int(t.Sub(now).Hours() / 24)
	rel := "today"
	switch {
	case d < 0:
		rel = fmt.Sprintf("%dd late", -d)
	case d > 0:
		rel = fmt.Sprintf("in %dd", d)
	}
	return t.Format("Mon 02/01") + " · " + rel
}

// Due-date urgency levels, as returned by shortDue.
const (
	dueLate = iota
	dueToday
	dueSoon
	dueLater
)

// shortDue words a due date the way people say it ("2d late", "today", "tomorrow", "Fri 16", "16 Oct")
// and says how urgent it is, so cards stay one short line and the color can carry the urgency.
func shortDue(due string) (string, int) {
	t, err := time.ParseInLocation(isoDate, due, time.Local)
	if err != nil {
		return due, dueLater
	}
	now, _ := time.ParseInLocation(isoDate, todayStr(), time.Local)
	d := int(math.Round(t.Sub(now).Hours() / 24))
	switch {
	case d < 0:
		return fmt.Sprintf("%dd late", -d), dueLate
	case d == 0:
		return "today", dueToday
	case d == 1:
		return "tomorrow", dueSoon
	case d < 7:
		return t.Format("Mon 2"), dueSoon
	case t.Year() != now.Year():
		return t.Format("2 Jan 06"), dueLater
	}
	return t.Format("2 Jan"), dueLater
}

// dueStyled renders a due date with the icon, colored by urgency (late red, today orange). Cards in
// the last column (done) are never shown as late.
func dueStyled(due string, done bool) string {
	label, lvl := shortDue(due)
	st := lipgloss.NewStyle()
	switch {
	case done:
		st = dimStyle
		if lvl == dueLate { // finished: the date it was due is plain history
			label = due
		}
	case lvl == dueLate:
		st = errStyle.Bold(true)
	case lvl == dueToday:
		st = lipgloss.NewStyle().Foreground(th.Warn).Bold(true)
	case lvl == dueSoon:
		st = lipgloss.NewStyle().Foreground(th.Info)
	default:
		st = dimStyle
	}
	return st.Render("⏰ " + label)
}

// viewToday lists due cards across all boards, grouped by urgency. h is the body height (0 = unknown).
func (m *Model) viewToday(h int) string {
	inner := m.detailInnerWidth()
	bucketColor := []color.Color{th.Purple, th.Err, th.Warn, th.Info}
	var lines []string
	selStart, selEnd := 0, 0
	type span struct{ y0, y1, idx int }
	var spans []span

	if len(m.dueCards) == 0 {
		lines = append(lines, dimStyle.Render("Nothing due in the next 7 days and nothing pinned (press * on a card to pin it). 🎉"))
	}
	last := -1
	for i, d := range m.dueCards {
		if b := bucketOf(d); b != last {
			if last != -1 {
				lines = append(lines, "")
			}
			cnt := m.todayCounts()[b]
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(bucketColor[b]).Render(fmt.Sprintf("%s (%d)", bucketTitles[b], cnt)))
			last = b
		}
		sel := i == m.todayIdx
		bg := lipgloss.NewStyle()
		glyph := "┃"
		if sel {
			bg = bg.Background(th.SelBg)
			glyph = "▌"
		}
		bar := lipgloss.NewStyle().Foreground(bucketColor[bucketOf(d)]).Render(glyph)
		proj := ""
		if d.Project != "" {
			proj = lipgloss.NewStyle().Bold(true).Foreground(projectColor(d.Project)).Render(strings.ToUpper(d.Project)) + "  "
		}
		titleSt := lipgloss.NewStyle()
		if sel {
			titleSt = titleSt.Bold(true).Foreground(th.Bright)
		}
		room := inner - 2 - lipgloss.Width(proj)
		l1 := proj + titleSt.Render(truncate(d.Title, max(8, room)))
		meta := []string{dueLabel(d.Due), d.BoardName + " › " + d.ColumnName}
		if dn, tot := checkProgress(d.Description); tot > 0 {
			meta = append(meta, fmt.Sprintf("☑ %d/%d", dn, tot))
		}
		if d.Tags != "" {
			meta = append(meta, "#"+strings.ReplaceAll(d.Tags, ",", " #"))
		}
		l2 := dimStyle.Render(truncate(strings.Join(meta, " · "), inner-2))
		y0 := len(lines)
		for _, l := range []string{l1, l2} {
			lines = append(lines, bar+bg.Render(" ")+bg.Width(inner-2).Render(l))
		}
		spans = append(spans, span{y0, len(lines) - 1, i})
		if sel {
			selStart, selEnd = y0, len(lines)-1
		}
	}

	// window: keep the selected item visible
	avail := len(lines)
	if h > 0 {
		avail = max(4, h-2-lipgloss.Height(m.todayHeader()))
	}
	off := 0
	if selEnd-avail+1 > 0 {
		off = selEnd - avail + 1
	}
	_ = selStart
	end := min(len(lines), off+avail)
	out := m.todayHeader() + "\n" + strings.Join(lines[off:end], "\n")

	// hit boxes (screen rows): panel border (1) + header rows
	y := m.bodyY + 1 + lipgloss.Height(m.todayHeader())
	for _, sp := range spans {
		if sp.y1 < off || sp.y0 >= end {
			continue
		}
		m.hits = append(m.hits, hit{0, max(m.width, 80), y + sp.y0 - off, y + sp.y1 - off, hitToday, 0, sp.idx})
	}
	st := panelStyle.Width(inner + 4)
	if h > 0 {
		st = st.Height(h)
	}
	return st.Render(out)
}

func (m *Model) todayHeader() string {
	// the summary counts by date, so a pinned card that is also late still counts as overdue
	var focus, late, today, week int
	t := todayStr()
	for _, d := range m.dueCards {
		if d.Focus {
			focus++
		}
		switch {
		case d.Due == "":
		case d.Due < t:
			late++
		case d.Due == t:
			today++
		default:
			week++
		}
	}
	return titleStyle.Render("Due soon") + dimStyle.Render(fmt.Sprintf("   %d focus · %d overdue · %d today · %d this week", focus, late, today, week)) + "\n" + ruleStyle.Render(strings.Repeat("─", m.detailInnerWidth()))
}
