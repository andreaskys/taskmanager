package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// Diary (space j): one text per day, kept forever and searchable. The window opens on today; [ and ]
// walk day by day, { and } jump between days that have an entry. Under the text it lists what happened
// to the cards that day (from the card history). Unfinished checkboxes of the last entry can be
// carried to today with p.

type journalEditor struct {
	area textarea.Model
	day  string
}

func dayLabel(day string) string {
	t, err := time.ParseInLocation(isoDate, day, time.Local)
	if err != nil {
		return day
	}
	label := t.Format("Mon 2006-01-02")
	switch day {
	case todayStr():
		label += " · today"
	case time.Now().AddDate(0, 0, -1).Format(isoDate):
		label += " · yesterday"
	}
	return label
}

func addDays(day string, n int) string {
	t, err := time.ParseInLocation(isoDate, day, time.Local)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, n).Format(isoDate)
}

// loadJournal reads the entry and the card activity of m.jDay.
func (m *Model) loadJournal() {
	e, _ := m.st.Journal(m.jDay)
	m.jEntry = e
	ev, err := m.st.EventsOn(m.jDay)
	m.fail(err)
	m.jEvents = ev
	n := len(strings.Split(e.Body, "\n"))
	m.jSel = max(0, min(m.jSel, n-1))
	if e.Body == "" {
		m.jSel = 0
	}
}

func (m *Model) openJournal(day string) {
	if day == "" {
		day = todayStr()
	}
	m.jDay, m.jSel, m.jOff = day, 0, 0
	m.loadJournal()
	m.mode = modeJournal
}

func (m *Model) gotoDay(day string) {
	m.jDay, m.jSel, m.jOff = day, 0, 0
	m.loadJournal()
}

// pendingLines returns the unchecked checkbox lines of the latest entry before day.
func (m *Model) pendingLines(day string) []string {
	prev, ok := m.st.JournalNeighbor(day, -1)
	if !ok {
		return nil
	}
	e, _ := m.st.Journal(prev)
	var out []string
	for _, l := range strings.Split(e.Body, "\n") {
		if _, kind, _ := parseLine(l); kind == lineOpen {
			out = append(out, l)
		}
	}
	return out
}

func (m *Model) updateJournal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	lines := strings.Split(m.jEntry.Body, "\n")
	switch key {
	case "esc", "q":
		m.mode = modeBoard
		m.fail(m.reload())
	case "[", "h", "left":
		m.gotoDay(addDays(m.jDay, -1))
	case "]", "l", "right":
		m.gotoDay(addDays(m.jDay, 1))
	case "{", "H":
		if d, ok := m.st.JournalNeighbor(m.jDay, -1); ok {
			m.gotoDay(d)
		} else {
			m.notice = "no earlier entry"
		}
	case "}", "L":
		if d, ok := m.st.JournalNeighbor(m.jDay, 1); ok {
			m.gotoDay(d)
		} else {
			m.notice = "no later entry"
		}
	case "t":
		m.gotoDay(todayStr())
	case "g":
		return m, m.ask("Go to day", m.jDay, func(s string) error {
			s = strings.TrimSpace(s)
			if _, err := time.Parse(isoDate, s); err != nil {
				return fmt.Errorf("invalid date, use YYYY-MM-DD")
			}
			m.gotoDay(s)
			return nil
		}, withHint("YYYY-MM-DD"))
	case "j", "down":
		if m.jSel < len(lines)-1 {
			m.jSel++
		}
	case "k", "up":
		if m.jSel > 0 {
			m.jSel--
		}
	case "space", "x": // toggle the checkbox under the cursor
		if toggleLines(lines, m.jSel) {
			m.fail(m.st.SaveJournal(m.jDay, strings.Join(lines, "\n")))
			m.loadJournal()
		}
	case "e", "enter", "i":
		return m, m.openJournalEditor(m.jDay)
	case "p": // carry the unfinished checkboxes of the last entry
		pend := m.pendingLines(m.jDay)
		have := map[string]bool{}
		for _, l := range lines {
			have[strings.TrimSpace(l)] = true
		}
		var add []string
		for _, l := range pend {
			if !have[strings.TrimSpace(l)] {
				add = append(add, l)
			}
		}
		if len(add) == 0 {
			m.notice = "nothing pending to carry over"
			break
		}
		body := strings.TrimRight(m.jEntry.Body, "\n")
		if body != "" {
			body += "\n"
		}
		m.fail(m.st.SaveJournal(m.jDay, body+strings.Join(add, "\n")))
		m.loadJournal()
		m.notice = fmt.Sprintf("carried over %d pending item(s)", len(add))
	case "o":
		m.openLinks(m.jEntry.Body, "", 0)
	}
	return m, nil
}

func (m *Model) openJournalEditor(day string) tea.Cmd {
	e, _ := m.st.Journal(day)
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.CharLimit = 0
	a.Placeholder = "what happened today, what is left for tomorrow…  (shift+enter adds a checkbox)"
	a.SetValue(e.Body)
	a.MoveToEnd()
	m.jEd = &journalEditor{area: a, day: day}
	m.jDay = day
	m.mode = modeJournalEdit
	return m.jEd.area.Focus()
}

