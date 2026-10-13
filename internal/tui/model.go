// Package tui implements the kanban terminal UI (Bubble Tea).
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/andreas/kbn/internal/store"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type mode int

const (
	modeBoard mode = iota
	modeDetail
	modeInput
	modeBoards
	modeConfirm
	modeSearch // floating search picker
	modeToday
	modeNewCard     // floating new-card form
	modeScratch     // floating meeting-notes window
	modeHelp        // floating cheat sheet
	modeColumns     // floating columns manager
	modeNotes       // floating sticky notes
	modeNoteEdit    // floating note editor
	modeJournal     // floating diary
	modeJournalEdit // floating diary editor
	modeSummary     // floating week/month summary
	modeProjects    // floating project overview
	modeLinks       // floating list of links and attachments to open
)

const isoDate = "2006-01-02"

// sort modes for the cards inside a column
const (
	sortManual = iota
	sortDue
	sortPriority
	sortModes
)

var sortNames = []string{"manual", "due date", "priority"}

type Model struct {
	st *store.Store

	mode     mode
	prevMode mode // where to return after input/confirm
	width    int
	height   int
	err      string
	notice   string // one-shot status message (e.g. "undid: delete card")

	boards   []store.Board
	boardIdx int
	columns  []store.Column
	cards    [][]store.Card // cards[col], after filter and sort

	col, row int // selection on the board
	colOff   int // first board column on screen (see visibleCols)

	// view options
	minPrio  int // priority filter: show only cards with priority >= minPrio (0 = off)
	sortMode int
	totals   []int // unfiltered card count per column
	showHelp bool
	pick     *picker         // search window state
	form     *newCardForm    // new-card window state
	scratch  *scratch        // notes window state
	draft    string          // saved notes draft (persisted in settings)
	helpOff  int             // help window scroll offset
	helpBack mode            // mode to return to when the help closes
	colSel   int             // selected row of the columns window
	focus    []store.DueCard // cards pinned as focus of the day (all boards)
	notes    []store.Note    // sticky notes
	noteSel  int
	noteCols int // columns of stickies in the last render (for j/k movement)
	noteEd   *noteEditor
	noteUndo []func() error // reverts for the notes window (in memory)
	notesDue int            // notes with a reminder due today or earlier
	lastKey  string         // previous board key, for the "dd" macro
	leader   bool           // space was pressed: the next key picks a leader action

	// diary
	jDay    string
	jEntry  store.JournalEntry
	jEvents []store.CardEvent // card activity of jDay
	jSel    int               // selected line of the entry
	jOff    int               // scroll offset of the entry
	jEd     *journalEditor

	// summary
	sum       store.Summary
	sumAnchor string // a day inside the period shown
	sumMonth  bool
	sumOff    int

	// projects
	projAll   []store.DueCard
	projRows  []projRow
	projSel   int
	projItems []projItem
	projCur   int
	projFocus bool

	// links and attachments
	links     []linkItem
	linkIdx   int
	linkKind  string
	linkOwner int64
	linkText  string

	// detail extras: history, attachments and mentions of the open card
	detHist    []store.CardEvent
	detAtt     []store.Attachment
	detMention []string
	noteAtt    map[int64]int // attachments per note
	dataVer    int64         // last seen store.DataVersion (see syncExternal)

	// detail view: card text is free-form; lines starting with "[ ] " / "[x] " are checkboxes
	lineIdx int
	editing bool
	area    textarea.Model

	// today view
	dueCards  []store.DueCard
	todayIdx  int
	selectAft int64 // card id to select after the next reload (e.g. a new card)

	// mouse
	hits      []hit
	bodyY     int // screen row where the body starts (below the header)
	drag      *dragState
	lastClick click

	// generic input
	input        textinput.Model
	inputMsg     string
	inputHint    string              // dim hint under the field
	afterSubmit  func()              // runs after a successful submit
	complete     func(string) string // tab completion for the field (nil = none)
	suggestList  []string            // completion candidates shown under the field
	suggestMulti bool                // the field holds a comma separated list
	projects     []string            // known projects and tags, loaded on reload (for suggestions)
	tags         []string
	templates    []Template
	onSubmit     func(string) error

	// confirmation
	confirmMsg string
	onConfirm  func() error
}

