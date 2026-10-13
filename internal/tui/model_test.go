package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
)

func press(m *Model, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		case " ":
			m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		default:
			m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		}
	}
}

func TestNewCardForm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 36

	press(m, "n")
	if m.mode != modeNewCard || m.form.focus != fieldProject {
		t.Fatalf("n should open the form on the project field: mode=%v", m.mode)
	}
	press(m, "Acme", "enter")
	if m.form.focus != fieldDesc {
		t.Fatal("enter on project should move to description")
	}
	out := m.render()
	for _, want := range []string{"New card", "Project", "Description", "Notes", "Acme"} {
		if !strings.Contains(out, want) {
			t.Fatalf("window is missing %q:\n%s", want, out)
		}
	}
	ctrlS(m) // empty description is rejected
	if m.mode != modeNewCard || m.form.err == "" {
		t.Fatal("empty description should not save")
	}
	press(m, "Fix login", "enter")
	if m.form.focus != fieldNotes {
		t.Fatal("enter on description should move to notes (not save)")
	}
	press(m, "context")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // plain enter = new line inside notes
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	press(m, "step one")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	press(m, "step two")
	if m.mode != modeNewCard {
		t.Fatal("enter inside notes must not save")
	}
	ctrlS(m)
	if m.mode != modeBoard || m.err != "" {
		t.Fatalf("after save: mode=%v err=%q", m.mode, m.err)
	}
	c := m.cards[0]
	want := "context\n[ ] step one\n[ ] step two"
	if len(c) != 1 || c[0].Project != "Acme" || c[0].Title != "Fix login" || c[0].Description != want {
		t.Fatalf("card not saved as expected: %+v (want description %q)", c, want)
	}
}

func TestNewCardNotesCheckboxOnEmptyLine(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "n", "P", "enter", "T", "enter") // focus on notes
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	press(m, "first")
	if got := m.form.notes.Value(); got != "[ ] first" {
		t.Fatalf("shift+enter on an empty notes field should not add a blank line, got %q", got)
	}
}

func newModelWithCard(t *testing.T) *Model {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	press(m, "n", "Acme", "enter", "Fix login")
	ctrlS(m)
	return m
}

func TestDeleteNeedsDD(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "d")
	if len(m.cards[0]) != 1 {
		t.Fatal("single d must not delete")
	}
	press(m, "j", "d") // another key in between resets the sequence
	if len(m.cards[0]) != 1 {
		t.Fatal("d, j, d must not delete")
	}
	press(m, "d")
	if len(m.cards[0]) != 0 {
		t.Fatal("dd must delete")
	}
}

func TestInlineChecklist(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "enter", "e") // open details, start editing
	if !m.editing {
		t.Fatal("e should start editing")
	}
	press(m, "intro")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	press(m, "task one")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	press(m, "task two")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.editing {
		t.Fatal("esc should leave editing")
	}
	if got := m.cards[0][0].Description; got != "intro\n[ ] task one\n[ ] task two" {
		t.Fatalf("description = %q", got)
	}
	press(m, "j", " ") // select the checkbox line and toggle it
	if got := m.cards[0][0].Description; got != "intro\n[x] task one\n[ ] task two" {
		t.Fatalf("after toggle = %q", got)
	}
	if d, tot := checkProgress(m.cards[0][0].Description); d != 1 || tot != 2 {
		t.Fatalf("progress = %d/%d", d, tot)
	}
}

func TestDetailWrapsToTerminalWidth(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 60, 30
	press(m, "enter", "e")
	press(m, strings.Repeat("palavra ", 30))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	for i, line := range strings.Split(m.render(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line %d is %d cols wide (> %d): %q", i, w, m.width, line)
		}
	}
}

func ctrlS(m *Model) { m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) }

func addCard(m *Model, project, title string) {
	press(m, "n", project, "enter", title)
	ctrlS(m)
}

