package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type helpEntry struct{ keys, desc string }

type helpSection struct {
	title   string
	entries []helpEntry
}

// helpSections is the cheat sheet shown by `space h`. Keep it in sync with the key handlers
// (TestHelpCoversLeaderKeys fails if a leader key is missing here).
var helpSections = []helpSection{
	{"Leader  (press space, then…)", []helpEntry{
		{"space space", "search all cards, with preview (space / too)"},
		{"space n", "new card: Project, Description and Notes with checkboxes"},
		{"space enter", "meeting notes scratchpad; ctrl+s saves them as a task"},
		{"space x", "delete the selected card (u undoes)"},
		{"space e", "open card details (space o does the same)"},
		{"space t", "today view: overdue, today, next 7 days"},
		{"space f", "cycle priority filter: off → high → medium+ → any"},
		{"space s", "cycle sort: manual → due date → priority"},
		{"space c", "clear the priority filter"},
		{"space u", "undo the last change (kept between sessions)"},
		{"space U", "redo (ctrl+r too)"},
		{"space r", "sticky notes: write things to remember (ctrl+t in the editor adds a title and tags; searchable)"},
		{"space j", "diary: one text per day, kept and searchable (see Diary below)"},
		{"space w", "summary of the week or month: done, created, by project; y copies Markdown"},
		{"space p", "projects: all cards and tagged notes of a project, across boards"},
		{"space .", "pin / unpin the card as focus of the day (* too)"},
		{"space m", "manage columns: new, rename, reorder, delete, WIP limit"},
		{"space a", "archive every card in the Done column"},
		{"space E", "export everything to JSON and Markdown files"},
		{"space b", "boards list (switch / create / delete)"},
		{"space y", "cycle color theme (remembered)"},
		{"space h", "this help"},
		{"space ?", "toggle the full key list in the footer"},
		{"space q", "quit"},
	}},
	{"Board", []helpEntry{
		{"h / l", "previous / next column"},
		{"j / k", "next / previous card"},
		{"H / L", "move the card to the left / right column"},
		{"J / K", "reorder the card down / up (manual sort only)"},
		{"n", "new card (same window as space n)"},
		{"enter", "open card details"},
		{"p", "cycle priority (none → low → medium → high)"},
		{"dd", "delete the card (u undoes)"},
		{"u / ctrl+z", "undo (last 200 changes, kept between sessions)"},
		{"ctrl+r", "redo"},
		{"*", "pin / unpin as focus of the day (★ on the card and in the bar)"},
		{"/", "search window"},
		{"T", "today view"},
		{"esc", "clear the priority filter"},
		{"?", "toggle the full key list in the footer"},
		{"q", "quit"},
	}},
	{"Card details", []helpEntry{
		{"j / k", "move between text lines"},
		{"space", "toggle the checkbox (a parent also toggles its sub-tasks)"},
		{"*", "pin / unpin as focus of the day"},
		{"e / enter", "edit the text"},
		{"t  r  u  g", "edit title · project · due date · tags"},
		{"p", "cycle priority"},
		{"a", "attach a file path or URL (\"Name | path\" names it)"},
		{"o", "open attachments, URLs and #id links of the card (one target opens at once)"},
		{"#12", "in any text, links to card 12 (ids show next to the card chips); the card lists where it is mentioned"},
		{"esc", "back to the board"},
	}},
	{"Diary  (space j)", []helpEntry{
		{"[ / ]", "previous / next day (h l too)"},
		{"{ / }", "previous / next day that has an entry (H L too)"},
		{"t  g", "go to today · go to a date"},
		{"e / enter", "write the day (esc or ctrl+s saves)"},
		{"j / k  space", "select a line · toggle its checkbox"},
		{"p", "carry the unfinished checkboxes of the last entry to this day"},
		{"o", "open #id links and URLs of the entry"},
		{"", "under the text: the cards created, moved or finished that day"},
	}},
	{"Summary and projects  (space w, space p)", []helpEntry{
		{"[ / ]  m  t", "summary: previous / next period · week ↔ month · now"},
		{"y  s", "summary: copy as Markdown · save it next to the database"},
		{"j / k  enter", "projects: choose a project · enter focuses its cards, enter again opens one"},
		{"/", "projects: search the project's name everywhere"},
	}},
	{"Editing text  (details, notes, new-card notes)", []helpEntry{
		{"enter", "new line, keeping the indentation (on an empty [ ] it ends the list)"},
		{"shift+enter", "new checkbox below (alt+enter also works)"},
		{"tab / shift+tab", "indent / outdent: nested sub-tasks (2 spaces per level)"},
		{"alt+x", "toggle the checkbox on this line (a plain line becomes one)"},
		{"esc", "save and leave"},
	}},
	{"Search window", []helpEntry{
		{"type", "filter by project, description, tags, board, sticky notes (title, #tag, text) and diary days"},
		{"#tag", "match tags only"},
		{"↑ ↓  ctrl+n/p", "move the selection (pgup / pgdn jump 10)"},
		{"enter", "open the card's details to edit it (an archived card is restored first)"},
		{"tab", "just show the card on its board"},
		{"esc", "close"},
	}},
	{"New card window", []helpEntry{
		{"enter", "Project → Description → Notes (new line inside Notes)"},
		{"tab", "complete a known project, else next field (indent inside Notes)"},
		{"ctrl+t", "template: fills Notes, tags and priority (edit templates.json)"},
		{"shift+enter", "checkbox line in Notes (alt+enter also works)"},
		{"ctrl+s", "save the card from any field"},
		{"shift+tab", "previous field (outdent inside Notes)"},
		{"esc", "cancel"},
	}},
	{"Sticky notes  (space r)", []helpEntry{
		{"h j k l", "move between the notes"},
		{"n", "new note (esc or ctrl+s saves it)"},
		{"enter / e", "edit the note: shift+enter checkbox, tab indent, alt+x toggle"},
		{"c", "change the note color"},
		{"p", "pin / unpin (pinned notes come first)"},
		{"a  o", "attach a file or URL · open the note's links and attachments"},
		{"D", "reminder date: the header shows 🔔 when one is due"},
		{"t", "turn the note into a task (the note is removed)"},
		{"dd  or  x", "delete the note (u undoes it)"},
		{"u", "undo in the notes window"},
		{"esc / r", "close"},
	}},
	{"Columns window  (space m)", []helpEntry{
		{"j / k", "select a column"},
		{"n / r", "new column (before Done) / rename"},
		{"w", "WIP limit: a full column refuses new cards (0 = none)"},
		{"J / K", "move the column down / up"},
		{"d", "delete (its cards go to the previous column)"},
		{"esc", "close"},
	}},
	{"Meeting notes", []helpEntry{
		{"enter", "new line"},
		{"shift+enter", "new line with a checkbox (alt+enter also works)"},
		{"ctrl+s", "save as a task in the first column"},
		{"ctrl+x ×2", "discard the draft"},
		{"esc", "close; the draft is kept for next time"},
	}},
	{"Today view", []helpEntry{
		{"j / k", "move"},
		{"enter", "open the card on its board"},
		{"x  or  space x", "mark done (moves it to the last column)"},
		{"u", "undo"},
		{"esc / T", "back"},
	}},
	{"Mouse", []helpEntry{
		{"click", "select a card / column / line"},
		{"double-click", "open a card, board or today item"},
		{"drag", "drop a card exactly where the bar shows, in any column"},
		{"wheel", "scroll the selection"},
		{"click ⬜", "toggle a checkbox in card details"},
	}},
	{"Boards list  (space b)", []helpEntry{
		{"j / k", "move"},
		{"enter", "open the board (double-click works too)"},
		{"n", "new board (comes with To do / Doing / Testing / Done)"},
		{"d", "delete the board and its cards (asks y/n; cannot be undone)"},
		{"esc", "back"},
	}},
	{"Input window  (title, due, tags, project, columns, new board)", []helpEntry{
		{"enter", "confirm"},
		{"esc", "cancel"},
		{"tab", "complete a known project / tag"},
		{"due date", "YYYY-MM-DD; empty clears it"},
		{"tags", "comma separated"},
	}},
	{"Command line", []helpEntry{
		{"kbn -theme NAME", "default, monokai, gruvbox, nord, tokyonight, dracula (also $KBN_THEME)"},
		{"kbn -db file.db", "use another database"},
		{"kbn export [-format md] [-o f]", "write every board as JSON (default) or Markdown"},
		{"kbn import file.json", "add the boards of an export as new boards"},
		{"kbn backup", "copy the database now (automatic: every start, 14 kept)"},
		{"$KBN_ARCHIVE_DAYS", "archive Done cards after N days (default 14, 0 = never)"},
	}},
}

