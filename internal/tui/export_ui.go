package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// exportNow writes the whole board data as JSON and Markdown next to the database (space E).
func (m *Model) exportNow() {
	e, err := m.st.Export()
	if err != nil {
		m.fail(err)
		return
	}
	dir := filepath.Join(filepath.Dir(m.st.Path()), "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.fail(err)
		return
	}
	base := filepath.Join(dir, "kbn-"+time.Now().Format("20060102-150405"))
	for ext, write := range map[string]func(*os.File) error{
		".json": func(f *os.File) error { return e.WriteJSON(f) },
		".md":   func(f *os.File) error { return e.WriteMarkdown(f) },
	} {
		f, err := os.Create(base + ext)
		if err != nil {
			m.fail(err)
			return
		}
		err = write(f)
		f.Close()
		if err != nil {
			m.fail(err)
			return
		}
	}
	m.notice = fmt.Sprintf("exported to %s.json / .md", base)
}
