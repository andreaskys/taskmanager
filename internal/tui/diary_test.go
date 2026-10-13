package tui

import (
	"github.com/andreas/kbn/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func ctrlKey(m *Model, code rune) {
	m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
}

func TestDiaryWriteToggleCarryOverAndSearch(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 36
	yesterday := addDays(todayStr(), -1)
	m.st.SaveJournal(yesterday, "reunião\n[ ] ligar para o cliente\n[x] enviar proposta")

	press(m, " ", "j")
	if m.mode != modeJournal || m.jDay != todayStr() {
		t.Fatalf("space j should open today's diary: mode=%v day=%s", m.mode, m.jDay)
	}
	press(m, "e", "primeira nota do dia")
	ctrlS(m)
	if e, _ := m.st.Journal(todayStr()); e.Body != "primeira nota do dia" || m.mode != modeJournal {
		t.Fatalf("diary not saved: %+v mode=%v", e, m.mode)
	}
	press(m, "p") // carries only the unfinished item of yesterday
	e, _ := m.st.Journal(todayStr())
	if !strings.Contains(e.Body, "[ ] ligar para o cliente") || strings.Contains(e.Body, "proposta") {
		t.Fatalf("carry over wrong: %q", e.Body)
	}
	press(m, "p")
	if e2, _ := m.st.Journal(todayStr()); e2.Body != e.Body {
		t.Fatalf("carrying twice must not duplicate: %q", e2.Body)
	}
	press(m, "j", " ") // select the checkbox line and toggle it
	if e3, _ := m.st.Journal(todayStr()); !strings.Contains(e3.Body, "[x] ligar") {
		t.Fatalf("checkbox not toggled: %q", e3.Body)
	}
	press(m, "[") // yesterday
	if m.jDay != yesterday || !strings.Contains(m.jEntry.Body, "reunião") {
		t.Fatalf("[ should show yesterday: %s", m.jDay)
	}
	press(m, "]", "}")
	if m.notice == "" {
		t.Fatal("no later entry should say so")
	}
	if out := m.render(); !strings.Contains(out, "Diary") || !strings.Contains(out, "today") {
		t.Fatalf("diary window not rendered:\n%s", out)
	}
	key(m, tea.KeyEscape)

	press(m, " ", " ", "proposta")
	if len(m.pick.dayRes) != 1 || m.pick.dayRes[0].Day != yesterday {
		t.Fatalf("diary should be searchable: %+v", m.pick.dayRes)
	}
	if !strings.Contains(m.render(), "📅") {
		t.Fatal("diary row not rendered in search")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeJournalEdit || m.jEd == nil || m.jEd.day != yesterday {
		t.Fatalf("enter on a diary day should open its editor: mode=%v", m.mode)
	}
}

func TestDiaryShowsCardActivityAndDetailHistory(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 40
	press(m, "L") // move the card to Doing
	if ev, _ := m.st.EventsOn(todayStr()); len(ev) < 2 {
		t.Fatalf("create + move should be logged: %+v", ev)
	}
	press(m, " ", "j")
	if out := m.render(); !strings.Contains(out, "Cards that day") || !strings.Contains(out, "Fix login") || !strings.Contains(out, "To do → Doing") {
		t.Fatalf("activity missing:\n%s", out)
	}
	key(m, tea.KeyEscape)
	m.selectCard(m.current().ID)
	m.openDetail()
	out := m.render()
	if !strings.Contains(out, "History") || !strings.Contains(out, "created in To do") || !strings.Contains(out, "To do → Doing") {
		t.Fatalf("history missing in the details:\n%s", out)
	}
	if !strings.Contains(out, "#1") {
		t.Fatalf("card id missing in the details:\n%s", out)
	}
}

func TestLinksAndAttachments(t *testing.T) {
	m := newModelWithCard(t) // card #1 Acme / Fix login
	m.width, m.height = 120, 36
	addCard(m, "Acme", "Second")
	var opened []string
	old := opener
	opener = func(target string) error { opened = append(opened, target); return nil }
	defer func() { opener = old }()

	// a note mentioning card #1 opens that card with o (single target → immediate)
	m.st.AddNote("ver #1 antes da reunião", 0)
	press(m, " ", "r", "o")
	if m.mode != modeDetail || m.current() == nil || m.current().Title != "Fix login" {
		t.Fatalf("#1 should open card 1: mode=%v cur=%+v", m.mode, m.current())
	}
	if !strings.Contains(m.render(), "Mentioned in") {
		t.Fatalf("the card should list where it is mentioned:\n%s", m.render())
	}

	// attach a file and a URL to the card; two targets → list, enter opens the selected one
	file := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(file, []byte("x"), 0o644)
	press(m, "a", file)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, "a", "Spec | https://example.com/spec")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.detAtt) != 2 || m.detAtt[1].Label != "Spec" {
		t.Fatalf("attachments not saved: %+v", m.detAtt)
	}
	press(m, "o")
	if m.mode != modeLinks || len(m.links) != 2 {
		t.Fatalf("two targets should show the list: mode=%v links=%d", m.mode, len(m.links))
	}
	if !strings.Contains(m.render(), "doc.pdf") {
		t.Fatal("list not rendered")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(opened) != 1 || opened[0] != file {
		t.Fatalf("file not opened: %v", opened)
	}
	press(m, "o", "j", "x") // remove the URL attachment
	if len(m.detAtt) != 1 {
		t.Fatalf("attachment not removed: %+v", m.detAtt)
	}
	key(m, tea.KeyEscape) // the list stays open with the remaining item
	press(m, "o")         // one target left → opens right away
	if len(opened) != 2 || opened[1] != file {
		t.Fatalf("single target should open at once: %v", opened)
	}
	press(m, "a", "/does/not/exist.txt")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(m, "o", "j", "enter")
	if m.err == "" || len(opened) != 2 {
		t.Fatalf("a missing file must be reported, not opened: err=%q opened=%v", m.err, opened)
	}
}

