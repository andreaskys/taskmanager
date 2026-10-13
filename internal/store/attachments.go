package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Attachment is a link (URL) or a file path attached to a card or a note.
type Attachment struct {
	ID      int64
	Kind    string // "card" or "note"
	OwnerID int64
	Target  string
	Label   string
}

// AddAttachment attaches a URL or file path; the label defaults to the file name or the URL.
func (s *Store) AddAttachment(kind string, ownerID int64, target, label string) (Attachment, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return Attachment{}, fmt.Errorf("attachment is empty")
	}
	if label = strings.TrimSpace(label); label == "" {
		label = target
		if !strings.Contains(target, "://") {
			label = filepath.Base(target)
		}
	}
	res, err := s.db.Exec(`INSERT INTO attachments(kind, owner_id, target, label, created_at) VALUES(?,?,?,?,?)`,
		kind, ownerID, target, label, now())
	if err != nil {
		return Attachment{}, err
	}
	id, _ := res.LastInsertId()
	return Attachment{ID: id, Kind: kind, OwnerID: ownerID, Target: target, Label: label}, nil
}

func (s *Store) Attachments(kind string, ownerID int64) ([]Attachment, error) {
	rows, err := s.db.Query(`SELECT id, kind, owner_id, target, label FROM attachments WHERE kind=? AND owner_id=? ORDER BY id`, kind, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.Kind, &a.OwnerID, &a.Target, &a.Label); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAttachment(id int64) error {
	_, err := s.db.Exec(`DELETE FROM attachments WHERE id=?`, id)
	return err
}

// AttachmentCounts returns how many attachments each owner of a kind has.
func (s *Store) AttachmentCounts(kind string) map[int64]int {
	out := map[int64]int{}
	rows, err := s.db.Query(`SELECT owner_id, COUNT(*) FROM attachments WHERE kind=? GROUP BY owner_id`, kind)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}
