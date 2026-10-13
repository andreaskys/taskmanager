package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type hitKind int

const (
	hitCard   hitKind = iota // a card on the board (col, row)
	hitColumn                // empty space inside a column (col)
	hitToday                 // an item of the today list (row = index)
	hitBoard                 // an item of the boards list (row = index)
	hitLine                  // a text line of the open card (row = line)
)

// hit is a clickable screen rectangle (inclusive), rebuilt on every render.
type hit struct {
	x0, x1, y0, y1 int
	kind           hitKind
	col, row       int
}

type dragState struct {
	card    int64
	over    int // column under the pointer, -1 if none
	idx     int // insertion index in that column (cards above it stay above), -1 if unknown
	started bool
}

type click struct {
	at   time.Time
	kind hitKind
	col  int
	row  int
}

const doubleClick = 400 * time.Millisecond

func (m *Model) hitAt(x, y int) (hit, bool) {
	for i := len(m.hits) - 1; i >= 0; i-- { // later hits are on top (cards over columns)
		h := m.hits[i]
		if x >= h.x0 && x <= h.x1 && y >= h.y0 && y <= h.y1 {
			return h, true
		}
	}
	return hit{}, false
}

// isDouble records the click and reports whether it repeats the previous one on the same target.
func (m *Model) isDouble(h hit) bool {
	now := time.Now()
	dbl := m.lastClick.kind == h.kind && m.lastClick.col == h.col && m.lastClick.row == h.row &&
		now.Sub(m.lastClick.at) < doubleClick
	m.lastClick = click{now, h.kind, h.col, h.row}
	if dbl {
		m.lastClick = click{}
	}
	return dbl
}

func (m *Model) updateMouse(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == modeInput || m.mode == modeConfirm || m.mode == modeSearch || m.mode == modeNewCard || m.mode == modeScratch || m.mode == modeHelp || m.mode == modeColumns || m.mode == modeNotes || m.mode == modeNoteEdit || m.mode == modeJournal || m.mode == modeJournalEdit || m.mode == modeSummary || m.mode == modeProjects || m.mode == modeLinks || m.editing {
		return m, nil
	}
	m.notice, m.err = "", ""
	m.leader = false
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if mm := msg.Mouse(); mm.Button == tea.MouseLeft {
			m.mouseDown(mm.X, mm.Y)
		}
	case tea.MouseMotionMsg:
		if m.drag != nil {
			mm := msg.Mouse()
			m.drag.started = true
			m.drag.over, m.drag.idx = -1, -1
			if h, ok := m.hitAt(mm.X, mm.Y); ok && (h.kind == hitCard || h.kind == hitColumn) {
				m.drag.over = h.col
				m.drag.idx = len(m.cards[h.col]) // blank space: end of the column
				if h.kind == hitCard {
					m.drag.idx = h.row // upper half: before this card, lower half: after it
					if mm.Y > (h.y0+h.y1)/2 {
						m.drag.idx++
					}
				}
			}
		}
	case tea.MouseReleaseMsg:
		m.mouseUp()
	case tea.MouseWheelMsg:
		mm := msg.Mouse()
		d := 0
		switch mm.Button {
		case tea.MouseWheelUp:
			d = -1
		case tea.MouseWheelDown:
			d = 1
		}
		m.mouseWheel(mm.X, mm.Y, d)
	}
	return m, nil
}

func (m *Model) mouseDown(x, y int) {
	h, ok := m.hitAt(x, y)
	if !ok {
		return
	}
	dbl := m.isDouble(h)
	switch m.mode {
	case modeBoard:
		switch h.kind {
		case hitCard:
			m.col, m.row = h.col, h.row
			if dbl {
				m.openDetail()
				return
			}
			if c := m.current(); c != nil {
				m.drag = &dragState{card: c.ID, over: h.col, idx: -1}
			}
		case hitColumn:
			m.col = h.col
			m.clamp()
		}
	case modeDetail:
		if h.kind == hitLine {
			m.lineIdx = h.row
			if x <= 6 { // on the checkbox glyph
				m.toggleLine(h.row)
			}
		}
	case modeToday:
		if h.kind == hitToday {
			m.todayIdx = h.row
			if dbl && h.row < len(m.dueCards) {
				m.jumpTo(m.dueCards[h.row])
			}
		}
	case modeBoards:
		if h.kind == hitBoard {
			m.boardIdx = h.row
			if dbl {
				m.openBoard()
			}
		}
	}
}

// mouseUp drops a dragged card at the marked position of the column under the pointer.
func (m *Model) mouseUp() {
	d := m.drag
	m.drag = nil
	if d == nil || !d.started || d.over < 0 || d.over >= len(m.columns) {
		return
	}
	for ci, cs := range m.cards {
		for ri, c := range cs {
			if c.ID != d.card {
				continue
			}
			to := m.columns[d.over].ID
			var err error
			switch {
			case m.sortMode != sortManual || d.idx < 0: // sorted view: position is not meaningful, append
				err = m.moveCard(c, to)
			default:
				idx := d.idx
				if ci == d.over && ri < idx {
					idx-- // the card itself no longer occupies a slot above the drop point
				}
				if ci == d.over && idx == ri {
					return // dropped where it already is
				}
				err = m.moveCardTo(c, to, idx)
			}
			m.fail(err)
			m.fail(m.reload())
			m.selectCard(c.ID)
			return
		}
	}
}

func (m *Model) mouseWheel(x, y, d int) {
	if d == 0 {
		return
	}
	switch m.mode {
	case modeBoard:
		if h, ok := m.hitAt(x, y); ok && (h.kind == hitCard || h.kind == hitColumn) {
			if m.col != h.col {
				m.col = h.col
				m.clamp()
			}
		}
		if m.col < len(m.cards) {
			m.row = max(0, min(m.row+d, len(m.cards[m.col])-1))
		}
	case modeDetail:
		m.lineIdx = max(0, min(m.lineIdx+d, len(splitLines(m.currentText()))-1))
	case modeToday:
		m.todayIdx = max(0, min(m.todayIdx+d, len(m.dueCards)-1))
	case modeBoards:
		m.boardIdx = max(0, min(m.boardIdx+d, len(m.boards)-1))
	}
}
