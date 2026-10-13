package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const (
	settingScratch = "scratch"
	autosaveEvery  = time.Second
)

// scratch is the floating notes window for meetings: free text with checkboxes, kept as a draft until saved as a task.
type scratch struct {
	area      textarea.Model
	lastSave  time.Time
	confirmCl bool // ctrl+x pressed once; a second press clears the draft
}

func (m *Model) openScratch() tea.Cmd {
	a := textarea.New()
	a.ShowLineNumbers = false
	a.Prompt = ""
	a.CharLimit = 0
	a.Placeholder = "meeting notes…  (shift+enter adds a checkbox)"
	a.SetValue(m.draft)
	a.MoveToEnd()
	m.scratch = &scratch{area: a, lastSave: time.Now()}
	m.mode = modeScratch
	return m.scratch.area.Focus() // focus the stored copy, not the local one
}

// saveDraft persists the draft so a crash or an early esc never loses meeting notes.
func (m *Model) saveDraft(text string) {
	m.draft = strings.TrimRight(text, " \n")
	m.fail(m.st.SetSetting(settingScratch, m.draft))
}

// autosave stores the draft when the text changed, at most once per second.
func (m *Model) autosave(before string) {
	s := m.scratch
	if v := s.area.Value(); v != before && time.Since(s.lastSave) > autosaveEvery {
		m.saveDraft(v)
		s.lastSave = time.Now()
	}
}

func (m *Model) closeScratch() {
	if m.scratch != nil {
		m.saveDraft(m.scratch.area.Value())
	}
	m.scratch = nil
	m.mode = modeBoard
}

func (m *Model) updateScratch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratch
	key := msg.String()
	if key != "ctrl+x" {
		s.confirmCl = false
	}
	switch key {
	case "esc":
		m.closeScratch()
		if m.draft != "" {
			m.notice = "draft kept — space enter to continue"
		}
		return m, nil
	case "ctrl+s":
		text := strings.TrimSpace(s.area.Value())
		if text == "" {
			m.notice = "nothing to save yet"
			return m, nil
		}
		m.saveDraft(text)
		return m, m.openSaveNotes(text, modeScratch, 0)
	case "ctrl+x":
		if !s.confirmCl {
			s.confirmCl = true
			m.notice = "ctrl+x again to discard the draft"
			return m, nil
		}
		s.area.SetValue("")
		m.saveDraft("")
		m.notice = "draft discarded"
		return m, nil
	}
	before := s.area.Value()
	if handleEditorKey(&s.area, key, true) {
		m.autosave(before)
		return m, nil
	}
	var cmd tea.Cmd
	s.area, cmd = s.area.Update(msg)
	m.autosave(before)
	return m, cmd
}

// firstLine returns the index and cleaned text of the first non-empty line (checkbox markers removed).
func firstLine(text string) (int, string) {
	for i, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(t, "[ ]"), "[x]"), "[X]"))
		if t != "" {
			return i, t
		}
	}
	return 0, ""
}

// openSaveNotes opens the "save as task" window; the first line of the notes becomes the title.
// back is the mode esc returns to; noteID (if not 0) is a sticky note that is deleted once it became a task.
func (m *Model) openSaveNotes(text string, back mode, noteID int64) tea.Cmd {
	cmd := m.openNewCard()
	f := m.form
	if f == nil {
		return nil
	}
	f.colID = m.columns[0].ID // new work from meetings lands in the first column
	f.where = m.boards[m.boardIdx].Name + " › " + m.columns[0].Name
	f.title = "Save notes as task"
	f.body = text
	f.back = back
	f.noteID = noteID
	f.firstIdx, f.firstText = firstLine(text)
	f.desc.SetValue(f.firstText)
	f.desc.CursorEnd()
	return cmd
}

// notesBody returns the description for the saved task: the notes minus the line used as title.
func (f *newCardForm) notesBody(title string) string {
	if f.body == "" {
		return ""
	}
	if strings.TrimSpace(title) != f.firstText {
		return f.body // title was rewritten: keep every line of the notes
	}
	lines := strings.Split(f.body, "\n")
	if f.firstIdx < len(lines) {
		lines = append(lines[:f.firstIdx], lines[f.firstIdx+1:]...)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (m *Model) viewScratch(base string) string {
	s := m.scratch
	sw, sh := m.screenSize()
	W := clamp(sw*70/100, 60, 120)
	H := clamp(sh*72/100, 12, 34)
	s.area.SetWidth(W - 4)
	s.area.SetHeight(H - 2 - 3)
	var lines []string
	for _, l := range strings.Split(s.area.View(), "\n") {
		lines = append(lines, " "+l)
	}
	lines = append(lines, ruleStyleLine(W-2))
	lines = append(lines, " "+dimStyle.Render("enter new line · shift+enter checkbox · ")+keyStyle.Render("ctrl+s")+dimStyle.Render(" save as task"))
	lines = append(lines, " "+dimStyle.Render("esc close (draft is kept) · ctrl+x discard"))
	return m.center(base, drawBox("Scratchpad · meeting notes", lines, W, H, th.Accent))
}

func ruleStyleLine(w int) string { return ruleStyle.Render(strings.Repeat("─", w)) }
