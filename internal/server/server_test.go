package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andreas/kbn/internal/store"
)

const tok = "0123456789abcdef0123456789abcdef"

func newTest(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(st, tok)
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}

// call runs one request against the handler and decodes the JSON answer into out (when not nil).
func call(t *testing.T, s *Server, method, path, body string, out any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: bad JSON %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec
}

func TestNeedsAToken(t *testing.T) {
	if _, err := New(nil, "short"); err == nil {
		t.Fatal("a short token must be refused")
	}
	s, _ := newTest(t)
	for _, auth := range []string{"", "Bearer wrong", "Bearer " + tok + "x", tok} {
		req := httptest.NewRequest("GET", "/api/boards", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		start := time.Now()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: status %d, want 401", auth, rec.Code)
		}
		if time.Since(start) < 200*time.Millisecond {
			t.Fatal("a failed login should be slowed down")
		}
	}
	// the page and the health check are public, and carry the security headers
	for _, p := range []string{"/", "/healthz", "/manifest.webmanifest"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s: code=%d headers=%v", p, rec.Code, rec.Header())
		}
		if p == "/" && rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("the page must be revalidated so a phone never keeps an old version")
		}
	}
	for _, p := range []string{"/apple-touch-icon.png", "/icon-192.png", "/icon-512.png", "/icon.svg"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 || rec.Body.Len() == 0 {
			t.Fatalf("%s: code=%d len=%d", p, rec.Code, rec.Body.Len())
		}
	}
}

func TestCardsLifecycle(t *testing.T) {
	s, st := newTest(t)
	b, _ := st.CreateBoard("Work")
	cols, _ := st.Columns(b.ID)

	var boards []map[string]any
	call(t, s, "GET", "/api/boards", "", &boards)
	if len(boards) != 1 || boards[0]["name"] != "Work" {
		t.Fatalf("boards = %v", boards)
	}

	var c cardDTO
	rec := call(t, s, "POST", "/api/cards", `{"column_id":`+itoa(cols[0].ID)+`,"title":" Fix login ","project":"Acme","tags":"#Bug, urgent","due":"2030-01-02","priority":3}`, &c)
	if rec.Code != 201 || c.Title != "Fix login" || c.Tags != "bug,urgent" || c.Priority != 3 {
		t.Fatalf("create: %d %+v", rec.Code, c)
	}
	for body, why := range map[string]string{
		`{"column_id":1}`:                          "title is required",
		`{"column_id":1,"title":"x","due":"soon"}`: "bad date",
		`{"column_id":1,"title":"x","priority":9}`: "bad priority",
		`{"column_id":1,"title":"x","hack":true}`:  "unknown field",
		`{"column_id":1,"title":"   "}`:            "blank title",
		`not json`:                                 "invalid JSON",
	} {
		if rec := call(t, s, "POST", "/api/cards", body, nil); rec.Code != 400 {
			t.Fatalf("%s: status %d, want 400", why, rec.Code)
		}
	}

	rec = call(t, s, "PATCH", "/api/cards/"+itoa(c.ID), `{"description":"[ ] a","focus":true}`, &c)
	if rec.Code != 200 || c.Description != "[ ] a" || !c.Focus || c.Title != "Fix login" {
		t.Fatalf("patch should change only the sent fields: %d %+v", rec.Code, c)
	}
	var moved cardDTO
	last := cols[len(cols)-1].ID
	call(t, s, "POST", "/api/cards/"+itoa(c.ID)+"/move", `{"column_id":`+itoa(last)+`}`, &moved)
	if !moved.Done || moved.Column != "Done" {
		t.Fatalf("move to the last column marks it done: %+v", moved)
	}
	hist, _ := st.CardHistory(c.ID)
	if len(hist) < 2 || hist[len(hist)-1].Kind != store.EvDone {
		t.Fatalf("a move from the phone is logged like any other: %+v", hist)
	}

	var board struct {
		Columns []struct {
			Name  string
			Cards []cardDTO
		}
	}
	call(t, s, "GET", "/api/boards/"+itoa(b.ID), "", &board)
	if len(board.Columns) != 4 || len(board.Columns[3].Cards) != 1 {
		t.Fatalf("board = %+v", board)
	}
	if rec := call(t, s, "GET", "/api/boards/999", "", nil); rec.Code != 404 {
		t.Fatalf("missing board: %d", rec.Code)
	}
	if rec := call(t, s, "PATCH", "/api/cards/999", `{}`, nil); rec.Code != 404 {
		t.Fatalf("missing card: %d", rec.Code)
	}
	if rec := call(t, s, "DELETE", "/api/cards/"+itoa(c.ID), "", nil); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, ok := st.CardByID(c.ID); ok {
		t.Fatal("card should be gone")
	}
}

