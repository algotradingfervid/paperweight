package authweb_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// CD-150: web.NewRouter with AUTH's middleware as Options.Auth and Sessions
// nil gives working flash, and the session is saved once. Setting both
// Sessions and Auth is the documented misuse (each session saved twice).
func TestRouterWiring_FlashThroughAuthMiddleware(t *testing.T) {
	w := authtest.NewWorld(t)
	cfg := auth.DefaultSessionConfig(false)
	st := auth.NewSessionStore(w.DB, w.Clock, cfg)
	sm := auth.NewSessionManager(st, cfg)
	srv := authweb.New(w.Auth, st, sm, views.Default{}, authweb.Config{})

	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: srv.Middleware})
	r.Post("/save", func(rw http.ResponseWriter, r *http.Request) {
		web.Flash(r.Context(), "success", "Saved.")
		http.Redirect(rw, r, "/next", http.StatusSeeOther)
	})
	r.Get("/next", func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(web.TakeFlash(r.Context()))
	})
	hs := httptest.NewServer(r)
	defer hs.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	do := func(method, path string, status int) []web.Notice {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), method, hs.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != status {
			t.Fatalf("%s %s: status %d, want %d", method, path, resp.StatusCode, status)
		}
		var got []web.Notice
		if status == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("%s %s: decode: %v", method, path, err)
			}
		}
		return got
	}

	do(http.MethodPost, "/save", http.StatusSeeOther)
	if got, want := do(http.MethodGet, "/next", http.StatusOK), []web.Notice{{Level: "success", Text: "Saved."}}; !reflect.DeepEqual(got, want) {
		t.Errorf("GET /next flash = %v, want %v", got, want)
	}

	u, err := url.Parse(hs.URL)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	for _, c := range jar.Cookies(u) {
		if c.Name == sm.Cookie.Name {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie after the flash round trip")
	}
	var mine, all int
	ctx := context.Background()
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, auth.HashToken(token)).Scan(&mine); err != nil {
			return err
		}
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&all)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if mine != 1 || all != 1 {
		t.Errorf("sessions rows: %d for the cookie's session, %d in all; want 1 and 1", mine, all)
	}
}
