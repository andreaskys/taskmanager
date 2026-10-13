// Package server is the phone companion of kbn: a small HTTP API plus an installable web page (PWA)
// over the same SQLite database the terminal app uses. It is meant to listen only on loopback or on
// the Tailscale interface, and every API call needs the access token.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andreas/kbn/internal/store"
)

//go:embed web
var webFS embed.FS

type Server struct {
	st    *store.Store
	token string
	mux   *http.ServeMux
}

// New builds the handler. token must be non-empty: a server without a token never starts.
func New(st *store.Store, token string) (*Server, error) {
	if len(token) < 16 {
		return nil, errors.New("access token is missing or too short (min 16 characters)")
	}
	s := &Server{st: st, token: token, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return secure(s.mux) }

// secure adds the headers every response gets and caps request bodies.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}

// auth wraps an API handler: the token comes in "Authorization: Bearer …".
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			time.Sleep(300 * time.Millisecond) // slows down guessing
			writeErr(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decode reads a JSON body into v, rejecting unknown fields.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func (s *Server) routes() {
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(sub) // the page holds no data: the API does the checking
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache") // always revalidate: a phone must never keep an old page
		files.ServeHTTP(w, r)
	})
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	s.api()
}

// CheckAddr refuses to listen on every interface: only loopback or a Tailscale (100.64.0.0/10) address.
func CheckAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("use an IP address (127.0.0.1 or your Tailscale 100.x address), not %q", host)
	}
	if ip.IsLoopback() {
		return nil
	}
	if _, cgnat, _ := net.ParseCIDR("100.64.0.0/10"); cgnat.Contains(ip) {
		return nil
	}
	if _, ts6, _ := net.ParseCIDR("fd7a:115c:a1e0::/48"); ts6.Contains(ip) {
		return nil
	}
	return fmt.Errorf("%s is not loopback or a Tailscale address; refusing to expose the board (use tailscale serve with 127.0.0.1)", ip)
}

// Run serves until the listener fails.
func (s *Server) Run(addr string) error {
	if err := CheckAddr(addr); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv.ListenAndServe()
}

// DefaultTokenPath is ~/.config/kbn/token (honoring XDG_CONFIG_HOME).
func DefaultTokenPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kbn", "token"), nil
}

// LoadOrCreateToken reads the token file, creating it (mode 0600, 256 random bits) when it is missing.
func LoadOrCreateToken(path string) (token string, created bool, err error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return token, true, nil
}