func TestTodayNotesDiarySearchSummary(t *testing.T) {
	s, st := newTest(t)
	b, _ := st.CreateBoard("Work")
	cols, _ := st.Columns(b.ID)
	today := time.Now().Format("2006-01-02")
	st.AddCardWith(store.Card{ColumnID: cols[0].ID, Project: "Jurídico", Title: "Rever contrato", Due: today, Tags: "legal"})
	d, _ := st.AddCardWith(store.Card{ColumnID: cols[0].ID, Title: "Focus me", Focus: true})
	st.MoveCard(d.ID, cols[3].ID)

	var td struct {
		Today string
		Due   []cardDTO
		Focus []cardDTO
	}
	call(t, s, "GET", "/api/today", "", &td)
	if td.Today != today || len(td.Due) != 1 || td.Due[0].DoneColumn != cols[3].ID {
		t.Fatalf("today = %+v", td)
	}

	var n noteDTO
	rec := call(t, s, "POST", "/api/notes", `{"title":"Senha do servidor","tags":"Infra","body":"ssh em 10.0.0.1","pinned":true}`, &n)
	if rec.Code != 201 || n.Tags != "infra" || !n.Pinned {
		t.Fatalf("note: %d %+v", rec.Code, n)
	}
	if rec := call(t, s, "POST", "/api/notes", `{"title":"","body":""}`, nil); rec.Code != 400 {
		t.Fatalf("empty note: %d", rec.Code)
	}
	call(t, s, "PUT", "/api/notes/"+itoa(n.ID), `{"title":"Senha nova","tags":"infra,vpn","body":"x"}`, &n)
	if n.Title != "Senha nova" || n.Tags != "infra,vpn" || !n.Pinned {
		t.Fatalf("update keeps what was not sent: %+v", n)
	}

	var j dayDTO
	call(t, s, "PUT", "/api/journal/"+today, `{"body":"ligar para o cliente #1"}`, &j)
	if j.Body != "ligar para o cliente #1" {
		t.Fatalf("journal: %+v", j)
	}
	if rec := call(t, s, "GET", "/api/journal/ontem", "", nil); rec.Code != 400 {
		t.Fatalf("bad day: %d", rec.Code)
	}
	var day struct {
		Body   string
		Events []map[string]any
	}
	call(t, s, "GET", "/api/journal/"+today, "", &day)
	if day.Body == "" || len(day.Events) == 0 {
		t.Fatalf("a day carries its card activity: %+v", day)
	}

	var res struct {
		Cards   []cardDTO
		Notes   []noteDTO
		Journal []dayDTO
	}
	call(t, s, "GET", "/api/search?q=juridico", "", &res) // accent-insensitive
	if len(res.Cards) != 1 || res.Cards[0].Title != "Rever contrato" {
		t.Fatalf("card search: %+v", res)
	}
	call(t, s, "GET", "/api/search?q=%23vpn", "", &res)
	if len(res.Notes) != 1 || len(res.Cards) != 0 {
		t.Fatalf("#tag finds the note only: %+v", res)
	}
	call(t, s, "GET", "/api/search?q=cliente", "", &res)
	if len(res.Journal) != 1 {
		t.Fatalf("diary search: %+v", res)
	}
	call(t, s, "GET", "/api/search?q=", "", &res)
	if len(res.Cards)+len(res.Notes)+len(res.Journal) != 0 {
		t.Fatal("empty query returns nothing")
	}

	var sm struct {
		Created, Done int
		Markdown      string
		PerDay        map[string]int `json:"per_day"`
	}
	call(t, s, "GET", "/api/summary", "", &sm)
	if sm.Created != 2 || sm.Done != 1 || !strings.Contains(sm.Markdown, "# Summary") {
		t.Fatalf("summary: %+v", sm)
	}
	if sm.PerDay[today] != 1 {
		t.Fatalf("per_day should count the card finished today: %v", sm.PerDay)
	}
	if rec := call(t, s, "GET", "/api/summary?from=2026-02-01&to=2026-01-01", "", nil); rec.Code != 400 {
		t.Fatalf("reversed range: %d", rec.Code)
	}
	if rec := call(t, s, "DELETE", "/api/notes/"+itoa(n.ID), "", nil); rec.Code != 204 {
		t.Fatalf("delete note: %d", rec.Code)
	}
}

