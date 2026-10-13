package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const tsLayout = "2006-01-02 15:04:05"

func now() string { return time.Now().UTC().Format(tsLayout) }

const cardCols = `id, column_id, project, title, description, priority, due, tags, position, focus, done_at, archived_at, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanCard(sc scanner, extra ...any) (Card, error) {
	var c Card
	var created string
	dest := append([]any{&c.ID, &c.ColumnID, &c.Project, &c.Title, &c.Description, &c.Priority, &c.Due, &c.Tags, &c.Position,
		&c.Focus, &c.DoneAt, &c.ArchivedAt, &created}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return c, err
	}
	c.CreatedAt, _ = time.Parse(tsLayout, created)
	return c, nil
}

func (s *Store) queryCards(q string, args ...any) ([]Card, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Cards returns the visible (not archived) cards of a column, in board order.
func (s *Store) Cards(columnID int64) ([]Card, error) {
	return s.queryCards(`SELECT `+cardCols+` FROM cards WHERE column_id=? AND archived_at='' ORDER BY position, id`, columnID)
}

// CardStates returns every card, archived ones included. Used to diff states for undo/redo.
func (s *Store) CardStates() ([]Card, error) {
	return s.queryCards(`SELECT ` + cardCols + ` FROM cards ORDER BY id`)
}

func (s *Store) doneAtFor(columnID int64, current string) string {
	if !s.isLastColumn(columnID) {
		return ""
	}
	if current != "" {
		return current
	}
	return now()
}

// AddCardWith creates a card at the end of its column, keeping every field of c.
func (s *Store) AddCardWith(c Card) (Card, error) {
	c.Title = strings.TrimSpace(c.Title)
	c.Project = strings.TrimSpace(c.Project)
	if c.Title == "" {
		return Card{}, fmt.Errorf("title is empty")
	}
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM cards WHERE column_id=?`, c.ColumnID).Scan(&c.Position); err != nil {
		return Card{}, err
	}
	c.DoneAt = s.doneAtFor(c.ColumnID, "")
	res, err := s.db.Exec(`INSERT INTO cards(column_id, project, title, description, priority, due, tags, position, focus, done_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ColumnID, c.Project, c.Title, c.Description, c.Priority, c.Due, c.Tags, c.Position, c.Focus, c.DoneAt)
	if err != nil {
		return Card{}, err
	}
	c.ID, _ = res.LastInsertId()
	s.db.Exec(`DELETE FROM attachments WHERE kind='card' AND owner_id=?`, c.ID) // leftovers of a deleted card that had this id
	s.logEvent(c.ID, EvCreated, "")
	return c, nil
}

func (s *Store) AddCard(columnID int64, project, title string) (Card, error) {
	return s.AddCardWith(Card{ColumnID: columnID, Project: project, Title: title})
}

// UpdateCard saves project, title, description, priority, due date, tags and focus.
func (s *Store) UpdateCard(c Card) error {
	_, err := s.db.Exec(`UPDATE cards SET project=?, title=?, description=?, priority=?, due=?, tags=?, focus=? WHERE id=?`,
		c.Project, c.Title, c.Description, c.Priority, c.Due, c.Tags, c.Focus, c.ID)
	return err
}

// ApplyCard writes the complete state of a card (inserting it if it does not exist).
// Undo and redo use it to restore any previous state exactly.
func (s *Store) ApplyCard(c Card) error {
	var prevCol int64
	existed := s.db.QueryRow(`SELECT column_id FROM cards WHERE id=?`, c.ID).Scan(&prevCol) == nil
	prevName := s.columnOf(c.ID)
	created := c.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO cards(id, column_id, project, title, description, priority, due, tags, position, focus, done_at, archived_at, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET column_id=excluded.column_id, project=excluded.project, title=excluded.title,
			description=excluded.description, priority=excluded.priority, due=excluded.due, tags=excluded.tags,
			position=excluded.position, focus=excluded.focus, done_at=excluded.done_at, archived_at=excluded.archived_at`,
		c.ID, c.ColumnID, c.Project, c.Title, c.Description, c.Priority, c.Due, c.Tags, c.Position, c.Focus, c.DoneAt, c.ArchivedAt,
		created.UTC().Format(tsLayout))
	if err == nil {
		switch {
		case !existed:
			s.logEvent(c.ID, EvRestored, "")
		case prevCol != c.ColumnID:
			s.logMove(c.ID, prevName)
		}
	}
	return err
}

// MoveCard moves the card to the end of another column.
func (s *Store) MoveCard(cardID, toColumnID int64) error {
	from := s.columnOf(cardID)
	var cur string
	s.db.QueryRow(`SELECT done_at FROM cards WHERE id=?`, cardID).Scan(&cur)
	_, err := s.db.Exec(`UPDATE cards SET column_id=?, done_at=?,
		position=(SELECT COALESCE(MAX(position)+1, 0) FROM cards WHERE column_id=?) WHERE id=?`,
		toColumnID, s.doneAtFor(toColumnID, cur), toColumnID, cardID)
	if err == nil {
		s.logMove(cardID, from)
	}
	return err
}

