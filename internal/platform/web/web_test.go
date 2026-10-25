package web_test

// Tests for WP0 Task 4 (web platform). They run against the files on disk in
// static/ (the embedded files keep their plain names, CD-47) and assume:
//
//   - web.Static() serves the full request path: a GET of web.HTMXURL
//     ("/static/htmx.<hash>.js") is answered by web.Static() as is, and the
//     router serves static files with r.Handle("/static/*", web.Static()).
//   - static/htmx.min.js is htmx 2.0.11 dist/htmx.min.js and
//     static/alpine.min.js is @alpinejs/csp 3.17.5 dist/cdn.min.js.
//
// Expected format of static/SOURCES.md (TestWeb_StaticFilesMatchSources):
// one section per file in static/ (other than SOURCES.md itself), headed by
// "## " and the plain file name, holding one "- Key: value" line per fact:
//
//	## htmx.min.js
//
//	- Package: htmx.org
//	- Version: 2.0.11
//	- Source: https://registry.npmjs.org/htmx.org/-/htmx.org-2.0.11.tgz (dist/htmx.min.js)
//	- Licence: 0BSD
//	- SHA-256: <64 lowercase hex digits of the file>
//
//	## alpine.min.js
//
//	- Package: @alpinejs/csp
//	- Version: 3.17.5
//	- Source: https://registry.npmjs.org/@alpinejs/csp/-/csp-3.17.5.tgz (dist/cdn.min.js)
//	- Licence: MIT
//	- SHA-256: <64 lowercase hex digits of the file>

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
)

const (
	immutable  = "public, max-age=31536000, immutable"
	busyText   = "The system is busy. Please try again in a moment."
	panicText  = "Something went wrong. Try again, and if it keeps happening tell the administrator."
	forbidText = "You don't have permission to read fixture.Order. Ask your administrator for a role that allows it."
	qtyText    = "Quantity must be more than 0."
)

var securityHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Referrer-Policy":         "same-origin",
	"X-Frame-Options":         "DENY",
	"Content-Security-Policy": "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'",
}

// alertRe matches a role="alert" div and captures its text content.
var alertRe = regexp.MustCompile(`<div[^>]*\brole="alert"[^>]*>\s*([^<]*?)\s*</div>`)

// teapotErr is a test-local HTTP error: web.ErrorFrom must map it through its
// own Status() method without platform/web importing any kernel package (CD-90).
type teapotErr struct{}

func (teapotErr) Error() string { return "Short and stout." }
func (teapotErr) Status() int   { return 418 }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func readStatic(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name)) //nolint:gosec // G304: test reads its own package files
	if err != nil {
		t.Fatalf("read static/%s: %v", name, err)
	}
	return b
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serve runs one request; a panic that escapes the handler fails the test
// instead of aborting the whole run.
func serve(t *testing.T, h http.Handler, req *http.Request) (rec *httptest.ResponseRecorder) {
	t.Helper()
	rec = httptest.NewRecorder()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s %s: panic escaped the handler: %v", req.Method, req.URL.Path, p)
		}
	}()
	h.ServeHTTP(rec, req)
	return rec
}

// needURL stops the test when a static URL is empty, which would make
// httptest.NewRequest panic and abort the whole run.
func needURL(t *testing.T, name, url string) {
	t.Helper()
	if url == "" {
		t.Fatalf("web.%s is empty, want /static/<base>.<hash>.js", name)
	}
}

