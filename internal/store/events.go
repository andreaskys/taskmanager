package store

import (
	"fmt"
	"time"
)

// Card history: every creation, move, completion, archive and deletion is logged with a snapshot of
// the names involved, so the history stays readable after boards, columns or cards are renamed or deleted.
// Logging is best effort: a failure to log never fails the change itself.

const (
	EvCreated    = "created"
	EvMoved      = "moved"
	EvDone       = "done"
	EvArchived   = "archived"
	EvUnarchived = "unarchived"
	EvDeleted    = "deleted"
	EvRestored   = "restored" // brought back by undo
)

type CardEvent struct {
	ID      int64
	CardID  int64
	Kind    string
	Title   string
	Project string
	Board   string
	From    string // column the card left (moves)
	To      string // column the card is in after the event
	At      time.Time
}

// cardSnapshot reads the names that describe a card right now.
func (s *Store) cardSnapshot(id int64) (title, project, column, board string, ok bool) {
	err := s.db.QueryRow(`SELECT c.title, c.project, col.name, b.name FROM cards c
		JOIN columns col ON col.id = c.column_id JOIN boards b ON b.id = col.board_id WHERE c.id=?`, id).
		Scan(&title, &project, &column, &board)
	return title, project, column, board, err == nil
}

// columnOf returns the name of the column a card is in ("" when it does not exist).
func (s *Store) columnOf(cardID int64) string {
	_, _, col, _, _ := s.cardSnapshot(cardID)
	return col
}

// logEvent records an event for a card using its current names (call it before deleting a card).
func (s *Store) logEvent(cardID int64, kind, from string) {
	title, project, col, board, ok := s.cardSnapshot(cardID)
	if !ok {
		return
	}
	s.db.Exec(`INSERT INTO card_events(card_id, kind, title, project, board, from_col, to_col, at) VALUES(?,?,?,?,?,?,?,?)`,
		cardID, kind, title, project, board, from, col, now())
}

// logMove logs a move from the column `from` to wherever the card is now (nothing if it did not change).
func (s *Store) logMove(cardID int64, from string) {
	_, _, to, _, ok := s.cardSnapshot(cardID)
	if !ok || to == from {
		return
	}
	var colID int64
	s.db.QueryRow(`SELECT column_id FROM cards WHERE id=?`, cardID).Scan(&colID)
	kind := EvMoved
	if s.isLastColumn(colID) {
		kind = EvDone
	}
	s.logEvent(cardID, kind, from)
}

const eventCols = `id, card_id, kind, title, project, board, from_col, to_col, at`

func (s *Store) queryEvents(tail string, args ...any) ([]CardEvent, error) {
	rows, err := s.db.Query(`SELECT `+eventCols+` FROM card_events `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardEvent
	for rows.Next() {
		var e CardEvent
		var at string
		if err := rows.Scan(&e.ID, &e.CardID, &e.Kind, &e.Title, &e.Project, &e.Board, &e.From, &e.To, &at); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(tsLayout, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CardHistory lists the events of one card, oldest first.
func (s *Store) CardHistory(cardID int64) ([]CardEvent, error) {
	return s.queryEvents(`WHERE card_id=? ORDER BY id`, cardID)
}

// EventsOn lists the events of a local day, oldest first.
func (s *Store) EventsOn(day string) ([]CardEvent, error) {
	from, to, err := DayRange(day)
	if err != nil {
		return nil, err
	}
	return s.queryEvents(`WHERE at >= ? AND at < ? ORDER BY id`, from, to)
}

// Summary describes a period (From..To, local dates, both inclusive) of work.
type Summary struct {
	From, To  string
	Created   int
	Done      int
	Moves     int
	Overdue   int     // open cards past their due date right now
	AvgDays   float64 // average days from creation to completion of the cards done in the period
	DoneCards []DueCard
	Projects  map[string]int // done cards per project ("" = none)
	Journal   int            // days with a diary entry in the period
}

// Summarize computes the numbers of a period. Created and done come from the cards themselves, so
// they cover the time before the history existed; moves come from the event log.
func (s *Store) Summarize(from, to string) (Summary, error) {
	sum := Summary{From: from, To: to, Projects: map[string]int{}}
	lo, _, err := DayRange(from)
	if err != nil {
		return sum, err
	}
	_, hi, err := DayRange(to)
	if err != nil {
		return sum, err
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM cards WHERE created_at >= ? AND created_at < ?`, lo, hi).Scan(&sum.Created)
	s.db.QueryRow(`SELECT COUNT(*) FROM card_events WHERE kind IN ('moved','done') AND at >= ? AND at < ?`, lo, hi).Scan(&sum.Moves)
	s.db.QueryRow(`SELECT COUNT(*) FROM journal WHERE day >= ? AND day <= ?`, from, to).Scan(&sum.Journal)
	sum.DoneCards, err = s.queryDue(`WHERE c.done_at != '' AND c.done_at >= ? AND c.done_at < ? ORDER BY c.done_at, c.id`, lo, hi)
	if err != nil {
		return sum, err
	}
	sum.Done = len(sum.DoneCards)
	var total float64
	for _, d := range sum.DoneCards {
		sum.Projects[d.Project]++
		if done, err := time.Parse(tsLayout, d.DoneAt); err == nil && !d.CreatedAt.IsZero() {
			total += done.Sub(d.CreatedAt).Hours() / 24
		}
	}
	if sum.Done > 0 {
		sum.AvgDays = total / float64(sum.Done)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM cards c JOIN columns col ON col.id=c.column_id
		WHERE c.due != '' AND c.due < ? AND c.archived_at='' AND `+notDoneCol, time.Now().Format("2006-01-02")).Scan(&sum.Overdue)
	return sum, nil
}

const notDoneCol = `col.id != (SELECT id FROM columns WHERE board_id=col.board_id ORDER BY position DESC LIMIT 1)`

// Markdown renders the summary as text ready to paste in a report or a daily.
func (sm Summary) Markdown() string {
	out := fmt.Sprintf("# Summary %s → %s\n\n", sm.From, sm.To)
	out += fmt.Sprintf("- Done: %d\n- Created: %d\n- Moves between columns: %d\n- Overdue now: %d\n", sm.Done, sm.Created, sm.Moves, sm.Overdue)
	if sm.Done > 0 {
		out += fmt.Sprintf("- Average time to finish: %.1f days\n", sm.AvgDays)
	}
	if sm.Journal > 0 {
		out += fmt.Sprintf("- Diary entries: %d\n", sm.Journal)
	}
	if len(sm.DoneCards) > 0 {
		out += "\n## Done\n\n"
		for _, d := range sm.DoneCards {
			p := ""
			if d.Project != "" {
				p = "[" + d.Project + "] "
			}
			out += fmt.Sprintf("- %s%s (#%d, %s)\n", p, d.Title, d.ID, d.BoardName)
		}
	}
	return out
}
