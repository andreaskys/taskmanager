package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

// Sticky notes (space r): a window of colored notes kept outside the boards, to remember things.
// A note is free text (checkboxes and sub-tasks work like everywhere), with a color, an optional
// pin and an optional reminder date; a note can be turned into a task.

const (
	noteMinW   = 30 // minimum width of one sticky
	noteHeight = 10 // height of one sticky, border included
)

type noteEditor struct {
	title textinput.Model
	tags  textinput.Model
	area  textarea.Model
	focus int        // 0 title, 1 tags, 2 body
	orig  store.Note // zero ID = new note
}

// setFocus moves the cursor between title, tags and body.
func (e *noteEditor) setFocus(f int) tea.Cmd {
	e.focus = (f + 3) % 3
	e.title.Blur()
	e.tags.Blur()
	e.area.Blur()
	switch e.focus {
	case 0:
		return e.title.Focus()
	case 1:
		return e.tags.Focus()
	}
	return e.area.Focus()
}

func (m *Model) loadNotes() {
	notes, err := m.st.Notes()
	m.fail(err)
	m.notes = notes
	m.noteAtt = m.st.AttachmentCounts("note")
	m.noteSel = max(0, min(m.noteSel, len(notes)-1))
}

func (m *Model) openNotes() {
	m.loadNotes()
	m.noteSel = 0
	m.mode = modeNotes
}

func (m *Model) selectNote(id int64) {
	for i, n := range m.notes {
		if n.ID == id {
			m.noteSel = i
		}
	}
}

func (m *Model) pushNoteUndo(f func() error) {
	m.noteUndo = append(m.noteUndo, f)
	if len(m.noteUndo) > 50 {
		m.noteUndo = m.noteUndo[1:]
	}
}

// editNote saves a changed note and records how to revert it.
func (m *Model) editNote(old, n store.Note) {
	if err := m.st.UpdateNote(n); err != nil {
		m.fail(err)
		return
	}
	m.pushNoteUndo(func() error { return m.st.ApplyNote(old) })
	m.loadNotes()
	m.selectNote(n.ID)
}

func (m *Model) updateNotes(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	prev := m.lastKey
	m.lastKey = key
	cols := max(1, m.noteCols)
	var cur *store.Note
	if m.noteSel < len(m.notes) {
		cur = &m.notes[m.noteSel]
	}
	switch key {
	case "esc", "q", "r":
		m.mode = modeBoard
		m.fail(m.reload())
	case "h", "left":
		m.noteSel = max(0, m.noteSel-1)
	case "l", "right":
		m.noteSel = min(len(m.notes)-1, m.noteSel+1)
	case "j", "down":
		if m.noteSel+cols < len(m.notes) {
			m.noteSel += cols
		}
	case "k", "up":
		if m.noteSel-cols >= 0 {
			m.noteSel -= cols
		}
	case "n":
		return m, m.openNoteEditor(store.Note{})
	case "enter", "e":
		if cur != nil {
			return m, m.openNoteEditor(*cur)
		}
	case "d", "x": // dd or x deletes (u brings it back)
		if cur != nil && (key == "x" || prev == "d") {
			m.lastKey = ""
			old := *cur
			if err := m.st.DeleteNote(old.ID); err != nil {
				m.fail(err)
				break
			}
			m.pushNoteUndo(func() error { return m.st.ApplyNote(old) })
			m.loadNotes()
		}
	case "c": // next color
		if cur != nil {
			n := *cur
			n.Color = wrapIdx(n.Color+1, len(th.Projects))
			m.editNote(*cur, n)
		}
	case "p": // pin / unpin
		if cur != nil {
			n := *cur
			n.Pinned = !n.Pinned
			m.editNote(*cur, n)
		}
	case "D": // reminder date
		if cur == nil {
			break
		}
		n, old := *cur, *cur
		return m, m.ask("Remind me on", n.Due, func(s string) error {
			s = strings.TrimSpace(s)
			if s != "" {
				if _, err := time.Parse(isoDate, s); err != nil {
					return fmt.Errorf("invalid date, use YYYY-MM-DD")
				}
			}
			n.Due = s
			m.editNote(old, n)
			return nil
		}, withHint("YYYY-MM-DD · empty clears it · the header shows a 🔔 when a reminder is due"))
	case "a": // attach a file or URL
		if cur != nil {
			return m, m.attach("note", cur.ID)
		}
	case "o": // open the links, URLs and attachments of the note
		if cur != nil {
			m.openLinks(cur.Title+"\n"+cur.Body, "note", cur.ID)
		}
	case "t": // turn the note into a task
		if cur != nil {
			return m, m.openSaveNotes(cur.Body, modeNotes, cur.ID)
		}
	case "u", "ctrl+z":
		if len(m.noteUndo) == 0 {
			m.notice = "nothing to undo"
			break
		}
		f := m.noteUndo[len(m.noteUndo)-1]
		m.noteUndo = m.noteUndo[:len(m.noteUndo)-1]
		m.fail(f())
		m.loadNotes()
		m.notice = "undo"
	}
	return m, nil
}