func TestWeb_SecurityHeaders(t *testing.T) {
	needURL(t, "HTMXURL", web.HTMXURL)
	r := web.NewRouter(web.Options{Logger: discardLogger()})
	r.Handle("/static/*", web.Static())

	rec := serve(t, r, httptest.NewRequest(http.MethodGet, web.HTMXURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %q status = %d, want 200", web.HTMXURL, rec.Code)
	}
	for name, want := range securityHeaders {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Errorf("header Cache-Control = %q, want %q", got, immutable)
	}
}

func TestWeb_StaticHashedNames(t *testing.T) {
	cases := []struct {
		name, url, pattern, file string
	}{
		{"htmx", web.HTMXURL, `^/static/htmx\.[0-9a-f]{8}\.js$`, "htmx.min.js"},
		{"alpine", web.AlpineURL, `^/static/alpine-csp\.[0-9a-f]{8}\.js$`, "alpine.min.js"},
	}
	h := web.Static()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !regexp.MustCompile(c.pattern).MatchString(c.url) {
				t.Fatalf("URL = %q, want match for %s", c.url, c.pattern)
			}
			body := readStatic(t, c.file)
			wantHash := sha256Hex(body)[:8]
			if !strings.Contains(c.url, "."+wantHash+".") {
				t.Errorf("URL = %q, want hash %s (first 8 hex of SHA-256 of static/%s)", c.url, wantHash, c.file)
			}
			rec := serve(t, h, httptest.NewRequest(http.MethodGet, c.url, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %q status = %d, want 200", c.url, rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != immutable {
				t.Errorf("GET %q Cache-Control = %q, want %q", c.url, got, immutable)
			}
			if !reflect.DeepEqual(rec.Body.Bytes(), body) {
				t.Errorf("GET %q body (%d bytes) differs from static/%s (%d bytes)", c.url, rec.Body.Len(), c.file, len(body))
			}
		})
	}
	for _, path := range []string{"/static/htmx.min.js", "/static/alpine.min.js", "/static/htmx.00000000.js"} {
		t.Run("404 "+path, func(t *testing.T) {
			rec := serve(t, h, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %q status = %d, want 404", path, rec.Code)
			}
		})
	}
}

func TestWeb_CrossOriginPostRejected(t *testing.T) {
	authCalls := 0
	r := web.NewRouter(web.Options{
		Logger: discardLogger(),
		Auth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authCalls++
				next.ServeHTTP(w, r)
			})
		},
	})
	r.Post("/save", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cross := httptest.NewRequest(http.MethodPost, "http://example.com/save", nil)
	cross.Header.Set("Origin", "https://evil.example")
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := serve(t, r, cross)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST status = %d, want 403", rec.Code)
	}
	if authCalls != 0 {
		t.Errorf("cross-origin POST: Auth middleware called %d times, want 0", authCalls)
	}

	same := httptest.NewRequest(http.MethodPost, "http://example.com/save", nil)
	same.Header.Set("Origin", "http://example.com")
	same.Header.Set("Sec-Fetch-Site", "same-origin")
	rec = serve(t, r, same)
	if rec.Code != http.StatusOK {
		t.Errorf("same-origin POST status = %d, want 200", rec.Code)
	}
	if authCalls != 1 {
		t.Errorf("same-origin POST: Auth middleware called %d times, want 1", authCalls)
	}
}

func TestWeb_MiddlewareOrder(t *testing.T) {
	t.Run("auth after session load", func(t *testing.T) {
		sm := scs.New()
		authCalls := 0
		r := web.NewRouter(web.Options{
			Logger:   discardLogger(),
			Sessions: sm,
			Auth: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					authCalls++
					sm.Put(r.Context(), "principal", "user-1") // panics if the session is not loaded yet
					next.ServeHTTP(w, r)
				})
			},
		})
		var seen string
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			seen = sm.GetString(r.Context(), "principal")
			w.WriteHeader(http.StatusOK)
		})
		rec := serve(t, r, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		if authCalls != 1 {
			t.Errorf("Auth middleware called %d times, want 1", authCalls)
		}
		if seen != "user-1" {
			t.Errorf("handler session value = %q, want %q (put by Auth)", seen, "user-1")
		}
	})

	t.Run("security headers and recover wrap auth", func(t *testing.T) {
		r := web.NewRouter(web.Options{
			Logger: discardLogger(),
			Auth: func(http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/panic" {
						panic("auth blew up")
					}
					w.WriteHeader(http.StatusUnauthorized)
				})
			},
		})
		r.Get("/*", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

		rec := serve(t, r, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET /x status = %d, want 401 from Auth", rec.Code)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("Auth's 401: X-Frame-Options = %q, want DENY (security headers run before Auth)", got)
		}
		rec = serve(t, r, httptest.NewRequest(http.MethodGet, "/panic", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("panic in Auth: status = %d, want 500 (recover runs before Auth)", rec.Code)
		}
	})

	t.Run("nil sessions and auth", func(t *testing.T) {
		r := web.NewRouter(web.Options{Logger: discardLogger()})
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
		})
		rec := serve(t, r, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Errorf("GET / = %d %q, want 200 %q", rec.Code, rec.Body.String(), "ok")
		}
	})
}

