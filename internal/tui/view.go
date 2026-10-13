package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// Styles are (re)built by applyTheme; see theme.go.
var (
	titleStyle, dimStyle, errStyle, selStyle             lipgloss.Style
	colStyle, colSelStyle, colDropSty                    lipgloss.Style
	panelStyle, keyStyle, ruleStyle, barStyle, chipStyle lipgloss.Style
	noticeStyle                                          lipgloss.Style
	prioLabels                                           = []string{"", "low", "medium", "high"}
	boxOpen                                              = "⬜" // checkbox glyphs (double-width, so they read bigger)
	boxDone                                              = "✅"
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = th.BG, th.FG
	return v
}

func hint(k, label string) string { return keyStyle.Render(k) + " " + dimStyle.Render(label) }

// windowHints are the key hints of the floating windows, most important first: the footer is the one
// place that shows them (so they never get cut off inside a window) and drops the last ones when the
// terminal is narrow.
func (m *Model) windowHints() [][2]string {
	switch m.mode {
	case modeInput:
		return [][2]string{{"enter", "confirm"}, {"esc", "cancel"}}
	case modeSearch:
		return [][2]string{{"↑/↓", "navigate"}, {"enter", "edit"}, {"tab", "show on board"}, {"esc", "close"}}
	case modeJournal:
		return [][2]string{{"e", "edit"}, {"[ ]", "day"}, {"space", "check"}, {"p", "carry over"}, {"{ }", "entries"}, {"g", "go to"}, {"t", "today"}, {"o", "links"}, {"esc", "close"}}
	case modeJournalEdit, modeNoteEdit:
		h := [][2]string{{"esc", "save"}, {"shift+enter", "checkbox"}, {"tab", "indent"}, {"alt+x", "toggle"}}
		if m.mode == modeNoteEdit {
			h = append(h, [2]string{"ctrl+t", "title/tags"})
		}
		return h
	case modeSummary:
		return [][2]string{{"[ ]", "period"}, {"m", "week/month"}, {"y", "copy Markdown"}, {"s", "save"}, {"t", "now"}, {"j/k", "scroll"}, {"esc", "close"}}
	case modeProjects:
		return [][2]string{{"j/k", "project"}, {"enter", "cards"}, {"/", "search name"}, {"esc", "back"}}
	case modeLinks:
		return [][2]string{{"enter", "open"}, {"x", "remove attachment"}, {"esc", "close"}}
	case modeNotes:
		return [][2]string{{"n", "new"}, {"enter", "edit"}, {"c", "color"}, {"p", "pin"}, {"D", "remind"}, {"a", "attach"}, {"o", "open"}, {"t", "to task"}, {"dd", "delete"}, {"u", "undo"}, {"esc", "close"}}
	case modeColumns:
		return [][2]string{{"n", "new"}, {"r", "rename"}, {"w", "WIP limit"}, {"J/K", "move"}, {"d", "delete"}, {"esc", "close"}}
	case modeHelp:
		return [][2]string{{"j/k", "scroll"}, {"esc", "close"}}
	case modeScratch:
		return [][2]string{{"ctrl+s", "save as task"}, {"shift+enter", "checkbox"}, {"esc", "close"}}
	case modeNewCard:
		return [][2]string{{"enter", "next"}, {"ctrl+s", "save"}, {"ctrl+t", "template"}, {"tab", "complete / next"}, {"esc", "cancel"}}
	}
	return nil
}

