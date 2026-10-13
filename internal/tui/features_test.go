package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func shiftEnter(m *Model) { m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}) }
func tab(m *Model)        { m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }
func shiftTab(m *Model)   { m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) }
func altX(m *Model)       { m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt}) }
func ctrl(m *Model, r rune) {
	m.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
}

// plainFrame renders the screen without colors, so text can be matched across styled segments.
func plainFrame(m *Model) string { return ansi.Strip(m.render()) }

// --- undo / redo, persisted ---

func TestRedoAndPersistentUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, _ := store.Open(path)
	m, _ := New(st)
	press(m, "n", "Acme", "enter", "task one")
	ctrlS(m)
	press(m, "p", "L") // priority up, then move to Doing
	if m.cards[1][0].Priority != 1 {
		t.Fatal("setup failed")
	}
	st.Close()

	// "restart" the app on the same database: the history is still there
	st2, _ := store.Open(path)
	defer st2.Close()
	m2, _ := New(st2)
	press(m2, "u")
	if len(m2.cards[0]) != 1 || m2.cards[0][0].Priority != 1 {
		t.Fatalf("undo after restart should undo the move: %+v %+v", m2.cards[0], m2.cards[1])
	}
	press(m2, "u")
	if m2.cards[0][0].Priority != 0 {
		t.Fatal("second undo should revert the priority")
	}
	ctrl(m2, 'r') // redo
	if m2.cards[0][0].Priority != 1 {
		t.Fatalf("ctrl+r should redo the priority change: %+v", m2.cards[0])
	}
	press(m2, " ", "U") // leader redo: moves it to Doing again
	if len(m2.cards[1]) != 1 {
		t.Fatalf("space U should redo the move: %+v", m2.cards)
	}
	press(m2, "u", "u")
	press(m2, "p") // a new change clears the redo stack
	ctrl(m2, 'r')
	if m2.notice != "nothing to redo" {
		t.Fatalf("redo after a new change should be empty, notice=%q", m2.notice)
	}
}

func TestUndoRestoresDeletedCardWithAllFields(t *testing.T) {
	m := newModelWithCard(t)
	c := m.cards[0][0]
	c.Description, c.Tags, c.Due, c.Focus = "[ ] a\n  [x] b", "go,api", "2030-01-01", true
	m.updateCard(c, "fill")
	m.reload()
	press(m, "d", "d", "u")
	got := m.cards[0]
	if len(got) != 1 || got[0].Description != c.Description || got[0].Tags != "go,api" || got[0].Due != "2030-01-01" || !got[0].Focus || got[0].ID != c.ID {
		t.Fatalf("undo of delete lost data: %+v", got)
	}
}

// --- columns, WIP ---

func TestColumnsWindowAndWIP(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 34
	press(m, " ", "m")
	if m.mode != modeColumns {
		t.Fatalf("space m should open the columns window, mode=%v", m.mode)
	}
	if out := m.render(); !strings.Contains(out, "Columns · Trabalho") || !strings.Contains(out, "Doing") {
		t.Fatalf("columns window not drawn:\n%s", out)
	}
	press(m, "j", "w") // select Doing, set the WIP limit
	if m.mode != modeInput {
		t.Fatalf("w should open the limit input, mode=%v", m.mode)
	}
	if out := m.render(); !strings.Contains(out, "WIP limit") {
		t.Fatal("the input window should float over the columns window")
	}
	press(m, "1", "enter")
	if m.columns[1].WIPLimit != 1 || m.mode != modeColumns {
		t.Fatalf("limit not saved or wrong mode: limit=%d mode=%v", m.columns[1].WIPLimit, m.mode)
	}
	press(m, "n", "Review", "enter") // new column goes before Done
	names := ""
	for _, c := range m.columns {
		names += c.Name + ","
	}
	if names != "To do,Doing,Testing,Review,Done," {
		t.Fatalf("columns = %s", names)
	}
	press(m, "esc")

	// Doing holds 1: the second card cannot enter
	m.col = 0
	addCard(m, "B", "second")
	m.col, m.row = 0, 0
	press(m, "L")
	if len(m.cards[1]) != 1 {
		t.Fatal("first move into Doing should work")
	}
	m.col, m.row = 0, 0
	press(m, "L")
	if len(m.cards[1]) != 1 || len(m.cards[0]) != 1 || !strings.Contains(m.err, "Doing is full") {
		t.Fatalf("a full WIP column must refuse the card: doing=%d todo=%d err=%q", len(m.cards[1]), len(m.cards[0]), m.err)
	}
	m.width, m.height = 100, 24
	if out := m.render(); !strings.Contains(out, "1/1") {
		t.Fatalf("column header should show the limit:\n%s", out)
	}
	// new cards are blocked too
	m.col = 1
	press(m, "n", "P", "enter", "third")
	ctrlS(m)
	if m.mode != modeNewCard || !strings.Contains(m.form.err, "full") {
		t.Fatalf("creating in a full column should be refused, mode=%v err=%q", m.mode, m.form.err)
	}
}