func TestWeb_BusyIs503(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantText   string
	}{
		{"db busy", fmt.Errorf("save order: %w", db.ErrBusy), http.StatusServiceUnavailable, busyText},
		{"status error", fmt.Errorf("x: %w", teapotErr{}), 418, "Short and stout."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := web.NewRouter(web.Options{Logger: discardLogger()})
			r.Get("/", func(w http.ResponseWriter, r *http.Request) { web.ErrorFrom(w, r, c.err) })
			rec := serve(t, r, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != c.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), c.wantText) {
				t.Errorf("body = %q, want it to contain %q", rec.Body.String(), c.wantText)
			}
		})
	}
	t.Run("other error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		web.ErrorFrom(rec, httptest.NewRequest(http.MethodGet, "/", nil), errors.New("disk on fire"))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

func TestWeb_PanicGives500Page(t *testing.T) {
	r := web.NewRouter(web.Options{Logger: discardLogger()})
	r.Get("/", func(http.ResponseWriter, *http.Request) { panic("boom") })
	rec := serve(t, r, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), panicText) {
		t.Errorf("body = %q, want it to contain %q", rec.Body.String(), panicText)
	}
}

func htmxRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/save", nil)
	req.Header.Set("HX-Request", "true")
	return req
}

func TestWeb_HTMXErrorToAlertRegion(t *testing.T) {
	rec := httptest.NewRecorder()
	web.Error(rec, htmxRequest(), http.StatusForbidden, forbidText)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	trigger := rec.Header().Get("HX-Trigger")
	var got any
	if err := json.Unmarshal([]byte(trigger), &got); err != nil {
		t.Fatalf("HX-Trigger = %q is not JSON: %v", trigger, err)
	}
	want := map[string]any{"notice": map[string]any{"level": "error", "text": forbidText}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HX-Trigger = %s, want %v", trigger, want)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestWeb_HTMXSwappedErrorsAreFragments(t *testing.T) {
	cases := []struct {
		status        int
		message, want string
	}{
		{http.StatusUnprocessableEntity, qtyText, qtyText},
		{http.StatusConflict, "<b>x</b>", "&lt;b&gt;x&lt;/b&gt;"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			rec := httptest.NewRecorder()
			web.Error(rec, htmxRequest(), c.status, c.message)
			body := rec.Body.String()
			if rec.Code != c.status {
				t.Errorf("status = %d, want %d", rec.Code, c.status)
			}
			if n := strings.Count(body, `role="alert"`); n != 1 {
				t.Errorf("body = %q has %d role=\"alert\" elements, want 1", body, n)
			}
			m := alertRe.FindStringSubmatch(body)
			if m == nil || m[1] != c.want {
				t.Errorf("body = %q, want one role=\"alert\" div holding %q", body, c.want)
			}
			if strings.Contains(body, "<b>") {
				t.Errorf("body = %q contains unescaped <b>", body)
			}
			if got := rec.Header().Get("HX-Trigger"); got != "" {
				t.Errorf("HX-Trigger = %q, want none", got)
			}
		})
	}
}

func TestWeb_FullPageErrorHasAlert(t *testing.T) {
	rec := httptest.NewRecorder()
	web.Error(rec, httptest.NewRequest(http.MethodPost, "/save", nil), http.StatusUnprocessableEntity, qtyText)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(strings.ToLower(body), "<html") {
		t.Errorf("body = %q, want a full HTML page", body)
	}
	m := alertRe.FindStringSubmatch(body)
	if m == nil || m[1] != qtyText {
		t.Errorf("body = %q, want a role=\"alert\" div holding %q", body, qtyText)
	}
}

