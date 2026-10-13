package tui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/andreas/kbn/internal/store"
)

// Fields of the new-card window, in tab order.
const (
	fieldProject = iota
	fieldDesc
	fieldNotes
)

// newCardForm is the floating "new card" window: one bordered box per field.
// Project and Description are single lines; Notes is free text where "[ ] " lines are checkboxes
// (indent them for sub-tasks). ctrl+t fills the notes, tags and priority from a template.
type newCardForm struct {
	project, desc textinput.Model
	notes         textarea.Model
	focus         int
	colID         int64
	where         string // "Board › Column", shown in the window title
	err           string

	projects  []string   // known projects, for completion
	templates []Template // available templates
	tpl       int        // selected template, -1 = none
	tplNotes  string     // notes last inserted by a template (to know whether the user edited them)
	tags      string
	priority  int

	// "save notes as task" variant (opened from the scratchpad): the notes already exist, so no Notes field
	title     string // window title; "" = "New card"
	body      string // the notes; becomes the card description
	firstIdx  int
	firstText string
	back      mode  // where esc returns to (modeBoard by default)
	noteID    int64 // sticky note to delete after it became a task
}

func (f *newCardForm) fields() int {
	if f.body != "" {
		return 2
	}
	return 3
}

func (m *Model) openNewCard() tea.Cmd {
	if m.col >= len(m.columns) {
		return nil
	}
	mk := func(ph string) textinput.Model {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = ph
		in.CharLimit = 200
		return in
	}
	n := textarea.New()
	n.ShowLineNumbers = false
	n.Prompt = ""
	n.CharLimit = 0
	n.Placeholder = "to-do and notes…  (shift+enter adds a checkbox)"
	if m.templates == nil {
		m.templates = loadTemplates(m.st.Path())
	}
	f := &newCardForm{
		project:   mk("client or system"),
		desc:      mk("what needs to be done"),
		notes:     n,
		colID:     m.columns[m.col].ID,
		where:     m.boards[m.boardIdx].Name + " › " + m.columns[m.col].Name,
		projects:  m.projects,
		templates: m.templates,
		tpl:       -1,
	}
	m.form = f
	m.mode = modeNewCard
	return f.project.Focus()
}

func (f *newCardForm) setFocus(i int) tea.Cmd {
	f.focus = (i + f.fields()) % f.fields()
	f.project.Blur()
	f.desc.Blur()
	f.notes.Blur()
	switch f.focus {
	case fieldProject:
		return f.project.Focus()
	case fieldDesc:
		return f.desc.Focus()
	}
	return f.notes.Focus()
}

// nextTemplate cycles none → template 1 → … → none and fills the notes, tags and priority.
func (f *newCardForm) nextTemplate() {
	if len(f.templates) == 0 {
		return
	}
	f.tpl++
	if f.tpl >= len(f.templates) {
		f.tpl = -1
	}
	untouched := strings.TrimSpace(f.notes.Value()) == "" || f.notes.Value() == f.tplNotes
	if f.tpl < 0 {
		f.tags, f.priority = "", 0
		if untouched {
			f.notes.SetValue("")
			f.tplNotes = ""
		}
		return
	}
	t := f.templates[f.tpl]
	f.tags, f.priority = t.Tags, t.Priority
	if untouched {
		f.notes.SetValue(t.Notes)
		f.tplNotes = t.Notes
	}
}

func (m *Model) saveNewCard() (tea.Model, tea.Cmd) {
	f := m.form
	if strings.TrimSpace(f.desc.Value()) == "" {
		f.err = "description is empty"
		return m, f.setFocus(fieldDesc)
	}
	body := f.notesBody(f.desc.Value())
	if f.body == "" {
		body = strings.TrimSpace(f.notes.Value())
	}
	if err := m.wipBlocked(store.Card{}, f.colID); err != nil {
		f.err = err.Error()
		return m, nil
	}
	card := store.Card{ColumnID: f.colID, Project: f.project.Value(), Title: f.desc.Value(), Description: body,
		Tags: f.tags, Priority: f.priority}
	if err := m.addCard(card); err != nil {
		f.err = err.Error()
		return m, nil
	}
	switch {
	case f.back == modeScratch: // notes are now a task: clear the draft and close the window
		m.scratch = nil
		m.saveDraft("")
		m.notice = "notes saved as a task"
	case f.noteID != 0: // a sticky note became a task: it is replaced by the card
		m.fail(m.st.DeleteNote(f.noteID))
		m.notice = "note turned into a task"
	}
	m.form = nil
	m.mode = modeBoard
	m.fail(m.reload())
	return m, nil
}