func (m *Model) hints() [][2]string {
	if m.leader {
		if m.mode == modeToday {
			return [][2]string{{"x", "mark done"}, {"t", "close"}, {"u", "undo"}, {"h", "help"}, {"esc", "cancel"}}
		}
		return [][2]string{{"space", "search"}, {"n", "new"}, {"x", "delete"}, {"enter", "notes"}, {"e", "open"}, {"t", "today"}, {".", "focus"}, {"f", "filter"}, {"s", "sort"}, {"r", "notes"}, {"j", "diary"}, {"w", "summary"}, {"p", "projects"}, {"u", "undo"}, {"U", "redo"}, {"m", "columns"}, {"a", "archive"}, {"E", "export"}, {"c", "clear"}, {"b", "boards"}, {"y", "theme"}, {"h", "help"}, {"q", "quit"}, {"esc", "cancel"}}
	}
	if w := m.windowHints(); w != nil {
		return w
	}
	switch m.mode {
	case modeDetail:
		return [][2]string{{"j/k", "line"}, {"space", "toggle"}, {"e", "edit text"}, {"t", "title"}, {"r", "project"}, {"u", "due"}, {"g", "tags"}, {"p", "priority"}, {"a", "attach"}, {"o", "open links"}, {"esc", "back"}}
	case modeBoards:
		return [][2]string{{"j/k", "navigate"}, {"enter", "open"}, {"n", "new board"}, {"d", "delete"}, {"esc", "back"}}
	case modeToday:
		return [][2]string{{"j/k", "navigate"}, {"enter", "open on board"}, {"x", "mark done"}, {"u", "undo"}, {"space", "menu"}, {"esc", "back"}}
	}
	if m.showHelp {
		return [][2]string{{"h/l", "column"}, {"j/k", "card"}, {"H/L", "move card"}, {"J/K", "reorder"}, {"n", "new"}, {"enter", "details"}, {"p", "priority"}, {"dd", "delete"}, {"u", "undo"}, {"ctrl+r", "redo"}, {"*", "pin as focus"}, {"f", "priority filter"}, {"s", "sort"}, {"T", "today"}, {"space space", "search"}, {"space enter", "meeting notes"}, {"esc", "clear filters"}, {"b", "boards"}, {"mouse", "click · double-click open · drag to move · wheel"}, {"?", "less"}, {"q", "quit"}}
	}
	return [][2]string{{"space", "menu"}, {"n", "new"}, {"enter", "open"}, {"dd", "delete"}, {"u", "undo"}, {"H/L", "move"}, {"T", "today"}, {"space h", "help"}, {"q", "quit"}}
}

// fitHints joins hints on ONE line no wider than width: when they do not fit, the least important
// (last) ones are dropped and "…" is added. prefixW is the width already used by a prefix.
func fitHints(hs [][2]string, width, prefixW int) string {
	sep := dimStyle.Render("  ·  ")
	sepW := lipgloss.Width(sep)
	var out strings.Builder
	used := prefixW
	for i, h := range hs {
		part := hint(h[0], h[1])
		w := lipgloss.Width(part)
		extra := 0
		if i > 0 {
			extra = sepW
		}
		if used+extra+w > width-2 && i > 0 { // leave room for the ellipsis
			out.WriteString(dimStyle.Render(" …"))
			break
		}
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(part)
		used += extra + w
	}
	return out.String()
}

func (m *Model) footer() string {
	width := max(20, m.width)
	var out string
	switch {
	case m.mode == modeConfirm:
		out = errStyle.Render(m.confirmMsg)
	case m.showHelp && m.mode == modeBoard && !m.leader: // the full key list may use several lines
		var parts []string
		for _, h := range m.hints() {
			parts = append(parts, hint(h[0], h[1]))
		}
		out = lipgloss.NewStyle().Width(width).Render(strings.Join(parts, dimStyle.Render("  ·  ")))
	default:
		prefix, pw := "", 0
		if m.leader {
			prefix = chipStyle.Foreground(th.Warn).Bold(true).Render("SPACE") + " "
			pw = lipgloss.Width(prefix)
		}
		out = prefix + fitHints(m.hints(), width, pw)
	}
	if m.notice != "" { // messages show in every mode, above the hints
		out = noticeStyle.Render(m.notice) + "\n" + out
	}
	if m.err != "" {
		out = errStyle.Render("error: "+m.err) + "\n" + out
	}
	return out
}

// baseMode is the screen drawn under a floating window: help, input and confirm remember where
// they were opened from; the columns window floats over the board.
func (m *Model) baseMode() mode {
	bm := m.mode
	switch bm {
	case modeHelp:
		bm = m.helpBack
	case modeInput, modeConfirm, modeLinks:
		bm = m.prevMode
	}
	if bm == modeColumns || bm == modeNotes || bm == modeNoteEdit || bm == modeJournal || bm == modeJournalEdit || bm == modeSummary || bm == modeProjects || bm == modeSearch || bm == modeNewCard || bm == modeScratch {
		return modeBoard
	}
	return bm
}