func TestSearchPicker(t *testing.T) {
	m := newModelWithCard(t) // Acme / Fix login
	addCard(m, "Jurídico", "automatizar contratos")
	m.width, m.height = 120, 36
	press(m, "g") // other key in between
	press(m, " ")
	if m.mode == modeSearch {
		t.Fatal("a single space must not open search")
	}
	press(m, " ")
	if m.mode != modeSearch || len(m.pick.results) != 2 {
		t.Fatalf("space space should open the picker with all cards: mode=%v", m.mode)
	}
	out := m.render()
	if !strings.Contains(out, "Search") || !strings.Contains(out, "2/2") || !strings.Contains(out, "Preview") && !strings.Contains(out, "Fix login") {
		t.Fatalf("picker window not rendered:\n%s", out)
	}
	press(m, "juridico") // accent-insensitive
	if len(m.pick.results) != 1 || m.pick.results[0].Project != "Jurídico" {
		t.Fatalf("filter failed: %+v", m.pick.results)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeDetail || m.current() == nil || m.current().Project != "Jurídico" {
		t.Fatalf("enter should open the card's details: mode=%v cur=%+v", m.mode, m.current())
	}
	key(m, tea.KeyEscape) // back to the board, card still selected
	press(m, "/", "automatizar")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // tab only shows it on the board
	if m.mode != modeBoard || m.current() == nil || m.current().Project != "Jurídico" {
		t.Fatalf("tab should jump to the card without opening it: mode=%v", m.mode)
	}
	// navigate with the arrows and close with esc
	press(m, "/")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.pick.idx != 1 {
		t.Fatal("down should move the selection")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeBoard || m.pick != nil {
		t.Fatal("esc should close the picker")
	}
	press(m, "/", "#nomatch")
	if len(m.pick.results) != 0 {
		t.Fatal("#tag should only match tags")
	}
}

func TestBoardFitsTerminal(t *testing.T) {
	m := newModelWithCard(t)
	for i := 0; i < 12; i++ {
		addCard(m, "Proj", strings.Repeat("long title ", 6))
	}
	m.width, m.height = 90, 24
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) > m.height {
		t.Fatalf("rendered %d lines, terminal has %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line %d is %d cols (> %d)", i, w, m.width)
		}
	}
	if !strings.Contains(out, "more") {
		t.Fatal("overflowing column should show a 'more' indicator")
	}
}

func key(m *Model, code rune) { m.Update(tea.KeyPressMsg{Code: code}) }

func TestLeaderKeys(t *testing.T) {
	m := newModelWithCard(t)
	press(m, " ")
	if !m.leader {
		t.Fatal("space should arm the leader")
	}
	press(m, "z") // unknown
	if m.leader || m.notice == "" {
		t.Fatal("unknown leader key should cancel with a notice")
	}
	press(m, " ", "n") // space n = new card
	if m.mode != modeNewCard {
		t.Fatalf("space n should open the new-card form, mode=%v", m.mode)
	}
	key(m, tea.KeyEscape)
	press(m, " ", "x") // space x = delete
	if len(m.cards[0]) != 0 {
		t.Fatal("space x should delete the selected card")
	}
	press(m, "u") // undo brings it back
	if len(m.cards[0]) != 1 {
		t.Fatal("undo should restore the deleted card")
	}
}

