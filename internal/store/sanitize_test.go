package store

import (
	"strings"
	"testing"
)

func TestCleanTextDropsTerminalEscapesAndBidiControls(t *testing.T) {
	evil := "ok\x1b]0;pwned\x07\x1b[31mred\x1b[0m‮evil‬ \x9b2J end\r\nnext\tline\x7f"
	line := CleanLine(evil)
	for _, bad := range []string{"\x1b", "\x07", "\x9b", "\x7f", "‮", "\r", "\n", "\t"} {
		if strings.Contains(line, bad) {
			t.Fatalf("CleanLine kept %q: %q", bad, line)
		}
	}
	if !strings.Contains(line, "ok") || !strings.Contains(line, "end") {
		t.Fatalf("the readable text must survive: %q", line)
	}
	text := CleanText(evil)
	if strings.ContainsAny(text, "\x1b\x07\x9b\x7f\r") || strings.Contains(text, "‮") {
		t.Fatalf("CleanText kept a control character: %q", text)
	}
	if !strings.Contains(text, "\n") || !strings.Contains(text, "\t") {
		t.Fatalf("newline and tab are legitimate in a text: %q", text)
	}
}

func TestStoreNeverKeepsControlCharactersOrHugeTexts(t *testing.T) {
	s := openTemp(t)
	b, _ := s.CreateBoard("B")
	cols, _ := s.Columns(b.ID)
	c, err := s.AddCardWith(Card{ColumnID: cols[0].ID, Title: "T\x1b[2Jitle", Project: "P\x1b]52;c;xx\x07", Description: "d\x1b[31m\nline", Tags: "a\x1bb"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.CardByID(c.ID)
	for _, f := range []string{got.Title, got.Project, got.Description, got.Tags} {
		if strings.Contains(f, "\x1b") {
			t.Fatalf("an escape sequence reached the database: %q", f)
		}
	}
	got.Title = "x\x1b[0mx"
	s.UpdateCard(got.Card)
	if g, _ := s.CardByID(c.ID); strings.Contains(g.Title, "\x1b") {
		t.Fatalf("UpdateCard must sanitize: %q", g.Title)
	}
	if _, err := s.AddCardWith(Card{ColumnID: cols[0].ID, Title: strings.Repeat("a", MaxLine+1)}); err == nil {
		t.Fatal("a title over the limit must be refused")
	}
	if _, err := s.AddNoteFull(Note{Body: strings.Repeat("a", MaxText+1)}); err == nil {
		t.Fatal("a note over the limit must be refused")
	}
	if err := s.SaveJournal("2026-01-01", strings.Repeat("a", MaxText+1)); err == nil {
		t.Fatal("a diary entry over the limit must be refused")
	}
	n, err := s.AddNoteFull(Note{Title: "n\x1b[1m", Body: "b\x1b[1m", Color: -5})
	if err != nil || strings.Contains(n.Title+n.Body, "\x1b") || n.Color != 0 {
		t.Fatalf("note: %+v %v", n, err)
	}
	if _, err := s.AddAttachment("card", c.ID, "https://x.y/\x1b[2J", "la\x07bel"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Attachments("card", c.ID); strings.ContainsAny(list[0].Target+list[0].Label, "\x1b\x07") {
		t.Fatalf("attachment kept control characters: %+v", list[0])
	}
	// an import file is as untrusted as the API
	e := Export{Version: 1, Boards: []ExportBoard{{Name: "I", Columns: []ExportColumn{{Name: "C", Cards: []ExportCard{{Title: "i\x1b[2Jmp", Description: "d\x1b[0m"}}}}}},
		Notes: []ExportNote{{Title: "n\x1b[0m", Body: "b", Color: -9}}, Journal: []ExportJournal{{Day: "2026-02-02", Body: "j\x1b[0m"}}}
	if _, _, err := s.Import(e); err != nil {
		t.Fatal(err)
	}
	all, _ := s.AllCards()
	for _, d := range all {
		if strings.Contains(d.Title+d.Description, "\x1b") {
			t.Fatalf("import kept an escape: %+v", d.Card)
		}
	}
	if j, _ := s.Journal("2026-02-02"); strings.Contains(j.Body, "\x1b") {
		t.Fatalf("import kept an escape in the diary: %q", j.Body)
	}
	notes, _ := s.Notes()
	for _, nn := range notes {
		if nn.Color < 0 || strings.Contains(nn.Title, "\x1b") {
			t.Fatalf("import kept a bad note: %+v", nn)
		}
	}
}
