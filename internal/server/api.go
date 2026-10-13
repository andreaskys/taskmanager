package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/andreas/kbn/internal/store"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type cardDTO struct {
	ID          int64  `json:"id"`
	ColumnID    int64  `json:"column_id"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	Due         string `json:"due"`
	Tags        string `json:"tags"`
	Focus       bool   `json:"focus"`
	Done        bool   `json:"done"`
	Archived    bool   `json:"archived"`
	Board       string `json:"board,omitempty"`
	Column      string `json:"column,omitempty"`
	BoardID     int64  `json:"board_id,omitempty"`
	DoneColumn  int64  `json:"done_column_id,omitempty"` // last column of the card's board
}

func toCard(c store.Card) cardDTO {
	return cardDTO{ID: c.ID, ColumnID: c.ColumnID, Project: c.Project, Title: c.Title, Description: c.Description,
		Priority: c.Priority, Due: c.Due, Tags: c.Tags, Focus: c.Focus, Done: c.DoneAt != "", Archived: c.ArchivedAt != ""}
}

func toDue(d store.DueCard) cardDTO {
	c := toCard(d.Card)
	c.Board, c.Column = d.BoardName, d.ColumnName
	c.BoardID, c.DoneColumn = d.BoardID, d.DoneColumnID
	c.Done = d.ColumnID == d.DoneColumnID
	return c
}

type noteDTO struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Tags   string `json:"tags"`
	Body   string `json:"body"`
	Color  int    `json:"color"`
	Pinned bool   `json:"pinned"`
	Due    string `json:"due"`
}

func toNote(n store.Note) noteDTO {
	return noteDTO{n.ID, n.Title, n.Tags, n.Body, n.Color, n.Pinned, n.Due}
}

type dayDTO struct {
	Day  string `json:"day"`
	Body string `json:"body"`
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func validDate(s string) bool { _, err := time.Parse("2006-01-02", s); return err == nil }

func (s *Server) fail(w http.ResponseWriter, err error) {
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) api() {
	a := s.auth
	m := s.mux
	m.HandleFunc("GET /api/boards", a(s.listBoards))
	m.HandleFunc("GET /api/boards/{id}", a(s.getBoard))
	m.HandleFunc("GET /api/today", a(s.today))
	m.HandleFunc("POST /api/cards", a(s.createCard))
	m.HandleFunc("PATCH /api/cards/{id}", a(s.patchCard))
	m.HandleFunc("POST /api/cards/{id}/move", a(s.moveCard))
	m.HandleFunc("DELETE /api/cards/{id}", a(s.deleteCard))
	m.HandleFunc("GET /api/notes", a(s.listNotes))
	m.HandleFunc("POST /api/notes", a(s.createNote))
	m.HandleFunc("PUT /api/notes/{id}", a(s.updateNote))
	m.HandleFunc("DELETE /api/notes/{id}", a(s.deleteNote))
	m.HandleFunc("GET /api/journal", a(s.listJournal))
	m.HandleFunc("GET /api/journal/{day}", a(s.getJournal))
	m.HandleFunc("PUT /api/journal/{day}", a(s.putJournal))
	m.HandleFunc("GET /api/search", a(s.search))
	m.HandleFunc("GET /api/summary", a(s.summary))
}

// --- boards and cards ---

func (s *Server) listBoards(w http.ResponseWriter, r *http.Request) {
	boards, err := s.st.Boards()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := []map[string]any{}
	for _, b := range boards {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name})
	}
	writeJSON(w, 200, out)
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad board id")
		return
	}
	boards, err := s.st.Boards()
	if err != nil {
		s.fail(w, err)
		return
	}
	var name string
	for _, b := range boards {
		if b.ID == id {
			name = b.Name
		}
	}
	if name == "" {
		writeErr(w, 404, "board not found")
		return
	}
	cols, err := s.st.Columns(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	type colDTO struct {
		ID    int64     `json:"id"`
		Name  string    `json:"name"`
		WIP   int       `json:"wip_limit"`
		Cards []cardDTO `json:"cards"`
	}
	out := struct {
		ID      int64    `json:"id"`
		Name    string   `json:"name"`
		Columns []colDTO `json:"columns"`
	}{ID: id, Name: name, Columns: []colDTO{}}
	for _, c := range cols {
		cards, err := s.st.Cards(c.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		cd := colDTO{ID: c.ID, Name: c.Name, WIP: c.WIPLimit, Cards: []cardDTO{}}
		for _, k := range cards {
			cd.Cards = append(cd.Cards, toCard(k))
		}
		out.Columns = append(out.Columns, cd)
	}
	writeJSON(w, 200, out)
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	until := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	due, err := s.st.DueCards(until)
	if err != nil {
		s.fail(w, err)
		return
	}
	focus, err := s.st.FocusCards()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := map[string]any{"today": time.Now().Format("2006-01-02"), "due": []cardDTO{}, "focus": []cardDTO{}}
	dues, foc := []cardDTO{}, []cardDTO{}
	for _, d := range due {
		dues = append(dues, toDue(d))
	}
	for _, d := range focus {
		foc = append(foc, toDue(d))
	}
	out["due"], out["focus"] = dues, foc
	writeJSON(w, 200, out)
}

type cardIn struct {
	ColumnID    int64   `json:"column_id"`
	Project     *string `json:"project"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *int    `json:"priority"`
	Due         *string `json:"due"`
	Tags        *string `json:"tags"`
	Focus       *bool   `json:"focus"`
}