// columnsOpen reports whether the columns window is on screen (alone or under an input/confirm).
func (m *Model) columnsOpen() bool { return m.windowOpen(modeColumns) }

// windowOpen reports whether a floating window is on screen (alone or under an input/confirm/links window).
func (m *Model) windowOpen(w mode) bool {
	return m.mode == w || ((m.mode == modeInput || m.mode == modeConfirm || m.mode == modeLinks) && m.prevMode == w)
}

func (m *Model) header() string {
	name := ""
	if m.boardIdx < len(m.boards) {
		name = m.boards[m.boardIdx].Name
	}
	if m.baseMode() == modeToday {
		name = "Today"
	}
	total, late := 0, 0
	for i, cs := range m.cards {
		total += len(cs)
		if i == len(m.cards)-1 {
			continue // done column
		}
		for _, c := range cs {
			if overdue(c.Due) {
				late++
			}
		}
	}
	h := titleStyle.Render("▌kbn") + dimStyle.Render(" · ") + lipgloss.NewStyle().Bold(true).Render(name)
	if m.baseMode() != modeToday {
		h += dimStyle.Render(fmt.Sprintf("   %d cards", total))
		if off, vis := m.visibleCols(); vis < len(m.columns) { // narrow terminal: say which columns are on screen
			h += "  " + chipStyle.Render(fmt.Sprintf("◀ columns %d-%d of %d ▶", off+1, off+vis, len(m.columns)))
		}
		if late > 0 {
			h += "  " + errStyle.Render(fmt.Sprintf("%d overdue", late))
		}
	}
	orange := chipStyle.Foreground(th.Warn)
	if m.minPrio > 0 {
		h += "  " + orange.Render("priority ≥ "+prioLabels[m.minPrio])
	}
	if m.draft != "" {
		h += "  " + orange.Render("📝 notes draft")
	}
	if m.notesDue > 0 {
		h += "  " + chipStyle.Foreground(th.Err).Render(fmt.Sprintf("🔔 %d reminder(s)", m.notesDue))
	}
	if m.sortMode != sortManual {
		h += "  " + chipStyle.Render("sort: "+sortNames[m.sortMode])
	}
	if m.drag != nil && m.drag.started {
		h += "  " + orange.Render("✋ drop on a column")
	}
	return h
}

func (m *Model) render() string {
	m.hits = m.hits[:0]
	head, foot := m.header()+"\n"+m.focusStrip(), "\n"+m.footer()
	m.bodyY = strings.Count(head, "\n")
	h := 0
	if m.height > 0 {
		h = m.height - lipgloss.Height(head) - lipgloss.Height(foot)
	}
	var body string
	switch m.baseMode() {
	case modeBoards:
		body = m.viewBoards()
	case modeDetail:
		body = m.viewDetail()
	case modeToday:
		body = m.viewToday(h)
	default:
		body = m.viewBoard(h)
	}
	out := head + body + foot
	if m.columnsOpen() {
		out = m.viewColumns(out)
	}
	if m.notesOpen() {
		out = m.viewNotes(out)
	}
	if m.windowOpen(modeJournal) {
		out = m.viewJournal(out)
	}
	if m.windowOpen(modeSummary) {
		out = m.viewSummary(out)
	}
	if m.windowOpen(modeProjects) {
		out = m.viewProjects(out)
	}
	switch {
	case m.mode == modeLinks:
		out = m.viewLinks(out)
	case m.mode == modeJournalEdit && m.jEd != nil:
		out = m.viewJournalEdit(out)
	case m.mode == modeInput:
		out = m.viewInput(out)
	case m.mode == modeNoteEdit && m.noteEd != nil:
		out = m.viewNoteEdit(out)
	case m.mode == modeSearch && m.pick != nil:
		out = m.viewPicker(out)
	case m.mode == modeNewCard && m.form != nil:
		out = m.viewNewCard(out)
	case m.mode == modeScratch && m.scratch != nil:
		out = m.viewScratch(out)
	case m.mode == modeHelp:
		out = m.viewHelp(out)
	}
	// the footer holds the key hints and messages: it is never dimmed with the backdrop
	if n := strings.Count(foot, "\n"); n > 0 && m.mode != modeBoard && m.mode != modeDetail && m.mode != modeToday && m.mode != modeBoards {
		lines := strings.Split(out, "\n")
		if len(lines) >= n {
			copy(lines[len(lines)-n:], strings.Split(foot[1:], "\n"))
			out = strings.Join(lines, "\n")
		}
	}
	return out
}