func TestColumnsWindowRenameReorderDelete(t *testing.T) {
	m := newModelWithCard(t)
	press(m, " ", "m", "j", "r")
	m.input.SetValue("In progress")
	key(m, tea.KeyEnter)
	if m.columns[1].Name != "In progress" {
		t.Fatalf("rename failed: %+v", m.columns)
	}
	press(m, "J") // move down: swaps with Testing
	if m.columns[2].Name != "In progress" || m.columns[1].Name != "Testing" || m.colSel != 2 {
		t.Fatalf("reorder failed: %+v sel=%d", m.columns, m.colSel)
	}
	press(m, "d")
	if m.mode != modeConfirm {
		t.Fatal("delete should ask for confirmation")
	}
	press(m, "y")
	if len(m.columns) != 3 || m.mode != modeColumns {
		t.Fatalf("delete failed: %d columns, mode=%v", len(m.columns), m.mode)
	}
}

// --- archive ---

func TestArchiveAndSearchRestore(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "L", "L", "L") // To do -> Done
	if len(m.cards[3]) != 1 {
		t.Fatal("card should be in Done")
	}
	press(m, " ", "a")
	if len(m.cards[3]) != 0 || !strings.Contains(m.notice, "archived 1") {
		t.Fatalf("space a should archive Done: %d notice=%q", len(m.cards[3]), m.notice)
	}
	press(m, "u")
	if len(m.cards[3]) != 1 {
		t.Fatal("archive should be undoable")
	}
	press(m, " ", "a")

	m.width, m.height = 120, 36
	press(m, "/", "login")
	if len(m.pick.results) != 1 || m.pick.results[0].ArchivedAt == "" {
		t.Fatalf("archived cards stay searchable: %+v", m.pick.results)
	}
	if !strings.Contains(m.render(), "archived") {
		t.Fatal("the picker should mark archived cards")
	}
	key(m, tea.KeyEnter) // opening it restores it and shows its details
	if m.mode != modeDetail || m.current() == nil || m.current().Title != "Fix login" || m.current().ArchivedAt != "" {
		t.Fatalf("enter should restore and select the card: %+v", m.current())
	}
}

// --- focus of the day ---

func TestFocusOfTheDay(t *testing.T) {
	m := newModelWithCard(t)
	addCard(m, "Fin", "pay invoices")
	m.col, m.row = 0, 1
	press(m, "*")
	if !m.cards[0][1].Focus || len(m.focus) != 1 {
		t.Fatalf("* should pin the card: %+v", m.cards[0])
	}
	m.width, m.height = 110, 24
	out := m.render()
	if !strings.Contains(out, "★ focus") || !strings.Contains(out, "pay invoices") {
		t.Fatalf("the focus bar is missing:\n%s", out)
	}
	press(m, "T")
	if m.mode != modeToday || len(m.dueCards) != 1 || !strings.Contains(m.render(), "★ FOCUS") {
		t.Fatalf("pinned cards lead the today view: %+v", m.dueCards)
	}
	key(m, tea.KeyEscape)
	press(m, " ", ".") // leader toggles it off again
	if m.cards[0][1].Focus || len(m.focus) != 0 {
		t.Fatal("space . should unpin")
	}
	press(m, "u")
	if !m.cards[0][1].Focus {
		t.Fatal("pinning is undoable")
	}
}

// --- suggestions and templates ---