// apply copies the fields that were sent onto c, validating them.
func (in cardIn) apply(c *store.Card) error {
	if in.Project != nil {
		c.Project = strings.TrimSpace(*in.Project)
	}
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			return fmt.Errorf("title is empty")
		}
		c.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		c.Description = *in.Description
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 3 {
			return fmt.Errorf("priority must be 0..3")
		}
		c.Priority = *in.Priority
	}
	if in.Due != nil {
		if *in.Due != "" && !validDate(*in.Due) {
			return fmt.Errorf("due must be YYYY-MM-DD")
		}
		c.Due = *in.Due
	}
	if in.Tags != nil {
		c.Tags = store.NormTags(*in.Tags)
	}
	if in.Focus != nil {
		c.Focus = *in.Focus
	}
	return nil
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	var in cardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.ColumnID == 0 || in.Title == nil {
		writeErr(w, 400, "column_id and title are required")
		return
	}
	c := store.Card{ColumnID: in.ColumnID}
	if err := in.apply(&c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	c, err := s.st.AddCardWith(c)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, toCard(c))
}

func (s *Server) patchCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad card id")
		return
	}
	d, found := s.st.CardByID(id)
	if !found {
		writeErr(w, 404, "card not found")
		return
	}
	var in cardIn
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	c := d.Card
	if err := in.apply(&c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.st.UpdateCard(c); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, toCard(c))
}

func (s *Server) moveCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad card id")
		return
	}
	var in struct {
		ColumnID int64 `json:"column_id"`
	}
	if err := decode(r, &in); err != nil || in.ColumnID == 0 {
		writeErr(w, 400, "column_id is required")
		return
	}
	if _, found := s.st.CardByID(id); !found {
		writeErr(w, 404, "card not found")
		return
	}
	if err := s.st.MoveCard(id, in.ColumnID); err != nil {
		s.fail(w, err)
		return
	}
	d, _ := s.st.CardByID(id)
	writeJSON(w, 200, toDue(d))
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad card id")
		return
	}
	if _, found := s.st.CardByID(id); !found {
		writeErr(w, 404, "card not found")
		return
	}
	if err := s.st.DeleteCard(id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- notes ---

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := s.st.Notes()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := []noteDTO{}
	for _, n := range notes {
		out = append(out, toNote(n))
	}
	writeJSON(w, 200, out)
}

type noteIn struct {
	Title  string `json:"title"`
	Tags   string `json:"tags"`
	Body   string `json:"body"`
	Color  *int   `json:"color"`
	Pinned *bool  `json:"pinned"`
	Due    string `json:"due"`
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	var in noteIn
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Due != "" && !validDate(in.Due) {
		writeErr(w, 400, "due must be YYYY-MM-DD")
		return
	}
	n := store.Note{Title: in.Title, Tags: in.Tags, Body: in.Body}
	if in.Color != nil {
		n.Color = *in.Color
	}
	n, err := s.st.AddNoteFull(n)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Pinned != nil || in.Due != "" { // pin and reminder are saved by the update
		n.Pinned = in.Pinned != nil && *in.Pinned
		n.Due = in.Due
		if err := s.st.UpdateNote(n); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, 201, toNote(n))
}

func (s *Server) findNote(id int64) (store.Note, bool) {
	notes, _ := s.st.Notes()
	for _, n := range notes {
		if n.ID == id {
			return n, true
		}
	}
	return store.Note{}, false
}

