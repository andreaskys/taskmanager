package store

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// The export format is plain JSON so it can be read, diffed and edited by hand.

type ExportCard struct {
	Project     string `json:"project,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    int    `json:"priority,omitempty"`
	Due         string `json:"due,omitempty"`
	Tags        string `json:"tags,omitempty"`
	Focus       bool   `json:"focus,omitempty"`
	DoneAt      string `json:"done_at,omitempty"`
	ArchivedAt  string `json:"archived_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type ExportColumn struct {
	Name     string       `json:"name"`
	WIPLimit int          `json:"wip_limit,omitempty"`
	Cards    []ExportCard `json:"cards"`
}

type ExportBoard struct {
	Name    string         `json:"name"`
	Columns []ExportColumn `json:"columns"`
}

type ExportNote struct {
	Title     string `json:"title,omitempty"`
	Tags      string `json:"tags,omitempty"`
	Body      string `json:"body"`
	Color     int    `json:"color,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	Due       string `json:"due,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type ExportJournal struct {
	Day  string `json:"day"`
	Body string `json:"body"`
}

type Export struct {
	Version    int             `json:"version"`
	ExportedAt string          `json:"exported_at"`
	Boards     []ExportBoard   `json:"boards"`
	Notes      []ExportNote    `json:"notes,omitempty"`
	Journal    []ExportJournal `json:"journal,omitempty"`
}

// Export reads every board, column and card (archived included).
func (s *Store) Export() (Export, error) {
	out := Export{Version: 1, ExportedAt: time.Now().Format(time.RFC3339)}
	notes, err := s.Notes()
	if err != nil {
		return out, err
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, ExportNote{n.Title, n.Tags, n.Body, n.Color, n.Pinned, n.Due, n.CreatedAt.UTC().Format(tsLayout), n.UpdatedAt.UTC().Format(tsLayout)})
	}
	days, err := s.JournalEntries()
	if err != nil {
		return out, err
	}
	for _, d := range days {
		out.Journal = append(out.Journal, ExportJournal{d.Day, d.Body})
	}
	boards, err := s.Boards()
	if err != nil {
		return out, err
	}
	for _, b := range boards {
		eb := ExportBoard{Name: b.Name}
		cols, err := s.Columns(b.ID)
		if err != nil {
			return out, err
		}
		for _, c := range cols {
			cards, err := s.queryCards(`SELECT `+cardCols+` FROM cards WHERE column_id=? ORDER BY position, id`, c.ID)
			if err != nil {
				return out, err
			}
			ec := ExportColumn{Name: c.Name, WIPLimit: c.WIPLimit, Cards: []ExportCard{}}
			for _, k := range cards {
				ec.Cards = append(ec.Cards, ExportCard{k.Project, k.Title, k.Description, k.Priority, k.Due, k.Tags, k.Focus,
					k.DoneAt, k.ArchivedAt, k.CreatedAt.UTC().Format(tsLayout)})
			}
			eb.Columns = append(eb.Columns, ec)
		}
		out.Boards = append(out.Boards, eb)
	}
	return out, nil
}

func (e Export) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(e)
}