func TestProjectSuggestionsInNewCard(t *testing.T) {
	m := newModelWithCard(t) // project "Acme" exists
	addCard(m, "Juridico", "x")
	press(m, "n", "ju")
	tab(m)
	if m.form.project.Value() != "Juridico" || m.form.focus != fieldProject {
		t.Fatalf("tab should complete the project, got %q focus=%d", m.form.project.Value(), m.form.focus)
	}
	tab(m) // nothing left to complete: next field
	if m.form.focus != fieldDesc {
		t.Fatal("tab without suggestions should move on")
	}
	m.width, m.height = 110, 36
	key(m, tea.KeyEscape)
	press(m, "n", "ac")
	if out := m.render(); !strings.Contains(out, "tab → ") || !strings.Contains(out, "Acme") {
		t.Fatalf("suggestion line missing:\n%s", out)
	}
}

func TestTagSuggestionsInInputWindow(t *testing.T) {
	m := newModelWithCard(t)
	c := m.cards[0][0]
	c.Tags = "backend,urgent"
	m.updateCard(c, "tags")
	m.reload()
	press(m, "enter", "g") // details -> edit tags
	m.input.SetValue("backend, ur")
	tab(m)
	if got := m.input.Value(); got != "backend, urgent, " {
		t.Fatalf("tab should complete the last tag, got %q", got)
	}
	if s := suggestFor("backend, ", []string{"backend", "urgent"}, true); len(s) != 1 || s[0] != "urgent" {
		t.Fatalf("already used tags must not be suggested: %v", s)
	}
}

func TestTemplates(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 110, 40
	press(m, "n", "Acme", "enter", "Fix crash")
	ctrl(m, 't') // first template: bug
	f := m.form
	if f.tpl != 0 || !strings.Contains(f.notes.Value(), "[ ] reproduce") || f.tags != "bug" || f.priority != 2 {
		t.Fatalf("template not applied: %+v notes=%q", f.tpl, f.notes.Value())
	}
	if out := m.render(); !strings.Contains(out, "template: ") || !strings.Contains(out, "bug") {
		t.Fatalf("the window should show the template:\n%s", out)
	}
	ctrl(m, 't') // next one replaces untouched notes
	if f.tpl != 1 || strings.Contains(f.notes.Value(), "reproduce") {
		t.Fatalf("next template should replace untouched notes: %q", f.notes.Value())
	}
	ctrlS(m)
	var got *store.Card
	for i := range m.cards[0] {
		if m.cards[0][i].Title == "Fix crash" {
			got = &m.cards[0][i]
		}
	}
	if got == nil || got.Tags != "feature" || !strings.Contains(got.Description, "[ ] implement") {
		t.Fatalf("card should carry the template: %+v", got)
	}
}

func TestTemplatesFileIsCreatedAndEditable(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "n")
	if len(m.form.templates) < 3 {
		t.Fatal("default templates expected")
	}
	if _, err := store.Open(filepath.Join(t.TempDir(), "x.db")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	if got := loadTemplates(path); len(got) != len(defaultTemplates) {
		t.Fatal("missing file should give defaults")
	}
	if _, err := store.Open(path); err != nil {
		t.Fatal(err)
	}
	// a broken file never breaks the app
	if err := writeFile(templatesPath(path), "{not json"); err != nil {
		t.Fatal(err)
	}
	if got := loadTemplates(path); len(got) != len(defaultTemplates) {
		t.Fatal("broken templates.json should fall back to the defaults")
	}
	writeFile(templatesPath(path), `[{"name":"mine","notes":"[ ] one","priority":3}]`)
	if got := loadTemplates(path); len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("custom templates should load: %+v", got)
	}
}

// --- nested sub-tasks and keyboard checkboxes ---

func TestNestedSubtasks(t *testing.T) {
	lines := []string{"intro", "[ ] parent", "  [ ] child a", "  [x] child b", "    [ ] grandchild", "[ ] sibling", "tail"}
	if d, n := checkProgress(strings.Join(lines, "\n")); d != 1 || n != 5 {
		t.Fatalf("nested checkboxes count too: %d/%d", d, n)
	}
	if !toggleLines(lines, 1) { // parent -> done, cascades into the sub-tree only
		t.Fatal("toggle failed")
	}
	want := []string{"intro", "[x] parent", "  [x] child a", "  [x] child b", "    [x] grandchild", "[ ] sibling", "tail"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("cascade wrong:\n%q\n%q", lines, want)
	}
	toggleLines(lines, 1) // and back
	if lines[2] != "  [ ] child a" || lines[4] != "    [ ] grandchild" || lines[5] != "[ ] sibling" {
		t.Fatalf("un-toggling should reopen the sub-tree only: %q", lines)
	}
	if toggleLines(lines, 0) {
		t.Fatal("plain text lines are not checkboxes")
	}
	// toggling a child leaves the parent alone
	toggleLines(lines, 2)
	if lines[1] != "[ ] parent" || lines[2] != "  [x] child a" {
		t.Fatalf("child toggle: %q", lines)
	}
}

