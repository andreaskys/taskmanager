package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultBackupDir returns the folder next to the database where backups are kept.
func DefaultBackupDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

// Backup writes a consistent copy of the database (VACUUM INTO, safe with WAL) to dir.
// It does nothing when the newest backup is younger than minAge, and keeps only the newest `keep` files.
func (s *Store) Backup(dir string, keep int, minAge time.Duration) (path string, made bool, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "kbn-*.db"))
	sort.Strings(files) // names embed the timestamp, so lexical order is chronological
	if n := len(files); n > 0 {
		if info, err := os.Stat(files[n-1]); err == nil && time.Since(info.ModTime()) < minAge {
			return files[n-1], false, nil
		}
	}
	path = filepath.Join(dir, "kbn-"+time.Now().Format("20060102-150405")+".db")
	if _, err := s.db.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", false, fmt.Errorf("backup: %w", err)
	}
	files = append(files, path)
	sort.Strings(files)
	for len(files) > keep {
		os.Remove(files[0])
		files = files[1:]
	}
	return path, true, nil
}

// SafeName turns a board name into something usable in a file name.
func SafeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			return '-'
		}
		return r
	}, s)
}