func (m *Model) updateNewCard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.form
	key := msg.String()
	switch key {
	case "esc", "ctrl+c":
		m.form = nil
		m.mode = f.back
		if f.back == modeScratch {
			return m, m.scratch.area.Focus()
		}
		return m, nil
	case "ctrl+s": // save from any field
		return m.saveNewCard()
	case "ctrl+t":
		if f.fields() == 3 {
			f.nextTemplate()
		}
		return m, nil
	case "tab":
		switch f.focus {
		case fieldProject: // tab completes a known project first, then moves on
			if v, ok := completeWith(f.project.Value(), f.projects, false); ok {
				f.project.SetValue(v)
				f.project.CursorEnd()
				return m, nil
			}
		case fieldNotes: // tab = indent the line (sub-task)
			indentLine(&f.notes, 1)
			return m, nil
		}
		return m, f.setFocus(f.focus + 1)
	case "shift+tab":
		if f.focus == fieldNotes && indentLine(&f.notes, -1) {
			return m, nil
		}
		return m, f.setFocus(f.focus - 1)
	case "down":
		if f.focus != fieldNotes {
			return m, f.setFocus(f.focus + 1)
		}
	case "up":
		if f.focus != fieldNotes {
			return m, f.setFocus(f.focus - 1)
		}
	case "enter":
		switch {
		case f.focus == fieldProject:
			return m, f.setFocus(fieldDesc)
		case f.focus == fieldDesc && f.fields() == 3:
			return m, f.setFocus(fieldNotes)
		case f.focus == fieldDesc: // notes already exist: description is the last field
			return m.saveNewCard()
		}
	}
	if f.focus == fieldNotes && handleEditorKey(&f.notes, key, false) {
		return m, nil
	}
	var cmd tea.Cmd
	switch f.focus {
	case fieldProject:
		f.project, cmd = f.project.Update(msg)
	case fieldDesc:
		f.desc, cmd = f.desc.Update(msg)
	default:
		f.notes, cmd = f.notes.Update(msg)
	}
	f.err = ""
	return m, cmd
}

func (m *Model) viewNewCard(base string) string {
	f := m.form
	sw, sh := m.screenSize()
	W := clamp(sw*60/100, 52, 90)
	iw := W - 4 // inside the outer border and its 1-cell padding
	f.project.SetWidth(max(5, iw-6))
	f.desc.SetWidth(max(5, iw-6))

	borderFor := func(i int) color.Color {
		if f.focus == i {
			return th.Accent
		}
		return th.Rule
	}
	var lines []string
	add := func(ls []string) {
		for _, l := range ls {
			lines = append(lines, " "+l)
		}
	}
	if f.fields() == 3 {
		name := dimStyle.Render("none")
		if f.tpl >= 0 {
			t := f.templates[f.tpl]
			name = keyStyle.Render(t.Name)
			if t.Tags != "" {
				name += dimStyle.Render("  #" + strings.ReplaceAll(t.Tags, ",", " #"))
			}
			if t.Priority > 0 {
				name += dimStyle.Render("  ● " + prioLabels[t.Priority])
			}
		}
		lines = append(lines, " "+dimStyle.Render("template: ")+name+dimStyle.Render("   (ctrl+t to change)"))
	}
	add(drawBox("Project", []string{" " + f.project.View()}, iw, 3, borderFor(fieldProject)))
	if s := suggestFor(f.project.Value(), f.projects, false); f.focus == fieldProject && len(s) > 0 {
		lines = append(lines, " "+dimStyle.Render("tab → ")+keyStyle.Render(s[0])+dimStyle.Render("   also: "+strings.Join(s[1:], " · ")))
	}
	add(drawBox("Description", []string{" " + f.desc.View()}, iw, 3, borderFor(fieldDesc)))
	if f.fields() == 3 {
		// notes get the remaining height, within sane bounds
		rows := clamp(sh-26, 3, 8)
		f.notes.SetWidth(max(10, iw-4))
		f.notes.SetHeight(rows)
		var nl []string
		for _, l := range strings.Split(f.notes.View(), "\n") {
			nl = append(nl, " "+l)
		}
		add(drawBox("Notes · to-do", nl, iw, rows+2, borderFor(fieldNotes)))
	}
	lines = append(lines, "")
	switch {
	case f.err != "":
		lines = append(lines, " "+errStyle.Render(f.err))
	case f.fields() == 3:
		lines = append(lines, " "+dimStyle.Render("enter next · shift+enter checkbox · tab indent (Notes)"))
		lines = append(lines, " "+keyStyle.Render("ctrl+s")+dimStyle.Render(" save · ")+keyStyle.Render("ctrl+t")+dimStyle.Render(" template · esc cancel"))
	default:
		lines = append(lines, " "+dimStyle.Render("enter save · tab switch field · esc back to the notes"))
	}
	title := f.title
	if title == "" {
		title = "New card"
	}
	return m.center(base, drawBox(title+" · "+f.where, lines, W, len(lines)+2, th.Accent))
}
