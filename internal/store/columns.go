package store

import (
	"fmt"
	"strings"
)

// --- columns ---

func (s *Store) Columns(boardID int64) ([]Column, error) {
	rows, err := s.db.Query(`SELECT id, board_id, name, position, wip_limit FROM columns WHERE board_id=? ORDER BY position, id`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Name, &c.Position, &c.WIPLimit); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// isLastColumn reports whether the column is the last (Done) column of its board.
func (s *Store) isLastColumn(columnID int64) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM columns c WHERE c.id=? AND c.position = (SELECT MAX(position) FROM columns WHERE board_id=c.board_id)`, columnID).Scan(&n)
	return n > 0
}

// AddColumn inserts a column just before the last (Done) column, so Done stays last.
func (s *Store) AddColumn(boardID int64, name string) (Column, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Column{}, fmt.Errorf("column name is empty")
	}
	cols, err := s.Columns(boardID)
	if err != nil {
		return Column{}, err
	}
	pos := len(cols)
	if len(cols) > 0 {
		pos = len(cols) - 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Column{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE columns SET position = position + 1 WHERE board_id=? AND position >= ?`, boardID, pos); err != nil {
		return Column{}, err
	}
	res, err := tx.Exec(`INSERT INTO columns(board_id, name, position) VALUES(?,?,?)`, boardID, name, pos)
	if err != nil {
		return Column{}, err
	}
	id, _ := res.LastInsertId()
	return Column{ID: id, BoardID: boardID, Name: name, Position: pos}, tx.Commit()
}

func (s *Store) RenameColumn(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("column name is empty")
	}
	_, err := s.db.Exec(`UPDATE columns SET name=? WHERE id=?`, name, id)
	return err
}

// SetWIPLimit sets the maximum number of cards of a column (0 removes the limit).
func (s *Store) SetWIPLimit(id int64, limit int) error {
	if limit < 0 {
		return fmt.Errorf("limit must be 0 or more")
	}
	_, err := s.db.Exec(`UPDATE columns SET wip_limit=? WHERE id=?`, limit, id)
	return err
}

// MoveColumn swaps the column with its neighbor (dir -1 = left, +1 = right).
func (s *Store) MoveColumn(id int64, dir int) error {
	var boardID int64
	if err := s.db.QueryRow(`SELECT board_id FROM columns WHERE id=?`, id).Scan(&boardID); err != nil {
		return err
	}
	cols, err := s.Columns(boardID)
	if err != nil {
		return err
	}
	for i, c := range cols {
		if c.ID != id {
			continue
		}
		j := i + dir
		if j < 0 || j >= len(cols) {
			return nil
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`UPDATE columns SET position=? WHERE id=?`, j, cols[i].ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE columns SET position=? WHERE id=?`, i, cols[j].ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	return nil
}

// DeleteColumn removes a column; its cards (archived ones too) move to the previous column
// (or the next one when it is the first). A board always keeps at least two columns.
func (s *Store) DeleteColumn(id int64) error {
	var boardID int64
	if err := s.db.QueryRow(`SELECT board_id FROM columns WHERE id=?`, id).Scan(&boardID); err != nil {
		return err
	}
	cols, err := s.Columns(boardID)
	if err != nil {
		return err
	}
	if len(cols) <= 2 {
		return fmt.Errorf("a board needs at least two columns")
	}
	var target int64
	for i, c := range cols {
		if c.ID == id {
			if i > 0 {
				target = cols[i-1].ID
			} else {
				target = cols[i+1].ID
			}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE cards SET column_id=?, position = position + (SELECT COALESCE(MAX(position)+1, 0) FROM cards WHERE column_id=?) WHERE column_id=?`, target, target, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM columns WHERE id=?`, id); err != nil {
		return err
	}
	// close the position gap
	for i, c := range cols {
		if c.ID == id {
			cols = append(cols[:i], cols[i+1:]...)
			break
		}
	}
	for i, c := range cols {
		if _, err := tx.Exec(`UPDATE columns SET position=? WHERE id=?`, i, c.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
