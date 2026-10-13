package store

import (
	"strings"
	"time"
)

// JournalEntry is the free text written for one day (Day is the local date, YYYY-MM-DD).
type JournalEntry struct {
	Day       string
	Body      string
	UpdatedAt time.Time
}

// Journal returns the entry of a day (ok is false when nothing was written).
func (s *Store) Journal(day string) (JournalEntry, bool) {
	var e JournalEntry
	var updated string
	err := s.db.QueryRow(`SELECT day, body, updated_at FROM journal WHERE day=?`, day).Scan(&e.Day, &e.Body, &updated)
	if err != nil {
		return JournalEntry{Day: day}, false
	}
	e.UpdatedAt, _ = time.Parse(tsLayout, updated)
	return e, true
}

// SaveJournal writes the text of a day; an empty text removes the entry.
func (s *Store) SaveJournal(day, body string) error {
	body = strings.TrimRight(CleanText(body), " \n")
	if err := tooLong("diary entry", body, MaxText); err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		_, err := s.db.Exec(`DELETE FROM journal WHERE day=?`, day)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO journal(day, body, updated_at) VALUES(?,?,?)
		ON CONFLICT(day) DO UPDATE SET body=excluded.body, updated_at=excluded.updated_at`, day, body, now())
	return err
}

// JournalEntries lists every entry, newest day first (for global search).
func (s *Store) JournalEntries() ([]JournalEntry, error) {
	rows, err := s.db.Query(`SELECT day, body, updated_at FROM journal ORDER BY day DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var updated string
		if err := rows.Scan(&e.Day, &e.Body, &updated); err != nil {
			return nil, err
		}
		e.UpdatedAt, _ = time.Parse(tsLayout, updated)
		out = append(out, e)
	}
	return out, rows.Err()
}

// JournalNeighbor returns the closest day with an entry before (dir < 0) or after (dir > 0) day.
func (s *Store) JournalNeighbor(day string, dir int) (string, bool) {
	q := `SELECT day FROM journal WHERE day > ? ORDER BY day ASC LIMIT 1`
	if dir < 0 {
		q = `SELECT day FROM journal WHERE day < ? ORDER BY day DESC LIMIT 1`
	}
	var d string
	if err := s.db.QueryRow(q, day).Scan(&d); err != nil {
		return "", false
	}
	return d, true
}

// DayRange converts a local date into the UTC timestamps [from, to) that bound it.
func DayRange(day string) (from, to string, err error) {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return "", "", err
	}
	return t.UTC().Format(tsLayout), t.AddDate(0, 0, 1).UTC().Format(tsLayout), nil
}
