package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// A handler whose context went through WithSessions inside the caller's own
// LoadAndSave (as AUTH's middleware does) can Flash and TakeFlash (CD-150).
func TestWeb_WithSessionsEnablesFlash(t *testing.T) {
	sm := scs.New()
	withSessions := func(next http.Handler) http.Handler {
		return sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(web.WithSessions(r.Context(), sm)))
		}))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /save", func(w http.ResponseWriter, r *http.Request) {
		web.Flash(r.Context(), "success", "Saved.")
		http.Redirect(w, r, "/next", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /next", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(r.Context()))
	})
	srv := httptest.NewServer(withSessions(mux))
	defer srv.Close()

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
		req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
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
		t.Errorf("first GET /next flash = %v, want %v", got, want)
	}
	if got := do(http.MethodGet, "/next", http.StatusOK); len(got) != 0 {
		t.Errorf("second GET /next flash = %v, want none", got)
	}
}