// parseSources reads static/SOURCES.md (format in the comment at the top of
// this file) into file name -> key -> value.
func parseSources(t *testing.T) map[string]map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("static", "SOURCES.md"))
	if err != nil {
		t.Fatalf("open static/SOURCES.md: %v", err)
	}
	defer func() { _ = f.Close() }()
	out := map[string]map[string]string{}
	var cur map[string]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if name, ok := strings.CutPrefix(line, "## "); ok {
			cur = map[string]string{}
			out[strings.Trim(strings.TrimSpace(name), "`")] = cur
			continue
		}
		if kv, ok := strings.CutPrefix(line, "- "); ok && cur != nil {
			if k, v, ok := strings.Cut(kv, ":"); ok {
				cur[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "`")
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read static/SOURCES.md: %v", err)
	}
	return out
}

func TestWeb_StaticFilesMatchSources(t *testing.T) {
	sources := parseSources(t)

	want := []struct {
		file, url, pkg, version, licence string
	}{
		{"htmx.min.js", web.HTMXURL, "htmx.org", "2.0.11", "0BSD"},
		{"alpine.min.js", web.AlpineURL, "@alpinejs/csp", "3.17.5", "MIT"},
	}
	for _, w := range want {
		t.Run(w.file, func(t *testing.T) {
			entry, ok := sources[w.file]
			if !ok {
				t.Fatalf("SOURCES.md has no \"## %s\" section", w.file)
			}
			for key, val := range map[string]string{"Package": w.pkg, "Version": w.version, "Licence": w.licence} {
				if entry[key] != val {
					t.Errorf("SOURCES.md %s %s = %q, want %q", w.file, key, entry[key], val)
				}
			}
			if !strings.HasPrefix(entry["Source"], "https://") {
				t.Errorf("SOURCES.md %s Source = %q, want an https URL", w.file, entry["Source"])
			}
			if got := sha256Hex(readStatic(t, w.file)); entry["SHA-256"] != got {
				t.Errorf("SOURCES.md %s SHA-256 = %q, file on disk has %q", w.file, entry["SHA-256"], got)
			}
			needURL(t, w.file, w.url)
			rec := serve(t, web.Static(), httptest.NewRequest(http.MethodGet, w.url, nil))
			if got := sha256Hex(rec.Body.Bytes()); rec.Code != http.StatusOK || entry["SHA-256"] != got {
				t.Errorf("served %q: status %d, SHA-256 %q; want 200 and %q from SOURCES.md", w.url, rec.Code, got, entry["SHA-256"])
			}
		})
	}

	entries, err := os.ReadDir("static")
	if err != nil {
		t.Fatalf("read static/: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "SOURCES.md" {
			continue
		}
		entry, ok := sources[e.Name()]
		if !ok {
			t.Errorf("static/%s has no section in SOURCES.md", e.Name())
			continue
		}
		if got := sha256Hex(readStatic(t, e.Name())); entry["SHA-256"] != got {
			t.Errorf("SOURCES.md %s SHA-256 = %q, file on disk has %q", e.Name(), entry["SHA-256"], got)
		}
	}
}

func TestWeb_FlashSurvivesRedirect(t *testing.T) {
	sm := scs.New()
	r := web.NewRouter(web.Options{Logger: discardLogger(), Sessions: sm})
	r.Post("/save", func(w http.ResponseWriter, r *http.Request) {
		web.Flash(r.Context(), "success", "Saved.")
		web.Flash(r.Context(), "warning", "Check the rate.")
		http.Redirect(w, r, "/next", http.StatusSeeOther)
	})
	r.Get("/next", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(r.Context()))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx := context.Background()

	do := func(method, path string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, b
	}

	resp, _ := do(http.MethodPost, "/save")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /save status = %d, want 303", resp.StatusCode)
	}

	readNotices := func(label string) []web.Notice {
		t.Helper()
		resp, body := do(http.MethodGet, "/next")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s GET /next status = %d, want 200 (body %q)", label, resp.StatusCode, body)
		}
		var got []web.Notice
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%s GET /next body %q: %v", label, body, err)
		}
		return got
	}

	want := []web.Notice{{Level: "success", Text: "Saved."}, {Level: "warning", Text: "Check the rate."}}
	if got := readNotices("first"); !reflect.DeepEqual(got, want) {
		t.Errorf("first GET /next notices = %+v, want %+v", got, want)
	}
	if got := readNotices("second"); len(got) != 0 {
		t.Errorf("second GET /next notices = %+v, want none", got)
	}
}