func (m *Model) openHelp() {
	m.helpBack = m.mode
	m.helpOff = 0
	m.mode = modeHelp
}

func (m *Model) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "h", "?", "ctrl+c":
		m.mode = m.helpBack
	case "j", "down":
		m.helpOff++
	case "k", "up":
		m.helpOff--
	case "pgdown", "ctrl+d", "space":
		m.helpOff += 10
	case "pgup", "ctrl+u":
		m.helpOff -= 10
	case "g", "home":
		m.helpOff = 0
	case "G", "end":
		m.helpOff = 1 << 20 // clamped when drawn
	}
	return m, nil
}

func helpLines(w int) []string {
	const keyW = 18
	var lines []string
	for i, sec := range helpSections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+titleStyle.Render(sec.title))
		for _, e := range sec.entries {
			k := keyStyle.Render(e.keys)
			pad := max(1, keyW-lipgloss.Width(k))
			lines = append(lines, "   "+k+strings.Repeat(" ", pad)+dimStyle.Render(truncate(e.desc, max(10, w-keyW-5))))
		}
	}
	return lines
}

func (m *Model) viewHelp(base string) string {
	sw, sh := m.screenSize()
	W := clamp(sw*80/100, 60, 110)
	H := clamp(sh*86/100, 14, 46)
	rows := H - 2 - 2 // border, hint + rule
	all := helpLines(W - 2)
	maxOff := max(0, len(all)-rows)
	m.helpOff = clamp(m.helpOff, 0, maxOff)
	body := all[m.helpOff:min(len(all), m.helpOff+rows)]

	lines := append([]string{}, body...)
	for len(lines) < rows {
		lines = append(lines, "")
	}
	pos := ""
	if maxOff > 0 {
		pos = fmt.Sprintf("  ·  %d–%d of %d", m.helpOff+1, m.helpOff+len(body), len(all))
	}
	lines = append(lines, ruleStyleLine(W-2))
	lines = append(lines, " "+dimStyle.Render("j/k scroll · pgup/pgdn · esc or h to close"+pos))
	return m.center(base, drawBox("Help · macros & keys", lines, W, H, th.Accent))
}