func TestEditorKeysIndentAndToggle(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "enter", "e") // details -> edit text
	press(m, "plan")
	shiftEnter(m)
	press(m, "parent")
	shiftEnter(m)
	tab(m) // indent the new checkbox line
	press(m, "child")
	if got := m.area.Value(); got != "plan\n[ ] parent\n  [ ] child" {
		t.Fatalf("indent: %q", got)
	}
	key(m, tea.KeyEnter) // keeps the indentation
	press(m, "note")
	if got := m.area.Value(); got != "plan\n[ ] parent\n  [ ] child\n  note" {
		t.Fatalf("enter should keep the indentation: %q", got)
	}
	altX(m) // the current line becomes a checkbox
	if got := lineAt(&m.area, 3); got != "  [ ] note" {
		t.Fatalf("alt+x on a plain line: %q", got)
	}
	altX(m) // and toggles it
	if got := lineAt(&m.area, 3); got != "  [x] note" {
		t.Fatalf("alt+x toggle: %q", got)
	}
	shiftTab(m)
	if got := lineAt(&m.area, 3); got != "[x] note" {
		t.Fatalf("outdent: %q", got)
	}
	shiftTab(m) // already at the left edge: nothing happens
	if got := lineAt(&m.area, 3); got != "[x] note" {
		t.Fatalf("outdent at 0: %q", got)
	}
	// enter on an empty checkbox ends the list
	shiftEnter(m)
	key(m, tea.KeyEnter)
	if got := lineAt(&m.area, 4); strings.TrimSpace(got) != "" {
		t.Fatalf("enter on an empty [ ] should clear the marker: %q", got)
	}
	key(m, tea.KeyEscape)
	if !strings.Contains(m.cards[0][0].Description, "  [ ] child") {
		t.Fatalf("saved text: %q", m.cards[0][0].Description)
	}
}

func TestNestedRenderingInDetails(t *testing.T) {
	m := newModelWithCard(t)
	c := m.cards[0][0]
	c.Description = "[ ] parent\n  [x] child"
	m.updateCard(c, "x")
	m.reload()
	m.width, m.height = 80, 24
	press(m, "enter")
	var lines []string
	for _, l := range strings.Split(plainFrame(m), "\n") {
		if strings.Contains(l, "parent") || strings.Contains(l, "child") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 body lines, got %v", lines)
	}
	idx := func(s, sub string) int { return lipgloss.Width(s[:strings.Index(s, sub)]) }
	if idx(lines[1], "child") <= idx(lines[0], "parent") {
		t.Fatalf("the child must be indented under the parent:\n%s\n%s", lines[0], lines[1])
	}
	press(m, " ") // toggle the selected parent line
	if !strings.Contains(m.cards[0][0].Description, "[x] parent") {
		t.Fatalf("toggle: %q", m.cards[0][0].Description)
	}
}

// --- drop at an exact position ---

