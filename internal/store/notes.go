package store

import (
	"fmt"
	"strings"
	"time"
)

// Note is a sticky note: free text kept outside the boards, to remember things.
type Note struct {
	ID        int64
	Title     string // optional short name, shown on the sticky and searched
	Tags      string // comma separated, searched with #tag
	Body      string
	Color     int    // index into the theme's color palette
	Pinned    bool   // pinned notes are listed first
	Due       string // optional reminder date, YYYY-MM-DD
	CreatedAt time.Time
	UpdatedAt time.Time
}

const noteCols = `id, title, tags, body, color, pinned, due, created_at, updated_at`

func scanNote(sc scanner) (Note, error) {
	var n Note
	var created, updated string
	if err := sc.Scan(&n.ID, &n.Title, &n.Tags, &n.Body, &n.Color, &n.Pinned, &n.Due, &created, &updated); err != nil {
		return n, err
	}
	n.CreatedAt, _ = time.Parse(tsLayout, created)
	n.UpdatedAt, _ = time.Parse(tsLayout, updated)
	return n, nil
}

// Notes lists the notes: pinned first, then the most recently edited.
func (s *Store) Notes() ([]Note, error) {
	rows, err := s.db.Query(`SELECT ` + noteCols + ` FROM notes ORDER BY pinned DESC, updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NormTags cleans a comma/space separated tag list: no '#', lowercase, no duplicates, joined by commas.
func NormTags(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		t = strings.ToLower(strings.TrimLeft(t, "#"))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// AddNote creates a note; it needs a body or a title.
func (s *Store) AddNote(body string, color int) (Note, error) {
	return s.AddNoteFull(Note{Body: body, Color: color})
}

// AddNoteFull creates a note from n (title, tags, body, color).
func (s *Store) AddNoteFull(n Note) (Note, error) {
	n.Body = strings.TrimRight(n.Body, " \n")
	n.Title = strings.TrimSpace(n.Title)
	n.Tags = NormTags(n.Tags)
	if strings.TrimSpace(n.Body) == "" && n.Title == "" {
		return Note{}, fmt.Errorf("note is empty")
	}
	res, err := s.db.Exec(`INSERT INTO notes(title, tags, body, color, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		n.Title, n.Tags, n.Body, n.Color, now(), now())
	if err != nil {
		return Note{}, err
	}
	n.ID, _ = res.LastInsertId()
	s.db.Exec(`DELETE FROM attachments WHERE kind='note' AND owner_id=?`, n.ID) // leftovers of a deleted note that had this id
	return n, nil
}

// UpdateNote saves title, tags, body, color, pin and reminder date, and marks the note as edited now.
func (s *Store) UpdateNote(n Note) error {
	if strings.TrimSpace(n.Body) == "" && strings.TrimSpace(n.Title) == "" {
		return fmt.Errorf("note is empty")
	}
	_, err := s.db.Exec(`UPDATE notes SET title=?, tags=?, body=?, color=?, pinned=?, due=?, updated_at=? WHERE id=?`,
		strings.TrimSpace(n.Title), NormTags(n.Tags), strings.TrimRight(n.Body, " \n"), n.Color, n.Pinned, n.Due, now(), n.ID)
	return err
}

// ApplyNote writes the complete state of a note, inserting it if needed (used to undo a delete).
func (s *Store) ApplyNote(n Note) error {
	created, updated := n.CreatedAt, n.UpdatedAt
	if created.IsZero() {
		created = time.Now()
	}
	if updated.IsZero() {
		updated = created
	}
	_, err := s.db.Exec(`INSERT INTO notes(id, title, tags, body, color, pinned, due, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, tags=excluded.tags, body=excluded.body, color=excluded.color, pinned=excluded.pinned, due=excluded.due,
			updated_at=excluded.updated_at`,
		n.ID, n.Title, n.Tags, n.Body, n.Color, n.Pinned, n.Due, created.UTC().Format(tsLayout), updated.UTC().Format(tsLayout))
	return err
}

func (s *Store) DeleteNote(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notes WHERE id=?`, id)
	return err // attachments stay: undoing the delete brings the note (same id) back with them
}

// NotesDue counts the notes whose reminder date is today or earlier.
func (s *Store) NotesDue(today string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE due != '' AND due <= ?`, today).Scan(&n)
	return n
}
