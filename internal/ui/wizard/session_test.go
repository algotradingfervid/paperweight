package wizard_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/ui/wizard"
)

func TestSessionValues_MergeAndClear(t *testing.T) {
	sm := scs.New()
	sv := wizard.SessionValues{SM: sm, Key: "wizard:party:new"}
	mux := http.NewServeMux()
	mux.HandleFunc("/step1", func(w http.ResponseWriter, r *http.Request) {
		sv.Merge(r.Context(), map[string]string{"name": "Acme", "code": "ACME"})
	})
	mux.HandleFunc("/step2", func(w http.ResponseWriter, r *http.Request) {
		sv.Merge(r.Context(), map[string]string{"city": "Pune", "code": "ACME-1"})
	})
	mux.HandleFunc("/clear", func(w http.ResponseWriter, r *http.Request) { sv.Clear(r.Context()) })
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(sv.Get(r.Context()))
	})
	h := sm.LoadAndSave(mux)

	var cookies []*http.Cookie
	call := func(path string) map[string]string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if cs := rec.Result().Cookies(); len(cs) > 0 {
			cookies = cs
		}
		var got map[string]string
		if path == "/get" {
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
		}
		return got
	}

	if got := call("/get"); len(got) != 0 {
		t.Errorf("empty session: Get = %v", got)
	}
	call("/step1")
	call("/step2")
	want := map[string]string{"name": "Acme", "code": "ACME-1", "city": "Pune"}
	if got := call("/get"); !reflect.DeepEqual(got, want) {
		t.Errorf("after two merges Get = %v, want %v", got, want)
	}
	call("/clear")
	if got := call("/get"); len(got) != 0 {
		t.Errorf("after Clear Get = %v", got)
	}
}