func TestSummaryAndProjects(t *testing.T) {
	m := newModelWithCard(t) // Acme / Fix login
	m.width, m.height = 120, 40
	addCard(m, "Jurídico", "contratos")
	press(m, "H", "L", "L", "L") // select Acme card? move whichever card is selected to Done
	var clip string
	old := clipboard
	clipboard = func(s string) error { clip = s; return nil }
	defer func() { clipboard = old }()

	press(m, " ", "w")
	if m.mode != modeSummary {
		t.Fatalf("space w opens the summary: %v", m.mode)
	}
	out := m.render()
	if !strings.Contains(out, "Week") || !strings.Contains(out, "created") || !strings.Contains(out, "done") {
		t.Fatalf("summary not rendered:\n%s", out)
	}
	if m.sum.Created != 2 {
		t.Fatalf("two cards were created: %+v", m.sum)
	}
	press(m, "y")
	if !strings.Contains(clip, "# Summary") {
		t.Fatalf("y should copy the Markdown: %q", clip)
	}
	press(m, "[")
	if m.sum.Created != 0 {
		t.Fatalf("last week had nothing: %+v", m.sum)
	}
	press(m, "m")
	if !m.sumMonth || !strings.Contains(m.render(), "Month") {
		t.Fatal("m should switch to the month")
	}
	key(m, tea.KeyEscape)

	press(m, " ", "p")
	if m.mode != modeProjects || len(m.projRows) != 2 {
		t.Fatalf("space p opens the projects: mode=%v rows=%d", m.mode, len(m.projRows))
	}
	if out := m.render(); !strings.Contains(out, "Acme") || !strings.Contains(out, "Jurídico") || !strings.Contains(out, "open") {
		t.Fatalf("projects not rendered:\n%s", out)
	}
	press(m, "enter") // focus the cards of the first project
	press(m, "enter") // open the first card
	if m.mode != modeDetail {
		t.Fatalf("enter on a project card should open it: mode=%v", m.mode)
	}
}

func TestDayHelpers(t *testing.T) {
	if got := weekStart("2026-10-14"); got != "2026-10-12" { // Wednesday → Monday
		t.Fatalf("weekStart = %s", got)
	}
	if a, b := monthBounds("2026-02-10"); a != "2026-02-01" || b != "2026-02-28" {
		t.Fatalf("monthBounds = %s %s", a, b)
	}
	if got := dayLabel(time.Now().Format(isoDate)); !strings.HasSuffix(got, "today") {
		t.Fatalf("dayLabel = %s", got)
	}
	if ids := cardRefs("ver #12, #3 e #12; abc#9 #x"); len(ids) != 2 || ids[0] != 12 || ids[1] != 3 {
		t.Fatalf("cardRefs = %v", ids)
	}
}

func TestBoardSeesChangesFromAnotherProcess(t *testing.T) {
	m := newModelWithCard(t)
	m.Init()
	other, err := store.Open(m.st.Path()) // what `kbn serve` does from the phone
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.AddCard(m.columns[0].ID, "Phone", "Added from the phone"); err != nil {
		t.Fatal(err)
	}
	m.syncExternal()
	n := 0
	for _, cs := range m.cards {
		n += len(cs)
	}
	if n != 2 {
		t.Fatalf("the board should show the card added elsewhere, has %d", n)
	}
	m.syncExternal() // nothing new: must not fail or reload again
}