func (s *Server) updateNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad note id")
		return
	}
	n, found := s.findNote(id)
	if !found {
		writeErr(w, 404, "note not found")
		return
	}
	var in noteIn
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Due != "" && !validDate(in.Due) {
		writeErr(w, 400, "due must be YYYY-MM-DD")
		return
	}
	n.Title, n.Tags, n.Body, n.Due = in.Title, in.Tags, in.Body, in.Due
	if in.Color != nil {
		n.Color = *in.Color
	}
	if in.Pinned != nil {
		n.Pinned = *in.Pinned
	}
	if err := s.st.UpdateNote(n); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	n, _ = s.findNote(id)
	writeJSON(w, 200, toNote(n))
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "bad note id")
		return
	}
	if _, found := s.findNote(id); !found {
		writeErr(w, 404, "note not found")
		return
	}
	if err := s.st.DeleteNote(id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- diary ---

func (s *Server) listJournal(w http.ResponseWriter, r *http.Request) {
	days, err := s.st.JournalEntries()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := []dayDTO{}
	for _, d := range days {
		out = append(out, dayDTO{d.Day, d.Body})
	}
	writeJSON(w, 200, out)
}

func (s *Server) getJournal(w http.ResponseWriter, r *http.Request) {
	day := r.PathValue("day")
	if !validDate(day) {
		writeErr(w, 400, "day must be YYYY-MM-DD")
		return
	}
	e, _ := s.st.Journal(day)
	ev, _ := s.st.EventsOn(day)
	type evDTO struct {
		Kind  string `json:"kind"`
		Title string `json:"title"`
		From  string `json:"from"`
		To    string `json:"to"`
		At    string `json:"at"`
	}
	events := []evDTO{}
	for _, x := range ev {
		events = append(events, evDTO{x.Kind, x.Title, x.From, x.To, x.At.Format(time.RFC3339)})
	}
	writeJSON(w, 200, map[string]any{"day": day, "body": e.Body, "events": events})
}

func (s *Server) putJournal(w http.ResponseWriter, r *http.Request) {
	day := r.PathValue("day")
	if !validDate(day) {
		writeErr(w, 400, "day must be YYYY-MM-DD")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.st.SaveJournal(day, in.Body); err != nil {
		s.fail(w, err)
		return
	}
	e, _ := s.st.Journal(day)
	writeJSON(w, 200, dayDTO{day, e.Body})
}

// --- search and summary ---

var foldT = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// fold lowercases and strips accents so "juridico" finds "jurídico" (same rule as the terminal search).
func fold(s string) string {
	out, _, err := transform.String(foldT, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return out
}

func matches(hay, tags string, terms []string) bool {
	for _, t := range terms {
		if strings.HasPrefix(t, "#") {
			if len(t) == 1 || !strings.Contains(tags, t[1:]) {
				return false
			}
		} else if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	terms := strings.Fields(fold(r.URL.Query().Get("q")))
	out := struct {
		Cards   []cardDTO `json:"cards"`
		Notes   []noteDTO `json:"notes"`
		Journal []dayDTO  `json:"journal"`
	}{[]cardDTO{}, []noteDTO{}, []dayDTO{}}
	if len(terms) == 0 {
		writeJSON(w, 200, out)
		return
	}
	cards, err := s.st.AllCards()
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, d := range cards {
		hay := fold(d.Project + " " + d.Title + " " + d.Tags + " " + d.Description + " " + d.BoardName + " " + d.ColumnName)
		if matches(hay, fold(d.Tags), terms) && len(out.Cards) < 50 {
			out.Cards = append(out.Cards, toDue(d))
		}
	}
	notes, _ := s.st.Notes()
	for _, n := range notes {
		if matches(fold(n.Title+" "+n.Tags+" "+n.Body), fold(n.Tags), terms) {
			out.Notes = append(out.Notes, toNote(n))
		}
	}
	days, _ := s.st.JournalEntries()
	for _, d := range days {
		if matches(fold(d.Day+" diary "+d.Body), "", terms) && len(out.Journal) < 50 {
			out.Journal = append(out.Journal, dayDTO{d.Day, d.Body})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" { // default: the current week, Monday to Sunday
		mon := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
		from, to = mon.Format("2006-01-02"), mon.AddDate(0, 0, 6).Format("2006-01-02")
	}
	if !validDate(from) || !validDate(to) || from > to {
		writeErr(w, 400, "from/to must be YYYY-MM-DD dates in order")
		return
	}
	sm, err := s.st.Summarize(from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	done := []cardDTO{}
	perDay := map[string]int{} // finished cards per local day, for the weekly chart
	for _, d := range sm.DoneCards {
		done = append(done, toDue(d))
		if t, err := time.Parse("2006-01-02 15:04:05", d.DoneAt); err == nil {
			perDay[t.In(time.Local).Format("2006-01-02")]++
		}
	}
	writeJSON(w, 200, map[string]any{
		"from": sm.From, "to": sm.To, "created": sm.Created, "done": sm.Done, "moves": sm.Moves,
		"overdue": sm.Overdue, "avg_days": sm.AvgDays, "diary_days": sm.Journal,
		"projects": sm.Projects, "per_day": perDay, "done_cards": done, "markdown": sm.Markdown(),
	})
}