func New(st *store.Store) (*Model, error) {
	m := &Model{st: st, input: textinput.New()}
	m.input.CharLimit = 200
	m.draft = st.Setting(settingScratch)
	boards, err := st.Boards()
	if err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		if _, err := st.CreateBoard("Trabalho"); err != nil {
			return nil, err
		}
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// syncEvery is how often the board checks whether another process (the phone server) changed the data.
const syncEvery = 2 * time.Second

type syncMsg struct{}

func syncTick() tea.Cmd { return tea.Tick(syncEvery, func(time.Time) tea.Msg { return syncMsg{} }) }

func (m *Model) Init() tea.Cmd {
	m.dataVer = m.st.DataVersion()
	return syncTick()
}

// syncExternal reloads the board when the database was changed by another process. It only acts
// while the plain board is on screen, so it never disturbs a window or an edit in progress.
func (m *Model) syncExternal() {
	v := m.st.DataVersion()
	if v == m.dataVer || m.mode != modeBoard || m.editing {
		return
	}
	m.dataVer = v
	cur := m.current()
	var id int64
	if cur != nil {
		id = cur.ID
	}
	m.fail(m.reload())
	if id != 0 {
		m.selectCard(id)
	}
}

func todayStr() string { return time.Now().Format(isoDate) }

// overdue reports whether a due date (YYYY-MM-DD) is before today.
func overdue(due string) bool { return due != "" && due < todayStr() }

func (m *Model) filtersActive() bool { return m.minPrio > 0 }

func (m *Model) clearFilters() {
	m.minPrio = 0
}

// reload re-reads state from the database (cheap with local SQLite).
func (m *Model) reload() error {
	var err error
	if m.boards, err = m.st.Boards(); err != nil {
		return err
	}
	if m.boardIdx >= len(m.boards) {
		m.boardIdx = len(m.boards) - 1
	}
	if m.boardIdx < 0 {
		m.boardIdx = 0
	}
	if len(m.boards) == 0 {
		m.columns, m.cards = nil, nil
		return nil
	}
	if m.columns, err = m.st.Columns(m.boards[m.boardIdx].ID); err != nil {
		return err
	}
	m.cards = make([][]store.Card, len(m.columns))
	m.totals = make([]int, len(m.columns))
	for i, c := range m.columns {
		all, err := m.st.Cards(c.ID)
		if err != nil {
			return err
		}
		m.totals[i] = len(all)
		cs := filterPriority(all, m.minPrio)
		m.cards[i] = sortCards(cs, m.sortMode)
	}
	if m.focus, err = m.st.FocusCards(); err != nil {
		return err
	}
	m.notesDue = m.st.NotesDue(todayStr())
	m.projects, _ = m.st.Projects()
	m.tags, _ = m.st.Tags()
	if m.selectAft != 0 {
		m.selectCard(m.selectAft)
		m.selectAft = 0
	}
	m.clamp()
	if m.mode == modeDetail {
		m.lineIdx = min(m.lineIdx, max(0, len(strings.Split(m.currentText(), "\n"))-1))
	}
	if m.mode == modeToday {
		return m.loadToday()
	}
	return err
}

func (m *Model) clamp() {
	if m.col >= len(m.columns) {
		m.col = max(0, len(m.columns)-1)
	}
	if m.col < len(m.cards) && m.row >= len(m.cards[m.col]) {
		m.row = max(0, len(m.cards[m.col])-1)
	}
}

// selectCard moves the board selection to the card with this id, if it is visible.
func (m *Model) selectCard(id int64) bool {
	for i, cs := range m.cards {
		for j, c := range cs {
			if c.ID == id {
				m.col, m.row = i, j
				return true
			}
		}
	}
	return false
}

func (m *Model) current() *store.Card {
	if m.col < len(m.cards) && m.row < len(m.cards[m.col]) {
		return &m.cards[m.col][m.row]
	}
	return nil
}

var foldT = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// fold lowercases and strips accents so "juridico" finds "jurídico".
func fold(s string) string {
	out, _, err := transform.String(foldT, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return out
}

func filterPriority(cards []store.Card, min int) []store.Card {
	if min <= 0 {
		return cards
	}
	var out []store.Card
	for _, c := range cards {
		if c.Priority >= min {
			out = append(out, c)
		}
	}
	return out
}

// sortCards returns the cards in the requested display order (manual keeps stored order).
func sortCards(cards []store.Card, by int) []store.Card {
	if by == sortManual || len(cards) < 2 {
		return cards
	}
	out := append([]store.Card(nil), cards...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if by == sortDue {
			if (a.Due == "") != (b.Due == "") {
				return a.Due != "" // cards without a date go last
			}
			if a.Due != b.Due {
				return a.Due < b.Due
			}
		}
		return a.Priority > b.Priority
	})
	return out
}

func (m *Model) currentText() string {
	if c := m.current(); c != nil {
		return c.Description
	}
	return ""
}

// fail records an error to show in the footer. It never clears one: errors disappear on the next key
// or mouse event, so a message set by an action survives the reload that follows it.
func (m *Model) fail(err error) {
	if err != nil {
		m.err = err.Error()
	}
}

// askOpt customizes an input window (see ask).
type askOpt func(*Model)

func withHint(h string) askOpt                  { return func(m *Model) { m.inputHint = h } }
func withAfter(f func()) askOpt                 { return func(m *Model) { m.afterSubmit = f } }
func withComplete(f func(string) string) askOpt { return func(m *Model) { m.complete = f } }

// ask opens the single-field input window; fn gets the text on enter.
func (m *Model) ask(msg, initial string, fn func(string) error, opts ...askOpt) tea.Cmd {
	m.prevMode = m.mode
	m.mode = modeInput
	m.inputMsg = msg
	m.inputHint, m.afterSubmit, m.complete = "", nil, nil
	m.suggestList, m.suggestMulti = nil, false
	m.input.SetValue(initial)
	m.input.CursorEnd()
	m.onSubmit = fn
	for _, o := range opts {
		o(m)
	}
	return m.input.Focus()
}

func (m *Model) confirm(msg string, fn func() error) {
	m.prevMode = m.mode
	m.mode = modeConfirm
	m.confirmMsg = msg
	m.onConfirm = fn
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.editing {
			m.area.SetWidth(m.detailInnerWidth())
			m.area.SetHeight(max(6, m.height-12))
		}
		return m, nil
	case syncMsg:
		m.syncExternal()
		return m, syncTick()
	case tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseMotionMsg, tea.MouseWheelMsg:
		return m.updateMouse(msg)
	case tea.KeyPressMsg:
		m.notice, m.err = "", ""
		switch m.mode {
		case modeInput:
			return m.updateInput(msg)
		case modeConfirm:
			if msg.String() == "y" {
				m.fail(m.onConfirm())
				m.mode = m.prevMode
				m.fail(m.reload())
			} else {
				m.mode = m.prevMode
			}
			return m, nil
		case modeSearch:
			return m.updateSearch(msg)
		case modeNewCard:
			return m.updateNewCard(msg)
		case modeScratch:
			return m.updateScratch(msg)
		case modeHelp:
			return m.updateHelp(msg)
		case modeColumns:
			return m.updateColumns(msg)
		case modeNotes:
			return m.updateNotes(msg)
		case modeNoteEdit:
			return m.updateNoteEdit(msg)
		case modeJournal:
			return m.updateJournal(msg)
		case modeJournalEdit:
			return m.updateJournalEdit(msg)
		case modeSummary:
			return m.updateSummary(msg)
		case modeProjects:
			return m.updateProjects(msg)
		case modeLinks:
			return m.updateLinks(msg)
		case modeBoards:
			return m.updateBoards(msg)
		case modeToday:
			return m.updateToday(msg)
		case modeDetail:
			if m.editing {
				return m.updateEditing(msg)
			}
			return m.updateDetail(msg)
		default:
			return m.updateBoard(msg)
		}
	}
	return m, nil
}