func (m *Model) updateJournalEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	e := m.jEd
	key := msg.String()
	if key == "esc" || key == "ctrl+s" { // both save: the day is never lost by leaving the editor
		m.fail(m.st.SaveJournal(e.day, e.area.Value()))
		m.jEd = nil
		m.mode = modeJournal
		m.loadJournal()
		return m, nil
	}
	if handleEditorKey(&e.area, key, true) {
		return m, nil
	}
	var cmd tea.Cmd
	e.area, cmd = e.area.Update(msg)
	return m, cmd
}

func (m *Model) viewJournalEdit(base string) string {
	e := m.jEd
	sw, sh := m.screenSize()
	W := clamp(sw*70/100, 56, 120)
	H := clamp(sh*70/100, 12, 34)
	e.area.SetWidth(W - 4)
	e.area.SetHeight(H - 2)
	var lines []string
	for _, l := range strings.Split(e.area.View(), "\n") {
		lines = append(lines, " "+l)
	}
	return m.center(base, drawBox("Diary · "+dayLabel(e.day), lines, W, H, th.Accent))
}

// activityLines describes the card events of a day, one line each.
func activityLines(events []store.CardEvent) []string {
	var out []string
	for _, ev := range events {
		at := ev.At.In(time.Local).Format("15:04")
		title := ev.Title
		if ev.Project != "" {
			title = ev.Project + " " + title
		}
		var what string
		switch ev.Kind {
		case store.EvCreated:
			what = "new → " + ev.To
		case store.EvMoved, store.EvDone:
			what = ev.From + " → " + ev.To
		case store.EvArchived:
			what = "archived"
		case store.EvUnarchived:
			what = "restored from the archive"
		case store.EvDeleted:
			what = "deleted"
		case store.EvRestored:
			what = "restored (undo)"
		}
		icon := "•"
		switch ev.Kind {
		case store.EvDone:
			icon = "✔"
		case store.EvCreated:
			icon = "+"
		case store.EvDeleted:
			icon = "✖"
		}
		out = append(out, fmt.Sprintf("%s %s %s  %s", dimStyle.Render(at), icon, title, dimStyle.Render(what)))
	}
	return out
}

func (m *Model) viewJournal(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*80/100, 60, 140)
	H := clamp(sh*84/100, 16, 48)
	inner := W - 2

	act := activityLines(m.jEvents)
	actRows := 0
	if len(act) > 0 {
		actRows = min(len(act), 6) + 1 // + title
	}
	rows := max(3, H-2-2-1-actRows) // header+blank, rule, activity

	days := 0
	if all, err := m.st.JournalEntries(); err == nil {
		days = len(all)
	}
	lines := []string{" " + titleStyle.Render(dayLabel(m.jDay)) + dimStyle.Render(fmt.Sprintf("   %d day(s) with entries", days)), ""}

	var body []string
	selFirst, selLast := 0, 0
	if m.jEntry.Body == "" {
		body = []string{dimStyle.Render("  Nothing written for this day. Press ") + keyStyle.Render("e") + dimStyle.Render(" to write")}
		if prev := m.pendingLines(m.jDay); len(prev) > 0 && m.jDay == todayStr() {
			body = append(body, dimStyle.Render(fmt.Sprintf("  or %s to carry over %d pending item(s) from the last entry.", keyStyle.Render("p"), len(prev))))
		}
	}
	for i, l := range strings.Split(m.jEntry.Body, "\n") {
		if m.jEntry.Body == "" {
			break
		}
		ind, kind, text := parseLine(l)
		prefix := strings.Repeat(" ", min(ind, 8))
		switch kind {
		case lineOpen:
			prefix += boxOpen + " "
		case lineDone:
			prefix += boxDone + " "
		default:
			prefix, text = "", l
		}
		pw := lipgloss.Width(prefix)
		if i == m.jSel {
			selFirst = len(body)
		}
		for j, seg := range wrapText(text, max(4, inner-4-pw)) {
			bar := "  "
			if i == m.jSel {
				bar = barStyle.Render("▌ ")
			}
			lead := prefix
			if j > 0 {
				lead = strings.Repeat(" ", pw)
			}
			body = append(body, bar+lead+highlightRefs(seg))
		}
		if i == m.jSel {
			selLast = len(body) - 1
		}
	}
	if selLast >= m.jOff+rows {
		m.jOff = selLast - rows + 1
	}
	if selFirst < m.jOff {
		m.jOff = selFirst
	}
	m.jOff = max(0, min(m.jOff, max(0, len(body)-rows)))
	for i := m.jOff; i < len(body) && i < m.jOff+rows; i++ {
		lines = append(lines, " "+body[i])
	}
	for len(lines) < 2+rows {
		lines = append(lines, "")
	}
	lines = append(lines, ruleStyleLine(inner))
	if len(act) > 0 {
		lines = append(lines, " "+dimStyle.Render("Cards that day"))
		for i := 0; i < len(act) && i < 6; i++ {
			lines = append(lines, "  "+act[i])
		}
	}
	return m.center(base, drawBox("Diary", lines, W, H, th.Accent))
}

// highlightRefs colors the #id references and URLs of a text segment.
func highlightRefs(s string) string {
	s = cardRef.ReplaceAllStringFunc(s, func(x string) string {
		i := strings.Index(x, "#")
		return x[:i] + keyStyle.Render(x[i:])
	})
	return urlRef.ReplaceAllStringFunc(s, func(x string) string { return keyStyle.Render(x) })
}