// MoveCardTo moves the card into a column at a given index (0 = top), renumbering the positions
// of the column. index is relative to the column's visible cards without the moved card.
func (s *Store) MoveCardTo(cardID, toColumnID int64, index int) error {
	cards, err := s.Cards(toColumnID)
	if err != nil {
		return err
	}
	var ids []int64
	for _, c := range cards {
		if c.ID != cardID {
			ids = append(ids, c.ID)
		}
	}
	index = max(0, min(index, len(ids)))
	ids = append(ids[:index], append([]int64{cardID}, ids[index:]...)...)

	from := s.columnOf(cardID)
	var cur string
	s.db.QueryRow(`SELECT done_at FROM cards WHERE id=?`, cardID).Scan(&cur)
	doneAt := s.doneAtFor(toColumnID, cur) // computed before the transaction: the pool has a single connection
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE cards SET column_id=?, done_at=? WHERE id=?`, toColumnID, doneAt, cardID); err != nil {
		return err
	}
	for pos, id := range ids {
		if _, err := tx.Exec(`UPDATE cards SET position=? WHERE id=?`, pos, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.logMove(cardID, from)
	return nil
}

// SwapCards swaps the position of two cards in the same column (reorder).
func (s *Store) SwapCards(a, b Card) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE cards SET position=? WHERE id=?`, b.Position, a.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE cards SET position=? WHERE id=?`, a.Position, b.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteCard(id int64) error {
	s.logEvent(id, EvDeleted, s.columnOf(id))
	_, err := s.db.Exec(`DELETE FROM cards WHERE id=?`, id)
	return err
}

// --- archive ---

// ArchiveDone archives the cards of every board's Done column that were completed more than
// `olderThan` ago (0 archives all of them). It returns how many cards were archived.
func (s *Store) ArchiveDone(olderThan time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(tsLayout)
	var ids []int64
	if rows, err := s.db.Query(`SELECT id FROM cards WHERE archived_at='' AND done_at != '' AND done_at <= ?
		AND column_id IN (SELECT c.id FROM columns c WHERE c.position = (SELECT MAX(position) FROM columns WHERE board_id=c.board_id))`, cutoff); err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
	}
	res, err := s.db.Exec(`UPDATE cards SET archived_at=?, focus=0 WHERE archived_at='' AND done_at != '' AND done_at <= ?
		AND column_id IN (SELECT c.id FROM columns c WHERE c.position = (SELECT MAX(position) FROM columns WHERE board_id=c.board_id))`,
		now(), cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, id := range ids {
		s.logEvent(id, EvArchived, "")
	}
	return int(n), nil
}

// Unarchive brings a card back to its column and restarts its archive clock.
func (s *Store) Unarchive(id int64) error {
	_, err := s.db.Exec(`UPDATE cards SET archived_at='', done_at=? WHERE id=?`, now(), id)
	if err == nil {
		s.logEvent(id, EvUnarchived, "")
	}
	return err
}

// --- cross-board queries ---

// DueCard is a card plus where it lives, for the cross-board views (today, focus, search).
type DueCard struct {
	Card
	BoardID      int64
	BoardName    string
	ColumnName   string
	DoneColumnID int64 // last column of the card's board
}

const dueSelect = `SELECT c.id, c.column_id, c.project, c.title, c.description, c.priority, c.due, c.tags, c.position, c.focus, c.done_at, c.archived_at, c.created_at,
		b.id, b.name, col.name,
		(SELECT id FROM columns WHERE board_id=b.id ORDER BY position DESC LIMIT 1)
	FROM cards c
	JOIN columns col ON col.id = c.column_id
	JOIN boards b ON b.id = col.board_id `

const notDone = `col.id != (SELECT id FROM columns WHERE board_id=b.id ORDER BY position DESC LIMIT 1)`

func (s *Store) queryDue(tail string, args ...any) ([]DueCard, error) {
	rows, err := s.db.Query(dueSelect+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueCard
	for rows.Next() {
		var d DueCard
		c, err := scanCard(rows, &d.BoardID, &d.BoardName, &d.ColumnName, &d.DoneColumnID)
		if err != nil {
			return nil, err
		}
		d.Card = c
		out = append(out, d)
	}
	return out, rows.Err()
}

// DueCards returns every open card with a due date up to and including `until` (YYYY-MM-DD), across all boards.
func (s *Store) DueCards(until string) ([]DueCard, error) {
	return s.queryDue(`WHERE c.due != '' AND c.due <= ? AND c.archived_at='' AND `+notDone+`
		ORDER BY c.due, c.priority DESC, c.id`, until)
}

// FocusCards returns the open cards pinned as focus of the day, across all boards.
func (s *Store) FocusCards() ([]DueCard, error) {
	return s.queryDue(`WHERE c.focus=1 AND c.archived_at='' AND ` + notDone + ` ORDER BY b.id, col.position, c.position, c.id`)
}

// AllCards returns every card of every board, archived included, for global search.
func (s *Store) AllCards() ([]DueCard, error) {
	return s.queryDue(`ORDER BY c.archived_at != '', b.id, col.position, c.position, c.id`)
}

// --- suggestions ---

// Projects lists the projects already used, most used first (then most recent).
func (s *Store) Projects() ([]string, error) {
	rows, err := s.db.Query(`SELECT project FROM cards WHERE project != '' GROUP BY project ORDER BY COUNT(*) DESC, MAX(id) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Tags lists the tags already used, most used first.
func (s *Store) Tags() ([]string, error) {
	rows, err := s.db.Query(`SELECT tags FROM cards WHERE tags != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := map[string]int{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, tag := range strings.Split(t, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				count[tag]++
			}
		}
	}
	out := make([]string, 0, len(count))
	for t := range count {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return out[i] < out[j]
	})
	return out, rows.Err()
}

// --- settings ---

// Setting returns a saved UI preference ("" if unset).
func (s *Store) Setting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil && err != sql.ErrNoRows {
		return ""
	}
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// CardByID finds a card (archived included) together with where it lives.
func (s *Store) CardByID(id int64) (DueCard, bool) {
	list, err := s.queryDue(`WHERE c.id=?`, id)
	if err != nil || len(list) == 0 {
		return DueCard{}, false
	}
	return list[0], true
}
