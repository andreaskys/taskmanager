// Package store persists boards, columns and cards in SQLite.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO
)

type Board struct {
	ID   int64
	Name string
}

type Column struct {
	ID       int64
	BoardID  int64
	Name     string
	Position int
	WIPLimit int // max cards allowed in the column (0 = no limit)
}

type Card struct {
	ID          int64
	ColumnID    int64
	Project     string
	Title       string
	Description string
	Priority    int    // 0 none, 1 low, 2 medium, 3 high
	Due         string // YYYY-MM-DD or empty
	Tags        string // comma separated
	Position    int
	Focus       bool   // pinned as "focus of the day"
	DoneAt      string // UTC "YYYY-MM-DD HH:MM:SS" when it entered the last (Done) column, else ""
	ArchivedAt  string // non-empty once archived: hidden from the board, still searchable
	CreatedAt   time.Time
}

type Store struct {
	db   *sql.DB
	path string
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

const schema = `
CREATE TABLE IF NOT EXISTS boards (
	id   INTEGER PRIMARY KEY,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS columns (
	id       INTEGER PRIMARY KEY,
	board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	name     TEXT NOT NULL,
	position INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cards (
	id          INTEGER PRIMARY KEY,
	column_id   INTEGER NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
	project     TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	priority    INTEGER NOT NULL DEFAULT 0,
	due         TEXT NOT NULL DEFAULT '',
	tags        TEXT NOT NULL DEFAULT '',
	position    INTEGER NOT NULL,
	created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS notes (
	id         INTEGER PRIMARY KEY,
	body       TEXT NOT NULL,
	color      INTEGER NOT NULL DEFAULT 0,
	pinned     INTEGER NOT NULL DEFAULT 0,
	due        TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS journal (
	day        TEXT PRIMARY KEY,
	body       TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS card_events (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	card_id  INTEGER NOT NULL,
	kind     TEXT NOT NULL,
	title    TEXT NOT NULL DEFAULT '',
	project  TEXT NOT NULL DEFAULT '',
	board    TEXT NOT NULL DEFAULT '',
	from_col TEXT NOT NULL DEFAULT '',
	to_col   TEXT NOT NULL DEFAULT '',
	at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS attachments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	kind       TEXT NOT NULL,
	owner_id   INTEGER NOT NULL,
	target     TEXT NOT NULL,
	label      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS undo_log (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	stack   TEXT NOT NULL,
	desc    TEXT NOT NULL,
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_card ON card_events(card_id, id);
CREATE INDEX IF NOT EXISTS idx_events_at ON card_events(at);
CREATE INDEX IF NOT EXISTS idx_attach_owner ON attachments(kind, owner_id);
CREATE INDEX IF NOT EXISTS idx_cards_column ON cards(column_id, position);
CREATE INDEX IF NOT EXISTS idx_columns_board ON columns(board_id, position);
`

var defaultColumns = []string{"To do", "Doing", "Testing", "Done"}

// DefaultPath returns ~/.local/share/kbn/kbn.db (honoring XDG_DATA_HOME).
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "kbn", "kbn.db"), nil
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single-user: one connection is enough and saves memory
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	st := &Store{db: db, path: path}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

// migrate upgrades databases created by earlier versions.
func (s *Store) migrate() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cards') WHERE name='project'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.db.Exec(`ALTER TABLE cards ADD COLUMN project TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// legacy checklist table: fold its items into the card text as "[ ] item" lines.
	var legacy int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='check_items'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		if _, err := s.db.Exec(`UPDATE cards SET description = TRIM(description || char(10) || (
				SELECT group_concat(CASE done WHEN 1 THEN '[x] ' ELSE '[ ] ' END || text, char(10))
				FROM (SELECT * FROM check_items WHERE card_id = cards.id ORDER BY id)), char(10))
			WHERE id IN (SELECT card_id FROM check_items)`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`DROP TABLE check_items`); err != nil {
			return err
		}
	}
	if err := s.addColumns(); err != nil {
		return err
	}
	for pt, en := range map[string]string{"A fazer": "To do", "Fazendo": "Doing", "Em teste": "Testing", "Feito": "Done"} {
		if _, err := s.db.Exec(`UPDATE columns SET name=? WHERE name=?`, en, pt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasColumn(table, col string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('`+table+`') WHERE name=?`, col).Scan(&n)
	return n > 0
}

// addColumns adds the columns introduced after the first release (idempotent).
func (s *Store) addColumns() error {
	add := func(table, col, def string) (bool, error) {
		if s.hasColumn(table, col) {
			return false, nil
		}
		_, err := s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + col + ` ` + def)
		return true, err
	}
	if _, err := add("notes", "title", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := add("notes", "tags", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := add("columns", "wip_limit", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := add("cards", "focus", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := add("cards", "archived_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	added, err := add("cards", "done_at", "TEXT NOT NULL DEFAULT ''")
	if err != nil {
		return err
	}
	if added { // start the archive clock for cards that are already done
		_, err = s.db.Exec(`UPDATE cards SET done_at = strftime('%Y-%m-%d %H:%M:%S','now')
			WHERE column_id IN (SELECT c.id FROM columns c WHERE c.position = (SELECT MAX(position) FROM columns WHERE board_id = c.board_id))`)
	}
	return err
}

// DataVersion changes whenever ANOTHER connection (e.g. `kbn serve` for the phone) commits to the
// database; this process's own writes do not change it.
func (s *Store) DataVersion() int64 {
	var v int64
	s.db.QueryRow(`PRAGMA data_version`).Scan(&v)
	return v
}

func (s *Store) Close() error { return s.db.Close() }

// --- boards ---

func (s *Store) Boards() ([]Board, error) {
	rows, err := s.db.Query(`SELECT id, name FROM boards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CreateBoard creates the board with the default columns.
func (s *Store) CreateBoard(name string) (Board, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Board{}, fmt.Errorf("board name is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO boards(name) VALUES(?)`, name)
	if err != nil {
		return Board{}, err
	}
	id, _ := res.LastInsertId()
	for i, c := range defaultColumns {
		if _, err := tx.Exec(`INSERT INTO columns(board_id, name, position) VALUES(?,?,?)`, id, c, i); err != nil {
			return Board{}, err
		}
	}
	return Board{ID: id, Name: name}, tx.Commit()
}

func (s *Store) DeleteBoard(id int64) error {
	_, err := s.db.Exec(`DELETE FROM boards WHERE id=?`, id)
	return err
}