func TestDragDropExactPosition(t *testing.T) {
	m := newModelWithCard(t) // Fix login
	addCard(m, "B", "second")
	addCard(m, "C", "third")
	m.width, m.height = 100, 34
	titles := func(col int) string {
		var s []string
		for _, c := range m.cards[col] {
			s = append(s, c.Title)
		}
		return strings.Join(s, ",")
	}
	m.render()
	cardHit := func(col, row int) hit {
		for _, h := range m.hits {
			if h.kind == hitCard && h.col == col && h.row == row {
				return h
			}
		}
		t.Fatalf("no hit for card %d/%d", col, row)
		return hit{}
	}
	// drag "third" (row 2) onto the top half of "Fix login" (row 0): it goes first
	third := cardHit(0, 2)
	target := cardHit(0, 0)
	m.Update(tea.MouseClickMsg{X: third.x0 + 1, Y: third.y0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: target.x0 + 1, Y: target.y0, Button: tea.MouseLeft})
	if m.drag.idx != 0 || m.drag.over != 0 {
		t.Fatalf("drop index should be 0, got over=%d idx=%d", m.drag.over, m.drag.idx)
	}
	if !strings.Contains(m.render(), "━━━") {
		t.Fatal("a drop marker bar should be drawn")
	}
	m.Update(tea.MouseReleaseMsg{X: target.x0 + 1, Y: target.y0, Button: tea.MouseLeft})
	if titles(0) != "third,Fix login,second" {
		t.Fatalf("reorder by drag: %s", titles(0))
	}

	// drag the first card to the Doing column (empty): it lands there
	m.render()
	first := cardHit(0, 0)
	var doing hit
	for _, h := range m.hits {
		if h.kind == hitColumn && h.col == 1 {
			doing = h
		}
	}
	m.Update(tea.MouseClickMsg{X: first.x0 + 1, Y: first.y0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: doing.x0 + 2, Y: doing.y0 + 4, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: doing.x0 + 2, Y: doing.y0 + 4, Button: tea.MouseLeft})
	if titles(1) != "third" || titles(0) != "Fix login,second" {
		t.Fatalf("drop into another column: todo=%s doing=%s", titles(0), titles(1))
	}

	// insert between two cards of another column: drop on the lower half of the first card
	m.lastClick = click{} // the earlier click on the same card would count as a double-click
	m.render()
	one := cardHit(0, 0)
	m.Update(tea.MouseClickMsg{X: one.x0 + 1, Y: one.y0, Button: tea.MouseLeft})
	m.render()
	dst := cardHit(1, 0)
	m.Update(tea.MouseMotionMsg{X: dst.x0 + 1, Y: dst.y1, Button: tea.MouseLeft}) // lower half: after it
	m.Update(tea.MouseReleaseMsg{X: dst.x0 + 1, Y: dst.y1, Button: tea.MouseLeft})
	if titles(1) != "third,Fix login" {
		t.Fatalf("lower half should insert after the card: %s", titles(1))
	}
	press(m, "u")
	if titles(1) != "third" {
		t.Fatalf("a drag is one undo step: %s", titles(1))
	}
}

// --- floating input windows ---

func TestInputIsAFloatingWindow(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 100, 30
	press(m, "enter", "t") // details -> edit title
	out := plainFrame(m)
	if !strings.Contains(out, "╭─ Title") || !strings.Contains(out, "enter") {
		t.Fatalf("the title editor should be a floating window:\n%s", out)
	}
	if !strings.Contains(out, "Fix login") {
		t.Fatal("the window should be prefilled and the card still visible behind it")
	}
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line %d is %d cols wide", i, w)
		}
	}
	key(m, tea.KeyEscape)
	if m.mode != modeDetail {
		t.Fatalf("esc should return to the details, mode=%v", m.mode)
	}
	press(m, "u") // due date
	if out := plainFrame(m); !strings.Contains(out, "Due date") || !strings.Contains(out, "YYYY-MM-DD") {
		t.Fatalf("due date window:\n%s", out)
	}
}

// --- export from the app, themes ---

func TestExportFromTheApp(t *testing.T) {
	m := newModelWithCard(t)
	press(m, " ", "E")
	if !strings.Contains(m.notice, "exported to") {
		t.Fatalf("notice = %q", m.notice)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(m.st.Path()), "exports", "kbn-*.json"))
	md, _ := filepath.Glob(filepath.Join(filepath.Dir(m.st.Path()), "exports", "kbn-*.md"))
	if len(files) != 1 || len(md) != 1 {
		t.Fatalf("expected a json and a md export, got %v %v", files, md)
	}
}