// focusStrip is the pinned "focus of the day" line shown under the header (cards from every board).
func (m *Model) focusStrip() string {
	if len(m.focus) == 0 || m.baseMode() == modeToday {
		return ""
	}
	room := max(20, m.width-14)
	var parts []string
	used := 0
	for i, d := range m.focus {
		label := d.Title
		if d.Project != "" {
			label = d.Project + " " + label
		}
		if d.BoardID != m.boards[m.boardIdx].ID {
			label += " (" + d.BoardName + ")"
		}
		label = truncate(label, 40)
		if used+lipgloss.Width(label)+3 > room && len(parts) > 0 {
			parts = append(parts, dimStyle.Render(fmt.Sprintf("+%d more", len(m.focus)-i)))
			break
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(th.Purple).Render(label))
		used += lipgloss.Width(label) + 3
	}
	return lipgloss.NewStyle().Foreground(th.Purple).Bold(true).Render("★ focus ") + strings.Join(parts, dimStyle.Render(" · ")) + "\n"
}

// viewInput draws the single-field input window (edit title, due date, tags, column name, …).
func (m *Model) viewInput(base string) string {
	sw, _ := m.screenSize()
	W := clamp(sw*50/100, 46, 84)
	m.input.SetWidth(max(8, W-8))
	lines := []string{"", " " + m.input.View(), ""}
	if len(m.suggestList) > 0 {
		if s := suggestFor(m.input.Value(), m.suggestList, m.suggestMulti); len(s) > 0 {
			lines = append(lines, " "+dimStyle.Render("tab → ")+keyStyle.Render(s[0])+dimStyle.Render("   also: "+strings.Join(s[1:], " · ")))
		}
	}
	if m.inputHint != "" {
		for _, l := range wrapText(m.inputHint, W-6) {
			lines = append(lines, " "+dimStyle.Render(l))
		}
	}
	lines = append(lines, " "+keyStyle.Render("enter")+dimStyle.Render(" confirm · ")+keyStyle.Render("esc")+dimStyle.Render(" cancel"))
	return m.center(base, drawBox(m.inputMsg, lines, W, len(lines)+2, th.Accent))
}

func (m *Model) viewBoards() string {
	var b strings.Builder
	for i, bd := range m.boards {
		line := "  " + bd.Name
		if i == m.boardIdx {
			line = selStyle.Render("> " + bd.Name)
		}
		b.WriteString(line + "\n")
		m.hits = append(m.hits, hit{0, max(m.width, 80), m.bodyY + i, m.bodyY + i, hitBoard, 0, i})
	}
	return b.String()
}

