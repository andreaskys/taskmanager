package store

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestJournal(t *testing.T) {
	s := openTemp(t)
	if _, ok := s.Journal("2026-05-01"); ok {
		t.Fatal("no entry yet")
	}
	for _, d := range []string{"2026-05-01", "2026-05-03", "2026-05-07"} {
		if err := s.SaveJournal(d, "dia "+d+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if e, ok := s.Journal("2026-05-03"); !ok || e.Body != "dia 2026-05-03" {
		t.Fatalf("entry not saved/trimmed: %+v", e)
	}
	if d, ok := s.JournalNeighbor("2026-05-03", 1); !ok || d != "2026-05-07" {
		t.Fatalf("next entry: %q", d)
	}
	if d, ok := s.JournalNeighbor("2026-05-03", -1); !ok || d != "2026-05-01" {
		t.Fatalf("previous entry: %q", d)
	}
	if _, ok := s.JournalNeighbor("2026-05-07", 1); ok {
		t.Fatal("nothing after the last entry")
	}
	s.SaveJournal("2026-05-03", "  \n")
	if all, _ := s.JournalEntries(); len(all) != 2 || all[0].Day != "2026-05-07" {
		t.Fatalf("empty text removes the entry, newest first: %+v", all)
	}
}

func TestCardHistoryAndSummary(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols, _ := s.Columns(b.ID) // To do, Doing, Testing, Done
	c, _ := s.AddCard(cols[0].ID, "Acme", "Fix login")
	s.MoveCard(c.ID, cols[1].ID)
	s.MoveCard(c.ID, cols[1].ID) // same column: no event
	s.MoveCardTo(c.ID, cols[3].ID, 0)
	hist, _ := s.CardHistory(c.ID)
	var kinds []string
	for _, e := range hist {
		kinds = append(kinds, e.Kind+":"+e.From+">"+e.To)
	}
	if got := strings.Join(kinds, " "); got != "created:>To do moved:To do>Doing done:Doing>Done" {
		t.Fatalf("history = %s", got)
	}
	today := time.Now().Format("2006-01-02")
	if ev, _ := s.EventsOn(today); len(ev) != 3 {
		t.Fatalf("events of today: %d", len(ev))
	}
	sum, err := s.Summarize(today, today)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Created != 1 || sum.Done != 1 || sum.Moves != 2 || sum.Projects["Acme"] != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if md := sum.Markdown(); !strings.Contains(md, "[Acme] Fix login") {
		t.Fatalf("markdown missing the done card:\n%s", md)
	}
	// undo (ApplyCard) back to Doing is logged as a move; deleting keeps the history readable
	back := c
	back.ColumnID = cols[1].ID
	back.DoneAt = ""
	s.ApplyCard(back)
	s.DeleteCard(c.ID)
	hist, _ = s.CardHistory(c.ID)
	if last := hist[len(hist)-1]; last.Kind != EvDeleted || last.Title != "Fix login" {
		t.Fatalf("delete should be logged with the title: %+v", last)
	}
}

func TestAttachmentsAndDiaryExport(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols, _ := s.Columns(b.ID)
	c, _ := s.AddCard(cols[0].ID, "", "Contract")
	a, err := s.AddAttachment("card", c.ID, "/home/x/docs/contract.pdf", "")
	if err != nil || a.Label != "contract.pdf" {
		t.Fatalf("label defaults to the file name: %+v %v", a, err)
	}
	s.AddAttachment("card", c.ID, "https://example.com/spec", "Spec")
	if list, _ := s.Attachments("card", c.ID); len(list) != 2 || list[1].Label != "Spec" {
		t.Fatalf("attachments: %+v", list)
	}
	if s.AttachmentCounts("card")[c.ID] != 2 {
		t.Fatal("count")
	}
	s.DeleteAttachment(a.ID)
	if list, _ := s.Attachments("card", c.ID); len(list) != 1 {
		t.Fatalf("delete: %+v", list)
	}
	if _, err := s.AddAttachment("card", c.ID, "  ", ""); err == nil {
		t.Fatal("empty target must fail")
	}

	s.SaveJournal("2026-05-01", "[ ] ligar")
	e, _ := s.Export()
	var buf bytes.Buffer
	e.WriteJSON(&buf)
	e2, err := ReadExport(&buf)
	if err != nil || len(e2.Journal) != 1 {
		t.Fatalf("journal should round-trip: %v %+v", err, e2.Journal)
	}
	o := openTemp(t)
	o.SaveJournal("2026-05-01", "texto existente")
	if _, _, err := o.Import(e2); err != nil {
		t.Fatal(err)
	}
	if got, _ := o.Journal("2026-05-01"); got.Body != "texto existente" {
		t.Fatalf("import must not overwrite a day: %q", got.Body)
	}
	var md bytes.Buffer
	e2.WriteMarkdown(&md)
	if !strings.Contains(md.String(), "# Diary") || !strings.Contains(md.String(), "- [ ] ligar") {
		t.Fatalf("markdown diary:\n%s", md.String())
	}
}