func TestUndoEdits(t *testing.T) {
	m := newModelWithCard(t)
	press(m, "p")
	if m.cards[0][0].Priority != 1 {
		t.Fatal("p should raise priority")
	}
	press(m, "u")
	if m.cards[0][0].Priority != 0 {
		t.Fatal("undo should revert priority")
	}
	press(m, "L") // move to Doing
	if len(m.cards[1]) != 1 {
		t.Fatal("L should move the card")
	}
	press(m, "u")
	if len(m.cards[0]) != 1 || len(m.cards[1]) != 0 {
		t.Fatal("undo should move the card back")
	}
	press(m, "u", "u") // new-card undo, then nothing left
	if len(m.cards[0]) != 0 {
		t.Fatal("undo should remove the created card")
	}
	press(m, "u")
	if m.notice != "nothing to undo" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestFilterAndSort(t *testing.T) {
	m := newModelWithCard(t)
	addCard(m, "B", "second")
	addCard(m, "C", "third")
	// priorities: Fix login=0, second=1, third=3 (set by selecting each and pressing p)
	m.col, m.row = 0, 1
	press(m, "p")
	m.row = 2
	press(m, "p", "p", "p")
	press(m, "f") // high only
	if len(m.cards[0]) != 1 || m.cards[0][0].Title != "third" {
		t.Fatalf("priority filter: %+v", m.cards[0])
	}
	press(m, "f", "f", "f") // wraps back to off
	if len(m.cards[0]) != 3 {
		t.Fatal("filter should cycle back to off")
	}
	press(m, "s", "s") // sort by priority
	if m.cards[0][0].Title != "third" {
		t.Fatalf("priority sort: %+v", m.cards[0])
	}
	press(m, "J")
	if m.notice == "" {
		t.Fatal("reordering under a sort should explain itself")
	}
}

func TestTodayView(t *testing.T) {
	m := newModelWithCard(t)
	c := m.cards[0][0]
	c.Due = "2000-01-01"
	m.st.UpdateCard(c)
	m.reload()
	press(m, "T")
	if m.mode != modeToday || len(m.dueCards) != 1 {
		t.Fatalf("today view: mode=%v due=%d", m.mode, len(m.dueCards))
	}
	if out := m.render(); !strings.Contains(out, "OVERDUE") {
		t.Fatalf("overdue section missing:\n%s", out)
	}
	press(m, " ", "x") // leader x = mark done
	if len(m.dueCards) != 0 || len(m.cards[3]) != 1 {
		t.Fatalf("mark done failed: due=%d done=%d", len(m.dueCards), len(m.cards[3]))
	}
	press(m, "esc")
}

func TestMouseSelectAndDrag(t *testing.T) {
	m := newModelWithCard(t)
	addCard(m, "B", "second")
	m.width, m.height = 100, 30
	m.render()
	var h hit
	for _, x := range m.hits {
		if x.kind == hitCard && x.row == 0 {
			h = x
		}
	}
	m.Update(tea.MouseClickMsg{X: h.x0 + 1, Y: h.y0, Button: tea.MouseLeft})
	if m.row != 0 || m.drag == nil {
		t.Fatalf("click should select card 0 and start a drag: row=%d", m.row)
	}
	// drag to the "Doing" column and release
	var doing hit
	for _, x := range m.hits {
		if x.kind == hitColumn && x.col == 1 {
			doing = x
		}
	}
	m.Update(tea.MouseMotionMsg{X: doing.x0 + 2, Y: doing.y0 + 3, Button: tea.MouseLeft})
	m.render()
	m.Update(tea.MouseReleaseMsg{X: doing.x0 + 2, Y: doing.y0 + 3, Button: tea.MouseLeft})
	if len(m.cards[1]) != 1 || m.cards[1][0].Title != "Fix login" {
		t.Fatalf("drag should move the card to Doing: %+v / %+v", m.cards[0], m.cards[1])
	}
	press(m, "u")
	if len(m.cards[1]) != 0 {
		t.Fatal("undo should revert the drag")
	}
}

func TestThemes(t *testing.T) {
	defer SetTheme("default")
	if SetTheme("monokai") != "monokai" {
		t.Fatal("monokai theme missing")
	}
	m := newModelWithCard(t)
	v := m.View()
	if v.BackgroundColor == nil {
		t.Fatal("monokai should set the terminal background")
	}
	if SetTheme("nope") != "default" {
		t.Fatal("unknown theme should fall back to default")
	}
	m.width, m.height = 80, 24
	press(m, " ", "y")
	if th.Name != "monokai" || m.st.Setting("theme") != "monokai" {
		t.Fatalf("space y should cycle and persist the theme, got %q / %q", th.Name, m.st.Setting("theme"))
	}
}

func TestFloatingWindowsAreAligned(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 110, 28
	for _, open := range []string{"/", "n"} {
		press(m, open)
		for i, l := range strings.Split(m.render(), "\n") {
			if w := lipgloss.Width(l); w > m.width {
				t.Fatalf("%s window: line %d is %d cols (> %d)", open, i, w, m.width)
			}
		}
		for _, l := range strings.Split(m.render(), "\n") {
			if strings.Contains(l, "╮─") || strings.Contains(l, "╯─") && !strings.Contains(l, "╰") {
				t.Fatalf("%s window: border overshoots: %q", open, l)
			}
		}
		key(m, tea.KeyEscape)
	}
}

func TestScratchpadToTask(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 110, 30
	press(m, " ", "enter")
	if m.mode != modeScratch {
		t.Fatalf("space enter should open the scratchpad, mode=%v", m.mode)
	}
	press(m, "Weekly sync")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, "decisions")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	press(m, "send contract")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	press(m, "book room")
	if out := m.render(); !strings.Contains(out, "Scratchpad") || !strings.Contains(out, "[ ] send contract") {
		t.Fatalf("scratchpad window not rendered:\n%s", out)
	}

	// esc keeps the draft; reopening restores it
	key(m, tea.KeyEscape)
	if m.mode != modeBoard || !strings.Contains(m.draft, "book room") || m.st.Setting(settingScratch) == "" {
		t.Fatalf("draft should be kept: mode=%v draft=%q", m.mode, m.draft)
	}
	if !strings.Contains(m.render(), "notes draft") {
		t.Fatal("header should flag a pending draft")
	}
	press(m, " ", "enter")
	if !strings.Contains(m.scratch.area.Value(), "Weekly sync") {
		t.Fatal("reopening should restore the draft")
	}

	// ctrl+s opens "save as task" with the first line as title
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.mode != modeNewCard || m.form.desc.Value() != "Weekly sync" {
		t.Fatalf("ctrl+s should open the save window: mode=%v", m.mode)
	}
	if out := m.render(); !strings.Contains(out, "Save notes as task") {
		t.Fatalf("save window missing:\n%s", out)
	}
	key(m, tea.KeyEscape) // back to the notes, nothing lost
	if m.mode != modeScratch {
		t.Fatal("esc in the save window should return to the notes")
	}
	m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	press(m, "Acme", "enter") // project -> title
	key(m, tea.KeyEnter)      // save
	if m.mode != modeBoard {
		t.Fatalf("should be back on the board, mode=%v err=%q", m.mode, m.err)
	}
	var got *store.Card
	for i := range m.cards[0] {
		if m.cards[0][i].Title == "Weekly sync" {
			got = &m.cards[0][i]
		}
	}
	if got == nil || got.Project != "Acme" {
		t.Fatalf("task not created: %+v", m.cards[0])
	}
	if want := "decisions\n[ ] send contract\n[ ] book room"; got.Description != want {
		t.Fatalf("description = %q, want %q", got.Description, want)
	}
	if m.draft != "" || m.st.Setting(settingScratch) != "" {
		t.Fatal("saving should clear the draft")
	}
	press(m, "u") // undo removes the task
	for _, c := range m.cards[0] {
		if c.Title == "Weekly sync" {
			t.Fatal("undo should remove the saved task")
		}
	}
}