func (m *Model) openNoteEditor(n store.Note) tea.Cmd {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.CharLimit = 0
	a.Placeholder = "write something to remember…  (shift+enter adds a checkbox)"
	a.SetValue(n.Body)
	a.MoveToEnd()
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "title (optional, searchable)"
	ti.SetValue(n.Title)
	ti.CursorEnd()
	tg := textinput.New()
	tg.Prompt = ""
	tg.Placeholder = "tags, comma separated (searchable with #tag)"
	tg.SetValue(strings.ReplaceAll(n.Tags, ",", ", "))
	tg.CursorEnd()
	m.noteEd = &noteEditor{title: ti, tags: tg, area: a, orig: n}
	m.mode = modeNoteEdit
	return m.noteEd.setFocus(2) // the cursor starts in the text (ctrl+t goes to title/tags); focus the stored copy, not the local one
}

func (m *Model) updateNoteEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	e := m.noteEd
	key := msg.String()
	switch key {
	case "esc", "ctrl+s": // both save: a note is never lost by leaving the editor
		body := strings.TrimRight(e.area.Value(), " \n")
		title := strings.TrimSpace(e.title.Value())
		tags := store.NormTags(e.tags.Value())
		m.noteEd = nil
		m.mode = modeNotes
		switch {
		case strings.TrimSpace(body) == "" && title == "": // nothing written: keep the old note (or create nothing)
		case e.orig.ID == 0:
			n, err := m.st.AddNoteFull(store.Note{Title: title, Tags: tags, Body: body, Color: len(m.notes) % len(th.Projects)}) // colors rotate
			if err != nil {
				m.fail(err)
				break
			}
			m.pushNoteUndo(func() error { return m.st.DeleteNote(n.ID) })
			m.loadNotes()
			m.selectNote(n.ID)
		case body != e.orig.Body || title != e.orig.Title || tags != e.orig.Tags:
			n := e.orig
			n.Body, n.Title, n.Tags = body, title, tags
			m.editNote(e.orig, n)
		}
		return m, nil
	case "ctrl+t": // jump to the title from anywhere
		return m, e.setFocus(0)
	}
	if e.focus < 2 { // single-line fields: enter/tab/down go on, shift+tab/up go back
		switch key {
		case "enter", "tab", "down":
			return m, e.setFocus(e.focus + 1)
		case "shift+tab", "up":
			return m, e.setFocus(e.focus - 1)
		}
		var cmd tea.Cmd
		if e.focus == 0 {
			e.title, cmd = e.title.Update(msg)
		} else {
			e.tags, cmd = e.tags.Update(msg)
		}
		return m, cmd
	}
	if handleEditorKey(&e.area, key, true) {
		return m, nil
	}
	var cmd tea.Cmd
	e.area, cmd = e.area.Update(msg)
	return m, cmd
}

// notesOpen reports whether the notes window is on screen (alone or under an input window).
func (m *Model) notesOpen() bool { return m.windowOpen(modeNotes) }

// stickyLines renders the body of a note into at most `rows` lines of width w.
func stickyLines(body string, w, rows int, selected bool) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		ind, kind, text := parseLine(l)
		prefix := strings.Repeat(" ", min(ind, 6))
		switch kind {
		case lineOpen:
			prefix += boxOpen + " "
		case lineDone:
			prefix += boxDone + " "
		default:
			prefix, text = "", l
		}
		pw := lipgloss.Width(prefix)
		for j, seg := range wrapText(text, max(4, w-pw)) {
			lead := prefix
			if j > 0 {
				lead = strings.Repeat(" ", pw)
			}
			out = append(out, lead+seg)
		}
	}
	if len(out) > rows {
		out = append(out[:rows-1], dimStyle.Render("…"))
	}
	st := lipgloss.NewStyle()
	if selected {
		st = st.Bold(true)
	}
	for i, l := range out {
		out[i] = st.Render(" " + l)
	}
	return out
}