// projectColor gives each project a stable color so cards are scannable.
func projectColor(name string) color.Color {
	h := 0
	for _, r := range fold(name) {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return th.Projects[h%len(th.Projects)]
}

// emptyColumnHint fills an empty column: the selected one (and the whole first-run board) say what
// to do next; the others stay quiet.
func (m *Model) emptyColumnHint(col, w int) string {
	total := 0
	for _, t := range m.totals {
		total += t
	}
	var msg string
	switch {
	case total == 0 && col == 0:
		msg = "No cards yet. Press n to add your first one, space h shows every key."
	case col == m.col:
		msg = "Nothing here. Press n to add a card."
	default:
		return dimStyle.Render("—")
	}
	var out []string
	for _, l := range wrapText(msg, w) {
		out = append(out, dimStyle.Render(l))
	}
	return strings.Join(out, "\n")
}

// minColW is the narrowest a board column may get: on a narrower terminal fewer columns are shown
// (the window follows the selection) instead of squeezing every card into a few characters.
const minColW = 26

// visibleCols returns the first visible column and how many are shown, keeping the selection in view.
func (m *Model) visibleCols() (off, vis int) {
	n := len(m.columns)
	vis = n
	if m.width > 0 {
		vis = max(1, min(n, m.width/minColW))
	}
	if m.col < m.colOff {
		m.colOff = m.col
	}
	if m.col >= m.colOff+vis {
		m.colOff = m.col - vis + 1
	}
	m.colOff = max(0, min(m.colOff, n-vis))
	return m.colOff, vis
}

// viewBoard draws the columns; boxH is the total height available (0 = unknown, no clipping).
func (m *Model) viewBoard(boxH int) string {
	n := len(m.columns)
	if n == 0 {
		return ""
	}
	off, vis := m.visibleCols()
	outer := 80 / vis
	if m.width > 0 {
		outer = m.width / vis
	}
	inner := max(12, outer-4) // border + padding
	avail := 1 << 20
	if boxH > 0 {
		avail = max(4, boxH-2-2) // minus border, minus column header + rule
	}
	var cols []string
	for i := off; i < off+vis; i++ {
		c := m.columns[i]
		x0 := (i - off) * outer
		x1 := x0 + outer - 1
		if i == off+vis-1 && m.width > x1 {
			x1 = m.width
		}
		colH := boxH
		if colH <= 0 {
			colH = 100
		}
		m.hits = append(m.hits, hit{x0, x1, m.bodyY, m.bodyY + colH - 1, hitColumn, i, 0})

		name := titleStyle.Render(c.Name)
		if i == n-1 {
			name = lipgloss.NewStyle().Bold(true).Foreground(th.Ok).Render(c.Name)
		}
		count := dimStyle.Render(fmt.Sprintf("%d", len(m.cards[i])))
		if m.filtersActive() {
			count = dimStyle.Render(fmt.Sprintf("%d/%d", len(m.cards[i]), m.totals[i]))
		}
		if lim := c.WIPLimit; lim > 0 { // WIP column: total/limit, warning when full, error when over
			cs := lipgloss.NewStyle().Foreground(th.Dim)
			switch {
			case m.totals[i] > lim:
				cs = lipgloss.NewStyle().Foreground(th.Err).Bold(true)
			case m.totals[i] == lim:
				cs = lipgloss.NewStyle().Foreground(th.Warn).Bold(true)
			}
			count = cs.Render(fmt.Sprintf("%d/%d", m.totals[i], lim))
		}
		// while dragging over this column, a bar marks where the card will land (it replaces a
		// spacer line, so the layout and the hit boxes do not move)
		dropAt := -1
		if m.drag != nil && m.drag.started && m.drag.over == i && m.sortMode == sortManual {
			dropAt = m.drag.idx
		}
		marker := lipgloss.NewStyle().Foreground(th.Warn).Bold(true).Render(strings.Repeat("━", inner))
		rule := ruleStyle.Render(strings.Repeat("─", inner))
		if dropAt == 0 {
			rule = marker
		}
		var b strings.Builder
		b.WriteString(name + " " + count + "\n")
		b.WriteString(rule + "\n")

		blocks := make([]string, len(m.cards[i]))
		heights := make([]int, len(blocks))
		total := 0
		for j, card := range m.cards[i] {
			blocks[j] = m.renderCard(i == m.col && j == m.row, i, card, inner)
			heights[j] = lipgloss.Height(blocks[j]) + 1 // + spacer
			total += heights[j]
		}
		start, end := 0, len(blocks)
		if total > avail { // overflow: scroll so the selected card stays visible
			room := avail - 2 // reserve lines for the "more" indicators
			sel := 0
			if i == m.col {
				sel = m.row
			}
			start = sel
			used := heights[sel]
			for start > 0 && used+heights[start-1] <= room {
				start--
				used += heights[start]
			}
			end = sel + 1
			for end < len(blocks) && used+heights[end] <= room {
				used += heights[end]
				end++
			}
			if start > 0 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("↑ %d more", start)) + "\n")
			}
		}
		y := m.bodyY + 3 // border + column header + rule
		if start > 0 {
			y++
		}
		for j := start; j < end; j++ {
			spacer := ""
			if dropAt == j+1 {
				spacer = marker
			}
			b.WriteString(blocks[j] + "\n" + spacer + "\n")
			m.hits = append(m.hits, hit{x0 + 1, x1 - 1, y, y + heights[j] - 1, hitCard, i, j})
			y += heights[j]
		}
		if end < len(blocks) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("↓ %d more", len(blocks)-end)))
		} else if len(blocks) == 0 {
			if dropAt >= 0 {
				b.WriteString(marker)
			} else {
				b.WriteString(m.emptyColumnHint(i, inner))
			}
		}
		style := colStyle
		switch {
		case m.drag != nil && m.drag.started && i == m.drag.over:
			style = colDropSty
		case i == m.col:
			style = colSelStyle
		}
		style = style.Width(outer)
		if boxH > 0 {
			style = style.Height(boxH)
		}
		cols = append(cols, style.Render(strings.TrimRight(b.String(), "\n")))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// renderCard draws one card: priority bar, project label, wrapped title, meta line.
