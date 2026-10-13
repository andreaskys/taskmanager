package store

import (
	"path/filepath"
	"testing"
)

func TestBasicFlow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	b, err := s.CreateBoard("Teste")
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := s.Columns(b.ID)
	if len(cols) != 4 {
		t.Fatalf("want 4 columns, got %d", len(cols))
	}
	c1, _ := s.AddCard(cols[0].ID, "p", "one")
	c2, _ := s.AddCard(cols[0].ID, "p", "two")
	if err := s.SwapCards(c1, c2); err != nil {
		t.Fatal(err)
	}
	cards, _ := s.Cards(cols[0].ID)
	if cards[0].Title != "two" {
		t.Fatalf("reorder failed: %v", cards)
	}
	if err := s.MoveCard(c1.ID, cols[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBoard(b.ID); err != nil {
		t.Fatal(err)
	}
}
