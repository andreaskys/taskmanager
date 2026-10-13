package store

// The undo log lives in the database so undo/redo survive closing the app.
// Payloads are opaque strings (JSON built by the UI).

const (
	StackUndo = "undo"
	StackRedo = "redo"
	maxUndo   = 200
)

type UndoEntry struct {
	ID      int64
	Desc    string
	Payload string
}

func (s *Store) UndoPush(stack, desc, payload string) error {
	if _, err := s.db.Exec(`INSERT INTO undo_log(stack, desc, payload) VALUES(?,?,?)`, stack, desc, payload); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM undo_log WHERE stack=? AND id NOT IN (SELECT id FROM undo_log WHERE stack=? ORDER BY id DESC LIMIT ?)`, stack, stack, maxUndo)
	return err
}

// UndoPop removes and returns the newest entry of a stack.
func (s *Store) UndoPop(stack string) (UndoEntry, bool, error) {
	var e UndoEntry
	err := s.db.QueryRow(`SELECT id, desc, payload FROM undo_log WHERE stack=? ORDER BY id DESC LIMIT 1`, stack).Scan(&e.ID, &e.Desc, &e.Payload)
	if err != nil {
		return e, false, nil // empty stack
	}
	_, err = s.db.Exec(`DELETE FROM undo_log WHERE id=?`, e.ID)
	return e, err == nil, err
}

func (s *Store) UndoClear(stack string) error {
	_, err := s.db.Exec(`DELETE FROM undo_log WHERE stack=?`, stack)
	return err
}

func (s *Store) UndoCount(stack string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM undo_log WHERE stack=?`, stack).Scan(&n)
	return n
}