// WriteMarkdown renders the boards as readable notes (cards with checkboxes stay checkboxes).
func (e Export) WriteMarkdown(w io.Writer) error {
	var b strings.Builder
	if len(e.Journal) > 0 {
		b.WriteString("# Diary\n\n")
		for _, d := range e.Journal {
			b.WriteString("### " + d.Day + "\n\n" + markdownBody(d.Body) + "\n\n")
		}
	}
	if len(e.Notes) > 0 {
		b.WriteString("# Sticky notes\n\n")
		for _, n := range e.Notes {
			head := "### Note"
			if n.Pinned {
				head += " 📌"
			}
			if n.Due != "" {
				head += " (remind " + n.Due + ")"
			}
			if n.Title != "" {
				head = "### " + n.Title
				if n.Pinned {
					head += " 📌"
				}
				if n.Due != "" {
					head += " (remind " + n.Due + ")"
				}
			}
			b.WriteString(head + "\n\n")
			if n.Tags != "" {
				b.WriteString("Tags: #" + strings.ReplaceAll(n.Tags, ",", " #") + "\n\n")
			}
			b.WriteString(markdownBody(n.Body) + "\n\n")
		}
	}
	for _, bd := range e.Boards {
		fmt.Fprintf(&b, "# %s\n\n", bd.Name)
		for _, c := range bd.Columns {
			fmt.Fprintf(&b, "## %s (%d)\n\n", c.Name, len(c.Cards))
			for _, k := range c.Cards {
				head := k.Title
				if k.Project != "" {
					head = "[" + k.Project + "] " + head
				}
				if k.Focus {
					head = "★ " + head
				}
				if k.ArchivedAt != "" {
					head += " (archived)"
				}
				fmt.Fprintf(&b, "### %s\n\n", head)
				var meta []string
				if k.Priority > 0 {
					meta = append(meta, "priority: "+[]string{"", "low", "medium", "high"}[k.Priority])
				}
				if k.Due != "" {
					meta = append(meta, "due: "+k.Due)
				}
				if k.Tags != "" {
					meta = append(meta, "tags: "+k.Tags)
				}
				if len(meta) > 0 {
					fmt.Fprintf(&b, "_%s_\n\n", strings.Join(meta, " · "))
				}
				if k.Description != "" {
					b.WriteString(markdownBody(k.Description) + "\n\n")
				}
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// markdownBody turns "[ ] item" lines into Markdown task items ("- [ ] item"), keeping indentation.
func markdownBody(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, "[ ] ") || strings.HasPrefix(t, "[x] ") || strings.HasPrefix(t, "[X] ") {
			lines[i] = l[:len(l)-len(t)] + "- " + t
		}
	}
	return strings.Join(lines, "\n")
}

// ReadExport parses an export produced by WriteJSON.
func ReadExport(r io.Reader) (Export, error) {
	var e Export
	if err := json.NewDecoder(r).Decode(&e); err != nil {
		return e, fmt.Errorf("not a kbn export: %w", err)
	}
	if e.Version != 1 {
		return e, fmt.Errorf("unsupported export version %d", e.Version)
	}
	return e, nil
}

// Import adds every board of the export as a new board (a name clash gets an " (imported)" suffix),
// so importing never overwrites existing work. It returns how many boards and cards were created.
func (s *Store) Import(e Export) (boards, cards int, err error) {
	existing, err := s.Boards()
	if err != nil {
		return 0, 0, err
	}
	names := map[string]bool{}
	for _, b := range existing {
		names[strings.ToLower(b.Name)] = true
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, eb := range e.Boards {
		name := strings.TrimSpace(eb.Name)
		if name == "" {
			name = "Imported"
		}
		for names[strings.ToLower(name)] {
			name += " (imported)"
		}
		names[strings.ToLower(name)] = true
		res, err := tx.Exec(`INSERT INTO boards(name) VALUES(?)`, name)
		if err != nil {
			return 0, 0, err
		}
		bid, _ := res.LastInsertId()
		boards++
		for ci, ec := range eb.Columns {
			res, err := tx.Exec(`INSERT INTO columns(board_id, name, position, wip_limit) VALUES(?,?,?,?)`, bid, ec.Name, ci, ec.WIPLimit)
			if err != nil {
				return 0, 0, err
			}
			cid, _ := res.LastInsertId()
			for pi, k := range ec.Cards {
				created := k.CreatedAt
				if created == "" {
					created = now()
				}
				if _, err := tx.Exec(`INSERT INTO cards(column_id, project, title, description, priority, due, tags, position, focus, done_at, archived_at, created_at)
					VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, cid, k.Project, k.Title, k.Description, k.Priority, k.Due, k.Tags, pi, k.Focus, k.DoneAt, k.ArchivedAt, created); err != nil {
					return 0, 0, err
				}
				cards++
			}
		}
	}
	for _, n := range e.Notes { // notes are added too; the sheet is small and duplicates are easy to delete
		created, updated := n.CreatedAt, n.UpdatedAt
		if created == "" {
			created = now()
		}
		if updated == "" {
			updated = created
		}
		if _, err := tx.Exec(`INSERT INTO notes(title, tags, body, color, pinned, due, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			n.Title, NormTags(n.Tags), n.Body, n.Color, n.Pinned, n.Due, created, updated); err != nil {
			return 0, 0, err
		}
	}
	for _, d := range e.Journal { // never overwrite a day that already has text
		if _, err := tx.Exec(`INSERT OR IGNORE INTO journal(day, body, updated_at) VALUES(?,?,?)`, d.Day, d.Body, now()); err != nil {
			return 0, 0, err
		}
	}
	return boards, cards, tx.Commit()
}