func (m *Model) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = m.prevMode
		return m, nil
	case "enter":
		fn, after := m.onSubmit, m.afterSubmit
		m.mode = m.prevMode
		err := fn(m.input.Value())
		m.fail(err)
		m.fail(m.reload())
		if err == nil && after != nil {
			after()
		}
		return m, nil
	case "tab":
		if m.complete != nil {
			m.input.SetValue(m.complete(m.input.Value()))
			m.input.CursorEnd()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) openDetail() {
	m.mode = modeDetail
	m.lineIdx = 0
	m.fail(m.reload())
	m.loadDetailExtra()
}

// loadDetailExtra reads the history, attachments and mentions of the open card.
func (m *Model) loadDetailExtra() {
	m.detHist, m.detAtt, m.detMention = nil, nil, nil
	c := m.current()
	if c == nil {
		return
	}
	hist, err := m.st.CardHistory(c.ID)
	m.fail(err)
	m.detHist = hist
	atts, err := m.st.Attachments("card", c.ID)
	m.fail(err)
	m.detAtt = atts
	m.detMention = m.mentions(c.ID)
}

// leaderKeys maps the key pressed after <space> to the board action it triggers.
// "dd" and these actions share one handler, so there is a single place to change behavior.
var leaderKeys = map[string]string{
	"space": "/", "/": "/", // search
	"n": "n",                   // new card
	"x": "X",                   // delete card
	"e": "enter", "o": "enter", // open details
	"t": "T",                       // today view
	"f": "f", "s": "s", "c": "esc", // filter, sort, clear filters
	"u": "u", "b": "b", "?": "?", "q": "q",
	"enter": "scratch", // meeting notes
	"h":     "help",
	"y":     "y",       // cycle color theme
	"r":     "notes",   // sticky notes
	"U":     "ctrl+r",  // redo
	"a":     "archive", // archive finished cards
	".":     "focus",   // pin / unpin as focus of the day
	"m":     "columns", // manage columns
	"E":     "export",  // write JSON + Markdown exports
	"j":     "journal", // diary
	"w":     "summary", // week / month summary
	"p":     "projects",
}