func TestAllThemesAreComplete(t *testing.T) {
	defer SetTheme("default")
	seen := map[string]bool{}
	for _, name := range ThemeNames() {
		if seen[name] {
			t.Fatalf("duplicate theme %q", name)
		}
		seen[name] = true
		if SetTheme(name) != name {
			t.Fatalf("theme %q does not apply", name)
		}
		if th.Accent == nil || th.Dim == nil || th.Rule == nil || th.Err == nil || th.Warn == nil || th.Ok == nil ||
			th.Info == nil || th.Purple == nil || th.Bright == nil || th.SelBg == nil || th.ChipBg == nil {
			t.Fatalf("theme %q has an unset color", name)
		}
		for i, c := range th.Prio {
			if c == nil {
				t.Fatalf("theme %q: priority color %d unset", name, i)
			}
		}
		if len(th.Projects) < 5 {
			t.Fatalf("theme %q needs a project palette", name)
		}
		m := newModelWithCard(t)
		m.width, m.height = 100, 30
		if out := m.render(); !strings.Contains(out, "To do") {
			t.Fatalf("theme %q renders nothing", name)
		}
	}
	for _, want := range []string{"default", "monokai", "gruvbox", "nord", "tokyonight"} {
		if !seen[want] {
			t.Errorf("theme %q is missing", want)
		}
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }

// --- sticky notes ---

func TestStickyNotes(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 130, 40
	press(m, " ", "r")
	if m.mode != modeNotes {
		t.Fatalf("space r should open the notes, mode=%v", m.mode)
	}
	if out := plainFrame(m); !strings.Contains(out, "No notes yet") || !strings.Contains(out, "Notes") {
		t.Fatalf("empty notes window:\n%s", out)
	}
	press(m, "n")
	if m.mode != modeNoteEdit {
		t.Fatal("n should open the note editor")
	}
	press(m, "call the bank")
	shiftEnter(m)
	press(m, "send the contract")
	key(m, tea.KeyEscape) // esc saves
	if m.mode != modeNotes || len(m.notes) != 1 || m.notes[0].Body != "call the bank\n[ ] send the contract" {
		t.Fatalf("note not saved: mode=%v notes=%+v", m.mode, m.notes)
	}
	// a second note, colored differently
	press(m, "n", "buy coffee")
	ctrlS(m)
	if len(m.notes) != 2 || m.notes[0].Color == m.notes[1].Color {
		t.Fatalf("notes should rotate colors: %+v", m.notes)
	}
	out := plainFrame(m)
	for _, want := range []string{"2 notes", "call the bank", "☐"[:0] + "send the contract", "buy coffee"} {
		if !strings.Contains(out, want) {
			t.Fatalf("window is missing %q:\n%s", want, out)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line %d is %d cols (> %d)", i, w, m.width)
		}
	}

	// pin the other note: it moves first and stays selected
	press(m, "j", "l") // grid navigation is bounded
	sel := m.notes[m.noteSel].ID
	press(m, "p")
	if !m.notes[0].Pinned || m.notes[0].ID != sel || m.noteSel != 0 {
		t.Fatalf("pinning should move the note first and keep it selected: %+v sel=%d", m.notes, m.noteSel)
	}
	oldColor := m.notes[0].Color
	press(m, "c")
	if m.notes[0].Color == oldColor {
		t.Fatal("c should change the color")
	}

	// edit
	press(m, "enter")
	if m.mode != modeNoteEdit || !strings.Contains(m.noteEd.area.Value(), "call the bank") && !strings.Contains(m.noteEd.area.Value(), "buy coffee") {
		t.Fatal("enter should edit the selected note")
	}
	press(m, " and milk")
	key(m, tea.KeyEscape)
	if !strings.HasSuffix(m.notes[0].Body, " and milk") {
		t.Fatalf("edit not saved: %q", m.notes[0].Body)
	}
	// delete with dd, undo with u
	press(m, "d", "d")
	if len(m.notes) != 1 {
		t.Fatalf("dd should delete: %d", len(m.notes))
	}
	press(m, "u")
	if len(m.notes) != 2 {
		t.Fatal("u should bring the note back")
	}
	press(m, "u", "u", "u") // revert color, pin... and then there is nothing left
	press(m, "r")
	if m.mode != modeBoard {
		t.Fatalf("r should close the notes, mode=%v", m.mode)
	}
}

func TestNoteReminderAndHeaderChip(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 36
	press(m, " ", "r", "n", "pay the invoice")
	ctrlS(m)
	press(m, "D")
	if m.mode != modeInput || !strings.Contains(plainFrame(m), "Remind me on") {
		t.Fatalf("D should open the reminder window, mode=%v", m.mode)
	}
	m.input.SetValue("nonsense")
	key(m, tea.KeyEnter)
	if m.err == "" || m.notes[0].Due != "" {
		t.Fatalf("an invalid date must be refused: err=%q due=%q", m.err, m.notes[0].Due)
	}
	press(m, "D")
	m.input.SetValue("2000-01-01") // long overdue
	key(m, tea.KeyEnter)
	if m.mode != modeNotes || m.notes[0].Due != "2000-01-01" {
		t.Fatalf("reminder not saved: mode=%v %+v", m.mode, m.notes)
	}
	press(m, "esc")
	if m.notesDue != 1 || !strings.Contains(plainFrame(m), "🔔 1 reminder") {
		t.Fatalf("the header should flag a due reminder: due=%d", m.notesDue)
	}
}

func TestNoteBecomesTask(t *testing.T) {
	m := newModelWithCard(t)
	press(m, " ", "r", "n", "Prepare the demo")
	shiftEnter(m)
	press(m, "slides")
	ctrlS(m)
	press(m, "t") // to task
	if m.mode != modeNewCard || m.form.desc.Value() != "Prepare the demo" {
		t.Fatalf("t should open the save-as-task window: mode=%v", m.mode)
	}
	key(m, tea.KeyEscape) // back to the notes, nothing lost
	if m.mode != modeNotes || len(m.notes) != 1 {
		t.Fatalf("esc should return to the notes, mode=%v notes=%d", m.mode, len(m.notes))
	}
	press(m, "t", "Acme", "enter")
	key(m, tea.KeyEnter) // save
	if m.mode != modeBoard {
		t.Fatalf("should land on the board, mode=%v err=%q", m.mode, m.err)
	}
	var got *store.Card
	for i := range m.cards[0] {
		if m.cards[0][i].Title == "Prepare the demo" {
			got = &m.cards[0][i]
		}
	}
	if got == nil || got.Project != "Acme" || got.Description != "[ ] slides" {
		t.Fatalf("task not created from the note: %+v", got)
	}
	if notes, _ := m.st.Notes(); len(notes) != 0 {
		t.Fatalf("the note should be gone once it became a task: %+v", notes)
	}
}

func TestNotesExportImportAndRedoMovedToU(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, _ := s.AddNote("remember [ ] this", 2)
	n.Pinned, n.Due = true, "2030-01-01"
	s.UpdateNote(n)
	e, _ := s.Export()
	if len(e.Notes) != 1 || !e.Notes[0].Pinned || e.Notes[0].Due != "2030-01-01" {
		t.Fatalf("notes should be exported: %+v", e.Notes)
	}
	o, _ := store.Open(filepath.Join(t.TempDir(), "b.db"))
	defer o.Close()
	if _, _, err := o.Import(e); err != nil {
		t.Fatal(err)
	}
	got, _ := o.Notes()
	if len(got) != 1 || got[0].Body != "remember [ ] this" || !got[0].Pinned {
		t.Fatalf("notes should be imported: %+v", got)
	}
	if _, ok := leaderKeys["r"]; !ok || leaderKeys["r"] != "notes" || leaderKeys["U"] != "ctrl+r" {
		t.Fatalf("space r opens notes and space U redoes: %v", leaderKeys)
	}
}

func TestNoteTitleTagsSearch(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 36
	if _, err := m.st.AddNoteFull(store.Note{Title: "Senha do servidor", Tags: "Infra, #Acesso", Body: "ssh em 10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.st.AddNote("outra nota sem titulo", 0); err != nil {
		t.Fatal(err)
	}
	press(m, " ", " ")
	press(m, "senha")
	if len(m.pick.noteRes) != 1 || m.pick.noteRes[0].Title != "Senha do servidor" {
		t.Fatalf("note should be found by title: %+v", m.pick.noteRes)
	}
	if !strings.Contains(m.render(), "Senha do servidor") {
		t.Fatal("note row not rendered in the search window")
	}
	m.pick.input.SetValue("#acesso")
	m.pick.refilter()
	if len(m.pick.noteRes) != 1 || len(m.pick.results) != 0 {
		t.Fatalf("#tag should find only the tagged note: cards=%d notes=%d", len(m.pick.results), len(m.pick.noteRes))
	}
	if m.pick.noteRes[0].Tags != "infra,acesso" {
		t.Fatalf("tags should be normalized: %q", m.pick.noteRes[0].Tags)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeNoteEdit || m.noteEd == nil || m.noteEd.orig.Title != "Senha do servidor" {
		t.Fatalf("enter should open the note editor: mode=%v", m.mode)
	}
	m.noteEd.title.SetValue("Senha nova")
	m.noteEd.tags.SetValue("infra, vpn")
	key(m, tea.KeyEscape)
	got, _ := m.st.Notes()
	for _, n := range got {
		if n.Title == "Senha nova" && n.Tags == "infra,vpn" {
			return
		}
	}
	t.Fatalf("title/tags edits not saved: %+v", got)
}