func (m *Model) renderCard(selected bool, col int, c store.Card, w int) string {
	tw := w - 2 // bar + space
	var lines []string
	star := ""
	if c.Focus {
		star = lipgloss.NewStyle().Foreground(th.Purple).Render("★ ")
	}
	if c.Project != "" {
		lines = append(lines, star+lipgloss.NewStyle().Bold(true).Foreground(projectColor(c.Project)).Render(strings.ToUpper(c.Project)))
	} else if star != "" {
		lines = append(lines, strings.TrimRight(star, " "))
	}
	ts := lipgloss.NewStyle()
	if selected {
		ts = ts.Bold(true).Foreground(th.Bright)
	}
	tl := wrapText(c.Title, tw)
	if len(tl) > 3 {
		tl = append(tl[:2], truncate(tl[2]+" "+tl[3], tw))
	}
	for _, l := range tl {
		lines = append(lines, ts.Render(l))
	}
	var meta []string
	if c.Due != "" {
		meta = append(meta, dueStyled(c.Due, col == len(m.columns)-1))
	}
	if done, total := checkProgress(c.Description); total > 0 {
		meta = append(meta, fmt.Sprintf("☑ %d/%d", done, total))
	}
	if c.Tags != "" {
		meta = append(meta, "#"+strings.ReplaceAll(c.Tags, ",", " #"))
	}
	if len(meta) > 0 {
		for _, l := range wrapText(strings.Join(meta, " · "), tw) {
			lines = append(lines, dimStyle.Render(l))
		}
	}

	var barColor color.Color = th.Rule
	if c.Priority > 0 {
		barColor = th.Prio[c.Priority]
	}
	glyph := "┃"
	bg := lipgloss.NewStyle()
	if selected {
		glyph = "▌"
		bg = bg.Background(th.SelBg)
	}
	bar := lipgloss.NewStyle().Foreground(barColor).Render(glyph)
	for i, l := range lines {
		lines[i] = bar + bg.Render(" ") + bg.Width(tw).Render(l)
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:max(1, w-1)]) + "…"
}

