package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// picker is the floating search window: query on top, matching cards on the left, preview on the right.
type picker struct {
	input   textinput.Model
	all     []store.DueCard
	results []store.DueCard
	notes   []store.Note         // every sticky note, searched by title, #tag and text
	noteRes []store.Note         // matching notes, listed after the matching cards
	days    []store.JournalEntry // every diary day
	dayRes  []store.JournalEntry // matching diary days, listed last
	idx     int                  // index over results, then noteRes, then dayRes
}

// total is the number of rows in the result list (cards, then notes, then diary days).
func (p *picker) total() int { return len(p.results) + len(p.noteRes) + len(p.dayRes) }

// matches reports whether every term (already folded) is in hay; a #term only matches tags.
func matches(hay, tags string, terms []string) bool {
	for _, t := range terms {
		if strings.HasPrefix(t, "#") {
			if len(t) == 1 || !strings.Contains(tags, t[1:]) {
				return false
			}
		} else if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func (m *Model) openSearch() tea.Cmd {
	all, err := m.st.AllCards()
	if err != nil {
		m.fail(err)
		return nil
	}
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "project, description, note, diary, #tag…"
	notes, err := m.st.Notes()
	if err != nil {
		m.fail(err)
		return nil
	}
	days, err := m.st.JournalEntries()
	if err != nil {
		m.fail(err)
		return nil
	}
	m.pick = &picker{input: in, all: all, notes: notes, days: days}
	m.pick.refilter()
	m.mode = modeSearch
	return m.pick.input.Focus()
}

func (p *picker) refilter() {
	terms := strings.Fields(fold(p.input.Value()))
	p.results = p.results[:0]
	for _, d := range p.all {
		hay := fold(d.Project + " " + d.Title + " " + d.Tags + " " + d.Description + " " + d.BoardName + " " + d.ColumnName)
		if matches(hay, fold(d.Tags), terms) {
			p.results = append(p.results, d)
		}
	}
	p.noteRes = p.noteRes[:0]
	for _, n := range p.notes {
		if matches(fold(n.Title+" "+n.Tags+" "+n.Body), fold(n.Tags), terms) {
			p.noteRes = append(p.noteRes, n)
		}
	}
	p.dayRes = p.dayRes[:0]
	for _, d := range p.days {
		if matches(fold(d.Day+" diary "+d.Body), "", terms) {
			p.dayRes = append(p.dayRes, d)
		}
	}
	p.idx = max(0, min(p.idx, p.total()-1))
}

func (m *Model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.pick
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeBoard
		m.pick = nil
		return m, nil
	case "enter", "tab": // enter opens the card's details to edit it; tab only jumps to it on its board
		if p.idx >= len(p.results)+len(p.noteRes) && p.idx < p.total() { // a diary day
			d := p.dayRes[p.idx-len(p.results)-len(p.noteRes)]
			m.pick = nil
			m.openJournal(d.Day)
			if msg.String() == "enter" {
				return m, m.openJournalEditor(d.Day)
			}
			return m, nil
		}
		if p.idx >= len(p.results) && p.idx < p.total() { // a sticky note: open the notes window on it
			n := p.noteRes[p.idx-len(p.results)]
			m.pick = nil
			m.openNotes()
			m.selectNote(n.ID)
			if msg.String() == "enter" {
				return m, m.openNoteEditor(n)
			}
			return m, nil
		}
		if p.idx < len(p.results) {
			d := p.results[p.idx]
			m.pick = nil
			if d.ArchivedAt != "" { // opening an archived card restores it to its column
				m.fail(m.unarchive(d.ID))
				m.notice = "restored from the archive"
			}
			m.jumpTo(d)
			if key := msg.String(); key == "enter" && m.current() != nil && m.current().ID == d.ID {
				m.openDetail()
			}
		}
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		p.idx = min(p.idx+1, p.total()-1)
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		p.idx = max(p.idx-1, 0)
		return m, nil
	case "pgdown":
		p.idx = max(0, min(p.idx+10, p.total()-1))
		return m, nil
	case "pgup":
		p.idx = max(0, p.idx-10)
		return m, nil
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.idx = 0
		p.refilter()
	}
	return m, cmd
}

func (m *Model) viewPicker(base string) string {
	p := m.pick
	sw, sh := m.screenSize()
	W := clamp(sw*88/100, 60, 150)
	H := clamp(sh*78/100, 14, 40)
	lw := W * 5 / 9
	rw := W - lw - 1
	rows := H - 2 - 2 // minus border, prompt and rule

	// left: prompt + count, rule, results
	p.input.SetWidth(max(5, lw-2-4-12))
	count := dimStyle.Render(fmt.Sprintf("%d/%d", p.total(), len(p.all)+len(p.notes)+len(p.days)))
	prompt := keyStyle.Render(" ❯ ") + p.input.View()
	gap := max(1, lw-2-lipgloss.Width(prompt)-lipgloss.Width(count)-1)
	left := []string{prompt + strings.Repeat(" ", gap) + count, ruleStyle.Render(strings.Repeat("─", lw-2))}
	start := 0
	if p.idx >= rows {
		start = p.idx - rows + 1
	}
	for i := start; i < p.total() && i < start+rows; i++ {
		sel := i == p.idx
		if i >= len(p.results)+len(p.noteRes) { // diary day row
			d := p.dayRes[i-len(p.results)-len(p.noteRes)]
			bg := lipgloss.NewStyle()
			bar := "  "
			if sel {
				bg = bg.Background(th.SelBg)
				bar = barStyle.Render("▌ ")
			}
			ts := lipgloss.NewStyle()
			if sel {
				ts = ts.Bold(true).Foreground(th.Bright)
			}
			first, _, _ := strings.Cut(strings.TrimSpace(d.Body), "\n")
			left = append(left, bg.Render(fit(bar+dimStyle.Render("📅 ")+ts.Render(d.Day)+dimStyle.Render("  "+first), lw-2)))
			continue
		}
		if i >= len(p.results) { // sticky note row
			n := p.noteRes[i-len(p.results)]
			bg := lipgloss.NewStyle()
			bar := "  "
			if sel {
				bg = bg.Background(th.SelBg)
				bar = barStyle.Render("▌ ")
			}
			ts := lipgloss.NewStyle()
			if sel {
				ts = ts.Bold(true).Foreground(th.Bright)
			}
			line := bar + dimStyle.Render("📝 ") + ts.Render(noteLabel(n))
			if n.Tags != "" {
				line += dimStyle.Render("  #" + strings.ReplaceAll(n.Tags, ",", " #"))
			}
			left = append(left, bg.Render(fit(line, lw-2)))
			continue
		}
		d := p.results[i]
		bg := lipgloss.NewStyle()
		bar := "  "
		if sel {
			bg = bg.Background(th.SelBg)
			bar = barStyle.Render("▌ ")
		}
		proj := ""
		if d.Project != "" {
			proj = lipgloss.NewStyle().Bold(true).Foreground(projectColor(d.Project)).Render(d.Project) + " "
		}
		ts := lipgloss.NewStyle()
		if sel {
			ts = ts.Bold(true).Foreground(th.Bright)
		}
		where := d.BoardName + " › " + d.ColumnName
		if d.ArchivedAt != "" {
			where = "🗃 archived · " + where
		}
		line := bar + proj + ts.Render(d.Title) + dimStyle.Render("  "+where)
		left = append(left, bg.Render(fit(line, lw-2)))
	}
	if p.total() == 0 {
		left = append(left, dimStyle.Render("  no matches"))
	}

	// right: preview of the selected card
	var right []string
	rtitle := "Preview"
	if p.idx < len(p.results) {
		d := p.results[p.idx]
		rtitle = d.Title
		right = m.previewLines(d, rw-4)
	} else if p.idx < len(p.results)+len(p.noteRes) {
		n := p.noteRes[p.idx-len(p.results)]
		rtitle = noteLabel(n)
		right = notePreviewLines(n, rw-4)
	} else if p.idx < p.total() {
		d := p.dayRes[p.idx-len(p.results)-len(p.noteRes)]
		rtitle = "Diary · " + dayLabel(d.Day)
		right = append([]string{" " + dimStyle.Render("diary day (enter edits it)"), " " + ruleStyle.Render(strings.Repeat("─", rw-4))}, stickyLines(d.Body, rw-4, 1000, false)...)
	}

	lb := drawBox("Search", left, lw, H, th.Accent)
	rb := drawBox(rtitle, right, rw, H, th.Rule)
	box := make([]string, H)
	for i := range box {
		box[i] = lb[i] + " " + rb[i]
	}
	return m.center(base, box)
}

// previewLines renders the details of a card inside a pane of width w (with a 1-cell left margin).
func (m *Model) previewLines(d store.DueCard, w int) []string {
	var out []string
	add := func(s string) { out = append(out, " "+s) }
	for _, l := range wrapText(d.Title, w) {
		add(titleStyle.Render(l))
	}
	where := d.BoardName + " › " + d.ColumnName
	if d.ArchivedAt != "" {
		where += " · archived (enter restores it)"
	}
	add(dimStyle.Render(where))
	if ch := m.chips(d.Card, w); ch != "" {
		for _, l := range strings.Split(ch, "\n") {
			add(l)
		}
	}
	add(ruleStyle.Render(strings.Repeat("─", w)))
	for _, l := range strings.Split(d.Description, "\n") {
		ind, kind, text := parseLine(l)
		prefix := strings.Repeat(" ", ind)
		switch kind {
		case lineOpen:
			prefix += boxOpen + " "
		case lineDone:
			prefix += boxDone + " "
		default:
			text, prefix = l, ""
		}
		pw := lipgloss.Width(prefix)
		for j, seg := range wrapText(text, max(4, w-pw)) {
			lead := prefix
			if j > 0 {
				lead = strings.Repeat(" ", pw)
			}
			add(lead + seg)
		}
	}
	return out
}

// noteLabel is what a note is called in lists: its title, or else the first line of its text.
func noteLabel(n store.Note) string {
	if n.Title != "" {
		return n.Title
	}
	l, _, _ := strings.Cut(strings.TrimSpace(n.Body), "\n")
	return l
}

// notePreviewLines renders a sticky note inside a pane of width w (with a 1-cell left margin).
func notePreviewLines(n store.Note, w int) []string {
	var out []string
	add := func(s string) { out = append(out, " "+s) }
	add(dimStyle.Render("sticky note (enter edits it)"))
	if n.Tags != "" {
		add(dimStyle.Render("#" + strings.ReplaceAll(n.Tags, ",", " #")))
	}
	if n.Due != "" {
		add(dimStyle.Render("🔔 " + n.Due))
	}
	add(ruleStyle.Render(strings.Repeat("─", w)))
	for _, l := range stickyLines(n.Body, w, 1000, false) {
		out = append(out, l)
	}
	return out
}
