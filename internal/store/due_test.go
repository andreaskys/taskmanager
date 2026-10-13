package store

import (
	"path/filepath"
	"testing"
)

func TestDueCardsAndRestore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, _ := s.CreateBoard("B")
	cols, _ := s.Columns(b.ID)
	a, _ := s.AddCard(cols[0].ID, "p", "late")
	a.Due = "2000-01-01"
	s.UpdateCard(a)
	d, _ := s.AddCard(cols[3].ID, "p", "done one") // last column is excluded
	d.Due = "2000-01-01"
	s.UpdateCard(d)

	due, err := s.DueCards("2100-01-01")
	if err != nil || len(due) != 1 || due[0].Title != "late" || due[0].DoneColumnID != cols[3].ID || due[0].BoardName != "B" {
		t.Fatalf("DueCards = %+v, err %v", due, err)
	}

	cards, _ := s.Cards(cols[0].ID)
	if err := s.DeleteCard(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyCard(cards[0]); err != nil {
		t.Fatal(err)
	}
	back, _ := s.Cards(cols[0].ID)
	if len(back) != 1 || back[0].ID != a.ID || back[0].Due != "2000-01-01" {
		t.Fatalf("restore lost data: %+v", back)
	}

	if s.Setting("theme") != "" {
		t.Fatal("unset setting should be empty")
	}
	s.SetSetting("theme", "monokai")
	s.SetSetting("theme", "default")
	if s.Setting("theme") != "default" {
		t.Fatal("setting upsert failed")
	}
}
