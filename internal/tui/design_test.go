package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/andreas/kbn/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterFitsOneLineAndKeepsTheImportantHints(t *testing.T) {
	m := newModelWithCard(t)
	for _, w := range []int{40, 60, 80, 120} {
		m.width, m.height = w, 30
		for _, md := range []mode{modeBoard, modeNotes, modeJournal, modeSummary, modeColumns, modeSearch} {
			m.mode = md
			foot := m.footer()
			if strings.Contains(foot, "\n") || lipgloss.Width(foot) > w {
				t.Fatalf("width %d mode %v: footer must be one line that fits, got %d cells: %q", w, md, lipgloss.Width(foot), ansi.Strip(foot))
			}
		}
	}
	m.mode = modeNotes
	m.width = 30
	foot := ansi.Strip(m.footer())
	if !strings.Contains(foot, "n new") || !strings.Contains(foot, "…") {
		t.Fatalf("a narrow footer keeps the first hints and shows an ellipsis: %q", foot)
	}
	m.width = 200
	if foot := ansi.Strip(m.footer()); !strings.Contains(foot, "esc close") || strings.Contains(foot, "…") {
		t.Fatalf("a wide footer shows every hint: %q", foot)
	}
}

func TestWindowsShowMessagesInTheFooter(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 36
	press(m, " ", "j", "}") // the diary says there is no later entry
	if m.notice == "" || !strings.Contains(ansi.Strip(m.footer()), m.notice) {
		t.Fatalf("a notice raised inside a window must reach the footer: notice=%q footer=%q", m.notice, ansi.Strip(m.footer()))
	}
}

func TestBackdropIsDimmedBehindWindows(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 120, 36
	plain := m.render()
	if !strings.Contains(plain, "\x1b[") || !strings.Contains(ansi.Strip(plain), "Fix login") {
		t.Fatal("the board itself is colored")
	}
	dim := dimBackdrop("\x1b[31mred\x1b[0m text\nline two")
	if strings.Contains(dim, "\x1b[31m") || !strings.Contains(ansi.Strip(dim), "red text") || !strings.Contains(ansi.Strip(dim), "line two") {
		t.Fatalf("dimBackdrop must drop the original colors and keep the text: %q", dim)
	}
	press(m, " ", "r") // notes window over the board
	out := m.render()
	if !strings.Contains(ansi.Strip(out), "Trabalho") || !strings.Contains(ansi.Strip(out), "Notes") {
		t.Fatal("the header stays readable behind the window, and the window is drawn")
	}
	if strings.Contains(strings.Split(out, "\n")[0], "\x1b[1;38;5;39m") { // the title color of the header is drained
		t.Fatal("the header behind a window must be dimmed")
	}
}

func TestBoardShowsFewerColumnsOnNarrowTerminals(t *testing.T) {
	m := newModelWithCard(t) // 4 columns
	m.width, m.height = 80, 30
	off, vis := m.visibleCols()
	if vis != 3 || off != 0 { // 80/26 = 3
		t.Fatalf("80 columns fit 3 board columns, got off=%d vis=%d", off, vis)
	}
	if !strings.Contains(ansi.Strip(m.header()), "columns 1-3 of 4") {
		t.Fatalf("the header says which columns are shown: %q", ansi.Strip(m.header()))
	}
	press(m, "l", "l", "l") // select the last column: the window follows
	if off, vis := m.visibleCols(); off != 1 || vis != 3 {
		t.Fatalf("the window must follow the selection: off=%d vis=%d col=%d", off, vis, m.col)
	}
	out := ansi.Strip(m.render())
	if strings.Contains(out, "To do") || !strings.Contains(out, "Done") {
		t.Fatalf("the first column scrolled out, the last is visible:\n%s", out)
	}
	m.width = 140
	if off, vis := m.visibleCols(); off != 0 || vis != 4 {
		t.Fatalf("a wide terminal shows every column: off=%d vis=%d", off, vis)
	}
	m.width = 20
	if _, vis := m.visibleCols(); vis != 1 {
		t.Fatalf("a tiny terminal still shows one column, got %d", vis)
	}
}

func TestShortDueWordsAndUrgency(t *testing.T) {
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format(isoDate) }
	for _, c := range []struct {
		due   string
		label string
		lvl   int
	}{
		{day(-3), "3d late", dueLate},
		{day(0), "today", dueToday},
		{day(1), "tomorrow", dueSoon},
		{day(10), "", dueLater},
		{"junk", "junk", dueLater},
	} {
		label, lvl := shortDue(c.due)
		if lvl != c.lvl || (c.label != "" && label != c.label) || label == "" {
			t.Errorf("shortDue(%q) = %q,%d want %q,%d", c.due, label, lvl, c.label, c.lvl)
		}
	}
	if label, _ := shortDue(day(3)); len(label) != len("Mon 2") && len(label) != len("Mon 12") {
		t.Errorf("a date within the week shows the weekday: %q", label)
	}
	if !strings.Contains(ansi.Strip(dueStyled(day(-2), false)), "2d late") {
		t.Error("late card says how late")
	}
	if got := ansi.Strip(dueStyled(day(-2), true)); strings.Contains(got, "late") {
		t.Errorf("a finished card is never shown as late: %q", got)
	}
}