func TestCheckAddr(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8787":     true,
		"[::1]:8787":         true,
		"100.101.102.103:80": true,
		"0.0.0.0:8787":       false,
		":8787":              false,
		"192.168.1.10:8787":  false,
		"localhost:8787":     false,
		"[::]:8787":          false,
		"8787":               false,
	} {
		if err := CheckAddr(addr); (err == nil) != ok {
			t.Errorf("CheckAddr(%q) = %v, want ok=%v", addr, err, ok)
		}
	}
}

func TestTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg", "token")
	a, created, err := LoadOrCreateToken(path)
	if err != nil || !created || len(a) != 64 {
		t.Fatalf("create: %q %v %v", a, created, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file must be private, mode %v", fi.Mode().Perm())
	}
	b, created, err := LoadOrCreateToken(path)
	if err != nil || created || b != a {
		t.Fatalf("reload: %q created=%v %v", b, created, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAPIRefusesEscapesBadColorsAndHugeInputs(t *testing.T) {
	s, st := newTest(t)
	b, _ := st.CreateBoard("W")
	cols, _ := st.Columns(b.ID)

	// terminal escape sequences sent through the API never reach the database
	var c cardDTO
	rec := call(t, s, "POST", "/api/cards", `{"column_id":`+itoa(cols[0].ID)+`,"title":"hi\u001b]0;pwned\u0007\u001b[2J","project":"p\u001b[31m","description":"d\u009b2J"}`, &c)
	if rec.Code != 201 || strings.ContainsAny(c.Title+c.Project+c.Description, "\x1b\x07\x9b") {
		t.Fatalf("escape sequences must be stripped: %d %+v", rec.Code, c)
	}
	var j dayDTO
	call(t, s, "PUT", "/api/journal/2026-01-01", `{"body":"x\u001b[2Jy"}`, &j)
	if strings.Contains(j.Body, "\x1b") {
		t.Fatalf("diary kept an escape: %q", j.Body)
	}

	// a color outside the palette (negative used to crash the terminal app) is refused
	for _, body := range []string{`{"title":"n","color":-1}`, `{"title":"n","color":64}`, `{"title":"n","color":9999999999}`} {
		if rec := call(t, s, "POST", "/api/notes", body, nil); rec.Code != 400 {
			t.Fatalf("%s: status %d, want 400", body, rec.Code)
		}
	}
	var n noteDTO
	call(t, s, "POST", "/api/notes", `{"title":"ok","color":7}`, &n)
	if rec := call(t, s, "PUT", "/api/notes/"+itoa(n.ID), `{"title":"ok","color":-3}`, nil); rec.Code != 400 {
		t.Fatalf("update with a bad color: %d", rec.Code)
	}

	// size limits answer 400, not 500
	long := strings.Repeat("a", store.MaxLine+1)
	if rec := call(t, s, "PATCH", "/api/cards/"+itoa(c.ID), `{"title":"`+long+`"}`, nil); rec.Code != 400 {
		t.Fatalf("long title on patch: %d", rec.Code)
	}
	if rec := call(t, s, "POST", "/api/cards", `{"column_id":`+itoa(cols[0].ID)+`,"title":"`+long+`"}`, nil); rec.Code != 400 {
		t.Fatalf("long title on create: %d", rec.Code)
	}
	if rec := call(t, s, "PUT", "/api/journal/2026-01-02", `{"body":"`+strings.Repeat("a", store.MaxText+1)+`"}`, nil); rec.Code != 400 {
		t.Fatalf("long diary entry: %d", rec.Code)
	}
	if rec := call(t, s, "GET", "/api/search?q="+strings.Repeat("a", 201), "", nil); rec.Code != 400 {
		t.Fatalf("long search query: %d", rec.Code)
	}
	if rec := call(t, s, "GET", "/api/summary?from=2000-01-01&to=2030-01-01", "", nil); rec.Code != 400 {
		t.Fatalf("a multi-year summary range must be refused: %d", rec.Code)
	}
	if rec := call(t, s, "GET", "/api/summary?from=2026-01-01&to=2026-12-31", "", nil); rec.Code != 200 {
		t.Fatalf("a one-year range is fine: %d", rec.Code)
	}
}