func (m *Model) updateBoard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.leader { // second key of a <space> sequence
		m.leader = false
		a, ok := leaderKeys[key]
		if !ok {
			if key != "esc" {
				m.notice = "no leader action on " + key
			}
			return m, nil
		}
		m.lastKey = ""
		key = a
	} else if key == "space" {
		m.leader = true
		m.lastKey = ""
		return m, nil
	}
	return m.boardAction(key)
}

func (m *Model) boardAction(key string) (tea.Model, tea.Cmd) {
	cur := m.current()
	prev := m.lastKey
	m.lastKey = key
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "h", "left":
		if m.col > 0 {
			m.col--
			m.clamp()
		}
	case "l", "right":
		if m.col < len(m.columns)-1 {
			m.col++
			m.clamp()
		}
	case "j", "down":
		if m.col < len(m.cards) && m.row < len(m.cards[m.col])-1 {
			m.row++
		}
	case "k", "up":
		if m.row > 0 {
			m.row--
		}
	case "H", "L": // move the card between columns
		to := m.col - 1
		if key == "L" {
			to = m.col + 1
		}
		if cur != nil && to >= 0 && to < len(m.columns) {
			id := cur.ID
			m.fail(m.moveCard(*cur, m.columns[to].ID))
			m.fail(m.reload())
			m.selectCard(id)
		}
	case "J", "K": // reorder within the column (manual sort only)
		if m.sortMode != sortManual {
			m.notice = "reordering needs manual sort — press s to cycle"
			break
		}
		other := m.row + 1
		if key == "K" {
			other = m.row - 1
		}
		if cur != nil && other >= 0 && other < len(m.cards[m.col]) {
			id := cur.ID
			m.fail(m.swapCards(*cur, m.cards[m.col][other]))
			m.fail(m.reload())
			m.selectCard(id)
		}
	case "n":
		return m, m.openNewCard()
	case "p":
		if cur != nil {
			c := *cur
			c.Priority = (c.Priority + 1) % 4
			m.fail(m.updateCard(c, "change priority"))
			m.fail(m.reload())
		}
	case "d", "X": // dd, or <space>x
		if cur != nil && (key == "X" || prev == "d") {
			m.lastKey = ""
			m.fail(m.deleteCard(*cur))
			m.fail(m.reload())
		}
	case "u", "ctrl+z":
		m.undoLast()
	case "f": // cycle the priority filter: off → high → medium+ → any priority
		m.minPrio = (m.minPrio + 3) % 4
		m.row = 0
		m.fail(m.reload())
	case "s": // cycle the sort order
		m.sortMode = (m.sortMode + 1) % sortModes
		m.fail(m.reload())
		if cur != nil {
			m.selectCard(cur.ID)
		}
	case "ctrl+r":
		m.redoLast()
	case "archive":
		n, err := m.archiveDone()
		m.fail(err)
		m.fail(m.reload())
		m.notice = fmt.Sprintf("archived %d done card(s) — still searchable, u undoes", n)
	case "focus", "*":
		if cur != nil {
			c := *cur
			c.Focus = !c.Focus
			m.fail(m.updateCard(c, "toggle focus"))
			m.fail(m.reload())
			m.selectCard(c.ID)
		}
	case "notes":
		m.openNotes()
	case "journal":
		m.openJournal("")
	case "summary":
		m.openSummary()
	case "projects":
		m.openProjects()
	case "columns":
		m.openColumns()
	case "export":
		m.exportNow()
	case "scratch":
		return m, m.openScratch()
	case "help":
		m.openHelp()
	case "y":
		name := nextTheme()
		m.fail(m.st.SetSetting("theme", name))
		m.notice = "theme: " + name
	case "T":
		m.mode = modeToday
		m.todayIdx = 0
		m.fail(m.loadToday())
	case "/":
		return m, m.openSearch()
	case "esc":
		if m.filtersActive() {
			m.clearFilters()
			m.fail(m.reload())
		}
	case "?":
		m.showHelp = !m.showHelp
	case "enter":
		if cur != nil {
			m.openDetail()
		}
	case "b":
		m.mode = modeBoards
	}
	return m, nil
}