func (m *Model) viewNotes(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*92/100, 64, 170)
	H := clamp(sh*84/100, 14, 52)
	cols := max(1, (W-3)/(noteMinW+1))
	cw := (W - 4 - (cols - 1)) / cols
	m.noteCols = cols
	visibleRows := max(1, (H-2-3)/noteHeight)
	off := 0
	if r := m.noteSel / cols; r >= visibleRows {
		off = r - visibleRows + 1
	}

	pinned, due := 0, 0
	for _, n := range m.notes {
		if n.Pinned {
			pinned++
		}
		if n.Due != "" && n.Due <= todayStr() {
			due++
		}
	}
	lines := []string{" " + dimStyle.Render(fmt.Sprintf("%d notes · %d pinned · %d reminder(s) due", len(m.notes), pinned, due)), ""}

	if len(m.notes) == 0 {
		lines = append(lines, " "+dimStyle.Render("No notes yet. Press ")+keyStyle.Render("n")+dimStyle.Render(" to write one."))
	}
	for r := off; r < off+visibleRows && r*cols < len(m.notes); r++ {
		var boxes [][]string
		for c := 0; c < cols && r*cols+c < len(m.notes); c++ {
			i := r*cols + c
			n := m.notes[i]
			title := n.Title
			if n.Pinned {
				if title != "" {
					title += " "
				}
				title += "📌"
			}
			if n.Due != "" {
				if title != "" {
					title += " "
				}
				title += "🔔 " + n.Due
			}
			if c := m.noteAtt[n.ID]; c > 0 {
				if title != "" {
					title += " "
				}
				title += fmt.Sprintf("📎%d", c)
			}
			border := th.Projects[wrapIdx(n.Color, len(th.Projects))]
			if n.Due != "" && n.Due <= todayStr() {
				border = th.Err
			}
			var body []string
			if n.Tags != "" {
				body = append(body, " "+dimStyle.Render(fit("#"+strings.ReplaceAll(n.Tags, ",", " #"), cw-3)))
			}
			body = append(body, stickyLines(n.Body, cw-3, noteHeight-2-len(body), i == m.noteSel)...)
			if i == m.noteSel {
				for j := range body {
					body[j] = lipgloss.NewStyle().Background(th.SelBg).Render(fit(body[j], cw-2))
				}
				border = th.Bright
			}
			boxes = append(boxes, drawBox(title, body, cw, noteHeight, border))
		}
		for row := 0; row < noteHeight; row++ {
			var parts []string
			for _, b := range boxes {
				parts = append(parts, b[row])
			}
			lines = append(lines, " "+strings.Join(parts, " "))
		}
	}
	return m.center(base, drawBox("Notes", lines, W, H, th.Accent))
}

func (m *Model) viewNoteEdit(base string) string {
	e := m.noteEd
	sw, sh := m.screenSize()
	W := clamp(sw*70/100, 56, 120)
	H := clamp(sh*70/100, 12, 34)
	e.area.SetWidth(W - 4)
	e.area.SetHeight(H - 2 - 3)
	e.title.SetWidth(W - 4 - 8)
	e.tags.SetWidth(W - 4 - 8)
	label := func(name string, f int) string {
		if e.focus == f {
			return keyStyle.Render(fmt.Sprintf("%-6s", name))
		}
		return dimStyle.Render(fmt.Sprintf("%-6s", name))
	}
	lines := []string{
		" " + label("Title", 0) + " " + e.title.View(),
		" " + label("Tags", 1) + " " + e.tags.View(),
		ruleStyleLine(W - 2),
	}
	for _, l := range strings.Split(e.area.View(), "\n") {
		lines = append(lines, " "+l)
	}
	title := "New note"
	if e.orig.ID != 0 {
		title = "Edit note"
	}
	return m.center(base, drawBox(title, lines, W, H, th.Accent))
}

// wrapIdx maps any integer (even a negative one coming from outside) into 0..n-1.
func wrapIdx(i, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i % n) + n) % n
}