func TestScratchpadDiscardNeedsTwoPresses(t *testing.T) {
	m := newModelWithCard(t)
	press(m, " ", "enter", "keep me")
	ctrlX := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
	m.Update(ctrlX)
	if m.scratch.area.Value() == "" {
		t.Fatal("first ctrl+x must not discard")
	}
	m.Update(ctrlX)
	if m.scratch.area.Value() != "" || m.draft != "" {
		t.Fatal("second ctrl+x should discard")
	}
}

func TestHelpWindow(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 40
	press(m, " ", "h")
	if m.mode != modeHelp {
		t.Fatalf("space h should open the help, mode=%v", m.mode)
	}
	out := m.render()
	for _, want := range []string{"Help · macros & keys", "Leader", "meeting notes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help is missing %q:\n%s", want, out)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("help line %d is %d cols (> %d)", i, w, m.width)
		}
	}
	var all strings.Builder
	for _, l := range helpLines(100) {
		all.WriteString(l + "\n")
	}
	if !strings.Contains(all.String(), "dd") {
		t.Fatal("the cheat sheet should document dd")
	}
	press(m, "j", "j")
	if m.helpOff != 2 {
		t.Fatalf("j should scroll, off=%d", m.helpOff)
	}
	press(m, "G")
	m.render()
	if m.helpOff == 0 || !strings.Contains(m.render(), "Command line") {
		t.Fatal("G should jump to the end of the help")
	}
	key(m, tea.KeyEscape)
	if m.mode != modeBoard {
		t.Fatal("esc should close the help")
	}
	// also reachable from the today view, and returns there
	press(m, "T", " ", "h")
	if m.mode != modeHelp || !strings.Contains(m.render(), "Today") {
		t.Fatal("help should open over the today view")
	}
	press(m, "h")
	if m.mode != modeToday {
		t.Fatalf("closing help should return to today, mode=%v", m.mode)
	}
}

// Every leader key must be documented, so the cheat sheet cannot silently drift.
func TestHelpCoversLeaderKeys(t *testing.T) {
	var sb strings.Builder
	for _, sec := range helpSections {
		for _, e := range sec.entries {
			sb.WriteString(e.keys + " " + e.desc + "\n")
		}
	}
	text := sb.String()
	for k := range leaderKeys {
		want := "space " + k
		if !strings.Contains(text, want) {
			t.Errorf("leader key %q is not documented in the help (want %q)", k, want)
		}
	}
}

// The cheat sheet must describe the current windows, not an older version of them.
func TestHelpDescribesCurrentFeatures(t *testing.T) {
	var sb strings.Builder
	for _, sec := range helpSections {
		sb.WriteString(sec.title + "\n")
		for _, e := range sec.entries {
			sb.WriteString(e.keys + " " + e.desc + "\n")
		}
	}
	text := sb.String()
	for _, want := range []string{"Notes", "ctrl+s", "Boards list", "Input window", "space enter", "shift+enter", "drag"} {
		if !strings.Contains(text, want) {
			t.Errorf("help should mention %q", want)
		}
	}
	if strings.Contains(text, "Project → Description,") || strings.Contains(text, "(Project → Description)") {
		t.Error("help still describes the old two-field new-card window")
	}
}
