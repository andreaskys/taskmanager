package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Template pre-fills the new-card window: notes (usually a checklist), tags and priority.
// They live in templates.json next to the database, so you can edit or add your own.
type Template struct {
	Name     string `json:"name"`
	Tags     string `json:"tags,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Notes    string `json:"notes"`
}

var defaultTemplates = []Template{
	{Name: "bug", Tags: "bug", Priority: 2, Notes: "[ ] reproduce\n[ ] find the cause\n[ ] fix\n[ ] add a regression test\n[ ] verify in staging"},
	{Name: "feature", Tags: "feature", Notes: "[ ] clarify the requirement\n[ ] design / plan\n[ ] implement\n[ ] tests\n[ ] docs\n[ ] review"},
	{Name: "meeting", Tags: "meeting", Notes: "agenda:\n[ ] \n\ndecisions:\n\nactions:\n[ ] "},
	{Name: "deploy", Tags: "deploy", Priority: 3, Notes: "[ ] changelog\n[ ] run tests\n[ ] backup / migrations\n[ ] deploy\n[ ] smoke test\n[ ] announce"},
	{Name: "review", Tags: "review", Notes: "[ ] read the description\n[ ] check the diff\n[ ] run it locally\n[ ] leave comments\n[ ] approve / request changes"},
}

func templatesPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "templates.json")
}

// loadTemplates reads templates.json, creating it with the defaults the first time.
// A broken file falls back to the defaults instead of breaking the app.
func loadTemplates(dbPath string) []Template {
	p := templatesPath(dbPath)
	data, err := os.ReadFile(p)
	if err != nil {
		if b, err := json.MarshalIndent(defaultTemplates, "", "  "); err == nil {
			os.WriteFile(p, b, 0o644)
		}
		return defaultTemplates
	}
	var t []Template
	if json.Unmarshal(data, &t) != nil || len(t) == 0 {
		return defaultTemplates
	}
	return t
}
