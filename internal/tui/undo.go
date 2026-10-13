package tui

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/andreas/kbn/internal/store"
)

// Undo works on state diffs: every change goes through mutate, which snapshots all cards before and
// after, and stores what changed. Undo/redo then restore those exact states. The log lives in the
// database, so it survives restarts (see store.UndoPush).

// cardChange is one card's before/after state; Before == nil means created, After == nil means deleted.
type cardChange struct {
	ID     int64
	Before *store.Card
	After  *store.Card
}

type undoPayload struct{ Changes []cardChange }

func diffStates(before, after []store.Card) []cardChange {
	b := map[int64]store.Card{}
	for _, c := range before {
		b[c.ID] = c
	}
	a := map[int64]store.Card{}
	for _, c := range after {
		a[c.ID] = c
	}
	var out []cardChange
	for id, old := range b {
		cur, ok := a[id]
		switch {
		case !ok:
			o := old
			out = append(out, cardChange{ID: id, Before: &o})
		case cur != old:
			o, n := old, cur
			out = append(out, cardChange{ID: id, Before: &o, After: &n})
		}
	}
	for id, cur := range a {
		if _, ok := b[id]; !ok {
			n := cur
			out = append(out, cardChange{ID: id, After: &n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// mutate runs fn (a store change) and records it for undo. A new change clears the redo stack.
func (m *Model) mutate(desc string, fn func() error) error {
	before, err := m.st.CardStates()
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	after, err := m.st.CardStates()
	if err != nil {
		return err
	}
	changes := diffStates(before, after)
	if len(changes) == 0 {
		return nil
	}
	payload, err := json.Marshal(undoPayload{changes})
	if err != nil {
		return err
	}
	if err := m.st.UndoPush(store.StackUndo, desc, string(payload)); err != nil {
		return err
	}
	return m.st.UndoClear(store.StackRedo)
}

// apply writes one side of the changes: backward (undo) restores Before, forward (redo) restores After.
func (m *Model) apply(changes []cardChange, forward bool) error {
	state := func(c cardChange) *store.Card {
		if forward {
			return c.After
		}
		return c.Before
	}
	for _, c := range changes { // deletions first, so restored cards never collide with leftovers
		if state(c) == nil {
			if err := m.st.DeleteCard(c.ID); err != nil {
				return err
			}
		}
	}
	for _, c := range changes {
		if s := state(c); s != nil {
			if err := m.st.ApplyCard(*s); err != nil {
				return err
			}
		}
	}
	return nil
}

// step pops one entry from `from`, applies it and pushes it onto `to` (undo: undo→redo, redo: redo→undo).
func (m *Model) step(from, to string, forward bool, verb string) {
	e, ok, err := m.st.UndoPop(from)
	if err != nil {
		m.fail(err)
		return
	}
	if !ok {
		m.notice = "nothing to " + verb
		return
	}
	var p undoPayload
	if err := json.Unmarshal([]byte(e.Payload), &p); err != nil {
		m.fail(err)
		return
	}
	if err := m.apply(p.Changes, forward); err != nil {
		m.notice = fmt.Sprintf("can't %s %q: %v", verb, e.Desc, err) // e.g. its column was deleted since
		return
	}
	m.fail(m.st.UndoPush(to, e.Desc, e.Payload))
	m.fail(m.reload())
	if len(p.Changes) > 0 && m.mode == modeBoard {
		m.selectCard(p.Changes[0].ID)
	}
	m.notice = verb + ": " + e.Desc
}

func (m *Model) undoLast() { m.step(store.StackUndo, store.StackRedo, false, "undo") }
func (m *Model) redoLast() { m.step(store.StackRedo, store.StackUndo, true, "redo") }

// --- the changes the UI can make; each one is a single undo step ---

func (m *Model) addCard(c store.Card) error {
	return m.mutate("new card", func() error {
		created, err := m.st.AddCardWith(c)
		if err == nil {
			m.selectAft = created.ID
		}
		return err
	})
}

func (m *Model) updateCard(c store.Card, desc string) error {
	return m.mutate(desc, func() error { return m.st.UpdateCard(c) })
}

func (m *Model) deleteCard(c store.Card) error {
	return m.mutate("delete card", func() error { return m.st.DeleteCard(c.ID) })
}

func (m *Model) swapCards(a, b store.Card) error {
	return m.mutate("reorder", func() error { return m.st.SwapCards(a, b) })
}

// wipBlocked returns an error when moving c into the column would exceed its WIP limit.
func (m *Model) wipBlocked(c store.Card, toColumn int64) error {
	if c.ColumnID == toColumn {
		return nil
	}
	for i, col := range m.columns {
		if col.ID == toColumn && col.WIPLimit > 0 && i < len(m.totals) && m.totals[i] >= col.WIPLimit {
			return fmt.Errorf("%s is full (%d/%d) — raise the limit in the columns window (space m)", col.Name, m.totals[i], col.WIPLimit)
		}
	}
	return nil
}

func (m *Model) moveCard(c store.Card, toColumn int64) error {
	if c.ColumnID == toColumn {
		return nil
	}
	if err := m.wipBlocked(c, toColumn); err != nil {
		return err
	}
	return m.mutate("move card", func() error { return m.st.MoveCard(c.ID, toColumn) })
}

// moveCardTo drops the card at an exact index of a column.
func (m *Model) moveCardTo(c store.Card, toColumn int64, index int) error {
	if err := m.wipBlocked(c, toColumn); err != nil {
		return err
	}
	return m.mutate("move card", func() error { return m.st.MoveCardTo(c.ID, toColumn, index) })
}

func (m *Model) archiveDone() (n int, err error) {
	err = m.mutate("archive done", func() error {
		n, err = m.st.ArchiveDone(0)
		return err
	})
	return n, err
}

func (m *Model) unarchive(id int64) error {
	return m.mutate("restore archived card", func() error { return m.st.Unarchive(id) })
}
