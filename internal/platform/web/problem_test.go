package web_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// problemMark is what the test renderer writes, so a test can tell its page
// from the bare error page.
const problemMark = "problem-page"

// problemRenderer records each call and writes problemMark, status and message.
func problemRenderer(calls *int) web.ProblemFunc {
	return func(w http.ResponseWriter, _ *http.Request, status int, msg string) {
		*calls++
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "%s %d %s", problemMark, status, msg)
	}
}

// errorVia answers req through web.Error behind mw (nil for none).
func errorVia(t *testing.T, mw func(http.Handler) http.Handler, req *http.Request, status int) *httptest.ResponseRecorder {
	t.Helper()
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		web.Error(w, r, status, forbidText)
	})
	if mw != nil {
		h = mw(h)
	}
	return serve(t, h, req)
}

// TestWeb_ProblemPageForFullPageGet: with a renderer set, web.Error hands a
// non-htmx GET answered 403 or 404 to it, and answers everything else as before.
func TestWeb_ProblemPageForFullPageGet(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		htmx    bool
		status  int
		problem bool
	}{
		{"GET 403", http.MethodGet, false, http.StatusForbidden, true},
		{"GET 404", http.MethodGet, false, http.StatusNotFound, true},
		{"GET 401", http.MethodGet, false, http.StatusUnauthorized, false},
		{"GET 405", http.MethodGet, false, http.StatusMethodNotAllowed, false},
		{"GET 409", http.MethodGet, false, http.StatusConflict, false},
		{"GET 410", http.MethodGet, false, http.StatusGone, false},
		{"GET 422", http.MethodGet, false, http.StatusUnprocessableEntity, false},
		{"GET 500", http.MethodGet, false, http.StatusInternalServerError, false},
		{"GET 503", http.MethodGet, false, http.StatusServiceUnavailable, false},
		{"POST 403", http.MethodPost, false, http.StatusForbidden, false},
		{"POST 404", http.MethodPost, false, http.StatusNotFound, false},
		{"HEAD 404", http.MethodHead, false, http.StatusNotFound, false},
		{"htmx GET 403", http.MethodGet, true, http.StatusForbidden, false},
		{"htmx GET 404", http.MethodGet, true, http.StatusNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			req := httptest.NewRequest(c.method, "/users", nil)
			if c.htmx {
				req.Header.Set("HX-Request", "true")
			}
			rec := errorVia(t, web.ProblemPages(problemRenderer(&calls)), req, c.status)
			if rec.Code != c.status {
				t.Errorf("status = %d, want %d", rec.Code, c.status)
			}
			body := rec.Body.String()
			if !c.problem {
				if calls != 0 || strings.Contains(body, problemMark) {
					t.Errorf("renderer called %d times, body = %q; want today's answer", calls, body)
				}
				return
			}
			if want := fmt.Sprintf("%s %d %s", problemMark, c.status, forbidText); calls != 1 || body != want {
				t.Errorf("renderer called %d times, body = %q; want once, %q", calls, body, want)
			}
		})
	}
}

// TestWeb_ProblemPageUnsetIsBarePage: with no renderer, a full-page GET 403
// or 404 keeps the bare page with the message in its alert.
func TestWeb_ProblemPageUnsetIsBarePage(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			rec := errorVia(t, nil, httptest.NewRequest(http.MethodGet, "/users", nil), status)
			if rec.Code != status {
				t.Errorf("status = %d, want %d", rec.Code, status)
			}
			body := rec.Body.String()
			if !strings.Contains(strings.ToLower(body), "<html") {
				t.Errorf("body = %q, want the bare HTML page", body)
			}
			if m := alertRe.FindStringSubmatch(body); m == nil || html.UnescapeString(m[1]) != forbidText {
				t.Errorf("body = %q, want a role=\"alert\" div holding %q", body, forbidText)
			}
		})
	}
}

// TestWeb_ProblemPageHTMXKeepsNotice: an htmx 403 under a renderer still gets
// the empty body and the notice trigger (CD-348).
func TestWeb_ProblemPageHTMXKeepsNotice(t *testing.T) {
	calls := 0
	rec := errorVia(t, web.ProblemPages(problemRenderer(&calls)), htmxRequest(), http.StatusForbidden)
	if calls != 0 || rec.Body.Len() != 0 {
		t.Errorf("renderer called %d times, body = %q; want none and empty", calls, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Trigger"); !strings.Contains(got, forbidText) {
		t.Errorf("HX-Trigger = %q, want the notice with %q", got, forbidText)
	}
}