func (m *Model) openBoard() {
	m.col, m.row = 0, 0
	m.mode = modeBoard
	m.fail(m.reload())
}

func (m *Model) updateBoards(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBoard
	case "j", "down":
		if m.boardIdx < len(m.boards)-1 {
			m.boardIdx++
		}
	case "k", "up":
		if m.boardIdx > 0 {
			m.boardIdx--
		}
	case "enter":
		m.openBoard()
	case "n":
		return m, m.ask("New board", "", func(s string) error {
			b, err := m.st.CreateBoard(s)
			if err != nil {
				return err
			}
			m.reload()
			for i, x := range m.boards {
				if x.ID == b.ID {
					m.boardIdx = i
				}
			}
			m.col, m.row = 0, 0
			m.mode = modeBoard
			return nil
		})
	case "d":
		if len(m.boards) > 1 {
			id := m.boards[m.boardIdx].ID
			m.confirm(fmt.Sprintf("Delete board %q and all its cards? (y/n)", m.boards[m.boardIdx].Name),
				func() error { return m.st.DeleteBoard(id) })
		}
	}
	return m, nil
}

func (m *Model) startEditing(c store.Card) tea.Cmd {
	m.area = textarea.New()
	m.area.ShowLineNumbers = false
	m.area.Prompt = ""
	m.area.CharLimit = 0
	m.area.SetWidth(m.detailInnerWidth())
	m.area.SetHeight(max(6, m.height-12))
	m.area.SetValue(c.Description)
	m.editing = true
	return m.area.Focus()
}