// wrapText word-wraps s to width w (lipgloss pads lines, so trim the right edge).
func wrapText(s string, w int) []string {
	lines := strings.Split(lipgloss.NewStyle().Width(max(1, w)).Render(s), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func (m *Model) detailInnerWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return max(20, w-4) // 2 border + 2 padding
}

func (m *Model) chips(c store.Card, w int) string {
	var chips []string
	if c.ID != 0 {
		chips = append(chips, dimStyle.Render(fmt.Sprintf("#%d", c.ID))) // the id other items can link to
	}
	if c.Project != "" {
		chips = append(chips, chipStyle.Render("▸ "+c.Project))
	}
	if c.Priority > 0 {
		chips = append(chips, chipStyle.Foreground(th.Prio[c.Priority]).Render("● "+prioLabels[c.Priority]))
	}
	if c.Due != "" {
		st := chipStyle
		if overdue(c.Due) {
			st = st.Foreground(th.Err)
		}
		label, _ := shortDue(c.Due)
		if label != c.Due {
			label += " · " + c.Due
		}
		chips = append(chips, st.Render("⏰ "+label))
	}
	for _, tag := range strings.Split(c.Tags, ",") {
		if tag != "" {
			chips = append(chips, chipStyle.Foreground(th.Purple).Render("#"+tag))
		}
	}
	// join chips with spaces, breaking to a new row when the width runs out
	var rows []string
	row, rowW := "", 0
	for _, ch := range chips {
		cw := lipgloss.Width(ch)
		if rowW > 0 && rowW+1+cw > w {
			rows = append(rows, row)
			row, rowW = "", 0
		}
		if rowW > 0 {
			row += " "
			rowW++
		}
		row += ch
		rowW += cw
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m *Model) viewDetail() string {
	c := m.current()
	if c == nil {
		return ""
	}
	inner := m.detailInnerWidth()
	rule := ruleStyle.Render(strings.Repeat("─", inner))

	var b strings.Builder
	titleLines := wrapText(c.Title, inner)
	b.WriteString(titleStyle.Render(strings.Join(titleLines, "\n")) + "\n")
	chipRows := 0
	if ch := m.chips(*c, inner); ch != "" {
		b.WriteString(ch + "\n")
		chipRows = strings.Count(ch, "\n") + 1
	}
	b.WriteString(rule + "\n")

	if m.editing {
		b.WriteString(m.area.View() + "\n" + rule + "\n")
		b.WriteString(dimStyle.Render("enter new line · shift+enter new checkbox · esc save"))
		return panelStyle.Width(inner + 4).Render(b.String())
	}

	// body: soft-wrapped, checkbox lines get a hanging indent (nested ones are indented); selected line gets a left bar
	const gutter = 2 // "▌ "
	y := m.bodyY + 1 + len(titleLines) + chipRows + 1
	for i, l := range strings.Split(c.Description, "\n") {
		ind, kind, text := parseLine(l)
		prefix := strings.Repeat(" ", ind)
		switch kind {
		case lineOpen:
			prefix += boxOpen + " "
		case lineDone:
			prefix += boxDone + " "
		default:
			text = l // plain text keeps its own spacing
			prefix = ""
		}
		pw := lipgloss.Width(prefix)
		segs := wrapText(text, max(4, inner-gutter-pw))
		m.hits = append(m.hits, hit{0, max(m.width, 80), y, y + len(segs) - 1, hitLine, 0, i})
		y += len(segs)
		for j, seg := range segs {
			bar := "  "
			if i == m.lineIdx {
				bar = barStyle.Render("▌ ")
			}
			lead := prefix
			if j > 0 {
				lead = strings.Repeat(" ", pw)
			}
			b.WriteString(bar + lead + seg + "\n")
		}
	}
	b.WriteString(m.detailExtras(c, inner))
	return panelStyle.Width(inner + 4).Render(strings.TrimRight(b.String(), "\n"))
}

// fmtSpan shortens a duration for the history ("12m", "5h", "3d").
func fmtSpan(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(1, int(d.Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// detailExtras renders what sits under the card text: attachments, mentions and the history.
func (m *Model) detailExtras(c *store.Card, inner int) string {
	var b strings.Builder
	section := func(title string) {
		b.WriteString("\n" + ruleStyle.Render("── "+title+" "+strings.Repeat("─", max(0, inner-len(title)-4))) + "\n")
	}
	if len(m.detAtt) > 0 {
		section("Attachments (o opens)")
		for _, a := range m.detAtt {
			b.WriteString("  📎 " + a.Label + dimStyle.Render("  "+a.Target) + "\n")
		}
	}
	if len(m.detMention) > 0 {
		section("Mentioned in")
		for _, x := range m.detMention {
			b.WriteString("  " + dimStyle.Render(truncate(x, inner-4)) + "\n")
		}
	}
	if n := len(m.detHist); n > 0 {
		section("History")
		from := max(0, n-6)
		if from > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  … %d earlier event(s)", from)) + "\n")
		}
		for i := from; i < n; i++ {
			ev := m.detHist[i]
			line := ev.At.In(time.Local).Format("2006-01-02 15:04") + "  "
			switch ev.Kind {
			case store.EvCreated:
				line += "created in " + ev.To
			case store.EvMoved, store.EvDone:
				line += ev.From + " → " + ev.To
			case store.EvArchived:
				line += "archived"
			case store.EvUnarchived:
				line += "restored from the archive"
			case store.EvRestored:
				line += "restored (undo)"
			default:
				line += ev.Kind
			}
			if i > 0 && (ev.Kind == store.EvMoved || ev.Kind == store.EvDone) {
				line += "  · " + fmtSpan(ev.At.Sub(m.detHist[i-1].At)) + " in " + ev.From
			}
			b.WriteString("  " + dimStyle.Render(truncate(line, inner-4)) + "\n")
		}
	}
	return b.String()
}
