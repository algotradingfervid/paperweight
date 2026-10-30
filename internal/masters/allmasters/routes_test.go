package allmasters_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// New fills every service, and the directory over the same services, which
// WP3 hands to AUTH and APR.
func TestServices_NewFillsEveryService(t *testing.T) {
	var s *allmasters.Services
	webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		s = allmasters.New(k)
		return func(chi.Router) {}
	})
	if s.Project == nil || s.Site == nil || s.Employee == nil || s.Directory == nil {
		t.Fatalf("allmasters.New left a service nil: %+v", s)
	}
	if s.Directory.Employees != s.Employee || s.Directory.Projects != s.Project || s.Directory.Sites != s.Site {
		t.Errorf("Directory = %+v; want the services of Services", s.Directory)
	}
}

// The import wizard is mounted through Routes on the session manager WP3
// passes: its first step's choice is still there on the next page.
func TestServices_RoutesMountTheImportWizard(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, "/m/import/choose"), http.StatusOK)

	resp := h.Post(t, h.Env.Kavya, "/m/import/choose", url.Values{"doctype": {uom.DocType}})
	webtest.WantRedirect(t, resp, "/m/import/upload")
	page := h.Follow(t, h.Env.Kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body, "doctype="+uom.DocType) {
		t.Errorf("upload step doesn't name the chosen master: %s", webtest.Squash(page.Text()))
	}
}

// Routes registers the real importers (CD-210), so a clean file goes through
// the wizard's steps as a browser sends them and ends as a stored UoM with
// csvio's audit entry. The row says Yes for Whole Number because No is also
// what a dropped mapping stores.
func TestServices_RoutesReachARealImporter(t *testing.T) {
	var svc *allmasters.Services
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		svc = allmasters.New(k)
		return func(r chi.Router) { svc.Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	kavya := h.Env.Kavya
	var cookies []*http.Cookie
	send := func(req *http.Request) webtest.Response {
		t.Helper()
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.Router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), kavya)))
		if set := (&http.Response{Header: rec.Header()}).Cookies(); len(set) > 0 {
			cookies = set
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		return webtest.Response{Status: rec.Code, Location: rec.Header().Get("Location"), Header: rec.Header(), Body: rec.Body.String(), Doc: doc}
	}
	post := func(target string, form url.Values) webtest.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return send(req)
	}

	webtest.WantRedirect(t, post("/m/import/choose", url.Values{"doctype": {uom.DocType}}), "/m/import/upload")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "uom.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("Code,Name,Whole Number,UQC,Disabled\r\nZED,Zed,Yes,NOS,No\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest(http.MethodPost, "/m/import/upload", &body)
	upload.Header.Set("Content-Type", mw.FormDataContentType())
	webtest.WantRedirect(t, send(upload), "/m/import/check")

	check := send(httptest.NewRequest(http.MethodGet, "/m/import/check", nil))
	webtest.WantStatus(t, check, http.StatusOK)
	webtest.WantNoText(t, check, "Import isn't ready")
	webtest.WantBanner(t, check, "1 row is ready, 0 rows have errors")

	webtest.WantRedirect(t, post("/m/import/check", url.Values{}), "/m/import/import")
	page := send(httptest.NewRequest(http.MethodGet, "/m/import/import", nil))
	webtest.WantStatus(t, page, http.StatusOK)
	form := page.Doc.Find(`form[action="/m/import/import"]`)
	if got := webtest.Squash(form.Find(`button.btn--primary`).Text()); got != "Import 1 UoM" {
		t.Fatalf("the import form's primary button = %q; want %q\ntext: %s", got, "Import 1 UoM", page.Text())
	}
	fields := url.Values{}
	form.Find(`input[type="hidden"]`).Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		value, _ := s.Attr("value")
		fields.Add(name, value)
	})

	webtest.WantRedirect(t, post("/m/import/import", fields), "/m/import/choose")
	webtest.WantBanner(t, send(httptest.NewRequest(http.MethodGet, "/m/import/choose", nil)), "Imported 1 UoMs.")

	zed := othersGet(t, h.Env, svc.UoM.ByCode, "ZED")
	if zed.Name != "Zed" || !zed.WholeNumber || zed.UQC != "NOS" {
		t.Errorf("ZED = %+v; want Name Zed, WholeNumber true, UQC NOS as in the file", zed)
	}
	var creates []string
	for _, e := range auditEntries(t, h.Env, uom.DocType, zed.ID) {
		if e.Action == "create" {
			creates = append(creates, e.Reason)
		}
	}
	if len(creates) != 1 || creates[0] != "created by import" {
		t.Errorf("create entries' reasons = %q; want exactly one, %q", creates, "created by import")
	}
}

// Each built master's screens are reachable through Routes.
func TestServices_RoutesMountTheMasters(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) { allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	for _, target := range []string{"/m/uom", "/m/tax-code", "/m/expense-type"} {
		webtest.WantStatus(t, h.Get(t, h.Env.Kavya, target), http.StatusOK)
	}
}