// updateEditing handles the free-text field: enter = new line, shift+enter = new line with a checkbox, esc saves.
func (m *Model) updateEditing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editing = false
		if cur := m.current(); cur != nil {
			c := *cur
			c.Description = strings.TrimRight(m.area.Value(), " \n")
			if c.Description != cur.Description {
				m.fail(m.updateCard(c, "edit text"))
			}
			m.fail(m.reload())
		}
		return m, nil
	}
	if handleEditorKey(&m.area, msg.String(), true) {
		return m, nil
	}
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(msg)
	return m, cmd
}

// toggleLine flips the checkbox on line i of the open card; a parent also flips its sub-tasks.
func (m *Model) toggleLine(i int) {
	cur := m.current()
	if cur == nil {
		return
	}
	c := *cur
	lines := strings.Split(c.Description, "\n")
	if !toggleLines(lines, i) {
		return
	}
	c.Description = strings.Join(lines, "\n")
	m.fail(m.updateCard(c, "toggle checkbox"))
	m.fail(m.reload())
}

func (m *Model) updateDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cur := m.current()
	if cur == nil {
		m.mode = modeBoard
		return m, nil
	}
	orig := *cur
	c := orig
	lines := strings.Split(c.Description, "\n")
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBoard
		m.fail(m.reload())
	case "j", "down":
		if m.lineIdx < len(lines)-1 {
			m.lineIdx++
		}
	case "k", "up":
		if m.lineIdx > 0 {
			m.lineIdx--
		}
	case "space", "x": // toggle the checkbox on the selected line
		m.toggleLine(m.lineIdx)
	case "e", "enter", "i":
		return m, m.startEditing(c)
	case "r":
		return m, m.ask("Project", c.Project, func(s string) error {
			c.Project = strings.TrimSpace(s)
			return m.updateCard(c, "edit project")
		}, withSuggestions(m.projects, false))
	case "t":
		return m, m.ask("Title", c.Title, func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("title is empty")
			}
			c.Title = strings.TrimSpace(s)
			return m.updateCard(c, "edit title")
		})
	case "u":
		return m, m.ask("Due date", c.Due, func(s string) error {
			s = strings.TrimSpace(s)
			if s != "" {
				if _, err := time.Parse(isoDate, s); err != nil {
					return fmt.Errorf("invalid date, use YYYY-MM-DD")
				}
			}
			c.Due = s
			return m.updateCard(c, "edit due date")
		}, withHint("YYYY-MM-DD · empty clears the date"))
	case "g":
		return m, m.ask("Tags", c.Tags, func(s string) error {
			var tags []string
			for _, p := range strings.Split(s, ",") {
				if p = strings.TrimSpace(p); p != "" {
					tags = append(tags, p)
				}
			}
			c.Tags = strings.Join(tags, ",")
			return m.updateCard(c, "edit tags")
		}, withSuggestions(m.tags, true), withHint("comma separated · tab completes the last tag"))
	case "p":
		c.Priority = (c.Priority + 1) % 4
		m.fail(m.updateCard(c, "change priority"))
		m.fail(m.reload())
	case "a":
		return m, m.attach("card", c.ID)
	case "o":
		m.openLinks(c.Title+"\n"+c.Description, "card", c.ID)
	}
	return m, nil
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

// SetTheme applies a theme by name (see themeNames); unknown names fall back to default.
func SetTheme(name string) string { return applyTheme(name) }

// ThemeNames lists the available themes.
func ThemeNames() []string { return themeNames() }

// SetNotice shows a one-shot message in the footer (used for startup news such as "backup saved").
func (m *Model) SetNotice(s string) { m.notice = s }