func TestCardShowsShortDueAndEmptyColumnHint(t *testing.T) {
	m := newModelWithCard(t)
	m.width, m.height = 140, 40
	c := m.current()
	cc := *c
	cc.Due = time.Now().AddDate(0, 0, -2).Format(isoDate)
	if err := m.st.UpdateCard(cc); err != nil {
		t.Fatal(err)
	}
	m.reload()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "2d late") {
		t.Fatalf("board card shows the short due date:\n%s", out)
	}
	if !strings.Contains(out, "Nothing here") && !strings.Contains(out, "—") {
		t.Fatalf("empty columns are marked:\n%s", out)
	}
	m.col = 1 // an empty column is selected
	m.clamp()
	if out := ansi.Strip(m.render()); !strings.Contains(out, "Nothing here. Press n") {
		t.Fatalf("the selected empty column says what to do:\n%s", out)
	}
	// first run: nothing anywhere
	st2, _ := store.Open(t.TempDir() + "/e.db")
	defer st2.Close()
	m2, _ := New(st2)
	m2.width, m2.height = 140, 40
	if out := ansi.Strip(m2.render()); !strings.Contains(out, "No cards yet") {
		t.Fatalf("an empty board welcomes the user:\n%s", out)
	}
}

func TestTodayHeaderCountsLatePinnedCards(t *testing.T) {
	m := newModelWithCard(t)
	c := *m.current()
	c.Due, c.Focus = time.Now().AddDate(0, 0, -2).Format(isoDate), true
	m.st.UpdateCard(c)
	press(m, "T")
	if h := ansi.Strip(m.todayHeader()); !strings.Contains(h, "1 focus") || !strings.Contains(h, "1 overdue") {
		t.Fatalf("a late pinned card is both focus and overdue: %q", h)
	}
}

func TestExtraThemesExistAndRender(t *testing.T) {
	names := strings.Join(ThemeNames(), ",")
	for _, want := range []string{"light", "catppuccin", "contrast"} {
		if !strings.Contains(names, want) {
			t.Fatalf("theme %q missing from %s", want, names)
		}
	}
	defer applyTheme("default")
	m := newModelWithCard(t)
	m.width, m.height = 100, 30
	for _, name := range []string{"light", "catppuccin", "contrast"} {
		applyTheme(name)
		if out := ansi.Strip(m.render()); !strings.Contains(out, "Fix login") {
			t.Fatalf("theme %s does not render the board", name)
		}
	}
}

// Data that comes from outside (the phone API, an import) must never be able to crash or garble the UI.
func TestRenderSurvivesHostileDataAndTinyTerminals(t *testing.T) {
	m := newModelWithCard(t)
	c := *m.current()
	c.Due = "9999-12-31"
	m.st.UpdateCard(c)
	n, _ := m.st.AddNoteFull(store.Note{Title: "n", Body: "b"})
	n.Color = -7 // what the phone API used to be able to store
	m.st.ApplyNote(n)
	m.st.AddCardWith(store.Card{ColumnID: m.columns[0].ID, Title: "far past", Due: "0001-01-01"})
	m.reload()
	for _, size := range [][2]int{{1, 1}, {10, 4}, {20, 6}, {40, 12}, {80, 24}, {300, 90}} {
		m.width, m.height = size[0], size[1]
		for _, md := range []mode{modeBoard, modeToday, modeNotes, modeJournal, modeSummary, modeProjects, modeColumns, modeHelp, modeSearch, modeDetail} {
			m.mode = md
			switch md {
			case modeNotes:
				m.loadNotes()
			case modeJournal:
				m.openJournal("")
			case modeSummary:
				m.openSummary()
			case modeProjects:
				m.openProjects()
			case modeColumns:
				m.openColumns()
			case modeSearch:
				m.mode = modeBoard
				m.openSearch()
			case modeHelp:
				m.helpBack = modeBoard
			case modeToday:
				m.loadToday()
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("render panicked at %dx%d in mode %v: %v", size[0], size[1], md, r)
					}
				}()
				m.render()
			}()
			m.mode = modeBoard
		}
	}
	if wrapIdx(-7, 6) != 5 || wrapIdx(13, 6) != 1 || wrapIdx(3, 0) != 0 {
		t.Fatal("wrapIdx must map any integer into the palette")
	}
}

func TestProjectColorIsStableAndNeverOutOfRange(t *testing.T) {
	long := strings.Repeat("Projeto Muito Longo ", 200) // enough to wrap a signed hash many times
	for _, name := range []string{"", "a", "Acme", long, strings.Repeat("\U0010ffff", 5000)} {
		if projectColor(name) == nil || projectColor(name) != projectColor(name) {
			t.Fatalf("projectColor(%.20q) must be stable and defined", name)
		}
	}
}
