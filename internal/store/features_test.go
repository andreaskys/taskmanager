package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestColumnManagement(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	col, err := s.AddColumn(b.ID, "Review")
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := s.Columns(b.ID)
	names := []string{}
	for _, c := range cols {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "To do,Doing,Testing,Review,Done" {
		t.Fatalf("new column should go before Done: %v", names)
	}
	if err := s.RenameColumn(col.ID, "QA"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWIPLimit(col.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveColumn(col.ID, -1); err != nil { // QA goes left of Testing
		t.Fatal(err)
	}
	cols, _ = s.Columns(b.ID)
	if cols[2].Name != "QA" || cols[2].WIPLimit != 2 || cols[3].Name != "Testing" {
		t.Fatalf("reorder/limit failed: %+v", cols)
	}
	// deleting a column moves its cards to the previous one
	c, _ := s.AddCard(cols[2].ID, "p", "in qa")
	if err := s.DeleteColumn(cols[2].ID); err != nil {
		t.Fatal(err)
	}
	cols, _ = s.Columns(b.ID)
	if len(cols) != 4 || cols[0].Position != 0 || cols[3].Position != 3 {
		t.Fatalf("positions should be contiguous: %+v", cols)
	}
	moved, _ := s.Cards(cols[1].ID) // previous column of QA was Doing
	if len(moved) != 1 || moved[0].ID != c.ID {
		t.Fatalf("cards should move to the previous column: %+v", moved)
	}
	// a board keeps at least two columns
	if err := s.DeleteColumn(mustCols(t, s, b.ID)[1].ID); err != nil { // 4 -> 3
		t.Fatal(err)
	}
	if err := s.DeleteColumn(mustCols(t, s, b.ID)[1].ID); err != nil { // 3 -> 2
		t.Fatal(err)
	}
	if err := s.DeleteColumn(mustCols(t, s, b.ID)[0].ID); err == nil {
		t.Fatal("deleting below two columns should fail")
	}
}

func mustCols(t *testing.T, s *Store, board int64) []Column {
	t.Helper()
	c, err := s.Columns(board)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDoneAtAndArchive(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols := mustCols(t, s, b.ID)
	c, _ := s.AddCard(cols[0].ID, "p", "work")
	if c.DoneAt != "" {
		t.Fatal("a new card in To do has no done_at")
	}
	s.MoveCard(c.ID, cols[3].ID)
	got, _ := s.Cards(cols[3].ID)
	if got[0].DoneAt == "" {
		t.Fatal("moving into Done should set done_at")
	}
	s.MoveCard(c.ID, cols[1].ID)
	got, _ = s.Cards(cols[1].ID)
	if got[0].DoneAt != "" {
		t.Fatal("leaving Done should clear done_at")
	}
	s.MoveCard(c.ID, cols[3].ID)

	if n, _ := s.ArchiveDone(time.Hour); n != 0 {
		t.Fatalf("a card done just now must not be archived after 1h, archived %d", n)
	}
	if n, err := s.ArchiveDone(0); err != nil || n != 1 {
		t.Fatalf("ArchiveDone(0) = %d, %v", n, err)
	}
	if vis, _ := s.Cards(cols[3].ID); len(vis) != 0 {
		t.Fatal("archived cards must be hidden from the board")
	}
	all, _ := s.AllCards()
	if len(all) != 1 || all[0].ArchivedAt == "" {
		t.Fatalf("archived cards stay searchable: %+v", all)
	}
	if due, _ := s.DueCards("2100-01-01"); len(due) != 0 {
		t.Fatal("archived cards must not appear in due lists")
	}
	if err := s.Unarchive(c.ID); err != nil {
		t.Fatal(err)
	}
	if vis, _ := s.Cards(cols[3].ID); len(vis) != 1 {
		t.Fatal("unarchive should bring the card back")
	}
}

func TestFocusCards(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols := mustCols(t, s, b.ID)
	a, _ := s.AddCard(cols[0].ID, "p", "pinned")
	a.Focus = true
	s.UpdateCard(a)
	d, _ := s.AddCard(cols[3].ID, "p", "done and pinned") // Done column is excluded
	d.Focus = true
	s.UpdateCard(d)
	f, err := s.FocusCards()
	if err != nil || len(f) != 1 || f[0].Title != "pinned" || f[0].BoardName != "B" {
		t.Fatalf("FocusCards = %+v, %v", f, err)
	}
}

func TestMoveCardTo(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols := mustCols(t, s, b.ID)
	var ids []int64
	for _, title := range []string{"a", "b", "c"} {
		c, _ := s.AddCard(cols[0].ID, "", title)
		ids = append(ids, c.ID)
	}
	x, _ := s.AddCard(cols[1].ID, "", "x")
	order := func(col int64) string {
		cs, _ := s.Cards(col)
		var t []string
		for _, c := range cs {
			t = append(t, c.Title)
		}
		return strings.Join(t, "")
	}
	s.MoveCardTo(x.ID, cols[0].ID, 1) // between a and b
	if order(cols[0].ID) != "axbc" || order(cols[1].ID) != "" {
		t.Fatalf("insert at index: %q", order(cols[0].ID))
	}
	s.MoveCardTo(ids[2], cols[0].ID, 0) // c to the top, same column
	if order(cols[0].ID) != "caxb" {
		t.Fatalf("reorder in the same column: %q", order(cols[0].ID))
	}
	s.MoveCardTo(ids[0], cols[0].ID, 99) // beyond the end clamps
	if order(cols[0].ID) != "cxba" {
		t.Fatalf("clamp: %q", order(cols[0].ID))
	}
}

func TestUndoLogPersistsAndTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, _ := Open(path)
	s.UndoPush(StackUndo, "one", "{1}")
	s.UndoPush(StackUndo, "two", "{2}")
	s.UndoPush(StackRedo, "r", "{r}")
	s.Close()

	s, _ = Open(path) // "restart"
	defer s.Close()
	if s.UndoCount(StackUndo) != 2 || s.UndoCount(StackRedo) != 1 {
		t.Fatal("undo log should survive reopening the database")
	}
	e, ok, _ := s.UndoPop(StackUndo)
	if !ok || e.Desc != "two" || e.Payload != "{2}" {
		t.Fatalf("pop = %+v", e)
	}
	s.UndoClear(StackRedo)
	if _, ok, _ := s.UndoPop(StackRedo); ok {
		t.Fatal("redo stack should be empty")
	}
	for i := 0; i < maxUndo+30; i++ {
		s.UndoPush(StackUndo, "x", "{}")
	}
	if n := s.UndoCount(StackUndo); n != maxUndo {
		t.Fatalf("log should be trimmed to %d, got %d", maxUndo, n)
	}
}

func TestApplyCardRestoresExactState(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols := mustCols(t, s, b.ID)
	c, _ := s.AddCardWith(Card{ColumnID: cols[0].ID, Project: "p", Title: "t", Description: "[ ] a", Priority: 2, Due: "2030-01-01", Tags: "x,y", Focus: true})
	before, _ := s.CardStates()
	s.DeleteCard(c.ID)
	if err := s.ApplyCard(before[0]); err != nil { // restores with the same id
		t.Fatal(err)
	}
	after, _ := s.CardStates()
	if len(after) != 1 || after[0] != before[0] {
		t.Fatalf("state differs:\n%+v\n%+v", before, after)
	}
}

func TestProjectsAndTagsSuggestions(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols := mustCols(t, s, b.ID)
	for _, p := range []string{"acme", "acme", "juridico"} {
		s.AddCardWith(Card{ColumnID: cols[0].ID, Project: p, Title: "t", Tags: "go, api"})
	}
	s.AddCardWith(Card{ColumnID: cols[0].ID, Title: "t", Tags: "go"})
	ps, _ := s.Projects()
	if strings.Join(ps, ",") != "acme,juridico" {
		t.Fatalf("projects by usage: %v", ps)
	}
	ts, _ := s.Tags()
	if strings.Join(ts, ",") != "go,api" {
		t.Fatalf("tags by usage: %v", ts)
	}
}

func TestBackupKeepsNewestAndHonorsMinAge(t *testing.T) {
	s := openTemp(t)
	s.CreateBoard("Precious")
	dir := t.TempDir()
	p1, made, err := s.Backup(dir, 2, time.Hour)
	if err != nil || !made {
		t.Fatalf("first backup: %v %v", made, err)
	}
	if _, made, _ := s.Backup(dir, 2, time.Hour); made {
		t.Fatal("a backup younger than minAge should be skipped")
	}
	// the copy is a real database with the data
	db, err := sql.Open("sqlite", p1)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM boards WHERE name='Precious'`).Scan(&n)
	db.Close()
	if n != 1 {
		t.Fatal("backup should contain the data")
	}
	for i := 0; i < 3; i++ { // force new ones with distinct names
		time.Sleep(1100 * time.Millisecond)
		s.Backup(dir, 2, 0)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "kbn-*.db"))
	if len(files) != 2 {
		t.Fatalf("should keep the 2 newest backups, have %d", len(files))
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("Work")
	cols := mustCols(t, s, b.ID)
	s.SetWIPLimit(cols[1].ID, 3)
	s.AddCardWith(Card{ColumnID: cols[0].ID, Project: "Acme", Title: "Ship", Description: "notes\n[ ] a\n  [x] nested", Priority: 3, Due: "2030-05-01", Tags: "go", Focus: true})

	e, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	var js bytes.Buffer
	if err := e.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	back, err := ReadExport(&js)
	if err != nil {
		t.Fatal(err)
	}

	other := openTemp(t)
	boards, cards, err := other.Import(back)
	if err != nil || boards != 1 || cards != 1 {
		t.Fatalf("import = %d boards, %d cards, %v", boards, cards, err)
	}
	if _, _, err := other.Import(back); err != nil { // importing twice never overwrites
		t.Fatal(err)
	}
	bs, _ := other.Boards()
	if len(bs) != 2 || bs[1].Name != "Work (imported)" {
		t.Fatalf("name clash should be suffixed: %+v", bs)
	}
	cs := mustCols(t, other, bs[0].ID)
	got, _ := other.Cards(cs[0].ID)
	if len(got) != 1 || got[0].Title != "Ship" || got[0].Priority != 3 || !got[0].Focus || got[0].Description != "notes\n[ ] a\n  [x] nested" {
		t.Fatalf("card lost data: %+v", got)
	}
	if cs[1].WIPLimit != 3 {
		t.Fatal("WIP limit should be exported")
	}

	var md bytes.Buffer
	e.WriteMarkdown(&md)
	for _, want := range []string{"# Work", "## To do (1)", "### ★ [Acme] Ship", "priority: high", "- [ ] a", "  - [x] nested"} {
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown missing %q:\n%s", want, md.String())
		}
	}
	if _, err := ReadExport(strings.NewReader(`{"version":99}`)); err == nil {
		t.Fatal("unknown version should be rejected")
	}
}

func TestMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, _ := sql.Open("sqlite", path)
	_, err := db.Exec(`
		CREATE TABLE boards (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE columns (id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL, name TEXT NOT NULL, position INTEGER NOT NULL);
		CREATE TABLE cards (id INTEGER PRIMARY KEY, column_id INTEGER NOT NULL, title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '', priority INTEGER NOT NULL DEFAULT 0, due TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO boards VALUES (1,'Old');
		INSERT INTO columns VALUES (1,1,'A fazer',0),(2,1,'Feito',1);
		INSERT INTO cards(id,column_id,title,position) VALUES (1,1,'todo',0),(2,2,'finished',0);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	defer s.Close()
	cols := mustCols(t, s, 1)
	if cols[0].Name != "To do" || cols[1].WIPLimit != 0 {
		t.Fatalf("columns not migrated: %+v", cols)
	}
	done, _ := s.Cards(2)
	todo, _ := s.Cards(1)
	if done[0].DoneAt == "" || todo[0].DoneAt != "" {
		t.Fatalf("done_at should start the clock only for already-done cards: %+v / %+v", done, todo)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
